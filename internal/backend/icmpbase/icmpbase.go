// Package icmpbase is an basic ICMP connection for use by other backends.
package icmpbase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
	"golang.org/x/time/rate"
)

const (
	icmpV4ProtoNum  = 1
	icmpV6ProtoNum  = 58
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
	protoNum int
	echoID   int
	limiter  *rate.Limiter

	// Write operations are locked so that TTL can be set and reset atomically.
	// Uses write locks for custom TTLs, and read locks for sends on the default
	// TTL. This allows concurrent writes for the more common case, and only
	// fully locks to set the TTL, write, and reset the TTL atomically.
	ttlMu  sync.RWMutex
	readMu sync.Mutex
	conn   *icmp.PacketConn
}

// New creates a new ICMP ping connection. The network arg should be:
func New(ipVer util.IPVersion) (*Conn, error) {
	select {
	case activeConns <- nil:
	default:
		return nil, errors.New("too many connections")
	}

	protoNum := icmpV4ProtoNum
	if ipVer == util.IPv6 {
		protoNum = icmpV6ProtoNum
	}

	conn, err := newConn(ipVer)
	if err != nil {
		return nil, fmt.Errorf("listen error: %v", err)
	}
	p := &Conn{
		protoNum: protoNum,
		echoID:   pingID(conn),
		limiter:  rate.NewLimiter(rate.Every(minPingInterval), 5),
		conn:     conn,
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
	err := p.conn.Close()
	<-activeConns
	return err
}

// EchoID returns the ICMP ID that must be used with this connection.
func (p *Conn) EchoID() int {
	return p.echoID
}

// Sets the time to live of sent packets.
func (p *Conn) setTTL(ttl int) error {
	switch p.protoNum {
	case icmpV4ProtoNum:
		return p.conn.IPv4PacketConn().SetTTL(ttl)
	case icmpV6ProtoNum:
		return p.conn.IPv6PacketConn().SetHopLimit(ttl)
	default:
		log.Panicf("Invalid protonum: %d", p.protoNum)
	}
	return nil
}

// Gets the time to live of sent packets.
func (p *Conn) ttl() (int, error) {
	switch p.protoNum {
	case icmpV4ProtoNum:
		return p.conn.IPv4PacketConn().TTL()
	case icmpV6ProtoNum:
		return p.conn.IPv6PacketConn().HopLimit()
	default:
		log.Panicf("Invalid protonum: %d", p.protoNum)
	}
	return 0, nil
}

// WriteTo sends an ICMP message.
func (p *Conn) WriteTo(msg *icmp.Message, dest net.Addr, opts ...backend.WriteOption) error {
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
		return p.writeToTTL(msg, dest, withTTL)
	}
	return p.writeToNormal(msg, dest)
}

func (p *Conn) writeToNormal(msg *icmp.Message, dest net.Addr) error {
	p.ttlMu.RLock()
	defer p.ttlMu.RUnlock()
	return p.baseWriteTo(msg, dest)
}

// writeToTTL sends an ICMP echo request with a given time to live.
func (p *Conn) writeToTTL(msg *icmp.Message, dest net.Addr, ttl int) error {
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
	return p.baseWriteTo(msg, dest)
}

// Core writeTo function. Callers must hold p.mu.
func (p *Conn) baseWriteTo(msg *icmp.Message, dest net.Addr) error {
	b, err := msg.Marshal(nil)
	if err != nil {
		return fmt.Errorf("marshal: %v", err)
	}
	if _, err := p.conn.WriteTo(b, dest); err != nil {
		return err
	}
	return nil
}

// Reads an ICMP message.
func (p *Conn) ReadFrom(ctx context.Context) (*icmp.Message, net.Addr, error) {
	buf := make([]byte, maxMTU)
	if dl, ok := ctx.Deadline(); ok {
		if err := p.conn.SetReadDeadline(dl); err != nil {
			return nil, nil, err
		}
	} else if err := p.conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, nil, err
	}
	n, peer, err := p.conn.ReadFrom(buf)
	if err != nil {
		if strings.HasSuffix(err.Error(), "timeout") {
			return nil, peer, backend.ErrTimeout
		}
		return nil, peer, fmt.Errorf("connection read error: %v", err)
	}

	rm, err := icmp.ParseMessage(p.protoNum, buf[:n])
	if err != nil {
		return nil, peer, fmt.Errorf("error parsing ICMP message: %v", err)
	}
	return rm, peer, nil
}
