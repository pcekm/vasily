// Package tui implements the text user interface.
package tui

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/lookup"
	"github.com/pcekm/graphping/internal/pinger"
	"github.com/pcekm/graphping/internal/tracer"
	"github.com/pcekm/graphping/internal/tui/help"
	"github.com/pcekm/graphping/internal/tui/logwindow"
	"github.com/pcekm/graphping/internal/tui/table"
)

const logHeight = 10

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

type updateRow struct {
	table.RowKey
}

type traceStepMsg struct {
	step tracer.Step
	host string
	next <-chan tracer.Step
}

// Model is the main text UI model.
type Model struct {
	width      int
	height     int
	table      *table.Model
	connV4     backend.NewConn
	connV6     backend.NewConn
	hosts      []string
	rowUpdates chan table.RowKey
	log        *logwindow.Model
	showLog    bool
	help       *help.Model
	fullHelp   bool
	helpRow    int
	helpCol    int
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
		log:        logwindow.New(),
		help:       help.New(defaultKeyMap),
		opts:       opts,
	}
	return m, nil
}

func (m *Model) quitNicely() tea.Cmd {
	return func() tea.Msg {
		log.SetOutput(os.Stderr)
		return tea.QuitMsg{}
	}
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	log.SetOutput(m.log)
	cmds := []tea.Cmd{
		func() tea.Msg { return updateRow{RowKey: <-m.rowUpdates} },
		m.log.Init(),
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
		m.log.Update(msg),
		m.help.Update(msg),
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmds = append(cmds, m.handleKeyMsg(msg))
	case tea.WindowSizeMsg:
		cmds = append(cmds, m.handleResize(msg))
	case updateRow:
		cmds = append(cmds, m.handleUpdateRow(msg))
	case traceStepMsg:
		cmds = append(cmds, m.updateTraceStep(msg))
	case error:
		log.Print(msg)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) handleUpdateRow(k updateRow) tea.Cmd {
	m.table.UpdateRow(k.RowKey)
	return func() tea.Msg {
		return updateRow{RowKey: <-m.rowUpdates}
	}
}

func (m *Model) connFuncForAddr(addr net.Addr) backend.NewConn {
	var ip net.IP
	switch addr := addr.(type) {
	case *net.IPAddr:
		ip = addr.IP
	case *net.UDPAddr:
		ip = addr.IP
	case *net.TCPAddr:
		ip = addr.IP
	default:
		log.Panicf("Wrong address type: %#v", addr)
	}
	if ip.To4() == nil {
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
	var cmds []tea.Cmd
	add := func(cmd tea.Cmd) {
		cmds = append(cmds, cmd)
	}
	switch {
	case key.Matches(msg, defaultKeyMap.Quit):
		add(m.quitNicely())
	case key.Matches(msg, defaultKeyMap.Suspend):
		add(tea.Suspend)
	case key.Matches(msg, defaultKeyMap.Log):
		add(m.toggleLog())
	}

	// Help is dismissed on any keypress.
	if key.Matches(msg, defaultKeyMap.Help) {
		add(m.setFullHelp(!m.fullHelp))
	} else if m.fullHelp {
		add(m.setFullHelp(false))
	}

	return tea.Batch(cmds...)
}

func (m *Model) toggleLog() tea.Cmd {
	m.showLog = !m.showLog
	m.updateSizes()
	return nil
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
	if m.showLog {
		m.table.SetSize(m.width, m.height-logHeight-helpHeight)
		m.log.SetSize(m.width, logHeight)
	} else {
		m.table.SetSize(m.width, m.height-helpHeight)
	}
}

func (m *Model) handleResize(msg tea.WindowSizeMsg) tea.Cmd {
	m.width = msg.Width
	m.height = msg.Height
	m.updateSizes()
	return nil
}

// View renders the model.
func (m *Model) View() string {
	var res []string
	res = append(res, m.table.View())
	if m.showLog {
		res = append(res, m.log.View())
	}
	res = append(res, m.help.View())
	return lipgloss.JoinVertical(lipgloss.Top, res...)
}
