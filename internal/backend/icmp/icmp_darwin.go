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
		network = "udp4"
	case util.IPv6:
		network = "udp6"
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}
	return icmp.ListenPacket(network, "")
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
