//go:build !rawsock

package icmpbase

import (
	"fmt"
	"net"
	"os"

	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/sys/unix"
)

func newConn(ipVer util.IPVersion) (net.PacketConn, *os.File, error) {
	fd, err := unix.Socket(ipVer.AddressFamily(), unix.SOCK_DGRAM, ipVer.ICMPProtoNum())
	if err != nil {
		return nil, nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, nil, err
	}
	if ipVer == util.IPv4 {
		if err := unix.SetsockoptInt(fd, ipVer.IPProtoNum(), unix.IP_STRIPHDR, 1); err != nil {
			return nil, nil, err
		}
	}

	f := os.NewFile(uintptr(fd), fmt.Sprintf("icmp:%v", ipVer))
	conn, err := net.FilePacketConn(f)
	if err != nil {
		return nil, nil, err
	}

	return conn, f, nil
}

func wrangleAddr(addr net.Addr) *net.UDPAddr {
	switch addr := addr.(type) {
	case *net.IPAddr:
		return &net.UDPAddr{IP: addr.IP}
	case *net.UDPAddr:
		return addr
	}
	return nil
}

// Gets the ICMP id for this session.
func pingID(net.PacketConn) int {
	return util.GenID()
}
