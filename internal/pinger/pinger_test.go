package pinger

import (
	"fmt"
	"log"
	"net"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/vasily/internal/backend"
	_ "github.com/pcekm/vasily/internal/backend/icmp"
	"github.com/pcekm/vasily/internal/backend/test"
	"github.com/pcekm/vasily/internal/util"
	"go.uber.org/mock/gomock"
)

var (
	supportedOS = map[string]bool{
		"darwin": true,
		"linux":  true,
	}
)

// Diffs two PingResults while accounting for the latency and timestamps.
func diffPingResults[T any](a, b T) string {
	return cmp.Diff(a, b,
		cmp.Comparer(func(a, b net.Addr) bool {
			return util.IP(a).Equal(util.IP(b))
		}),
		cmp.FilterValues(func(t1, t2 time.Duration) bool { return true }, cmp.Ignore()),
		cmp.FilterValues(func(t1, t2 time.Time) bool { return true }, cmp.Ignore()))
}

func TestLive(t *testing.T) {
	if !supportedOS[runtime.GOOS] && syscall.Getuid() != 0 {
		t.Skipf("Unsupported OS")
	}
	opts := &Options{
		NPings:   3,
		History:  3,
		Interval: time.Millisecond,
		Timeout:  5 * time.Millisecond,
	}
	p, err := New(backend.Name("icmp"), util.IPv4, test.LoopbackV4, opts)
	if err != nil {
		t.Fatalf("Error creating pinger: %v", err)
	}
	p.Run()

	if err := p.Close(); err != nil {
		t.Errorf("Error closing pinger: %v", err)
	}

	want := []PingResult{
		{Type: Success, Peer: test.LoopbackV4},
		{Type: Success, Peer: test.LoopbackV4},
		{Type: Success, Peer: test.LoopbackV4},
	}
	if diff := diffPingResults(want, p.History()); diff != "" {
		t.Errorf("Wrong history (-want, +got):\n%v", diff)
	}
}

func TestPacketLoss(t *testing.T) {
	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	pe := test.NewPingExchange(0).SetNoReply(true)
	conn.MockPingExchange(pe)
	pe = test.NewPingExchange(1)
	conn.MockPingExchange(pe)
	conn.MockClose()
	name := test.RegisterMock(conn)

	opts := &Options{
		NPings:   2,
		Interval: time.Microsecond,
		History:  2,
		Timeout:  time.Millisecond,
	}
	p, err := New(name, util.IPv4, test.LoopbackV4, opts)
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
	ctrl := gomock.NewController(t)
	conn := test.NewMockConn(ctrl)
	pe := test.NewPingExchange(0)
	conn.MockPingExchange(pe)
	pe = test.NewPingExchange(1)
	conn.MockPingExchange(pe)
	pe = test.NewPingExchange(2)
	pe.RecvPkt.Seq = 0
	conn.MockPingExchange(pe)
	conn.MockClose()
	name := test.RegisterMock(conn)

	opts := &Options{
		NPings:   3,
		Interval: time.Microsecond,
		History:  3,
		Timeout:  time.Millisecond,
	}
	p, err := New(name, util.IPv4, test.LoopbackV4, opts)
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
			ctrl := gomock.NewController(t)
			conn := test.NewMockConn(ctrl)
			name := test.RegisterMock(conn)
			for seq := 0; seq < c.nPings; seq++ {
				conn.MockPingExchange(test.NewPingExchange(seq).SetPeer(mkAddr(seq)))
			}
			conn.MockClose()

			opts := &Options{
				NPings:   c.nPings,
				Interval: 1 * time.Nanosecond,
				History:  c.nHist,
				Timeout:  1 * time.Millisecond,
			}
			p, err := New(name, util.IPv4, test.LoopbackV4, opts)
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
