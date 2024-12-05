//go:build !(darwin || linux)

package icmp

import (
	"log"
	"net"

	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
)

func newConn(ipVer util.IPVersion) (*icmp.PacketConn, error) {
	var network string
	switch ipVer {
	case util.IPv4:
		network = "ip4:icmp"
	case util.IPv6:
		network = "ip6:ipv6-icmp"
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}
	return icmp.ListenPacket(network, "")
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
