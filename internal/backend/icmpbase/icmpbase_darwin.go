//go:build !rawsock

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
	var domain, icmpProt, ipProt int
	switch ipVer {
	case util.IPv4:
		domain = unix.AF_INET
		icmpProt = unix.IPPROTO_ICMP
		ipProt = unix.IPPROTO_IP
	case util.IPv6:
		domain = unix.AF_INET6
		icmpProt = unix.IPPROTO_ICMPV6
		ipProt = unix.IPPROTO_IPV6
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}

	fd, err := unix.Socket(domain, unix.SOCK_DGRAM, icmpProt)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, nil, err
	}
	if ipVer == util.IPv4 {
		if err := unix.SetsockoptInt(fd, ipProt, unix.IP_STRIPHDR, 1); err != nil {
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
