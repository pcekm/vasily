//go:build !rawsock

package icmpbase

import (
	"fmt"
	"net"
	"os"

	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/sys/unix"
)

// creates a new ICMP ping connection.
func newInternalConn(ipVer util.IPVersion) (*internalConn, error) {
	fd, err := unix.Socket(ipVer.AddressFamily(), unix.SOCK_DGRAM, ipVer.ICMPProtoNum())
	if err != nil {
		return nil, err
	}
	sa := util.Choose[unix.Sockaddr](ipVer, &unix.SockaddrInet4{}, &unix.SockaddrInet6{})
	if err := unix.Bind(fd, sa); err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	recvErr := util.Choose(ipVer, unix.IP_RECVERR, unix.IPV6_RECVERR)
	if err := unix.SetsockoptInt(fd, ipVer.IPProtoNum(), recvErr, 1); err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), fmt.Sprintf("icmp:%v", ipVer))
	conn, err := net.FilePacketConn(f)
	if err != nil {
		return nil, err
	}

	p := &internalConn{
		ipVer: ipVer,
		conn:  conn,
		file:  f,
	}
	return p, nil
}
