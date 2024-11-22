// Package tui implements the text user interface.
package tui

import (
	"fmt"
	"log"
	"net"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/pinger"
	"github.com/pcekm/graphping/internal/tracer"
	"github.com/pcekm/graphping/internal/tui/table"
)

// Options contain main program options.
type Options struct {
	// Trace activates traceroute mode. Traces the path to each host and pings
	// each step in the path.
	Trace bool

	// PingerOpts holds options for creating new pingers.
	PingerOpts *pinger.Options
}

func (o *Options) trace() bool {
	return o != nil && o.Trace
}

func (o *Options) pingerOpts() *pinger.Options {
	if o == nil || o.PingerOpts == nil {
		return &pinger.Options{}
	}
	return o.PingerOpts
}

type traceStepMsg struct {
	step tracer.Step
	host string
	next <-chan tracer.Step
}

// Model is the main text UI model.
type Model struct {
	table      *table.Model
	connV4     backend.NewConn
	connV6     backend.NewConn
	hosts      []string
	rowUpdates chan table.RowKey
	opts       *Options
}

// New creates a new model.
func New(connV4, connV6 backend.NewConn, hosts []string, opts *Options) (*Model, error) {
	m := &Model{
		table:      table.New(),
		connV4:     connV4,
		connV6:     connV6,
		hosts:      hosts,
		rowUpdates: make(chan table.RowKey),
		opts:       opts,
	}
	return m, nil
}

// Close shuts down the model.
func (m *Model) Close() error {
	close(m.rowUpdates)
	return nil
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.readNextRow()}
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
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmds = append(cmds, m.handleKeyMsg(msg))
	case table.RowUpdated:
		cmds = append(cmds, m.readNextRow())
	case traceStepMsg:
		cmds = append(cmds, m.updateTraceStep(msg))
	case error:
		log.Print(msg)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) readNextRow() tea.Cmd {
	return func() tea.Msg {
		return table.RowUpdated{Key: <-m.rowUpdates}
	}
}

func (m *Model) connFuncForAddr(addr net.Addr) backend.NewConn {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		// This should never happen.
		log.Panicf("Wrong address type: %#v", addr)
	}
	if udpAddr.IP.To4() == nil {
		return m.connV6
	}
	return m.connV4
}

// Returns a command that starts running a new ping.
func (m *Model) startPingerCmd(key table.RowKey, target net.Addr) tea.Cmd {
	return func() tea.Msg {
		opts := *m.opts.pingerOpts()
		opts.Callback = func(int, pinger.PingResult) {
			m.rowUpdates <- key
		}
		ping, err := pinger.New(m.connFuncForAddr(target), target, &opts)
		if err != nil {
			return err
		}
		go ping.Run()
		return table.AddRow{
			Row: table.Row{
				RowKey:      key,
				DisplayHost: lookup.Addr(target),
				Pinger:      ping,
			},
		}
	}
}

func (m *Model) startTraceCmd(addr net.Addr) tea.Cmd {
	ch := make(chan tracer.Step)
	return tea.Batch(
		func() tea.Msg {
			err := tracer.TraceRoute(m.connFuncForAddr(addr), addr, ch)
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

func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "q":
		return tea.Quit
	case "ctrl+z":
		return tea.Suspend
	}
	return nil
}

// View renders the model.
func (m *Model) View() string {
	return m.table.View()
}
