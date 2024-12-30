package tracer

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/backend/test"
	"github.com/pcekm/vasily/internal/util"
	"go.uber.org/mock/gomock"
)

func hopAddr(hop int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(192, 0, 2, byte(hop))}
}

func traceExchange(ttl int, hopAddr *net.UDPAddr, dest net.Addr) *test.PingExchangeOpts {
	seq := ttl - 1
	opts := test.NewPingExchange(seq)
	opts.Dest = dest
	opts.TTL = ttl
	opts.RecvPkt.Type = backend.PacketTimeExceeded
	opts.Peer = hopAddr
	return opts
}

// Runs a trace and collects the validates the results.
func checkTrace(t *testing.T, name backend.Name, dest net.Addr, opts *Options, want []Step) error {
	t.Helper()
	ch := make(chan Step)
	errs := make(chan error)
	if opts == nil {
		opts = &Options{}
	}
	opts.Interval = noInterval
	go func() {
		if err := TraceRoute(name, util.IPv4, dest, ch, opts); err != nil {
			errs <- err
		}
		close(errs)
	}()
	var result []Step
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

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
			result = append(result, r)
		case <-ctx.Done():
			t.Error("Timed out waiting for result channel close.")
			break loop
		}
	}
	if diff := cmp.Diff(want, result); diff != "" {
		t.Errorf("Incorrect path (-want, +got):\n%v", diff)
	}
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return nil
	}
}

func TestTraceRoute(t *testing.T) {
	const pathLen = 3
	const nTries = 3

	dest := hopAddr(pathLen * nTries)

	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	name := test.RegisterMock(conn)

	for try := 0; try < nTries; try++ {
		for ttl := 0; ttl < pathLen; ttl++ {
			tp := backend.PacketTimeExceeded
			if ttl == pathLen-1 {
				tp = backend.PacketReply
			}
			opts := traceExchange(ttl+1, hopAddr((ttl+1)*10+try+1), dest)
			opts.RecvPkt.Type = tp
			conn.MockPingExchange(opts)
		}
	}

	want := []Step{
		{Pos: 1, Host: hopAddr(11)},
		{Pos: 2, Host: hopAddr(21)},
		{Pos: 3, Host: hopAddr(31)},
		{Pos: 1, Host: hopAddr(12)},
		{Pos: 2, Host: hopAddr(22)},
		{Pos: 3, Host: hopAddr(32)},
		{Pos: 1, Host: hopAddr(13)},
		{Pos: 2, Host: hopAddr(23)},
		{Pos: 3, Host: hopAddr(33)},
	}
	if err := checkTrace(t, name, dest, nil, want); err != nil {
		t.Errorf("TraceRoute error: %v", err)
	}

	ctrl.Finish()
}

func TestTraceRouteUnreachablePacket(t *testing.T) {
	const pathLen = 2

	dest := hopAddr(pathLen)

	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	name := test.RegisterMock(conn)
	conn.MockPingExchange(traceExchange(1, hopAddr(1), dest))
	opts := traceExchange(2, dest, dest)
	opts.RecvPkt.Type = backend.PacketDestinationUnreachable
	conn.MockPingExchange(opts)

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
	}
	if err := checkTrace(t, name, dest, nil, want); err == nil {
		t.Error("No error after destination unreachable.")
	}

	ctrl.Finish()
}

func TestTraceRouteDroppedPacket(t *testing.T) {
	const pathLen = 3

	dest := hopAddr(pathLen)

	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	name := test.RegisterMock(conn)
	conn.MockPingExchange(traceExchange(1, hopAddr(1), dest))

	opts := traceExchange(2, hopAddr(2), dest)
	opts.RecvErr = backend.ErrTimeout
	conn.MockPingExchange(opts)

	opts = traceExchange(3, dest, dest)
	opts.RecvPkt.Type = backend.PacketReply
	conn.MockPingExchange(opts)

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
		{Pos: 3, Host: hopAddr(3)},
	}
	if err := checkTrace(t, name, dest, &Options{ProbesPerHop: 1}, want); err != nil {
		t.Errorf("TraceRoute error: %v", err)
	}

	ctrl.Finish()
}

func TestTraceRouteDeduplication(t *testing.T) {
	const pathLen = 3

	dest := hopAddr(5)

	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	name := test.RegisterMock(conn)
	conn.MockPingExchange(traceExchange(1, hopAddr(1), dest))
	conn.MockPingExchange(traceExchange(2, hopAddr(2), dest))
	opt := traceExchange(3, hopAddr(5), dest)
	opt.RecvPkt.Type = backend.PacketReply
	conn.MockPingExchange(opt)

	conn.MockPingExchange(traceExchange(1, hopAddr(1), dest))
	conn.MockPingExchange(traceExchange(2, hopAddr(3), dest))
	opt = traceExchange(3, hopAddr(5), dest)
	opt.RecvPkt.Type = backend.PacketReply
	conn.MockPingExchange(opt)

	conn.MockPingExchange(traceExchange(1, hopAddr(1), dest))
	conn.MockPingExchange(traceExchange(2, hopAddr(4), dest))
	opt = traceExchange(3, hopAddr(5), dest)
	opt.RecvPkt.Type = backend.PacketReply
	conn.MockPingExchange(opt)

	want := []Step{
		{Pos: 1, Host: hopAddr(1)},
		{Pos: 2, Host: hopAddr(2)},
		{Pos: 3, Host: hopAddr(5)},
		{Pos: 2, Host: hopAddr(3)},
		{Pos: 2, Host: hopAddr(4)},
	}
	if err := checkTrace(t, name, dest, nil, want); err != nil {
		t.Errorf("TraceRoute error: %v", err)
	}

	ctrl.Finish()
}
