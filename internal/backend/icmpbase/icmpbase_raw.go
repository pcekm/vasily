//go:build rawsock || !(darwin || linux)

package icmpbase

import (
	"log"
	"net"
	"os"

	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
)

func newConn(ipVer util.IPVersion) (net.PacketConn, *os.File, error) {
	var network string
	switch ipVer {
	case util.IPv4:
		network = "ip4:icmp"
	case util.IPv6:
		network = "ip6:ipv6-icmp"
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}
	conn, err := icmp.ListenPacket(network, "")
	return conn, nil, err
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
