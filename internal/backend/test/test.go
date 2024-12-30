// Package test contains utilities for testing pings.
package test

import (
	context "context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/util"
	"go.uber.org/mock/gomock"
)

var (
	// LoopbackV4 is IPv4 loopback address.
	LoopbackV4 = &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}

	// LoopbackV6 is the IPv6 loopback address.
	LoopbackV6 = &net.UDPAddr{IP: net.ParseIP("::1")}

	mockMu      sync.Mutex
	nextMockNum int
)

// RegisterMock registers a mock connection in the backend registry and returns
// its name.
func RegisterMock(conn backend.Conn) backend.Name {
	mockMu.Lock()
	defer mockMu.Unlock()
	name := backend.Name(fmt.Sprintf("mock:%d", nextMockNum))
	nextMockNum++
	backend.Register(name, func(util.IPVersion) (backend.Conn, error) { return conn, nil })
	return name
}

// PingExchangeOpts holds various parameters for a send/receive exchange of
// pings.
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

	// RecvErr is the error to return from the reply operation.
	RecvErr error

	// NoReply says not to mock a call to readFrom.
	NoReply bool

	// Times sets the number of times this exchange will occur.
	Times int
}

// NewPingExchange creates a PingExchangeOpts struct with reasonable defaults
// for a successful request/reply.
func NewPingExchange(seq int) *PingExchangeOpts {
	return &PingExchangeOpts{
		SendPkt: backend.Packet{Seq: seq},
		Dest:    LoopbackV4,
		RecvPkt: backend.Packet{Type: backend.PacketReply, Seq: seq},
		Peer:    LoopbackV4,
		Times:   1,
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

// SetTimes sets the Times field.
func (p *PingExchangeOpts) SetTimes(times int) *PingExchangeOpts {
	p.Times = times
	return p
}

type readWait struct {
	T    time.Time
	Opts PingExchangeOpts
}

// MockPingExchange sets up a single mock ping request and reply.
func (c *MockConn) MockPingExchange(opt *PingExchangeOpts) {
	pingSent := make(chan time.Time)
	sendFunc := func(pkt *backend.Packet, _ net.Addr, _ ...backend.WriteOption) {
		close(pingSent)
	}
	sendPkt := opt.SendPkt // Deep-ish copy (all but payload).
	if opt.TTL == 0 {
		c.EXPECT().
			WriteTo(gomock.Eq(&sendPkt), gomock.Eq(opt.Dest)).
			Times(opt.Times).
			Do(sendFunc).
			Return(opt.SendErr)
	} else {
		c.EXPECT().
			WriteTo(&sendPkt, opt.Dest, backend.TTLOption{TTL: opt.TTL}).
			Times(opt.Times).
			Do(sendFunc).
			Return(opt.SendErr)
	}

	if !opt.NoReply {
		recvPkt := opt.RecvPkt
		c.EXPECT().
			ReadFrom(gomock.Not(gomock.Nil())).
			Times(opt.Times).
			Do(func(context.Context) {
				<-pingSent
			}).
			Return(&recvPkt, opt.Peer, opt.RecvErr)
	}
}

// MockClose mocks ReadFrom to block until Close() is called, after which it
// will return an error.
func (c *MockConn) MockClose() {
	closed := make(chan time.Time)
	c.EXPECT().
		Close().
		AnyTimes().
		Do(func() { close(closed) }).
		Return(nil)
	c.EXPECT().
		ReadFrom(gomock.Not(gomock.Nil())).
		AnyTimes().
		Do(func(context.Context) {
			<-closed
		}).
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

// DiffIP compares the IP part of two net.Addrs. Returns "" if they're the same,
// or a message if they're not.
func DiffIP(a, b net.Addr) string {
	ipa, ipb := util.IP(a), util.IP(b)
	return cmp.Diff(ipa, ipb)
}
