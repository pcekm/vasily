// Package pinger pings hosts.
package pinger

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	icmpV4ProtoNum = 1
	maxMTU         = 1500

	// Ping timeout default. How long to wait by default until a ping is assumed
	// to be lost.
	defaultTimeout = 500 * time.Millisecond

	// Time to wait for an ICMP packet. This is _not_ the same as a ping
	// timeout. It only affects how often the receive loop checks for context
	// done.
	readTimeout = 1 * time.Second

	// Maximum path length to search for traceroutes.
	maxTTL = 64

	// Maximum number of attempts to find a hop during a traceroute.
	maxTries = 3
)

// Logger is the interface used by Pinger to log messages. This is a subset of
// log.Logger.
type Logger interface {
	Print(v ...any)
	Printf(format string, v ...any)
}

// Options contains options for the pinger.
type Options struct {
	// Interface contains the network interface to listen on. Empty means listen
	// on all interfaces.
	Interface string

	// Log is used to log informational and debuging messages meant for human
	// consumption.
	Log Logger

	// Timeout is the maximum amount of time to wait before assuming no response
	// is coming. Defaults to 250ms.
	Timeout time.Duration
}

type activeKey struct {
	id  uint16
	seq uint16
}

type activeVal struct {
	sentAt time.Time
	result chan<- PingReply
}

// ReplyType is the type of reply received. This is a high-level view. More
// specifics will require delving into the resturned packet.
type ReplyType int

// Values for ReplyType.
const (
	// Unknown means a reply that doesn't fit in any other category was
	// received.
	Unknown ReplyType = iota

	// Dropped means no reply was received.
	Dropped

	// EchoReply is a normal ping response.
	EchoReply

	// TTLExceeded means the packet exceeded its maximum hop count.
	TTLExceeded

	// Unreachable means the host was unreachable.
	Unreachable
)

func (r ReplyType) String() string {
	switch r {
	case Unknown:
		return "Unknown"
	case Dropped:
		return "Dropped"
	case EchoReply:
		return "EchoReply"
	case TTLExceeded:
		return "TTLExceeded"
	case Unreachable:
		return "Unreachable"
	default:
		return fmt.Sprintf("(unknown:%d)", r)
	}
}

// PingReply holds the result of a ping, returned over a channel.
type PingReply struct {
	// Type is the type of reply exceeded.
	Type ReplyType

	// Peer is the host the reply was received from.
	Peer *net.UDPAddr

	// Seq is the sequence number of the echo packet.
	Seq uint16

	// Latency is the amount of time it took to receive a reply.
	Latency time.Duration
}

// Pinger listens for ICMP replies.
type Pinger struct {
	id      uint16
	seq     uint16
	timeout time.Duration
	log     Logger
	done    chan any

	mu sync.Mutex

	// The mutex only protects the setting and restoration of TTL and other send
	// options. All receive options and other operations may be set/invoked
	// concurrently.
	conn *icmp.PacketConn

	active map[activeKey]activeVal
}

// New creates a new Pinger.
func New(opts *Options) (*Pinger, error) {
	if opts == nil {
		opts = &Options{}
	}
	id, err := genID()
	if err != nil {
		return nil, fmt.Errorf("ID generation error: %v", err)
	}
	var logger = opts.Log
	if logger == nil {
		logger = log.Default()
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	conn, err := icmp.ListenPacket("udp4", opts.Interface)
	if err != nil {
		return nil, fmt.Errorf("listen error: %v", err)
	}
	p := &Pinger{
		conn:    conn,
		id:      id,
		timeout: timeout,
		log:     logger,
		done:    make(chan any),
		active:  make(map[activeKey]activeVal),
	}
	go p.receiveLoop()
	go p.timeoutLoop()
	return p, nil
}

// Close closes a Pinger.
func (p *Pinger) Close() {
	close(p.done)
}

// Randomly generates an ICMP echo identifier.
func genID() (uint16, error) {
	id, err := rand.Int(rand.Reader, big.NewInt(1<<16))
	if err != nil {
		return 0, err
	}
	return uint16(id.Uint64()), nil
}

func (p *Pinger) receiveLoop() {
	for {
		select {
		case <-p.done:
			return
		default:
			// Don't be tempted to create this once outside of the loop. That
			// would create a race condition since the result is handled in a
			// separate goroutine.
			buf := make([]byte, maxMTU)
			if err := p.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
				p.log.Printf("Error setting read deadline; read may block infinitely: %v", err)
			}
			n, peer, err := p.conn.ReadFrom(buf)
			if err != nil {
				if !os.IsTimeout(err) {
					p.log.Printf("Read error: %v", err)
				}
			} else {
				udpAddr := peer.(*net.UDPAddr)
				go p.handleReceive(udpAddr, buf[:n])
			}
		}
	}
}

