// Package tui implements the text user interface.
package tui

import (
	"fmt"
	"log"
	"net"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pcekm/graphping/lookup"
	"github.com/pcekm/graphping/ping/connection"
	"github.com/pcekm/graphping/ping/pinger"
	"github.com/pcekm/graphping/ping/tracer"
	"github.com/pcekm/graphping/tui/table"
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
	conn       *connection.PingConn
	hosts      []string
	rowUpdates chan table.UpdateRow
	opts       *Options
}

// New creates a new model.
func New(conn *connection.PingConn, hosts []string, opts *Options) (*Model, error) {
	m := &Model{
		table:      table.New(),
		conn:       conn,
		hosts:      hosts,
		rowUpdates: make(chan table.UpdateRow),
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
	cmds := []tea.Cmd{m.nextRowUpdateCmd()}
	for i, h := range m.hosts {
		addr, err := lookup.String(h)
		if err != nil {
			log.Printf("Error looking up %q: %v", h, err)
		}
		if m.opts.trace() {
			cmds = append(cmds, m.startTraceCmd(addr))
		} else {
			cmds = append(cmds, m.startPingerCmd(i+1, h, addr))
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
		cmds = append(cmds, m.updateKeyMsg(msg))
	case table.UpdateRow:
		cmds = append(cmds, m.nextRowUpdateCmd())
	case traceStepMsg:
		cmds = append(cmds, m.updateTraceStep(msg))
	case error:
		log.Print(msg)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) nextRowUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		return <-m.rowUpdates
	}
}

// Returns a command that starts running a new ping.
// TODO: Dest and target are confusing. idx and dest are for internal
// identification and sorting, target is what actually gets pinged.
func (m *Model) startPingerCmd(idx int, dest string, target net.Addr) tea.Cmd {
	return func() tea.Msg {
		opts := *m.opts.pingerOpts()
		opts.Callback = func(int, pinger.PingResult) {
			m.rowUpdates <- table.UpdateRow{Index: idx, Target: dest}
		}
		ping := pinger.Ping(m.conn, target, &opts)
		go ping.Run()
		return table.Row{
			Index:       idx,
			Target:      dest,
			DisplayHost: lookup.Addr(target),
			Pinger:      ping,
		}
	}
}

func (m *Model) startTraceCmd(addr net.Addr) tea.Cmd {
	ch := make(chan tracer.Step)
	return tea.Batch(
		func() tea.Msg {
			err := tracer.TraceRoute(m.conn, addr, ch)
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
		m.startPingerCmd(msg.step.Pos, msg.host, msg.step.Host),
		m.nextTraceCmd(msg.host, msg.next),
	)
}

func (m *Model) updateKeyMsg(msg tea.KeyMsg) tea.Cmd {
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
