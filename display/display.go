// Package display handles the ncurses display.
package display

// #include <ncurses.h>
// #include <locale.h>
import "C"

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/pcekm/graphping/pinger"
	gc "github.com/rthornton128/goncurses"
)

const (
	// Duration at which a ping latency displays at maximum height.
	graphMax = 250 * time.Millisecond

	hostCols      = 30
	bodyStartLine = 1
	latencySpace  = 17
)

var (
	bars     = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	statuses = map[pinger.ReplyType]string{
		pinger.Dropped:     "?",
		pinger.TTLExceeded: "!",
		pinger.Unreachable: "#",
		pinger.Unknown:     "*",
	}
)

// D contains the display state and performs all ncurses calls.
type D struct {
	mu    sync.Mutex
	scr   *gc.Window
	hosts []*gc.Window
}

// New initializes the display.
func New() (*D, error) {
	// Enables UTF-8 in goncurses.
	C.setlocale(C.LC_ALL, C.CString(""))

	scr, err := gc.Init()
	if err != nil {
		return nil, fmt.Errorf("error initing ncurses: %v", err)
	}

	scr.Clear()
	gc.Echo(false)
	gc.CBreak(true)
	gc.Cursor(0)

	d := &D{
		scr: scr,
	}
	d.DrawHeader()
	d.Update()

	return d, nil
}

// Cleans up the display.
func (d *D) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	gc.End()
}

// TempClose temporarily saves and closes the display in order to drop to the
// terminal. Returns a function that restores the display. All display
// operations will be blocked until the restore function is called.
func (d *D) TempClose() func() {
	d.mu.Lock()
	// These are not implemented in goncurses.
	C.def_prog_mode()
	C.endwin()
	return func() {
		C.reset_prog_mode()
		d.scr.Refresh()
		d.mu.Unlock()
	}
}

// Update updates the screen after making changes.
func (d *D) Update() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scr.Refresh()
}

// DrawHeader draws (or redraws) the header line.
func (d *D) DrawHeader() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.scr.MovePrint(0, 0, "Host")
	d.scr.MovePrint(0, hostCols+1, "Ping Latencies")
}

// Truncates s to fit in n chars.
func maybeTruncateStr(s string, n int) string {
	if len(s) >= n {
		return s[:n-1] + "…"
	}
	return s
}

// AppendHost adds a host line. Returns the index of the added host.
func (d *D) AppendHost(host string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.hosts = append(d.hosts, nil)
	i := len(d.hosts) - 1
	if err := d.setHost(i, host); err != nil {
		return -1, err
	}
	return i, nil
}

// AddHostAt adds a host line at the given index. The index does not have to
// exist and there may be gaps.
func (d *D) AddHostAt(i int, host string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.setHost(i, host)
}

// Adds a host at i; panics if d.hosts does not contain that index. Callers must
// hold d.mu.
func (d *D) setHost(i int, host string) error {
	if i >= len(d.hosts) {
		d.hosts = slices.Grow(d.hosts, i+1)
		d.hosts = d.hosts[:cap(d.hosts)]
	}
	_, w := d.scr.MaxYX()
	win, err := gc.NewWindow(1, w-hostCols+1, bodyStartLine+i, hostCols+1)
	if err != nil {
		return fmt.Errorf("error creating window: %v", err)
	}
	d.hosts[i] = win
	d.scr.MovePrint(bodyStartLine+i, 0, maybeTruncateStr(host, hostCols))
	d.scr.NoutRefresh()
	return nil
}

// UpdateHost adds new information to a host line.
func (d *D) UpdateHost(hostIndex int, rt pinger.ReplyType, rate float64, cur, avg time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	frac := float32(cur) / float32(graphMax)
	if frac > 1 {
		frac = 1
	}
	win := d.hosts[hostIndex]
	_, w := win.MaxYX()
	y, x := win.CursorYX()
	if x >= w-1-latencySpace {
		win.MoveDelChar(0, 0)
		y = 0
		x = w - 2 - latencySpace
	}
	c := bars[int(frac*float32(len(bars)-1))]
	if rt != pinger.EchoReply {
		c = statuses[rt]
	}
	win.MovePrintf(0, w-latencySpace-1, " %3d/%3d ms %3.0f%%", cur.Milliseconds(), avg.Milliseconds(), rate*100)
	win.MovePrint(y, x, c)
	win.NoutRefresh()
}
