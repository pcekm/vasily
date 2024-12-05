// Package logwindow collects and displays log messages.
package logwindow

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type logMessage struct {
	b []byte
}

// Model collects and displays log messages. To use, set this as the [log]
// library's output with
//
//	log.SetOutput(model)
//
// and hook up its Init, Update and View methods.
type Model struct {
	ready    bool
	vp       viewport.Model
	messages chan []byte
	content  strings.Builder
}

// New creates a new log handler.
func New() *Model {
	return &Model{
		messages: make(chan []byte),
	}
}

// Write receives a new log message from the logger.
func (l *Model) Write(b []byte) (int, error) {
	l.messages <- b
	return len(b), nil
}

// SetSize sets the size of the logging window.
func (l *Model) SetSize(width, height int) {
	if !l.ready {
		l.vp = viewport.New(width, height)
		l.vp.Style = lipgloss.NewStyle().
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			Padding(0, 1)
		l.ready = true
	}
	l.vp.Width = width
	l.vp.Height = height
}

func (l *Model) recvMessage() tea.Cmd {
	return func() tea.Msg {
		return logMessage{b: <-l.messages}
	}
}

// Init starts log handling with bubbletea.
func (l *Model) Init() tea.Cmd {
	return tea.Batch(l.vp.Init(), l.recvMessage())
}

// Update updates the log messages.
func (l *Model) Update(msg tea.Msg) tea.Cmd {
	var vpCmd tea.Cmd
	l.vp, vpCmd = l.vp.Update(msg)
	cmds := []tea.Cmd{vpCmd}
	switch msg := msg.(type) {
	case logMessage:
		l.content.Write(msg.b)
		l.vp.SetContent(l.content.String())
		l.vp.GotoBottom()
		l.vp.LineUp(1)
		cmds = append(cmds, l.recvMessage())
	}
	return tea.Batch(cmds...)
}

// View returns the log display.
func (l *Model) View() string {
	return l.vp.View()
}
