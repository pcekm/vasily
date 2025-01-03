// Package icmpbase is an basic ICMP connection for use by other backends.
package icmpbase

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/util"
	"golang.org/x/time/rate"
)

const (
	maxMTU          = 1500
	minPingInterval = time.Second
	maxActiveConns  = 100
)

var activeConns = make(chan struct{}, 100)

// Conn is a basic ICMP network connection. A connection may handle either IPv4
// or IPv6 but not both at the same time. Since this may run setuid root, the
// total number of open connections is limited.
type Conn struct {
	svc      *icmpService
	limiter  *rate.Limiter
	echoId   int
	proto    int
	receiver chan readResult
}

// New creates a new ICMP connection. The proto and id args filter what packets
// this will receive. Proto may be syscall.IPPROTO_ICMP, IPPROTO_ICMPV6 or
// IPPROTO_UDP. In the latter case, the id field is the source port number of
// the UDP packets that generate an ICMP error response (e.g. time exceeded).
func New(ipVer util.IPVersion, id, proto int) (*Conn, error) {
	select {
	case activeConns <- struct{}{}:
	default:
		return nil, errors.New("too many connections")
	}

	svc, err := serviceFor(ipVer)
	if err != nil {
		return nil, err
	}
	receiver := make(chan readResult)
	id = svc.RegisterReader(id, proto, receiver)

	return &Conn{
		svc:      svc,
		limiter:  rate.NewLimiter(rate.Every(minPingInterval), 5),
		echoId:   id,
		proto:    proto,
		receiver: receiver,
	}, nil
}

// NewUnlimited creates a new ICMP ping connection with no rate limiter. This is
// for use in tests.
func NewUnlimited(ipVer util.IPVersion, id, proto int) (*Conn, error) {
	c, err := New(ipVer, id, proto)
	if err != nil {
		return nil, err
	}
	c.limiter.SetLimit(rate.Inf)
	return c, nil
}

// Close implements backend.Conn.
func (c *Conn) Close() error {
	c.svc.UnregisterReader(c.echoId, c.proto)
	// Empty the receiver channel to avoid leaking any sender goroutines.
	for range c.receiver {
	}
	<-activeConns
	return nil
}

// EchoID returns the ICMP echo id or UDP src port used by this connection.
func (c *Conn) EchoID() int {
	return c.echoId
}

// ReadFrom implements backend.Conn.
func (c *Conn) ReadFrom(ctx context.Context) (pkt *backend.Packet, peer net.Addr, err error) {
	select {
	case msg, ok := <-c.receiver:
		if !ok {
			return nil, nil, errors.New("closed network connection") // Similar to error returned by icmp.PacketConn
		}
		return msg.Pkt, msg.Peer, nil
	case <-ctx.Done():
		return nil, nil, backend.ErrTimeout
	}
}

// WriteTo implements backend.Conn.
func (c *Conn) WriteTo(b []byte, dest net.Addr, opts ...backend.WriteOption) error {
	if !c.limiter.Allow() {
		return errors.New("rate limit exceeded")
	}
	return c.svc.WriteTo(b, dest, opts...)
}
