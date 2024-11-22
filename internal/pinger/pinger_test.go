package pinger

import (
	"fmt"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmp"
	"github.com/pcekm/graphping/internal/backend/test"
	"github.com/pcekm/graphping/internal/util"
	"go.uber.org/mock/gomock"
)

// Compares two durations to the nearest millisecond.
func msEq(a, b time.Duration) bool {
	// Sometimes packets can take a little over a millisecond even when no
	// latency has been set. This causes flakiness. Assume if both are less than
	// 1.5ms, that they are equal.
	if a < 1500*time.Microsecond && b < 1500*time.Microsecond {
		return true
	}
	return (a - b).Abs() < time.Millisecond
}

// Diffs two PingResults while accounting for the latency and timestamps.
func diffPingResults[T any](a, b T) string {
	return cmp.Diff(a, b,
		cmp.Comparer(msEq),
		cmp.FilterValues(func(t1, t2 time.Time) bool { return true }, cmp.Ignore()))
}

func newConnFunc(c backend.Conn) backend.NewConn {
	return func() (backend.Conn, error) {
		return c, nil
	}
}

func TestLive(t *testing.T) {
	var mu sync.Mutex
	callbackRes := make([]PingResult, 10)
	opts := &Options{
		NPings:   10,
		Interval: time.Millisecond,
		History:  10,
		Callback: func(seq int, res PingResult) {
			mu.Lock()
			defer mu.Unlock()
			callbackRes[seq] = res
		},
	}
	p, err := Ping(func() (backend.Conn, error) { return icmp.New("udp4", "") }, test.LoopbackV4, opts)
	if err != nil {
		t.Fatalf("Error creating pinger: %v", err)
	}
	p.Run()

	if err := p.Close(); err != nil {
		t.Errorf("Error closing pinger: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff(callbackRes, p.History()); diff != "" {
		t.Errorf("Callbacks received don't match call to History (-callbacks, +history):\n%v", diff)
	}
}

func TestCallbacks(t *testing.T) {
	id := util.GenID()
	addr := test.LoopbackV4
	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	pe := test.NewPingExchange(id, 0)
	conn.MockPingExchange(pe)
	pe = test.NewPingExchange(id, 1)
	conn.MockPingExchange(pe)
	conn.MockClose()

	var mu sync.Mutex
	var callbacks []PingResult
	opts := &Options{
		NPings:   2,
		Interval: time.Microsecond,
		History:  2,
		ID:       id,
		Timeout:  time.Millisecond,
		Callback: func(seq int, res PingResult) {
			mu.Lock()
			defer mu.Unlock()
			callbacks = append(callbacks, res)
		},
	}
	p, err := Ping(newConnFunc(conn), addr, opts)
	if err != nil {
		t.Fatalf("Error creating pinger: %v", err)
	}
	if !test.WithTimeout(p.Run, time.Second) {
		t.Error("Timed out waiting for pinger completion.")
	}
	if err := p.Close(); err != nil {
		t.Errorf("Error closing pinger: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []PingResult{
		{Type: Success, Peer: test.LoopbackV4},
		{Type: Success, Peer: test.LoopbackV4},
	}
	if diff := diffPingResults(want, callbacks); diff != "" {
		t.Errorf("Callbacks produced wrong result types (-want, +got):\n%v", diff)
	}

	ctrl.Finish()
}

func TestPacketLoss(t *testing.T) {
	id := util.GenID()
	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	pe := test.NewPingExchange(id, 0).SetNoReply(true)
	conn.MockPingExchange(pe)
	pe = test.NewPingExchange(id, 1)
	conn.MockPingExchange(pe)
	conn.MockClose()

	opts := &Options{
		NPings:   2,
		Interval: time.Microsecond,
		History:  2,
		ID:       id,
		Timeout:  time.Millisecond,
	}
	p, err := Ping(newConnFunc(conn), test.LoopbackV4, opts)
	if err != nil {
		t.Fatalf("Error creating pinger: %v", err)
	}
	if !test.WithTimeout(p.Run, time.Second) {
		t.Error("Timed out waiting for pinger completion.")
	}
	if err := p.Close(); err != nil {
		t.Errorf("Error closing pinger: %v", err)
	}

	want := []PingResult{
		{Type: Dropped},
		{Type: Success, Peer: test.LoopbackV4},
	}
	if diff := diffPingResults(want, p.History()); diff != "" {
		t.Errorf("Wrong ping results (-want, +got):\n%v", diff)
	}

	if pl := p.Stats().PacketLoss(); pl != 0.5 {
		t.Errorf("Wrong packet loss stats: %f (want %f)", pl, 0.5)
	}
	log.Printf("Stats: %+v", p.Stats())

	ctrl.Finish()
}

func TestDuplicatePacket(t *testing.T) {
	id := util.GenID()
	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	pe := test.NewPingExchange(id, 0)
	conn.MockPingExchange(pe)
	pe = test.NewPingExchange(id, 1)
	conn.MockPingExchange(pe)
	pe = test.NewPingExchange(id, 2)
	pe.RecvPkt.Seq = 0
	conn.MockPingExchange(pe)
	conn.MockClose()

	opts := &Options{
		NPings:   3,
		Interval: time.Microsecond,
		History:  3,
		ID:       id,
		Timeout:  time.Millisecond,
	}
	p, err := Ping(newConnFunc(conn), test.LoopbackV4, opts)
	if err != nil {
		t.Fatalf("Error creating pinger: %v", err)
	}
	if !test.WithTimeout(p.Run, time.Second) {
		t.Error("Timed out waiting for pinger completion.")
	}
	if err := p.Close(); err != nil {
		t.Errorf("Error closing pinger: %v", err)
	}

	want := []PingResult{
		{Type: Duplicate, Peer: test.LoopbackV4},
		{Type: Success, Peer: test.LoopbackV4},
		{Type: Dropped}}
	if diff := diffPingResults(want, p.History()); diff != "" {
		t.Errorf("Wrong ping results (-want, +got):\n%v", diff)
	}
	if pl := p.Stats().PacketLoss(); pl != 1/3. {
		t.Errorf("Wrong packet loss stats: %f (want %f)", pl, 1/3.)
	}
	log.Printf("Stats: %+v", p.Stats())

	ctrl.Finish()
}

// TODO: This doesn't work right. Trying to either inject a mock clock, or real
// delays into pinger is very difficult. The best approach is probably to
// refactor into pieces (e.g. separate sender, receiver, timeout, stats collection)
// that can each be tested more easily.
//
// func TestStats(t *testing.T) {
// 	id := util.GenID()
// 	addr := test.LoopbackV4
// 	cases := []struct {
// 		Name          string
// 		Opts          test.PingExchangeOpts
// 		WantErrResult PingResult
// 	}{
// 		{
// 			Name: backend.PacketTimeExceeded.String(),
// 			Opts: test.PingExchangeOpts{
// 				SendPkt: backend.Packet{ID: id, Seq: 3},
// 				Dest:    addr,
// 				RecvPkt: backend.Packet{Type: backend.PacketTimeExceeded, ID: id, Seq: 3},
// 				Peer:    addr,
// 				Latency: 4 * time.Millisecond,
// 			},
// 			WantErrResult: PingResult{Type: TTLExceeded, Latency: 4 * time.Millisecond, Peer: addr},
// 		},
// 		{
// 			Name: backend.PacketDestinationUnreachable.String(),
// 			Opts: test.PingExchangeOpts{
// 				SendPkt: backend.Packet{ID: id, Seq: 3},
// 				Dest:    addr,
// 				RecvPkt: backend.Packet{Type: backend.PacketDestinationUnreachable, ID: id, Seq: 3},
// 				Peer:    addr,
// 				Latency: 4 * time.Millisecond,
// 			},
// 			WantErrResult: PingResult{Type: Unreachable, Latency: 4 * time.Millisecond, Peer: addr},
// 		},
// 		{
// 			Name: "Dropped",
// 			Opts: test.PingExchangeOpts{
// 				SendPkt: backend.Packet{ID: id, Seq: 3},
// 				Dest:    addr,
// 				NoReply: true,
// 			},
// 			WantErrResult: PingResult{Type: Dropped},
// 		},
// 	}
// 	for _, c := range cases {
// 		t.Run(c.Name, func(t *testing.T) {
// 			ctrl := gomock.NewController(t)
// 			conn := test.NewMockConn(ctrl)
// 			pe := test.NewPingExchange(id, 0).SetLatency(time.Millisecond)
// 			conn.MockPingExchange(pe)
// 			pe = test.NewPingExchange(id, 1).SetLatency(time.Millisecond)
// 			conn.MockPingExchange(pe)
// 			pe = test.NewPingExchange(id, 2).SetLatency(time.Millisecond)
// 			conn.MockPingExchange(pe)
// 			conn.MockPingExchange(&c.Opts)
// 			conn.MockClose()
//
// 			opts := &Options{
// 				NPings:   4,
// 				Interval: time.Second,
// 				History:  4,
// 				ID:       id,
// 				Timeout:  4 * time.Millisecond,
// 			}
// 			p := Ping(conn, test.LoopbackV4, opts)
// 			if !test.WithTimeout(p.Run, time.Second) {
// 				t.Error("Timed out waiting for pinger completion.")
// 			}
// 			if err := p.Close(); err != nil {
// 				t.Errorf("Error closing pinger: %v", err)
// 			}
//
// 			want := []PingResult{
// 				{Type: Success, Latency: time.Millisecond, Peer: test.LoopbackV4},
// 				{Type: Success, Latency: 2 * time.Millisecond, Peer: test.LoopbackV4},
// 				{Type: Success, Latency: 3 * time.Millisecond, Peer: test.LoopbackV4},
// 				c.WantErrResult,
// 			}
// 			if diff := diffPingResults(want, p.History()); diff != "" {
// 				t.Errorf("Wrong results (-want, +got):\n%v", diff)
// 			}
//
// 			s := p.Stats()
// 			// (1ms + 2ms + 3ms) / 3 = 2ms
// 			if !msEq(s.AvgLatency, 2*time.Millisecond) {
// 				t.Errorf("Wrong AvgLatency: %v (want about %v)", s.AvgLatency, 2*time.Millisecond)
// 			}
// 			if s.N != 4 {
// 				t.Errorf("Wrong N packets sent: %v (want %v)", s.N, 4)
// 			}
// 			if s.Failures != 1 {
// 				t.Errorf("Wrong Failures: %v (want %v)", s.Failures, 1)
// 			}
// 			if pl := s.PacketLoss(); pl != 0.25 {
// 				t.Errorf("Wrong PacketLoss(): %v (want %v)", pl, 0.25)
// 			}
//
// 			ctrl.Finish()
// 		})
// 	}
// }

func TestHistory(t *testing.T) {
	mkAddr := func(i int) net.Addr {
		return &net.UDPAddr{IP: net.IPv4(192, 0, 2, byte(i+1))}
	}
	mkWant := func(firstSeq, nSeq int) []PingResult {
		var want []PingResult
		for i := 0; i < nSeq; i++ {
			want = append(want, PingResult{Type: Success, Peer: mkAddr(i + firstSeq)})
		}
		return want
	}

	cases := []struct {
		nPings, nHist int
		want          []PingResult
	}{
		{nPings: 27, nHist: 300, want: mkWant(0, 27)},
		{nPings: 5, nHist: 0, want: mkWant(0, 5)}, // Default when nHist = 0 is 300
		{nPings: 5, nHist: 6, want: mkWant(0, 5)},
		{nPings: 5, nHist: 5, want: mkWant(0, 5)},
		{nPings: 5, nHist: 4, want: mkWant(1, 4)},
		{nPings: 5, nHist: 3, want: mkWant(2, 3)},
		{nPings: 5, nHist: 2, want: mkWant(3, 2)},
		{nPings: 5, nHist: 1, want: mkWant(4, 1)},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("nPings=%d/nHist=%d", c.nPings, c.nHist), func(t *testing.T) {
			id := util.GenID()
			ctrl := gomock.NewController(t)
			conn := test.NewMockConn(ctrl)
			for seq := 0; seq < c.nPings; seq++ {
				conn.MockPingExchange(test.NewPingExchange(id, seq).SetPeer(mkAddr(seq)))
			}
			conn.MockClose()

			opts := &Options{
				NPings:   c.nPings,
				Interval: time.Millisecond,
				History:  c.nHist,
				ID:       id,
				Timeout:  4 * time.Millisecond,
			}
			p, err := Ping(newConnFunc(conn), test.LoopbackV4, opts)
			if err != nil {
				t.Fatalf("Error creating pinger: %v", err)
			}
			if !test.WithTimeout(p.Run, time.Second) {
				t.Error("Timed out waiting for pinger completion.")
			}
			if err := p.Close(); err != nil {
				t.Errorf("Error closing pinger: %v", err)
			}

			if diff := diffPingResults(c.want, p.History()); diff != "" {
				t.Errorf("Wrong results (-want, +got):\n%v", diff)
			}
			if diff := diffPingResults(c.want[len(c.want)-1], p.Latest()); diff != "" {
				t.Errorf("Wrong Latest() result (-want, +got):\n%v", diff)
			}

			ctrl.Finish()
		})
	}
}

func TestWrongIDRejection(t *testing.T) {
	const (
		id1 = 1
		id2 = 2
	)
	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	pe := test.NewPingExchange(id1, 0)
	pe.RecvPkt.ID = id2
	conn.MockPingExchange(pe)
	conn.MockClose()

	opts := &Options{
		NPings:   1,
		Interval: time.Microsecond,
		ID:       id1,
		Timeout:  100 * time.Microsecond,
	}
	p, err := Ping(newConnFunc(conn), test.LoopbackV4, opts)
	if err != nil {
		t.Fatalf("Error creating pinger: %v", err)
	}
	if !test.WithTimeout(p.Run, time.Second) {
		t.Error("Timed out waiting for pinger completion.")
	}
	if err := p.Close(); err != nil {
		t.Errorf("Error closing pinger: %v", err)
	}

	want := []PingResult{{Type: Dropped}}
	if diff := diffPingResults(want, p.History()); diff != "" {
		t.Errorf("Wrong results (-want, +got):\n%v", diff)
	}

	ctrl.Finish()
}
