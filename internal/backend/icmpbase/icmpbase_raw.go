//go:build rawsock || !(darwin || linux || windows)

package icmpbase

import (
	"fmt"
	"net"
	"os"

	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/sys/unix"
)

func newConn(ipVer util.IPVersion) (net.PacketConn, *os.File, error) {
	fd, err := unix.Socket(ipVer.AddressFamily(), unix.SOCK_RAW, ipVer.ICMPProtoNum())
	if err != nil {
		return nil, nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, nil, err
	}

	f := os.NewFile(uintptr(fd), fmt.Sprintf("icmp:%v", ipVer))
	conn, err := net.FilePacketConn(f)
	if err != nil {
		return nil, nil, err
	}

	return conn, f, nil
}

func wrangleAddr(addr net.Addr) *net.IPAddr {
	switch addr := addr.(type) {
	case *net.IPAddr:
		return addr
	case *net.UDPAddr:
		return &net.IPAddr{IP: addr.IP}
	}
	return nil
}

// Gets the ICMP id for this session.
func pingID(net.PacketConn) int {
	return util.GenID()
}
