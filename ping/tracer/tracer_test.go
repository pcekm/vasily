package tracer

import (
	"net"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/ping/connection"
	"github.com/pcekm/graphping/ping/test"
)

func hopAddr(hop int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(192, 0, 2, byte(hop))}
}

func traceExchange(id, seq, ttl int, dest net.Addr) *test.PingExchangeOpts {
	opts := test.NewPingExchange(id, seq)
	opts.Dest = dest
	opts.TTL = ttl
	opts.RecvPkt.Type = connection.PacketTimeExceeded
	opts.Peer = hopAddr(ttl)
	return opts
}

// Runs a trace and collects the validates the reults.
func checkTrace(t *testing.T, conn *test.MockConn, dest net.Addr, want []Step) error {
	ch := make(chan Step)
	done := make(chan any)
	var result []Step
	go func() {
		for r := range ch {
			i := r.Pos - 1
			result = slices.Grow(result, i+1)[:i+1]
			result[r.Pos-1] = r
		}
		close(done)
	}()
	err := TraceRoute(conn, dest, ch)
	select {
	case <-time.After(time.Second):
		t.Error("Timed out waiting for result channel close.")
	case <-done:
	}
	if diff := cmp.Diff(want, result); diff != "" {
		t.Errorf("Incorrect path (-want, +got):\n%v", diff)
	}
	return err
}

func TestTraceRoute(t *testing.T) {
	const (
		id      = 12345
		pathLen = 10
	)
	defer test.InjectID(id)()

	dest := hopAddr(pathLen)

	conn := &test.MockConn{}

	for i := 0; i < pathLen; i++ {
		tp := connection.PacketTimeExceeded
		if i == pathLen-1 {
			tp = connection.PacketReply
		}
		opts := traceExchange(id, i, i+1, dest)
		opts.RecvPkt.Type = tp
		conn.MockPingExchange(opts)
	}

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
		{Pos: 2, Host: hopAddr(2)},
		{Pos: 3, Host: hopAddr(3)},
		{Pos: 4, Host: hopAddr(4)},
		{Pos: 5, Host: hopAddr(5)},
		{Pos: 6, Host: hopAddr(6)},
		{Pos: 7, Host: hopAddr(7)},
		{Pos: 8, Host: hopAddr(8)},
		{Pos: 9, Host: hopAddr(9)},
		{Pos: 10, Host: hopAddr(10)},
	}
	if err := checkTrace(t, conn, dest, want); err != nil {
		t.Errorf("TraceRoute error: %v", err)
	}

	conn.AssertExpectations(t)
}

func TestTraceRouteUnreachablePacket(t *testing.T) {
	const (
		id      = 12345
		pathLen = 3
	)
	defer test.InjectID(id)()

	dest := hopAddr(pathLen)

	conn := &test.MockConn{}
	opts := traceExchange(id, 0, 1, dest)
	conn.MockPingExchange(opts)
	opts = traceExchange(id, 1, 2, dest)
	opts.RecvPkt.Type = connection.PacketDestinationUnreachable
	conn.MockPingExchange(opts)

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
	}
	if err := checkTrace(t, conn, dest, want); err == nil {
		t.Error("No error from after dest unreachable.")
	}

	conn.AssertExpectations(t)
}

func TestTraceRouteDroppedPacket(t *testing.T) {
	const (
		id      = 12345
		pathLen = 3
	)
	defer test.InjectID(id)()

	dest := hopAddr(pathLen)

	conn := &test.MockConn{}
	opts := traceExchange(id, 0, 1, dest)
	conn.MockPingExchange(opts)

	// Three retries for dropped packet:
	opts = traceExchange(id, 1, 2, dest)
	opts.NoReply = true
	conn.MockPingExchange(opts)
	opts = traceExchange(id, 2, 2, dest)
	opts.NoReply = true
	conn.MockPingExchange(opts)
	opts = traceExchange(id, 3, 2, dest)
	opts.NoReply = true
	conn.MockPingExchange(opts)

	opts = traceExchange(id, 4, 3, dest)
	opts.RecvPkt.Type = connection.PacketReply
	conn.MockPingExchange(opts)

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
		{},
		{Pos: 3, Host: hopAddr(3)},
	}
	if err := checkTrace(t, conn, dest, want); err != nil {
		t.Errorf("TraceRoute error: %v", err)
	}

	conn.AssertExpectations(t)
}
