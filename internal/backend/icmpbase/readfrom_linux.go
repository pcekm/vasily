// This (complicated) version of ReadFrom is necessary to run on linux without
// raw sockets.
//
//go:build !rawsock

package icmpbase

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"golang.org/x/sys/unix"
)

/*
2024/12/12 17:48:33 errq: 8 48 &{0 [142 250 189 238] {0 0 [0 0 0 0] [0 0 0 0 0 0 0 0]}} <nil>
Buf:
00000000  08 00 f7 d6 00 29 00 00                           |.....)..|
Oob:
00000000  30 00 00 00 00 00 00 00  00 00 00 00 0b 00 00 00  |0...............|
00000010  71 00 00 00 02 0b 00 00  00 00 00 00 00 00 00 00  |q...............|
00000020  02 00 00 00 c0 a8 56 01  00 00 00 00 00 00 00 00  |......V.........|
2024/12/12 17:48:33 scm: unix.SocketControlMessage{Header:unix.Cmsghdr{Len:0x30, Level:0, Type:11}, Data:[]uint8{0x71, 0x0, 0x0, 0x0, 0x2, 0xb, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0xc0, 0xa8, 0x56, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}}
2024/12/12 17:48:33 traceroute: 142.250.189.238:0: read error: unhandled ICMP message: &icmp.Message{Type:8, Code:0, Checksum:63446, Body:(*icmp.Echo)(0x400007ecc0)}
*/

func (c *Conn) ReadFrom(ctx context.Context, buf []byte) (n int, peer net.Addr, err error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(dl); err != nil {
			return -1, nil, err
		}
	} else if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return -1, nil, err
	}
	n, peer, err = c.conn.ReadFrom(buf)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) {
			if op.Timeout() {
				return -1, nil, backend.ErrTimeout
			}
			var sc *os.SyscallError
			if errors.As(op.Err, &sc) && sc.Err == unix.EHOSTUNREACH {
				var rawconn syscall.RawConn
				rawconn, err = c.conn.(*net.UDPConn).SyscallConn()

				err = rawconn.Read(func(fd uintptr) bool {
					var sa unix.Sockaddr
					oob := make([]byte, maxMTU)
					var oobn int
					n, oobn, _, sa, err = unix.Recvmsg(int(fd), buf, oob, unix.MSG_ERRQUEUE)
					log.Printf("errq: %v %v %v %v\nBuf:\n%vOob:\n%v", n, oobn, sa, err, hex.Dump(buf[:n]), hex.Dump(oob[:oobn]))
					if err != nil {
						return true
					}

					scms, err := unix.ParseSocketControlMessage(oob[:oobn])
					if err != nil {
						return true
					}
					if len(scms) != 1 {
						err = fmt.Errorf("expected exactly 1 control message (got %d)", len(scms))
						return true
					}
					if scms[0].Header.Type != unix.IP_RECVERR || scms[0].Header.Level != unix.IPPROTO_IP {
						err = fmt.Errorf("unexpected control header: %v", scms[0].Header)
						return true
					}

					var extErr unix.SockExtendedErr
					_, err = binary.Decode(scms[0].Data, binary.NativeEndian, &extErr)
					if err != nil {
						return true
					}

					// TODO: Sigh. I've cleaned everything up but ended up right
					// back where I started. Without the original ICMP error
					// message.

					return true
				})
			}
		}
		return -1, peer, fmt.Errorf("connection read error: %v", err)
	}

	return n, peer, nil
}

func saToNetAddr(sa unix.Sockaddr) (net.Addr, error) {
	switch sa := sa.(type) {
	case *unix.SockaddrInet4:
		return &net.UDPAddr{IP: net.IP(sa.Addr[:]), Port: sa.Port}, nil
	case *unix.SockaddrInet6:
		iface, err := net.InterfaceByIndex(int(sa.ZoneId))
		if err != nil {
			return nil, fmt.Errorf("can't find zone: %v", err)
		}
		return &net.UDPAddr{IP: net.IP(sa.Addr[:]), Port: sa.Port, Zone: iface.Name}, nil
	default:
		return nil, fmt.Errorf("unknown sockaddr: %#v", sa)
	}
}
