package icmpbase

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"syscall"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/util"
)

type internalConn struct {
	ipVer  util.IPVersion
	echoID int

	// Write operations are locked so that TTL can be set and reset atomically.
	// Uses write locks for custom TTLs, and read locks for sends on the default
	// TTL. This allows concurrent writes for the more common case, and only
	// fully locks to set the TTL, write, and reset the TTL atomically.
	ttlMu sync.RWMutex
	conn  net.PacketConn
	file  *os.File
}

// Close closes the connection.
func (p *internalConn) Close() error {
	err := errors.Join(
		p.conn.Close(),
		p.file.Close(),
	)
	return err
}

// Fd returns the file descriptor for the underlying socket.
func (p *internalConn) Fd() int {
	return int(p.file.Fd())
}

// Sets the time to live of sent packets.
func (p *internalConn) setTTL(ttl int) error {
	return syscall.SetsockoptInt(p.Fd(), p.ipVer.IPProtoNum(), p.ipVer.TTLSockOpt(), ttl)
}

// Gets the time to live of sent packets.
func (p *internalConn) ttl() (int, error) {
	return syscall.GetsockoptInt(p.Fd(), p.ipVer.IPProtoNum(), p.ipVer.TTLSockOpt())
}

// WriteTo sends an ICMP message.
func (p *internalConn) WriteTo(buf []byte, dest net.Addr, opts ...backend.WriteOption) error {
	var withTTL int
	for _, o := range opts {
		switch o := o.(type) {
		case backend.TTLOption:
			withTTL = o.TTL
		default:
			log.Panicf("Unsupported option: %#v", o)
		}
	}
	if withTTL != 0 {
		return p.writeToTTL(buf, dest, withTTL)
	}
	return p.writeToNormal(buf, dest)
}

func (p *internalConn) writeToNormal(buf []byte, dest net.Addr) error {
	p.ttlMu.RLock()
	defer p.ttlMu.RUnlock()
	return p.baseWriteTo(buf, dest)
}

// writeToTTL sends an ICMP message with a given time to live.
func (p *internalConn) writeToTTL(buf []byte, dest net.Addr, ttl int) error {
	p.ttlMu.Lock()
	defer p.ttlMu.Unlock()
	origTTL, err := p.ttl()
	if err != nil {
		return fmt.Errorf("unable to get current ttl: %v", err)
	}
	defer func() {
		if err := p.setTTL(origTTL); err != nil {
			log.Printf("Unable to set ttl: %v", err)
		}
	}()
	if err := p.setTTL(ttl); err != nil {
		return fmt.Errorf("unable to set ttl: %v", err)
	}
	return p.baseWriteTo(buf, dest)
}
