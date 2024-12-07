// Package pinger pings hosts.
package pinger

import (
	"container/list"
	"context"
	"fmt"
	"iter"
	"log"
	"math"
	"net"
	"sync"
	"time"

	"github.com/pcekm/graphping/internal/backend"
)

const (
	// Number of possible sequence numbers.
	sequenceNoMask = (1 << 16) - 1
)

// CallbackFunc is the signature for callback functions.
type CallbackFunc func(seq int, result PingResult)

// Options contains options for the pinger.
type Options struct {
	// NPings is the number of pings to send. Zero means infinite.
	NPings int

	// Interval is the time interval to send pings at. Defaults to 1s.
	Interval time.Duration

	// History is the maximum number of ping results to store. Defaults to 300.
	History int

	// Timeout is the maximum amount of time to wait before assuming no response
	// is coming. Defaults to 1s if unset.
	Timeout time.Duration

	// Callback is a function that gets called anytime a new result is
	// available.
	Callback CallbackFunc
}

func (o *Options) nPings() int {
	if o == nil || o.NPings == 0 {
		return math.MaxInt
	}
	return o.NPings
}

func (o *Options) interval() time.Duration {
	if o == nil || o.Interval == 0 {
		return time.Second
	}
	return o.Interval
}

func (o *Options) history() int {
	if o == nil || o.History == 0 {
		return 300
	}
	return o.History
}

func (o *Options) timeout() time.Duration {
	if o == nil || o.Timeout == 0 {
		return time.Second
	}
	return o.Timeout
}

func (o *Options) callback() CallbackFunc {
	if o == nil || o.Callback == nil {
		return func(int, PingResult) {}
	}
	return o.Callback
}

// ResultType is the type of reply received. This is a high-level view. More
// specifics will require delving into the returned packet.
type ResultType int

// Values for ReplyType.
const (
	// Waiting means we're still waiting for a reply.
	Waiting ResultType = iota

	// Success is a normal ping response.
	Success

	// Dropped means no reply was received in the allotted time.
	Dropped

	// Duplicate means a duplicate reply was received.
	Duplicate

	// TTLExceeded means the packet exceeded its maximum hop count.
	TTLExceeded

	// Unreachable means the host was unreachable.
	Unreachable
)

func (r ResultType) String() string {
	switch r {
	case Waiting:
		return "Unknown"
	case Success:
		return "Success"
	case Dropped:
		return "Dropped"
	case Duplicate:
		return "Duplicate"
	case TTLExceeded:
		return "TTLExceeded"
	case Unreachable:
		return "Unreachable"
	default:
		return fmt.Sprintf("(unknown:%d)", r)
	}
}

// PingResult holds the result of a ping, returned over a channel.
type PingResult struct {
	// Type is the type of result.
	Type ResultType

	// Time is the time the request was sent.
	Time time.Time

	// Latency is the time for a response.
	Latency time.Duration

	// Peer is the host that responded to the ping.
	Peer net.Addr
}

type readResult struct {
	pkt  *backend.Packet
	peer net.Addr
}

type timeoutDatum struct {
	seq int
	t   time.Time
}

// Pinger pings a specific host and reports the results.
type Pinger struct {
	conn backend.Conn
	dest net.Addr
	opts *Options
	done chan any

	mu   sync.Mutex
	hist *pingHistory
}

// New creates a new pinger and starts pinging. It will continue until Close()
// is called.
func New(newConn backend.NewConn, dest net.Addr, opts *Options) (*Pinger, error) {
	conn, err := newConn()
	if err != nil {
		return nil, err
	}

	return &Pinger{
		conn: conn,
		dest: dest,
		opts: opts,
		done: make(chan any),
		hist: newHistory(opts.history()),
	}, nil
}

// Close stops the Pinger and performs an orderly shutdown.
func (p *Pinger) Close() error {
	close(p.done)
	return p.conn.Close()
}

// Latest returns the most recent ping result or the zero result if no results
// are available.
func (p *Pinger) Latest() PingResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hist.Latest()
}

// RevResults iterates over sequence#, result from newest to oldest.
// Note: This locks the mutex for the lifetime of the iterator.
func (p *Pinger) RevResults() iter.Seq2[int, PingResult] {
	return p.hist.RevResults(&p.mu)
}

// History returns the ping history.
// Deprecated: Use RevResults() and iterate.
func (p *Pinger) History() []PingResult {
	return p.hist.History(&p.mu)
}

