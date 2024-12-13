// Package icmpbase is an basic ICMP connection for use by other backends.
package icmpbase

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/time/rate"
)

const (
	maxMTU          = 1500
	minPingInterval = time.Second
	maxActiveConns  = 100
)

// Sent to when a connection is created; received from when a connection is
// closed. This limits the total number of connections since the initial send
// will block (or fail) if the buffer is full.
var activeConns = make(chan any, maxActiveConns)

// Conn is a basic ICMP network connection. A connection may handle either
// IPv4 or IPv6 but not both at the same time. Since this may run setuid root,
// the total number of open connections is limited.
type Conn struct {
	ipVer   util.IPVersion
	echoID  int
	limiter *rate.Limiter

	// Write operations are locked so that TTL can be set and reset atomically.
	// Uses write locks for custom TTLs, and read locks for sends on the default
	// TTL. This allows concurrent writes for the more common case, and only
	// fully locks to set the TTL, write, and reset the TTL atomically.
	ttlMu  sync.RWMutex
	readMu sync.Mutex
	conn   net.PacketConn
	file   *os.File
}

// New creates a new ICMP ping connection. The network arg should be:
func New(ipVer util.IPVersion) (*Conn, error) {
	select {
	case activeConns <- nil:
	default:
		return nil, errors.New("too many connections")
	}

	conn, file, err := newConn(ipVer)
	if err != nil {
		return nil, fmt.Errorf("listen error: %v", err)
	}
	p := &Conn{
		ipVer:   ipVer,
		echoID:  pingID(conn),
		limiter: rate.NewLimiter(rate.Every(minPingInterval), 5),
		conn:    conn,
		file:    file,
	}
	return p, nil
}

// NewUnlimited creates a new ICMP ping connection with no rate limiter. This is
// for use in tests.
func NewUnlimited(ipVer util.IPVersion) (*Conn, error) {
	c, err := New(ipVer)
	if err != nil {
		return nil, err
	}
	c.limiter.SetLimit(rate.Inf)
	return c, nil
}

// Close closes the connection.
func (p *Conn) Close() error {
	err := errors.Join(
		p.conn.Close(),
		p.file.Close(),
	)
	<-activeConns
	return err
}

// Fd returns the file descriptor for the underlying socket.
func (p *Conn) Fd() int {
	return int(p.file.Fd())
}

// EchoID returns the ICMP ID that must be used with this connection.
func (p *Conn) EchoID() int {
	return p.echoID
}

// Sets the time to live of sent packets.
func (p *Conn) setTTL(ttl int) error {
	return syscall.SetsockoptInt(p.Fd(), p.ipVer.IPProtoNum(), p.ipVer.TTLSockOpt(), ttl)
}

// Gets the time to live of sent packets.
func (p *Conn) ttl() (int, error) {
	return syscall.GetsockoptInt(p.Fd(), p.ipVer.IPProtoNum(), p.ipVer.TTLSockOpt())
}

// WriteTo sends an ICMP message.
func (p *Conn) WriteTo(buf []byte, dest net.Addr, opts ...backend.WriteOption) error {
	if !p.limiter.Allow() {
		return errors.New("rate limit exceeded")
	}
	dest = wrangleAddr(dest)
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

func (p *Conn) writeToNormal(buf []byte, dest net.Addr) error {
	p.ttlMu.RLock()
	defer p.ttlMu.RUnlock()
	return p.baseWriteTo(buf, dest)
}

// writeToTTL sends an ICMP message with a given time to live.
func (p *Conn) writeToTTL(buf []byte, dest net.Addr, ttl int) error {
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

// Core writeTo function. Callers must hold p.mu.
func (p *Conn) baseWriteTo(buf []byte, dest net.Addr) error {
	if _, err := p.conn.WriteTo(buf, dest); err != nil {
		return err
	}
	return nil
}
