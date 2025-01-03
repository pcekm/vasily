package pinger

import (
	"iter"
	"log"
	"math"
	"slices"
	"sync"
	"time"

	"code.cloudfoundry.org/clock"
)

// Stats holds statistics for a ping session.
type Stats struct {
	// N is the number of pings represented in these stats.
	N int

	// Failures is the number of pings without a successful reply.
	Failures int

	// AvgLatency is the average latency of successful pings.
	AvgLatency time.Duration

	// StdDev is the standard deviation of successful ping latencies.
	StdDev time.Duration
}

// PacketLoss is the fraction of dropped packets.
func (s Stats) PacketLoss() float64 {
	return float64(s.Failures) / float64(s.N)
}

type pingHistory struct {
	// This is a ring buffer. The index for a given sequence number is given by:
	//    i = seq % len(history)
	history []PingResult
	stats   Stats
	// Intermediate value for calculating a streaming variance.
	m2      time.Duration
	len     int
	lastSeq int
	clock   clock.Clock
}

func newHistory(n int) *pingHistory {
	return &pingHistory{
		history: make([]PingResult, n),
		lastSeq: -1,
		clock:   clock.NewClock(),
	}
}

// Get gets the result for the given sequence number. Returns the zero value if
// that sequence number is no longer in the history.
func (h *pingHistory) Get(seq int) PingResult {
	if seq < h.lastSeq-len(h.history)+1 {
		// That seq is long gone.
		return PingResult{}
	}
	i := seq % len(h.history)
	return h.history[i]
}

// Add records a ping that has just been sent. The seq arg must match the next
// sequence number, and panics if it doesn't.
func (h *pingHistory) Add(seq int) {
	if h.lastSeq+1 != seq {
		log.Panicf("Wrong sequence number: %d (want %d)", seq, h.lastSeq+1)
	}
	i := seq % len(h.history)
	h.history[i] = PingResult{
		Type: Waiting,
		Time: h.clock.Now(),
	}
	h.lastSeq = seq
}

// Records sets the result for the given sequence number. Returns the PingResult
// updated with latency.
func (h *pingHistory) Record(seq int, r PingResult) PingResult {
	if h.lastSeq-seq >= len(h.history) {
		log.Printf("Seq %d too late to record in history.", seq)
		return r
	}
	i := seq % len(h.history)
	r.Latency = h.clock.Since(r.Time)
	h.history[i] = r
	if r.Type != Duplicate {
		h.addStatsFor(r)
	}
	return r
}

// Adds stats for a new record.
func (h *pingHistory) addStatsFor(r PingResult) {
	h.stats.N++
	if r.Type != Success {
		h.stats.Failures++
		return
	}
	n := time.Duration(h.stats.N - h.stats.Failures)
	prevAvg := h.stats.AvgLatency
	h.stats.AvgLatency = ((n-1)*h.stats.AvgLatency + r.Latency) / n
	h.m2 = h.m2 + (r.Latency-prevAvg)*(r.Latency-h.stats.AvgLatency)
	h.stats.StdDev = time.Duration(math.Sqrt(float64(h.m2) / float64(h.stats.N)))
}

// RevResults iterates over sequence#, result from newest to oldest.
// Note: This locks the mutex for the lifetime of the iterator.
func (h *pingHistory) RevResults(mu *sync.Mutex) iter.Seq2[int, PingResult] {
	return func(yield func(k int, v PingResult) bool) {
		mu.Lock()
		defer mu.Unlock()
		firstSeq := h.lastSeq - len(h.history) + 1
		if firstSeq < 0 {
			firstSeq = 0
		}
		for seq := h.lastSeq; seq >= firstSeq; seq-- {
			if !yield(seq, h.history[seq%len(h.history)]) {
				return
			}
		}
	}
}

// History returns the ping history.
// Deprecated: Use RevResults() and iterate.
func (h *pingHistory) History(mu *sync.Mutex) []PingResult {
	var res []PingResult
	for _, r := range h.RevResults(mu) {
		res = append(res, r)
	}
	slices.Reverse(res)
	return res
}

// Latest returns the most recent ping result or the zero result if no results
// are available.
func (h *pingHistory) Latest() PingResult {
	if h.lastSeq == -1 {
		return PingResult{}
	}
	return h.history[h.lastSeq%len(h.history)]
}

// Stats returns the current statistics.
func (h *pingHistory) Stats() Stats {
	return h.stats
}
