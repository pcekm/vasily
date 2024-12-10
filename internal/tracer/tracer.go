// Package tracer implements a traceroute function.
package tracer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/pcekm/graphping/internal/backend"
)

const (
	defaultMaxTTL       = 64
	defaultProbesPerHop = 3
	defaultInterval     = time.Second

	noInterval = time.Duration(-1)

	// Maximum time to wait for a reply.
	timeout = time.Second
)

// Options contains [TraceRoute] options.
type Options struct {
	// Interval is the time between route probes. Defaults to 1s.
	Interval time.Duration

	// ProbesPerHop is the number of times to probe each step in the route.
	// Defaults to 3.
	ProbesPerHop int

	// MaxTTL is the maximum path length to probe. Defaults to 64.
	MaxTTL int
}

func (o *Options) interval() time.Duration {
	if o == nil || o.Interval == 0 {
		return defaultInterval
	}
	if o.Interval == noInterval {
		return 0
	}
	return o.Interval
}

func (o *Options) probesPerHop() int {
	if o == nil || o.ProbesPerHop == 0 {
		return defaultProbesPerHop
	}
	return o.ProbesPerHop
}

func (o *Options) maxTTL() int {
	if o == nil || o.MaxTTL == 0 {
		return defaultMaxTTL
	}
	return o.MaxTTL
}

// Step describes a single step in the path to a remote host.
type Step struct {
	// Pos is the hosts position in the path.
	Pos int

	// Host is the address of the host at this step.
	Host net.Addr
}

// TraceRoute finds the path to a host. Steps in the path will be returned one at a
// time over the channel. The channel will be closed when the trace completes.
// Steps may be returned in any order or not at all.
func TraceRoute(newConn backend.NewConn, dest net.Addr, res chan<- Step, opts *Options) error {
	defer close(res)
	conn, err := newConn()
	if err != nil {
		return fmt.Errorf("error creating connection: %v", err)
	}
	pkt := &backend.Packet{
		Seq: 0,
	}
	seen := make(map[string]bool)
	tick := immediateTick(opts.interval())
	for tryNum := 0; tryNum < opts.probesPerHop(); tryNum++ {
		done := false
		for ttl := 1; !done && ttl < opts.maxTTL(); ttl++ {
			<-tick
			if err := conn.WriteTo(pkt, dest, backend.TTLOption{TTL: ttl}); err != nil {
				return fmt.Errorf("error sending ping: %v", err)
			}
			pkt.Seq++
			recvPkt, peer, err := readSeq(conn, pkt.Seq-1)
			if err != nil {
				if errors.Is(err, backend.ErrTimeout) {
					continue
				}
				return fmt.Errorf("read error: %v", err)
			}
			if recvPkt.Type == backend.PacketDestinationUnreachable {
				return fmt.Errorf("destination unreachable: %v", peer)
			}

			if recvPkt.Type == backend.PacketReply {
				done = true
			}

			k := fmt.Sprintf("%d:%v", ttl, peer.String())
			if seen[k] {
				continue
			}
			seen[k] = true
			res <- Step{Pos: ttl, Host: peer}
		}
		if !done {
			return fmt.Errorf("maximum TTL of %d reached", defaultMaxTTL)
		}
	}
	return nil
}

// Like time.Tick, but the first tick occurs immediately rather than after d.
func immediateTick(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	if d == 0 {
		close(ch) // No delays.
		return ch
	}
	ch <- time.Now()
	go func() {
		for t := range time.Tick(d) {
			ch <- t
		}
	}()
	return ch
}

func probe(conn backend.Conn, pkt *backend.Packet, dest net.Addr, ttl int) (*backend.Packet, error) {
	if err := conn.WriteTo(pkt, dest, backend.TTLOption{TTL: ttl}); err != nil {
		return nil, fmt.Errorf("error sending ping: %v", err)
	}
	pkt.Seq++
	recvPkt, peer, err := readSeq(conn, pkt.Seq-1)
	if err != nil {
		if errors.Is(err, backend.ErrTimeout) {
			return nil, nil
		}
		return nil, fmt.Errorf("read error: %v", err)
	}
	if recvPkt.Type == backend.PacketDestinationUnreachable {
		return nil, fmt.Errorf("destination unreachable: %v", peer)
	}

	return recvPkt, nil
}

func readSeq(conn backend.Conn, seq int) (*backend.Packet, net.Addr, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	for {
		pkt, peer, err := conn.ReadFrom(ctx)
		if pkt != nil && (pkt.Seq != seq || pkt.Type == backend.PacketRequest) {
			continue
		}
		return pkt, peer, err
	}
}
