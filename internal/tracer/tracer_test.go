package tracer

import (
	"net"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/test"
	"go.uber.org/mock/gomock"
)

func hopAddr(hop int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(192, 0, 2, byte(hop))}
}

func traceExchange(seq, ttl int, dest net.Addr) *test.PingExchangeOpts {
	opts := test.NewPingExchange(seq)
	opts.Dest = dest
	opts.TTL = ttl
	opts.RecvPkt.Type = backend.PacketTimeExceeded
	opts.Peer = hopAddr(ttl)
	opts.ReadDeadline = time.Now().Add(time.Second)
	return opts
}

// Runs a trace and collects the validates the results.
func checkTrace(t *testing.T, conn *test.MockConn, dest net.Addr, want []Step) error {
	ch := make(chan Step)
	errs := make(chan error)
	go func() {
		if err := TraceRoute(func() (backend.Conn, error) { return conn, nil }, dest, ch); err != nil {
			errs <- err
		}
		close(errs)
	}()
	var result []Step
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case r, ok := <-ch:
			if !ok {
				break loop
			}
			if r.Pos == 0 {
				t.Errorf("Invalid Step received: %+v", r)
				break
			}
			i := r.Pos - 1
			result = slices.Grow(result, i+1)[:i+1]
			result[r.Pos-1] = r
		case <-deadline:
			t.Error("Timed out waiting for result channel close.")
			break loop
		}
	}
	if diff := cmp.Diff(want, result); diff != "" {
		t.Errorf("Incorrect path (-want, +got):\n%v", diff)
	}
	return <-errs
}

func TestTraceRoute(t *testing.T) {
	const pathLen = 10

	dest := hopAddr(pathLen)

	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)

	for i := 0; i < pathLen; i++ {
		tp := backend.PacketTimeExceeded
		if i == pathLen-1 {
			tp = backend.PacketReply
		}
		opts := traceExchange(i, i+1, dest)
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

	ctrl.Finish()
}

func TestTraceRouteUnreachablePacket(t *testing.T) {
	const pathLen = 3

	dest := hopAddr(pathLen)

	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	opts := traceExchange(0, 1, dest)
	conn.MockPingExchange(opts)
	opts = traceExchange(1, 2, dest)
	opts.RecvPkt.Type = backend.PacketDestinationUnreachable
	conn.MockPingExchange(opts)

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
	}
	if err := checkTrace(t, conn, dest, want); err == nil {
		t.Error("No error after destination unreachable.")
	}

	ctrl.Finish()
}

func TestTraceRouteDroppedPacket(t *testing.T) {
	const pathLen = 3

	dest := hopAddr(pathLen)

	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	opts := traceExchange(0, 1, dest)
	conn.MockPingExchange(opts)

	// Three retries for dropped packet:
	opts = traceExchange(1, 2, dest)
	opts.RecvErr = test.ErrTimeout
	conn.MockPingExchange(opts)
	opts = traceExchange(2, 2, dest)
	opts.RecvErr = test.ErrTimeout
	conn.MockPingExchange(opts)
	opts = traceExchange(3, 2, dest)
	opts.RecvErr = test.ErrTimeout
	conn.MockPingExchange(opts)

	opts = traceExchange(4, 3, dest)
	opts.RecvPkt.Type = backend.PacketReply
	conn.MockPingExchange(opts)

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
		{},
		{Pos: 3, Host: hopAddr(3)},
	}
	if err := checkTrace(t, conn, dest, want); err != nil {
		t.Errorf("TraceRoute error: %v", err)
	}

	ctrl.Finish()
}
