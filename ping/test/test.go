// Package test contains utilities for testing pings.
package test

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/pcekm/graphping/ping/connection"
	"github.com/pcekm/graphping/ping/util"
	"github.com/stretchr/testify/mock"
)

var (
	// LoopbackV4 is IPv4 loopback address.
	LoopbackV4 = &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}

	// LoopbackV6 is the IPv6 loopback address.
	LoopbackV6 = &net.UDPAddr{IP: net.ParseIP("::1")}

	// ErrTimeout is a timeout error similar to the one returned by the icmp
	// library. That timeout is, unfortunately, just one with a string ending
	// with "timeout," without any other way to distinguish it.
	ErrTimeout = errors.New("mock timeout")
)

// PingExchangeOpts holds various parameters for a send/receive exchange of
// pings.
type PingExchangeOpts struct {
	// SendPkt is the packet expected to be sent in the ping.
	SendPkt connection.Packet

	// TTL is the TTL the packet is expected to be sent with. A zero value
	// means no TTL is set, and WriteTo will be used instead of WriteToTTL.
	TTL int

	// Dest is the expected address the ping will be sent to.
	Dest net.Addr

	// SendErr is the error to return from the send operation.
	SendErr error

	// RecvPkt connection.Packet is packet to respond with.
	RecvPkt connection.Packet

	// Peer is address the response will come from.
	Peer net.Addr

	// Latency is the time to wait before returning a reply.
	Latency time.Duration

	// RecvErr is the error to return from the reply operation.
	RecvErr error

	// NoReply says not to mock a call to readFrom.
	NoReply bool
}

// NewPingExchange creates a PingExchangeOpts struct with reasonable defaults
// for a successful request/reply.
func NewPingExchange(id, seq int) *PingExchangeOpts {
	return &PingExchangeOpts{
		SendPkt: connection.Packet{ID: id, Seq: seq},
		Dest:    LoopbackV4,
		RecvPkt: connection.Packet{Type: connection.PacketReply, ID: id, Seq: seq},
		Peer:    LoopbackV4,
	}
}

// SetTTL sets the time to live field.
func (p *PingExchangeOpts) SetTTL(ttl int) *PingExchangeOpts {
	p.TTL = ttl
	return p
}

// SetPeer sets the Peer field.
func (p *PingExchangeOpts) SetPeer(peer net.Addr) *PingExchangeOpts {
	p.Peer = peer
	return p
}

// SetLatency sets the Latency field.
func (p *PingExchangeOpts) SetLatency(d time.Duration) *PingExchangeOpts {
	p.Latency = d
	return p
}

// SetNoReply sets the NoReply field.
func (p *PingExchangeOpts) SetNoReply(nr bool) *PingExchangeOpts {
	p.NoReply = nr
	return p
}

// SetRespType sets the Type field in the RecvPkt field.
func (p *PingExchangeOpts) SetRespType(t connection.PacketType) *PingExchangeOpts {
	p.RecvPkt.Type = t
	return p
}

// MockConn is a mock PingConn built using testify/mock.
type MockConn struct {
	mock.Mock

	mu        sync.Mutex
	cbID      int
	callbacks map[int]connection.Callback // Lazy init
}

// AddCallback adds a callback.
func (c *MockConn) AddCallback(cb connection.Callback) connection.Remover {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.callbacks == nil {
		c.callbacks = make(map[int]connection.Callback)
	}
	id := c.cbID
	c.cbID++
	c.callbacks[id] = cb
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.callbacks, id)
	}
}

// WriteTo sends a ping.
func (c *MockConn) WriteTo(pkt *connection.Packet, addr net.Addr) error {
	args := c.Called(pkt, addr)
	return args.Error(0)
}

// WriteToTTL sends a ping with a given TTL.
func (c *MockConn) WriteToTTL(pkt *connection.Packet, addr net.Addr, ttl int) error {
	args := c.Called(pkt, addr, ttl)
	return args.Error(0)
}

// SetReadDeadline sets the read deadline.
func (c *MockConn) SetReadDeadline(t time.Time) error {
	args := c.Called(t)
	return args.Error(0)
}

// Close closes the connection.
func (c *MockConn) Close() error {
	args := c.Called()
	return args.Error(0)
}

// MockPingExchange sets up a single mock ping request and reply.
func (c *MockConn) MockPingExchange(opt *PingExchangeOpts) {
	sendFunc := func(_ mock.Arguments) {
		if opt.NoReply {
			return
		}
		latency := time.After(opt.Latency)
		go func() {
			<-latency
			recvPkt := opt.RecvPkt // Deep-ish copy (all but payload)
			c.mu.Lock()
			defer c.mu.Unlock()
			for _, cb := range c.callbacks {
				go cb(&recvPkt, opt.Peer)
			}
		}()
	}
	sendPkt := opt.SendPkt // Deep-ish copy (all but payload).
	if opt.TTL == 0 {
		c.On("WriteTo", &sendPkt, opt.Dest).
			Once().
			Run(sendFunc).
			Return(opt.SendErr)
	} else {
		c.On("WriteToTTL", &sendPkt, opt.Dest, opt.TTL).
			Once().
			Run(sendFunc).
			Return(opt.SendErr)
	}

}

// MockWaitTillClose mocks readFrom to block until Close() is called, after
// which it will return an error.
func (c *MockConn) MockWaitTilClose() {
	c.On("readFrom").
		Maybe().
		Return(nil, nil, errors.New("mock closed"))
}

// WithTimeout runs a function until it completes or the timeout elapses. It
// returns true if the function ran to completion, or false on timeout. Note
// that the function will continue to run after a timeout. There's no way o
// foricbly kill a goroutine.
func WithTimeout(f func(), timeout time.Duration) bool {
	done := make(chan any)
	tick := time.After(time.Second)
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-tick:
		return false
	}
}

type mockIDGen int

func (m mockIDGen) GenID() int {
	return int(m)
}

// InjectID rigs the ICMP echo ID generator to always return a specific value.
// Returns a function that restores the original functionality.
func InjectID(id int) func() {
	orig := util.IDGenerator
	util.IDGenerator = mockIDGen(id)
	return func() {
		util.IDGenerator = orig
	}
}
