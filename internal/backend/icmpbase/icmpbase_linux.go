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
	var domain, ipProt, icmpProt, recvErr int
	var sa unix.Sockaddr
	switch ipVer {
	case util.IPv4:
		domain = unix.AF_INET
		ipProt = unix.IPPROTO_IP
		icmpProt = unix.IPPROTO_ICMP
		recvErr = unix.IP_RECVERR
		sa = &unix.SockaddrInet4{}
	case util.IPv6:
		domain = unix.AF_INET6
		ipProt = unix.IPPROTO_IPV6
		icmpProt = unix.IPPROTO_ICMPV6
		recvErr = unix.IPV6_RECVERR
		sa = &unix.SockaddrInet6{}
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}

	fd, err := unix.Socket(domain, unix.SOCK_DGRAM, icmpProt)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.Bind(fd, sa); err != nil {
		return nil, nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, nil, err
	}
	if err := unix.SetsockoptInt(fd, ipProt, recvErr, 1); err != nil {
		return nil, nil, err
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
func pingID(conn net.PacketConn) int {
	return conn.LocalAddr().(*net.UDPAddr).Port
}
