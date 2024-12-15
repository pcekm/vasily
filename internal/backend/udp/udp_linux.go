//go:build !rawsock

package udp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/icmppkt"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/sys/unix"
)

// Conn is a UDP ping connection.
type Conn struct {
	ipVer util.IPVersion

	readMu  sync.Mutex
	writeMu sync.Mutex
	conn    *net.UDPConn
}

// New opens a new connection.
func New(ipVer util.IPVersion) (*Conn, error) {
	address := util.Choose(ipVer, "udp4", "udp6")
	conn, err := net.ListenUDP(address, nil)
	if err != nil {
		return nil, err
	}
	c := &Conn{
		ipVer: ipVer,
		conn:  conn,
	}
	reOpt := util.Choose(ipVer, unix.IP_RECVERR, unix.IPV6_RECVERR)
	err = c.control(func(fd int) error {
		return unix.SetsockoptInt(int(fd), ipVer.IPProtoNum(), reOpt, 1)
	})
	return c, err
}

// Close closes the connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

// Wrapper around RawConn.Control() to make things easier.
func (c *Conn) control(f func(fd int) error) error {
	rawconn, err := c.conn.SyscallConn()
	if err != nil {
		return err
	}
	rcErr := rawconn.Control(func(fd uintptr) {
		err = f(int(fd))
	})
	if rcErr != nil {
		return rcErr
	}
	return err
}

// Wrapper around RawConn.Read() to make things easier.
func (c *Conn) read(f func(fd int) error) error {
	rawconn, err := c.conn.SyscallConn()
	if err != nil {
		return err
	}
	rcErr := rawconn.Read(func(fd uintptr) bool {
		err = f(int(fd))
		return true
	})
	if rcErr != nil {
		return rcErr
	}
	return err
}

// WriteTo sends a request.
func (c *Conn) WriteTo(pkt *backend.Packet, dest net.Addr, opts ...backend.WriteOption) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	for _, o := range opts {
		if o, ok := o.(backend.TTLOption); ok {
			orig, err := c.ttl()
			if err != nil {
				return fmt.Errorf("can't get original TTL: %v", err)
			}
			defer func() {
				if err := c.setTTL(orig); err != nil {
					log.Printf("Error setting original TTL: %v", err)
				}
			}()
			c.setTTL(o.TTL)
		}
	}

	addr := *(dest.(*net.UDPAddr))
	addr.Port = basePort + pkt.Seq
	sa := unix.SockaddrInet4{
		Port: addr.Port,
	}
	copy(sa.Addr[:], addr.IP)

	err := c.control(func(fd int) error {
		return unix.Connect(fd, &sa)
	})

	_, err = c.conn.WriteTo(pkt.Payload, &addr)
	return err
}

func (c *Conn) ttl() (res int, err error) {
	opt := util.Choose(c.ipVer, unix.IP_TTL, unix.IPV6_UNICAST_HOPS)
	err = c.control(func(fd int) error {
		res, err = unix.GetsockoptInt(fd, c.ipVer.IPProtoNum(), opt)
		return err
	})
	return res, err
}

func (c *Conn) setTTL(ttl int) (err error) {
	opt := util.Choose(c.ipVer, unix.IP_TTL, unix.IPV6_UNICAST_HOPS)
	return c.control(func(fd int) error {
		return unix.SetsockoptInt(fd, c.ipVer.IPProtoNum(), opt, ttl)
	})
}

func (c *Conn) localPort() int {
	return c.conn.LocalAddr().(*net.UDPAddr).Port
}

// ReadFrom receives a reply. The received packet will likely not include any
// payload that was initially sent. That information isn't normally returned in
// the ICMP reply.
func (c *Conn) ReadFrom(ctx context.Context) (*backend.Packet, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(dl); err != nil {
			return nil, nil, err
		}
	} else if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, nil, err
	}

	buf := make([]byte, maxMTU)
	n, peer, err := c.conn.ReadFrom(buf)
	if err == nil {
		// Apparently the remote host is listening on the given port and has
		// sent a response. That's unexpected. Deal with it as best as possible.
		return &backend.Packet{
			Type:    backend.PacketReply,
			Seq:     peer.(*net.UDPAddr).Port - basePort,
			Payload: buf[:n],
		}, peer, nil
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return nil, nil, err
	}
	if opErr.Timeout() {
		return nil, nil, backend.ErrTimeout
	}

	oob := icmppkt.OOBBytes(c.ipVer)
	var oobn int
	var origDest unix.Sockaddr
	err = c.read(func(fd int) error {
		n, oobn, _, origDest, err = unix.Recvmsg(fd, buf, oob, unix.MSG_ERRQUEUE)
		return err
	})

	pktType, peer, err := icmppkt.ParseLinuxEE(oob[:oobn])
	if err != nil {
		return nil, nil, err
	}

	var seq int
	switch sa := origDest.(type) {
	case *unix.SockaddrInet4:
		seq = sa.Port
	case *unix.SockaddrInet6:
		seq = sa.Port
	}

	return &backend.Packet{Type: pktType, Seq: seq - basePort}, peer, nil
}
