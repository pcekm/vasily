// Package util contains utility functions of use for all of the ping networking
// code.
package util

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"syscall"
)

const (
	numSequenceNos = 1 << 16
)

// MaybeSetDefault sets field to val if field is zero.
func MaybeSetDefault[T comparable](field *T, val T) {
	if *field == *new(T) {
		*field = val
	}
}

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

// Choose picks between values for IPv4 or IPv6. This is a convenient shorthand
// for a switch statement.
func Choose[T any](v IPVersion, val4, val6 T) T {
	switch v {
	case IPv4:
		return val4
	case IPv6:
		return val6
	default:
		log.Panicf("Invalid IPVersion: %v", v)
		return *new(T)
	}
}

// AddressFamily returns the socket domain for this IP version.
func (v IPVersion) AddressFamily() int {
	return Choose(v, syscall.AF_INET, syscall.AF_INET6)
}

// IPProtoNum returns the socket domain for this IP version.
func (v IPVersion) IPProtoNum() int {
	return Choose(v, syscall.IPPROTO_IP, syscall.IPPROTO_IPV6)
}

// ICMPProtoNum returns the protocol number for ICMPv4 or ICMPv6 as appropriate.
func (v IPVersion) ICMPProtoNum() int {
	return Choose(v, syscall.IPPROTO_ICMP, syscall.IPPROTO_ICMPV6)
}

// TTLSockOpt returns socket option for accessing the time to live.
func (v IPVersion) TTLSockOpt() int {
	return Choose(v, syscall.IP_TTL, syscall.IPV6_UNICAST_HOPS)
}

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

// AddrVersion returns the IPVersion for the given address.
func AddrVersion(addr net.Addr) IPVersion {
	if IP(addr).To4() == nil {
		return IPv6
	}
	return IPv4
}

// IP returns the IP from an address. Returns nil if the address type doesn't
// have an IP or the address itself is nil.
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

// Port returns the port from an address. Returns zero if the address type
// doesn't have a port.
func Port(addr net.Addr) int {
	switch addr := addr.(type) {
	case *net.UDPAddr:
		return addr.Port
	case *net.TCPAddr:
		return addr.Port
	}
	return 0
}