// Stats returns ping statistics.
func (p *Pinger) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hist.Stats()
}

// Runs the callback (if any was given).
func (p *Pinger) runCallback(seq int, result PingResult) {
	go p.opts.callback()(seq, result)
}

func (p *Pinger) afterNextTimeout(timeouts *list.List) <-chan time.Time {
	fr := timeouts.Front()
	if fr == nil {
		return nil
	}
	return time.After(fr.Value.(timeoutDatum).t.Sub(time.Now()))
}

// Runs the pinger. Returns when complete, or Close().
func (p *Pinger) Run() {
	sentSeqs := make(chan int)
	go p.sendLoop(sentSeqs)
	receivedPkts := make(chan readResult)
	go p.receiveLoop(receivedPkts)

	timeouts := list.New()
	shutdown := false

	for {
		select {
		case seq, ok := <-sentSeqs:
			if !ok {
				log.Printf("Main loop: shutting down")
				shutdown = true
				sentSeqs = nil
				break
			}
			timeouts.PushBack(timeoutDatum{seq: seq, t: time.Now().Add(p.opts.timeout())})
		case res := <-receivedPkts:
			p.handleReply(res.pkt, res.peer)
		case <-p.afterNextTimeout(timeouts):
			fr := timeouts.Front()
			timeouts.Remove(fr)
			td := fr.Value.(timeoutDatum)
			p.maybeRecordTimeout(td.seq)
			if shutdown && timeouts.Len() == 0 {
				log.Printf("Main loop: finished shutdown")
				return
			}
		case <-p.done:
			log.Printf("Main loop: aborting")
			return
		}
	}
}

// Sends pings and emits the sent sequence numbers over the channel.
func (p *Pinger) sendLoop(sentSeqs chan<- int) {
	defer close(sentSeqs)
	// Note: This deliberately doesn't use p.clock because trying to manage
	// advancing the clock and getting this to fire correctly is a nightmare.
	ticker := time.NewTicker(p.opts.interval())
	defer ticker.Stop()
	pingsRemaining := p.opts.nPings()
	seq := 0
	for {
		select {
		case <-ticker.C:
			if pingsRemaining <= 0 {
				return
			}
			pingsRemaining--
			err := p.sendPing(seq)
			if err != nil {
				log.Printf("Ping error; exiting send loop: %v", err)
				return
			}
			sentSeqs <- seq
			seq = (seq + 1) & sequenceNoMask
		case <-p.done:
			return
		}
	}
}

// Sends a ping.
func (p *Pinger) sendPing(seq int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	pkt := &backend.Packet{Seq: seq}
	if err := p.conn.WriteTo(pkt, p.dest); err != nil {
		return fmt.Errorf("error pinging %v: %v", p.dest, err)
	}
	p.hist.Add(seq)
	return nil
}

// Receives pings and emits the results over the channel. Stops when conn is
// closed.
func (p *Pinger) receiveLoop(received chan<- readResult) {
	for {
		pkt, peer, err := p.conn.ReadFrom(context.TODO())
		if err != nil {
			log.Printf("ReadFrom error: %v", err)
			return
		}
		received <- readResult{pkt: pkt, peer: peer}
	}
}

func (p *Pinger) handleReply(pkt *backend.Packet, peer net.Addr) {
	p.mu.Lock()
	defer p.mu.Unlock()

	res := p.hist.Get(pkt.Seq)
	res.Peer = peer

	if t := res.Type; t != Waiting && t != Dropped {
		log.Printf("Duplicate packet: %v", pkt)
		res.Type = Duplicate
		res = p.hist.Record(pkt.Seq, res)
		p.runCallback(pkt.Seq, res)
		return
	}

	switch pkt.Type {
	case backend.PacketRequest:
		// This case should be filtered out by PingConnection.
		log.Panicf("Unexpected packet request received: %v", pkt)
	case backend.PacketReply:
		res.Type = Success
	case backend.PacketTimeExceeded:
		res.Type = TTLExceeded
	case backend.PacketDestinationUnreachable:
		res.Type = Unreachable
	}

	res = p.hist.Record(pkt.Seq, res)
	p.runCallback(pkt.Seq, res)
}

// Records a timeout if necessary.
func (p *Pinger) maybeRecordTimeout(seq int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := p.hist.Get(seq)
	if res.Type != Waiting {
		return
	}
	res.Type = Dropped
	res = p.hist.Record(seq, res)
	p.runCallback(seq, res)
}
