// Package test contains utilities for testing pings.
package test

import (
	"errors"
	"net"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/ping/util"
	"github.com/stretchr/testify/mock"
)

var (
	// LoopbackV4 is IPv4 loopback address.
	LoopbackV4 = &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}

	// LoopbackV6 is the IPv6 loopback address.
	LoopbackV6 = &net.UDPAddr{IP: net.ParseIP("::1")}

	// ErrTimeout is a timeout error similar to the one returned by the ICMP
	// library. That timeout is, unfortunately, just one with a string ending
	// with "timeout," without any other way to distinguish it.
	ErrTimeout = errors.New("mock timeout")
)

// PingExchangeOpts holds various parameters for a send/receive exchange of
// pings.
// TODO: There's some flakiness inherent in this and I'm not quite sure how to
// deal with it. In some cases, a ReadFrom can return before the associated
// WriteTo has completed. While I'm taking steps to prevent this, there's
// ultimately no way to completely eliminate it without returning a channel from
// ReadFrom. The problem with that approach is that it provides a better
// guarantee than an actual network connection, where it's totally possible for
// a WriteTo goroutine to get scheduled late, while a reply comes a bit early.
// So I'm either going to have to live with this flakiness, or figure out how to
// make the code work when effect precedes cause.
//
// One issue I noticed through benchmarking was that testify/mock is _very_
// slow when dealing with args. For example, calls to ReadFrom (which takes no
// args) are orders of magnitude faster than WriteTo (which takes three).
// WriteTo takes over 1ms complete. Which is a crazy amount of time, and really
// adds up when it gets called repeatedly.
type PingExchangeOpts struct {
	// SendPkt is the packet expected to be sent in the ping.
	SendPkt backend.Packet

	// TTL is the TTL the packet is expected to be sent with. A zero value
	// means set no TTL.
	TTL int

	// Dest is the expected address the ping will be sent to.
	Dest net.Addr

	// SendErr is the error to return from the send operation.
	SendErr error

	// RecvPkt backend.Packet is packet to respond with.
	RecvPkt backend.Packet

	// Peer is address the response will come from.
	Peer net.Addr

	// Latency is the time to wait before returning a reply.
	Latency time.Duration

	// ReadDeadline is the receive deadline set.
	ReadDeadline time.Time

	// RecvErr is the error to return from the reply operation.
	RecvErr error

	// NoReply says not to mock a call to readFrom.
	NoReply bool
}

// NewPingExchange creates a PingExchangeOpts struct with reasonable defaults
// for a successful request/reply.
func NewPingExchange(id, seq int) *PingExchangeOpts {
	return &PingExchangeOpts{
		SendPkt: backend.Packet{ID: id, Seq: seq},
		Dest:    LoopbackV4,
		RecvPkt: backend.Packet{Type: backend.PacketReply, ID: id, Seq: seq},
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
func (p *PingExchangeOpts) SetRespType(t backend.PacketType) *PingExchangeOpts {
	p.RecvPkt.Type = t
	return p
}

// SetPayload sets the payload in the send and reply fields.
func (p *PingExchangeOpts) SetPayload(b []byte) *PingExchangeOpts {
	p.SendPkt.Payload = b
	p.RecvPkt.Payload = b
	return p
}

// MockConn is a mock PingConn built using testify/mock.
type MockConn struct {
	mock.Mock
}

// WriteTo sends a ping.
func (c *MockConn) WriteTo(pkt *backend.Packet, addr net.Addr, opts ...backend.WriteOption) error {
	var args mock.Arguments
	args = c.Called(pkt, addr, opts)
	return args.Error(0)
}

// ReadFrom receives a ping.
func (c *MockConn) ReadFrom() (pkt *backend.Packet, peer net.Addr, err error) {
	args := c.Called()
	pkt, _ = args.Get(0).(*backend.Packet)
	peer, _ = args.Get(1).(net.Addr)
	return pkt, peer, args.Error(2)
}

// SetDeadline sets the read and write deadlines.
func (c *MockConn) SetDeadline(t time.Time) error {
	args := c.Called(t)
	return args.Error(0)
}

// SetReadDeadline sets the read deadline.
func (c *MockConn) SetReadDeadline(t time.Time) error {
	args := c.Called(t)
	return args.Error(0)
}

// SetWriteDeadline sets the write deadline.
func (c *MockConn) SetWriteDeadline(t time.Time) error {
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
	pingSent := make(chan time.Time)
	sendFunc := func(_ mock.Arguments) {
		close(pingSent)
	}
	sendPkt := opt.SendPkt // Deep-ish copy (all but payload).
	if opt.TTL == 0 {
		c.On("WriteTo", &sendPkt, opt.Dest, []backend.WriteOption(nil)).
			Once().
			Run(sendFunc).
			Return(opt.SendErr)
	} else {
		c.On("WriteTo", &sendPkt, opt.Dest, []backend.WriteOption{backend.TTLOption{TTL: opt.TTL}}).
			Once().
			Run(sendFunc).
			Return(opt.SendErr)
	}

	if !opt.NoReply {
		if !opt.ReadDeadline.IsZero() {
			c.On("SetReadDeadline", mock.MatchedBy(func(got time.Time) bool {
				delta := got.Unix() - opt.ReadDeadline.Unix()
				if delta < 0 {
					delta = -delta
				}
				return delta < 1
			})).
				Once().
				Return(nil)
		}
		recvPkt := opt.RecvPkt
		c.On("ReadFrom").
			Once().
			Run(func(_ mock.Arguments) {
				<-pingSent
				time.Sleep(opt.Latency)
			}).
			Return(&recvPkt, opt.Peer, opt.RecvErr)
	}
}

// MockClose mocks ReadFrom to block until Close() is called, after which it
// will return an error.
func (c *MockConn) MockClose() {
	closed := make(chan time.Time)
	c.On("Close").
		Maybe().
		Run(func(_ mock.Arguments) { close(closed) }).
		Return(nil)
	c.On("ReadFrom").
		Maybe().
		WaitUntil(closed).
		Return(nil, nil, errors.New("mock closed"))
}

// WithTimeout runs a function until it completes or the timeout elapses. It
// returns true if the function ran to completion, or false on timeout. Note
// that the function will continue to run after a timeout. There's no way o
// forcibly kill a goroutine.
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