func (p *Pinger) handleReceive(peer *net.UDPAddr, b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rm, err := icmp.ParseMessage(icmpV4ProtoNum, b)
	if err != nil {
		p.log.Printf("Error parsing ICMP message: %v", err)
		return
	}
	switch rm.Type {
	case ipv4.ICMPTypeEchoReply:
		p.handleEchoReply(peer, rm)
	case ipv4.ICMPTypeTimeExceeded:
		p.handleTimeExceeded(peer, rm)
	case ipv4.ICMPTypeDestinationUnreachable:
		p.handleDestinationUnreachable(peer, rm)
	default:
		p.log.Printf("got %+v; wanted echo reply", rm)
	}
}

func (p *Pinger) handleEchoReply(peer *net.UDPAddr, msg *icmp.Message) {
	body := msg.Body.(*icmp.Echo)
	key := activeKey{id: uint16(body.ID), seq: uint16(body.Seq)}
	waiter, ok := p.active[key]
	if !ok {
		return
	}
	delete(p.active, key)
	waiter.result <- PingReply{
		Type:    EchoReply,
		Peer:    peer,
		Seq:     uint16(body.Seq),
		Latency: time.Now().Sub(waiter.sentAt),
	}
}

func (p *Pinger) handleTimeExceeded(peer *net.UDPAddr, msg *icmp.Message) {
	body := msg.Body.(*icmp.TimeExceeded)

	ipHeader, err := ipv4.ParseHeader(body.Data)
	if err != nil {
		p.log.Printf("Error parsing returned IP header: %v", err)
		return
	}

	retIcmp, err := icmp.ParseMessage(icmpV4ProtoNum, body.Data[ipHeader.Len:])
	if err != nil {
		p.log.Printf("Error parsing returned ICMP message: %v", err)
		return
	}

	switch retIcmp.Type {
	case ipv4.ICMPTypeEcho:
		echo := retIcmp.Body.(*icmp.Echo)
		waiter, ok := p.active[activeKey{id: uint16(echo.ID), seq: uint16(echo.Seq)}]
		if !ok {
			return
		}
		waiter.result <- PingReply{
			Type:    TTLExceeded,
			Peer:    peer,
			Seq:     uint16(echo.Seq),
			Latency: time.Now().Sub(waiter.sentAt),
		}
	default:
		p.log.Printf("TimeExceeded from %v, unrecognized: %+v", peer, retIcmp)
	}
}

// TODO: Some sort of generic here? This is nigh identical to
// handleTimeExceeded().
func (p *Pinger) handleDestinationUnreachable(peer *net.UDPAddr, msg *icmp.Message) {
	body := msg.Body.(*icmp.DstUnreach)

	ipHeader, err := ipv4.ParseHeader(body.Data)
	if err != nil {
		p.log.Printf("Error parsing returned IP header: %v", err)
		return
	}
	retIcmp, err := icmp.ParseMessage(icmpV4ProtoNum, body.Data[ipHeader.Len:])
	if err != nil {
		p.log.Printf("Error parsing returned ICMP message: %v", err)
		return
	}
	if retIcmp.Type != ipv4.ICMPTypeEcho {
		p.log.Printf("Unreachable from %v, unrecognized: %+v", peer, retIcmp)
		return
	}

	echo := retIcmp.Body.(*icmp.Echo)
	waiter, ok := p.active[activeKey{id: uint16(echo.ID), seq: uint16(echo.Seq)}]
	if !ok {
		return
	}
	waiter.result <- PingReply{
		Type:    Unreachable,
		Peer:    peer,
		Seq:     uint16(echo.Seq),
		Latency: time.Now().Sub(waiter.sentAt),
	}
}

