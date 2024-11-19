// Package name contains name resolution functions.
//
// This is meant to add some ease of use to the base functions, but ultimately
// likely some caching as well.
package lookup

import (
	"errors"
	"fmt"
	"net"
)

// Addr finds the name for a given address, or returns the address itself as
// a string if no name can be found. If multiple names are found, this returns
// the first.
func Addr(addr net.Addr) string {
	var ipstr string
	switch addr := addr.(type) {
	case *net.UDPAddr:
		ipstr = addr.IP.String()
	case *net.TCPAddr:
		ipstr = addr.IP.String()
	case *net.IPAddr:
		ipstr = addr.IP.String()
	default:
		return addr.String()
	}
	names, err := net.LookupAddr(ipstr)
	if err != nil || len(names) == 0 {
		return ipstr
	}
	return names[0]
}

// String parses a string address or hostname. Returns the first IPv4 address if
// it exists, or the first IPv6 address otherwise.
func String(s string) (*net.UDPAddr, error) {
	ipAddrs, err := net.LookupIP(s)
	if err != nil {
		return nil, fmt.Errorf("lookup error: %v", err)
	}
	if len(ipAddrs) == 0 {
		return nil, errors.New("no addresses found")
	}
	ip := ipAddrs[0]
	for _, a := range ipAddrs {
		if a.To4() != nil {
			ip = a
		}
	}
	return &net.UDPAddr{IP: ip}, nil
}
