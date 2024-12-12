//go:build rawsock || !(darwin || linux || windows)

package icmpbase

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/sys/unix"
)

func newConn(ipVer util.IPVersion) (net.PacketConn, *os.File, error) {
	var domain, icmpProt int
	switch ipVer {
	case util.IPv4:
		domain = unix.AF_INET
		icmpProt = unix.IPPROTO_ICMP
	case util.IPv6:
		domain = unix.AF_INET6
		icmpProt = unix.IPPROTO_ICMPV6
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}

	fd, err := unix.Socket(domain, unix.SOCK_RAW, icmpProt)
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
