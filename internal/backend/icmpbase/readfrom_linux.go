// This (complicated) version of ReadFrom is necessary to run on linux without
// raw sockets.
//
//go:build !rawsock

package icmpbase

import (
	"errors"
	"net"
	"syscall"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/util"
	"github.com/pcekm/vasily/internal/util/icmppkt"
	"golang.org/x/sys/unix"
)

func (c *internalConn) ReadFrom() (*backend.Packet, net.Addr, listenerKey, error) {
	buf := make([]byte, maxMTU)
	var n int
	var peer net.Addr
	var err error
	n, peer, err = c.conn.ReadFrom(buf)
	if err != nil {
		var errno unix.Errno
		if errors.As(err, &errno) && errno == unix.EHOSTUNREACH {
			return c.readErr(buf)
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Timeout() {
			return nil, nil, listenerKey{}, backend.ErrTimeout
		}
		return nil, nil, listenerKey{}, err
	}

	pkt, id, proto, err := icmppkt.Parse(c.ipVer, buf[:n])
	if err != nil {
		return nil, nil, listenerKey{}, err
	}
	return pkt, peer, listenerKey{ID: id, Proto: proto}, err
}

func (c *internalConn) readErr(buf []byte) (*backend.Packet, net.Addr, listenerKey, error) {
	var rawconn syscall.RawConn
	rawconn, err := c.conn.(*net.UDPConn).SyscallConn()
	if err != nil {
		return nil, nil, listenerKey{}, err
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
		return nil, nil, listenerKey{}, rcErr
	}
	if err != nil {
		return nil, nil, listenerKey{}, err
	}
	sentPkt, _, _, err := icmppkt.Parse(c.ipVer, buf[:n])
	if err != nil {
		return nil, nil, listenerKey{}, err
	}
	pktType, peer, err := icmppkt.ParseLinuxEE(oob[:oobn])
	if err != nil {
		return nil, nil, listenerKey{}, err
	}
	pkt := &backend.Packet{
		Type:    pktType,
		Seq:     sentPkt.Seq,
		Payload: sentPkt.Payload,
	}
	id := util.Port(c.conn.LocalAddr())
	return pkt, peer, listenerKey{ID: id, Proto: c.ipVer.ICMPProtoNum()}, nil
}
