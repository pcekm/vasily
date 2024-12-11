// Package tui implements the text user interface.
package tui

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/pinger"
	"github.com/pcekm/graphping/internal/tracer"
	"github.com/pcekm/graphping/internal/tui/help"
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
	width    int
	height   int
	table    *table.Model
	hosts    []string
	help     *help.Model
	fullHelp bool
	helpRow  int
	helpCol  int
	opts     *Options
}

// New creates a new model.
func New(hosts []string, opts *Options) (*Model, error) {
	m := &Model{
		table: table.New(),
		hosts: hosts,
		help:  help.New(defaultKeyMap),
		opts:  opts,
	}
	return m, nil
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.updateRows(updateRows{}),
		m.help.Init(),
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
	cmds := []tea.Cmd{
		m.table.Update(msg),
		m.help.Update(msg),
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmds = append(cmds, m.handleKeyMsg(msg))
	case tea.WindowSizeMsg:
		cmds = append(cmds, m.handleResize(msg))
	case traceStepMsg:
		cmds = append(cmds, m.updateTraceStep(msg))
	case updateRows:
		cmds = append(cmds, m.updateRows(msg))
	case error:
		cmds = append(cmds, m.handleError(msg))
	}
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
			}
			err := tracer.TraceRoute(m.opts.traceBackend(), util.AddrVersion(addr), addr, ch, opts)
			if err != nil {
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

func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	var cmds []tea.Cmd
	add := func(cmd tea.Cmd) {
		cmds = append(cmds, cmd)
	}
	switch {
	case key.Matches(msg, defaultKeyMap.Quit):
		add(tea.Quit)
	case key.Matches(msg, defaultKeyMap.Suspend):
		add(tea.Suspend)
	}

	// Help is dismissed on any keypress.
	if key.Matches(msg, defaultKeyMap.Help) {
		add(m.setFullHelp(!m.fullHelp))
	} else if m.fullHelp {
		add(m.setFullHelp(false))
	}

	return tea.Batch(cmds...)
}

func (m *Model) setFullHelp(b bool) tea.Cmd {
	m.fullHelp = b
	m.help.SetFullHelp(b)
	m.updateSizes()
	return nil
}

func (m *Model) updateSizes() {
	m.help.SetWidth(m.width)
	helpHeight := m.help.GetHeight()
	m.table.SetSize(m.width, m.height-helpHeight)
}

func (m *Model) handleResize(msg tea.WindowSizeMsg) tea.Cmd {
	m.width = msg.Width
	m.height = msg.Height
	m.updateSizes()
	return nil
}

// View renders the model.
func (m *Model) View() string {
	return lipgloss.JoinVertical(lipgloss.Top, m.table.View(), m.help.View())
}
