// This (complicated) version of ReadFrom is necessary to run on linux without
// raw sockets.
//
//go:build !rawsock

package icmpbase

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/icmppkt"
	"golang.org/x/sys/unix"
)

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
	var n int
	var peer net.Addr
	var err error
	for {
		n, peer, err = c.conn.ReadFrom(buf)
		if err != nil {
			var errno unix.Errno
			if errors.As(err, &errno) && errno == unix.EHOSTUNREACH {
				// Do we need ID filtering? It may not be necessary because of
				// the way dgram+icmp sockets work on Linux.
				return c.readErr(buf)
			}
			var opErr *net.OpError
			if errors.As(err, &opErr) && opErr.Timeout() {
				return nil, nil, backend.ErrTimeout
			}
			return nil, nil, err
		}

		pkt, id, err := icmppkt.Parse(c.ipVer, buf[:n])
		if err != nil {
			return nil, nil, err
		}
		if id != c.EchoID() {
			continue
		}
		return pkt, peer, err
	}
}

func (c *Conn) readErr(buf []byte) (*backend.Packet, net.Addr, error) {
	var rawconn syscall.RawConn
	rawconn, err := c.conn.(*net.UDPConn).SyscallConn()
	if err != nil {
		return nil, nil, err
	}

	oob := icmppkt.OOBBytes(c.ipVer)
	var n, oobn int
	rcErr := rawconn.Read(func(fd uintptr) bool {
		// This returns a Sockaddr, but it always contains the original
		// destination, and not the host that generated the error. Which makes
		// it useless for traceroute. The actual host is at the end of oob and
		// gets extracted by parseErr().
		n, oobn, _, _, err = unix.Recvmsg(int(fd), buf, oob, unix.MSG_ERRQUEUE)
		return true
	})
	if rcErr != nil {
		return nil, nil, rcErr
	}
	if err != nil {
		return nil, nil, err
	}
	sentPkt, _, err := icmppkt.Parse(c.ipVer, buf[:n])
	if err != nil {
		return nil, nil, err
	}
	pktType, peer, err := icmppkt.ParseLinuxEE(oob[:oobn])
	if err != nil {
		return nil, nil, err
	}
	pkt := &backend.Packet{
		Type:    pktType,
		Seq:     sentPkt.Seq,
		Payload: sentPkt.Payload,
	}
	return pkt, peer, nil
}