func (p *Pinger) timeoutLoop() {
	ticker := time.NewTicker(p.timeout)
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.sendTimeouts()
		}
	}
}

// Times out lost pings.
func (p *Pinger) sendTimeouts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := time.Now().Add(-p.timeout)
	for k, v := range p.active {
		if v.sentAt.Before(cutoff) {
			go func() {
				v.result <- PingReply{
					Type: Dropped,
					Seq:  k.seq,
				}
			}()
			delete(p.active, k)
		}
	}
}

// Send asynchronously sends a ping.
func (p *Pinger) Send(addr *net.UDPAddr, result chan<- PingReply) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sendBase(addr, result)
}

// Sends a ping. Caller must hold p.mu.
func (p *Pinger) sendBase(host *net.UDPAddr, result chan<- PingReply) error {
	p.seq++
	seq := p.seq
	wm := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:  int(p.id),
			Seq: int(seq),
		},
	}
	wb, err := wm.Marshal(nil)
	if err != nil {
		return fmt.Errorf("marshal error: %v", err)
	}

	sentAt := time.Now()
	p.active[activeKey{id: p.id, seq: seq}] = activeVal{
		sentAt: sentAt,
		result: result,
	}

	if _, err := p.conn.WriteTo(wb, host); err != nil {
		return err
	}
	return nil
}

func (p *Pinger) setTTL(ttl int) (func(), error) {
	if c := p.conn.IPv4PacketConn(); c != nil {
		return p.setTTLv4(c, ttl)
	} else if c := p.conn.IPv6PacketConn(); c != nil {
		return p.setTTLv6(c, ttl)
	} else {
		return nil, errors.New("connection isn't IPv4 or IPv6")
	}
}

func (p *Pinger) setTTLv4(conn *ipv4.PacketConn, ttl int) (func(), error) {
	orig, err := conn.TTL()
	if err != nil {
		return nil, err
	}
	if err := conn.SetTTL(ttl); err != nil {
		return nil, err
	}
	return func() {
		if err := conn.SetTTL(orig); err != nil {
			p.log.Printf("Unable to reset TTL: %v", err)
		}
	}, nil
}

func (p *Pinger) setTTLv6(conn *ipv6.PacketConn, ttl int) (func(), error) {
	orig, err := conn.HopLimit()
	if err != nil {
		return nil, err
	}
	if err := conn.SetHopLimit(ttl); err != nil {
		return nil, err
	}
	return func() {
		if err := conn.SetHopLimit(orig); err != nil {
			p.log.Printf("Unable to reset TTL: %v", err)
		}
	}, nil
}

// PathComponent is a single hop in the path to a host.
type PathComponent struct {

	// Host is the address of the host.
	Host *net.UDPAddr

	// Pos is the number of hops in the path.
	Pos int

	// Latency is the amount of time the host took to respond.
	Latency time.Duration
}

// Sends a ping with a TTL. The channel buffer must be >= 1, or this could cause
// a deadlock.
func (p *Pinger) sendTTL(host *net.UDPAddr, ttl int, res chan<- PingReply) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cleanup, err := p.setTTL(ttl)
	defer cleanup()
	if err != nil {
		return err
	}
	if err := p.sendBase(host, res); err != nil {
		return err
	}
	return nil
}

// Trace finds the path to a host. Steps in the path will be returned one at a
// time over the channel. The channel will be closed when the trace completes.
// Steps not be returned in any order or not at all.
func (p *Pinger) Trace(host *net.UDPAddr, res chan<- PathComponent) error {
	defer close(res)
	for i := 1; i < maxTTL; i++ {
		for j := 0; j < maxTries; j++ {
			ch := make(chan PingReply, 1)
			p.sendTTL(host, i, ch)
			repl := <-ch
			comp := PathComponent{
				Host:    repl.Peer,
				Pos:     i,
				Latency: repl.Latency,
			}
			if repl.Type == TTLExceeded || repl.Type == EchoReply {
				res <- comp
				if host.IP.Equal(repl.Peer.IP) {
					return nil
				}
				break
			}
		}
	}

	return nil
}
