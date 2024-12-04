// Package util contains utility functions of use for all of the ping networking
// code.
package util

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
)

const (
	numSequenceNos = 1 << 16
)

// IDGen generates ICMP echo IDs.
type IDGen interface {
	// GenID returns an ICMP echo request.
	GenID() int
}

type idGen struct {
	sync.Mutex
	nextID int
}

func (g *idGen) GenID() int {
	g.Lock()
	defer g.Unlock()
	defer func() {
		g.nextID++
	}()
	return g.nextID
}

// IDGenerator is the default generator for ICMP echo IDs. It's exposed here for
// testing purposes.
var IDGenerator IDGen = &idGen{nextID: rand.Intn(numSequenceNos)}

// GenID returns an ICMP echo request ID that will be unique for this process,
// and _hopefully_ unique for any other pings running on the host.
func GenID() int {
	return IDGenerator.GenID()
}

// IPVersion is the version of IP to  use.
type IPVersion byte

const (
	IPv4 IPVersion = 4
	IPv6 IPVersion = 6
)

func (v IPVersion) String() string {
	switch v {
	case IPv4:
		return "IPv4"
	case IPv6:
		return "IPv6"
	default:
		return fmt.Sprintf("(unknown:%d)", v)
	}
}

// IP returns the IP from an address.
func IP(addr net.Addr) net.IP {
	switch addr := addr.(type) {
	case *net.UDPAddr:
		return addr.IP
	case *net.IPAddr:
		return addr.IP
	case *net.TCPAddr:
		return addr.IP
	default:
		return nil
	}
}
