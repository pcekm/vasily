//go:build rawsock || !(linux || darwin || windows)

package icmpbase

import (
	"fmt"
	"net"
	"os"

	"github.com/pcekm/vasily/internal/util"
	"golang.org/x/sys/unix"
)

// creates a new ICMP ping connection.
func newInternalConn(ipVer util.IPVersion) (*internalConn, error) {
	fd, err := unix.Socket(ipVer.AddressFamily(), unix.SOCK_RAW, ipVer.ICMPProtoNum())
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), fmt.Sprintf("icmp:%v", ipVer))
	conn, err := net.FilePacketConn(f)
	if err != nil {
		return nil, err
	}

	p := &internalConn{
		ipVer: ipVer,
		conn:  conn,
		file:  f,
	}
	return p, nil
}

// Core writeTo function. Callers must hold p.mu.
func (p *internalConn) baseWriteTo(buf []byte, dest net.Addr) error {
	if _, err := p.conn.WriteTo(buf, &net.IPAddr{IP: util.IP(dest)}); err != nil {
		return err
	}
	return nil
}
