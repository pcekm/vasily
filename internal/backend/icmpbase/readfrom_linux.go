// This (complicated) version of ReadFrom is necessary to run on linux without
// raw sockets.
//
//go:build !rawsock

package icmpbase

/*
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <linux/errqueue.h>
#include <netdb.h>

// Wraps the SO_EE_OFFENDER macro in a function so it can be called from Go
// code.
struct sockaddr* so_ee_offender(struct sock_extended_err *ee) {
	return SO_EE_OFFENDER(ee);
}
*/
import "C"

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
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

		pkt, id, err := c.icmpToBackendPacket(buf[:n])
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

	// Allocate enough space for the msg header, extended error, _and_ a
	// trailing sock_addr to be extracted by SO_EE_OFFENDER().
	saSize := util.Choose(c.ipVer, C.sizeof_struct_sockaddr_in, C.sizeof_struct_sockaddr_in6)
	oob := make([]byte, unix.CmsgSpace(int(C.sizeof_struct_sock_extended_err+saSize)))

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
	pkt, peer, err := c.parseErr(buf[:n], oob[:oobn])
	if err != nil {
		return nil, nil, err
	}
	return pkt, peer, err
}

// Extracts a sockaddr of what generated the error. This should be part of
// x/sys/unix or syscall, but, sadly it isn't. The cleanest way of getting it is
// with SE_EE_OFFENDER() via cgo.
//
// The macro in linux/errqueue.h is:
//
//	#define SO_EE_OFFENDER(ee) ((struct sockaddr *) ((ee) + 1))
//
// Which means the address immediately follows struct sock_extended_err. Which
// means we need to be sure there's enough room in b for it. Which means that
// C.sizeof_struct_sockaddr_in (or _in6) should be added to the length of oob in
// readErr().
func soEEOffender(b []byte) (net.Addr, error) {
	sa := C.so_ee_offender((*C.struct_sock_extended_err)(unsafe.Pointer(&b[0])))
	host := [C.INET6_ADDRSTRLEN]C.char{} // More than enough for IPv4
	port := [5]C.char{}                  // Longest numeric port number
	if _, err := C.getnameinfo(sa, C.socklen_t(len(b)), &host[0], C.socklen_t(len(host)), &port[0], C.socklen_t(len(port)), C.NI_NUMERICHOST|C.NI_NUMERICSERV); err != nil {
		return nil, err
	}
	portNum, _ := strconv.Atoi(C.GoString(&port[0])) // Error means zero, which is fine.
	addr := net.UDPAddr{
		IP:   net.ParseIP(C.GoString(&host[0])),
		Port: portNum,
	}
	return &addr, nil
}

// Parses the results of a unix.Recvmsg() called with the MSG_ERRORQUEUE flag
// set.
func (c *Conn) parseErr(buf, oob []byte) (*backend.Packet, net.Addr, error) {
	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, nil, err
	}
	if len(scms) != 1 {
		return nil, nil, fmt.Errorf("expected exactly 1 control message (got %d)", len(scms))
	}
	if !isRecvErrMessage(scms) {
		return nil, nil, fmt.Errorf("unexpected control header: %#v", scms[0].Header)
	}

	var extErr unix.SockExtendedErr
	if _, err := binary.Decode(scms[0].Data, binary.NativeEndian, &extErr); err != nil {
		return nil, nil, err
	}

	pktType, err := packetType(extErr)
	if err != nil {
		return nil, nil, err
	}

	origMsg, _, err := c.icmpToBackendPacket(buf)
	if err != nil {
		return nil, nil, err
	}

	peer, err := soEEOffender(scms[0].Data)
	if err != nil {
		return nil, nil, err
	}

	return &backend.Packet{
		Type:    pktType,
		Seq:     origMsg.Seq,
		Payload: origMsg.Payload,
	}, peer, nil
}

func isRecvErrMessage(scms []unix.SocketControlMessage) bool {
	h := scms[0].Header
	return (h.Type == unix.IP_RECVERR && h.Level == unix.IPPROTO_IP) ||
		(h.Type == unix.IPV6_RECVERR && h.Level == unix.IPPROTO_IPV6)
}

func packetType(extErr unix.SockExtendedErr) (backend.PacketType, error) {
	switch extErr.Origin {
	case unix.SO_EE_ORIGIN_ICMP:
		switch extErr.Type {
		case byte(ipv4.ICMPTypeTimeExceeded):
			if extErr.Code == codePortUnreachableV4 {
				return backend.PacketReply, nil
			} else {
				return backend.PacketTimeExceeded, nil
			}
		case byte(ipv4.ICMPTypeDestinationUnreachable):
			return backend.PacketDestinationUnreachable, nil
		}
	case unix.SO_EE_ORIGIN_ICMP6:
		switch extErr.Type {
		case byte(ipv6.ICMPTypeTimeExceeded):
			if extErr.Code == codePortUnreachableV6 {
				return backend.PacketReply, nil
			} else {
				return backend.PacketTimeExceeded, nil
			}
		case byte(ipv6.ICMPTypeDestinationUnreachable):
			return backend.PacketDestinationUnreachable, nil
		}
	default:
		return -1, fmt.Errorf("unrecognized origin %v", extErr.Origin) //log.Panicf("Invalid ipVer: %v", c.ipVer)
	}
	return -1, fmt.Errorf("unrecognized packet type %v", extErr.Type)
}
