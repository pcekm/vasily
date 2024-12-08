package pinger

import (
	"sync"
	"testing"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"github.com/google/go-cmp/cmp"
)

func TestAdd(t *testing.T) {
	c := fakeclock.NewFakeClock(time.Now())
	h := newHistory(1)
	h.clock = c
	h.Add(0)
	if diff := cmp.Diff(PingResult{Type: Waiting, Time: c.Now()}, h.Get(0)); diff != "" {
		t.Errorf("Wrong ping result (-want, +got):\n%v", diff)
	}
}

func TestAdd_WrongSeq(t *testing.T) {
	h := newHistory(1)
	var got any
	func() {
		defer func() {
			got = recover()
		}()
		h.Add(1)
	}()
	if diff := cmp.Diff("Wrong sequence number: 1 (want 0)", got); diff != "" {
		t.Errorf("Wrong panic result (-want, +got):\n%v", diff)
	}
}

func TestGet_Empty(t *testing.T) {
	h := newHistory(1)
	if diff := cmp.Diff(PingResult{}, h.Get(0)); diff != "" {
		t.Errorf("Wrong ping result (-want, +got):\n%v", diff)
	}
}

func TestGet_Missing(t *testing.T) {
	h := newHistory(1)
	h.Add(0)
	h.Add(1)
	if diff := cmp.Diff(PingResult{}, h.Get(0)); diff != "" {
		t.Errorf("Wrong ping result (-want, +got):\n%v", diff)
	}
}

func TestStats(t *testing.T) {
	start := time.Now()
	c := fakeclock.NewFakeClock(start)
	h := newHistory(4)
	h.clock = c

	addIncRec := func(seq, ms int, tp ResultType) {
		h.Add(seq)
		c.Increment(time.Duration(ms) * time.Millisecond)
		res := h.Get(seq)
		res.Type = tp
		h.Record(seq, res)
	}

	addIncRec(0, 10, Success)
	addIncRec(1, 20, Success)
	addIncRec(2, 30, Unreachable)
	addIncRec(3, 40, Dropped)

	want := Stats{
		N:          4,
		Failures:   2,
		AvgLatency: 15 * time.Millisecond,
	}

	if diff := cmp.Diff(want, h.Stats()); diff != "" {
		t.Errorf("Wrong stats (-want, +got):\n%v", diff)
	}
}

func TestStats_Overflow(t *testing.T) {
	start := time.Now()
	c := fakeclock.NewFakeClock(start)
	h := newHistory(4)
	h.clock = c

	addIncRec := func(seq, ms int, tp ResultType) {
		h.Add(seq)
		c.Increment(time.Duration(ms) * time.Millisecond)
		res := h.Get(seq)
		res.Type = tp
		h.Record(seq, res)
	}

	addIncRec(0, 10, Dropped)
	addIncRec(1, 20, TTLExceeded)
	addIncRec(2, 30, Success)
	addIncRec(3, 40, Success)
	addIncRec(4, 50, Success)

	want := Stats{
		N:          5,
		Failures:   2,
		AvgLatency: 40 * time.Millisecond,
	}

	if diff := cmp.Diff(want, h.Stats()); diff != "" {
		t.Errorf("Wrong stats (-want, +got):\n%v", diff)
	}
}

func TestStats_Empty(t *testing.T) {
	h := newHistory(10)
	if diff := cmp.Diff(Stats{}, h.Stats()); diff != "" {
		t.Errorf("Wrong stats (-want, +got):\n%v", diff)
	}
}

func TestRevResults(t *testing.T) {
	start := time.Now()
	c := fakeclock.NewFakeClock(start)
	h := newHistory(4)
	h.clock = c

	addIncRec := func(seq, ms int, tp ResultType) {
		h.Add(seq)
		c.Increment(time.Duration(ms) * time.Millisecond)
		res := h.Get(seq)
		res.Type = tp
		h.Record(seq, res)
	}

	addIncRec(0, 10, Dropped)
	addIncRec(1, 20, TTLExceeded)
	addIncRec(2, 30, Success)
	addIncRec(3, 40, Success)
	addIncRec(4, 50, Success)

	var mu sync.Mutex
	var got []PingResult
	for _, r := range h.RevResults(&mu) {
		got = append(got, r)
	}

	want := []PingResult{
		{Type: Success, Time: start.Add(100 * time.Millisecond), Latency: 50 * time.Millisecond},
		{Type: Success, Time: start.Add(60 * time.Millisecond), Latency: 40 * time.Millisecond},
		{Type: Success, Time: start.Add(30 * time.Millisecond), Latency: 30 * time.Millisecond},
		{Type: TTLExceeded, Time: start.Add(10 * time.Millisecond), Latency: 20 * time.Millisecond},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Wrong RevResults (-want, +got):\n%v", diff)
	}
}

func TestHistoryFunc(t *testing.T) {
	start := time.Now()
	c := fakeclock.NewFakeClock(start)
	h := newHistory(4)
	h.clock = c

	addIncRec := func(seq, ms int, tp ResultType) {
		h.Add(seq)
		c.Increment(time.Duration(ms) * time.Millisecond)
		res := h.Get(seq)
		res.Type = tp
		h.Record(seq, res)
	}

	addIncRec(0, 10, Dropped)
	addIncRec(1, 20, TTLExceeded)
	addIncRec(2, 30, Success)
	addIncRec(3, 40, Success)
	addIncRec(4, 50, Success)

	var mu sync.Mutex
	got := h.History(&mu)

	want := []PingResult{
		{Type: TTLExceeded, Time: start.Add(10 * time.Millisecond), Latency: 20 * time.Millisecond},
		{Type: Success, Time: start.Add(30 * time.Millisecond), Latency: 30 * time.Millisecond},
		{Type: Success, Time: start.Add(60 * time.Millisecond), Latency: 40 * time.Millisecond},
		{Type: Success, Time: start.Add(100 * time.Millisecond), Latency: 50 * time.Millisecond},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Wrong RevResults (-want, +got):\n%v", diff)
	}
}

func TestLatest(t *testing.T) {
	start := time.Now()
	c := fakeclock.NewFakeClock(start)
	h := newHistory(4)
	h.clock = c

	addIncRec := func(seq, ms int, tp ResultType) {
		h.Add(seq)
		c.Increment(time.Duration(ms) * time.Millisecond)
		res := h.Get(seq)
		res.Type = tp
		h.Record(seq, res)
	}

	addIncRec(0, 10, Dropped)
	addIncRec(1, 20, TTLExceeded)
	addIncRec(2, 30, Success)
	addIncRec(3, 40, Success)
	addIncRec(4, 50, Success)

	want := PingResult{Type: Success, Time: start.Add(100 * time.Millisecond), Latency: 50 * time.Millisecond}
	if diff := cmp.Diff(want, h.Latest()); diff != "" {
		t.Errorf("Wrong RevResults (-want, +got):\n%v", diff)
	}
}
