// Package tracer implements a traceroute function.
package tracer

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
)

const (
	// Maximum path length to search for traceroutes.
	maxTTL = 64

	// Maximum number of attempts to find a hop during a traceroute.
	maxTries = 3

	// Maximum time to wait for a reply.
	timeout = time.Second
)

// Step describes a single step in the path to a remote host.
type Step struct {
	// Pos is the hosts position in the path.
	Pos int

	// Host is the address of the host at this step.
	Host net.Addr
}

// TraceRoute finds the path to a host. Steps in the path will be returned one at a
// time over the channel. The channel will be closed when the trace completes.
// Steps not be returned in any order or not at all.
func TraceRoute(newConn backend.NewConn, dest net.Addr, res chan<- Step) error {
	defer close(res)
	conn, err := newConn()
	if err != nil {
		return fmt.Errorf("error creating connection: %v", err)
	}
	pkt := &backend.Packet{
		ID:  util.GenID(),
		Seq: 0,
	}
	for i := 1; i < maxTTL; i++ {
		for j := 0; j < maxTries; j++ {
			if err := conn.WriteTo(pkt, dest, backend.TTLOption{TTL: i}); err != nil {
				return fmt.Errorf("error sending ping: %v", err)
			}
			pkt.Seq++
			recvPkt, peer, err := readIDSeq(conn, pkt.ID, pkt.Seq-1)
			if err != nil {
				if strings.HasSuffix(err.Error(), "timeout") {
					continue
				}
				return fmt.Errorf("read error: %v", err)
			}
			if recvPkt.Type == backend.PacketDestinationUnreachable {
				return fmt.Errorf("destination unreachable: %v", peer)
			}
			res <- Step{Pos: i, Host: peer}
			if recvPkt.Type == backend.PacketReply {
				return nil
			}
			break
		}
	}
	return fmt.Errorf("maximum TTL of %d reached", maxTTL)
}

func readIDSeq(conn backend.Conn, id, seq int) (*backend.Packet, net.Addr, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		pkt, peer, err := conn.ReadFrom()
		if pkt != nil && (pkt.ID != id || pkt.Seq != seq || pkt.Type == backend.PacketRequest) {
			continue
		}
		return pkt, peer, err
	}
}
