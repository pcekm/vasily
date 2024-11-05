// Package display handles the ncurses display.
package display

// #include <locale.h>
import "C"

import (
	"fmt"
	"sync"
	"time"

	"github.com/pcekm/graphping/pinger"
	gc "github.com/rthornton128/goncurses"
)

const (
	// Duration at which a ping latency displays at maximum height.
	graphMax = 250 * time.Millisecond

	hostCols      = 15
	bodyStartLine = 1
	latencySpace  = 16
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
	d.scr.MovePrint(0, hostCols, "Ping Latencies")
}

// Cleans up the display.
func (d *D) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	gc.End()
}

// AddHost adds a host line. Returns the index of the added host.
func (d *D) AddHost(host string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := len(d.hosts)
	_, w := d.scr.MaxYX()
	win, err := gc.NewWindow(1, w-hostCols+1, bodyStartLine+idx, hostCols)
	if err != nil {
		return -1, fmt.Errorf("error creating window: %v", err)
	}
	d.hosts = append(d.hosts, win)
	d.scr.MovePrint(bodyStartLine+idx, 0, host)
	return idx, nil
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
	c := bars[int(frac*float32(len(bars)))]
	if rt != pinger.EchoReply {
		c = statuses[rt]
	}
	win.MovePrintf(0, w-latencySpace-1, "%3d/%3d ms %3.0f%%", cur.Milliseconds(), avg.Milliseconds(), rate*100)
	win.MovePrint(y, x, c)
	win.NoutRefresh()
}
