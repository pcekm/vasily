// Package tracer implements a traceroute function.
package tracer

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pcekm/graphping/ping/connection"
	"github.com/pcekm/graphping/ping/util"
)

const (
	// Maximum path length to search for traceroutes.
	maxTTL = 64

	// Maximum number of attempts to find a hop during a traceroute.
	maxTries = 3

	// Maximum time to wait for a reply.
	timeout = time.Second
)

// Conn is the portion of the PingConn interface required by TraceRoute.
type Conn interface {
	// WriteToTTL sends a ping with the given TTL.
	WriteToTTL(pkt *connection.Packet, dest net.Addr, ttl int) error

	// AddCallback adds a read callback.
	AddCallback(cb connection.Callback) connection.Remover
}

// Step describes a single step in the path to a remote host.
type Step struct {
	// Pos is the hosts position in the path.
	Pos int

	// Host is the address of the host at this step.
	Host net.Addr
}

type readRes struct {
	pkt  *connection.Packet
	peer net.Addr
}

// TraceRoute finds the path to a host. Steps in the path will be returned one at a
// time over the channel. The channel will be closed when the trace completes.
// Steps not be returned in any order or not at all.
func TraceRoute(conn Conn, dest net.Addr, res chan<- Step) error {
	defer close(res)

	readCh := make(chan readRes)
	defer conn.AddCallback(readCallback(readCh))()

	pkt := &connection.Packet{
		ID:  util.GenID(),
		Seq: 0,
	}

	for i := 1; i < maxTTL; i++ {
		for j := 0; j < maxTries; j++ {
			if err := conn.WriteToTTL(pkt, dest, i); err != nil {
				return fmt.Errorf("error sending ping: %v", err)
			}
			pkt.Seq++
			recvPkt, peer, err := readIDSeq(readCh, pkt.ID, pkt.Seq-1)
			if err != nil {
				if strings.HasSuffix(err.Error(), "timeout") {
					continue
				}
				return fmt.Errorf("read error: %v", err)
			}
			if recvPkt.Type == connection.PacketDestinationUnreachable {
				return fmt.Errorf("destination unreachable: %v", peer)
			}
			res <- Step{Pos: i, Host: peer}
			if recvPkt.Type == connection.PacketReply {
				return nil
			}
			break
		}
	}
	return fmt.Errorf("maximum TTL of %d reached", maxTTL)
}

func readCallback(ch chan<- readRes) connection.Callback {
	return func(pkt *connection.Packet, peer net.Addr) {
		ch <- readRes{pkt: pkt, peer: peer}
	}
}

func readIDSeq(ch <-chan readRes, id, seq int) (*connection.Packet, net.Addr, error) {
	timedOut := time.After(timeout)
	for {
		select {
		case res := <-ch:
			if res.pkt.ID != id || res.pkt.Seq != seq || res.pkt.Type == connection.PacketRequest {
				continue
			}
			return res.pkt, res.peer, nil
		case <-timedOut:
			return nil, nil, errors.New("timeout")
		}
	}
}
