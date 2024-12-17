// Package tui implements the text user interface.
package tui

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/pinger"
	"github.com/pcekm/graphping/internal/tracer"
	"github.com/pcekm/graphping/internal/tui/nav"
	"github.com/pcekm/graphping/internal/tui/sortselect"
	"github.com/pcekm/graphping/internal/tui/table"
	"github.com/pcekm/graphping/internal/util"
)

// Options contain main program options.
type Options struct {
	// Trace activates traceroute mode. Traces the path to each host and pings
	// each step in the path.
	Trace bool

	// PingInterval is the interval that pings are sent.
	PingInterval time.Duration

	// PingBackend is the backend to use for pings.
	PingBackend backend.Name

	// TraceInterval is the interval between route trace probes.
	TraceInterval time.Duration

	// TraceBackend is the backend to use for traces.
	TraceBackend backend.Name

	// TraceMaxTTL is the maximum ttl to trace.
	TraceMaxTTL int

	// ProbesPerHop is the number of times to probe for responses at each ttl.
	ProbesPerHop int
}

func (o *Options) trace() bool {
	return o != nil && o.Trace
}

func (o *Options) pingInterval() time.Duration {
	if o == nil || o.PingInterval == 0 {
		return time.Second
	}
	return o.PingInterval
}

func (o *Options) pingBackend() backend.Name {
	if o == nil || o.PingBackend == "" {
		return backend.Name("icmp")
	}
	return o.PingBackend
}

func (o *Options) traceInterval() time.Duration {
	if o == nil || o.TraceInterval == 0 {
		return time.Second
	}
	return o.TraceInterval
}

func (o *Options) traceBackend() backend.Name {
	if o == nil || o.TraceBackend == "" {
		return backend.Name("udp")
	}
	return o.TraceBackend
}

func (o *Options) traceMaxTTL() int {
	if o == nil {
		return 0
	}
	return o.TraceMaxTTL
}

func (o *Options) probesPerHop() int {
	if o == nil || o.ProbesPerHop == 0 {
		return 3
	}
	return o.ProbesPerHop
}

type updateRows struct{}

type traceStepMsg struct {
	step tracer.Step
	host string
	next <-chan tracer.Step
}

// Model is the main text UI model.
type Model struct {
	focus nav.Screen
	table *table.Model
	sort  *sortselect.Model
	hosts []string
	opts  *Options
}

// New creates a new model.
func New(hosts []string, opts *Options) (*Model, error) {
	tbl := table.New()
	m := &Model{
		focus: nav.Main,
		table: tbl,
		sort:  sortselect.New(tbl),
		hosts: hosts,
		opts:  opts,
	}
	return m, nil
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.updateRows(updateRows{}),
		m.sort.Init(),
	}
	for _, h := range m.hosts {
		addr, err := lookup.String(h)
		if err != nil {
			log.Printf("Error looking up %q: %v", h, err)
		}
		if m.opts.trace() {
			cmds = append(cmds, m.startTraceCmd(addr))
		} else {
			cmds = append(cmds, m.startPingerCmd(table.RowKey{Group: h}, addr))
		}
	}
	return tea.Batch(cmds...)
}

// Update process an update message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case traceStepMsg:
		cmd = m.updateTraceStep(msg)
	case updateRows:
		cmd = m.updateRows(msg)
	case tea.KeyMsg:
		// Key messages are conditionally passed on by handleKeyMsg, so return
		// here instead of unconditionally passing them on below.
		return m, m.handleKeyMsg(msg)
	case nav.GoMsg:
		m.focus = msg.Screen
	case error:
		cmd = m.handleError(msg)
	}

	cmds := append([]tea.Cmd{cmd},
		m.table.Update(msg),
		m.sort.Update(msg),
	)
	return m, tea.Batch(cmds...)
}

func (m *Model) handleError(err error) tea.Cmd {
	log.Panic(err)
	return nil
}

// Returns a command that starts running a new ping.
func (m *Model) startPingerCmd(key table.RowKey, target net.Addr) tea.Cmd {
	return func() tea.Msg {
		ping, err := pinger.New(m.opts.pingBackend(), util.AddrVersion(target), target, &pinger.Options{
			Interval: m.opts.pingInterval(),
		})
		if err != nil {
			return err
		}
		go ping.Run()
		m.table.AddRow(
			table.Row{
				RowKey:      key,
				DisplayHost: lookup.Addr(target),
				Pinger:      ping,
			})
		return nil
	}
}

func (m *Model) startTraceCmd(addr net.Addr) tea.Cmd {
	ch := make(chan tracer.Step)
	return tea.Batch(
		func() tea.Msg {
			opts := &tracer.Options{
				Interval:     m.opts.traceInterval(),
				ProbesPerHop: m.opts.probesPerHop(),
				MaxTTL:       m.opts.traceMaxTTL(),
			}
			err := tracer.TraceRoute(m.opts.traceBackend(), util.AddrVersion(addr), addr, ch, opts)
			if err != nil {
				if errors.Is(err, tracer.ErrMaxTTL) {
					log.Printf("Maximum TTL reached for %v", addr)
					return nil
				}
				return fmt.Errorf("traceroute: %v: %v", addr, err)
			}
			return nil
		},
		m.nextTraceCmd(addr.String(), ch),
	)
}

func (m *Model) nextTraceCmd(dest string, ch <-chan tracer.Step) tea.Cmd {
	return func() tea.Msg {
		step, ok := <-ch
		if !ok {
			return nil
		}
		return traceStepMsg{
			step: step,
			host: dest,
			next: ch,
		}
	}
}

func (m *Model) updateTraceStep(msg traceStepMsg) tea.Cmd {
	tea.Batch()
	return tea.Batch(
		m.startPingerCmd(table.RowKey{Index: msg.step.Pos, Group: msg.host}, msg.step.Host),
		m.nextTraceCmd(msg.host, msg.next),
	)
}

func (m *Model) updateRows(updateRows) tea.Cmd {
	m.table.UpdateRows()
	return tea.Tick(m.opts.pingInterval(), func(time.Time) tea.Msg {
		return updateRows{}
	})
}

// Global key definitions. These apply to everything everywhere all the time.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	var cmds []tea.Cmd
	add := func(cmd tea.Cmd) {
		cmds = append(cmds, cmd)
	}

	switch m.focus {
	case nav.Main:
		add(m.table.Update(msg))
	case nav.SortSelect:
		add(m.sort.Update(msg))
	}

	switch msg.String() {
	case "ctrl+c":
		add(tea.Quit)
	case "ctrl+z":
		add(tea.Suspend)
	}

	return tea.Batch(cmds...)
}

// View renders the model.
func (m *Model) View() string {
	switch m.focus {
	case nav.Main:
		return m.table.View()
	case nav.SortSelect:
		return m.sort.View()
	default:
		log.Panicf("Unhandled focus: %v", m.focus)
	}
	return ""
}
