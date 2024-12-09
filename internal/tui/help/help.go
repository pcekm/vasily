// Package help displays a help screen.
package help

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	shortBoxStyle = lipgloss.NewStyle().
			Padding(0, 1)
	fullBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true).
			Padding(0, 1)
)

type Model struct {
	keyMap  help.KeyMap
	keyHelp help.Model
	width   int
}

func New(km help.KeyMap) *Model {
	m := &Model{
		keyMap:  km,
		keyHelp: help.New(),
	}
	m.keyHelp.Styles.FullKey = m.keyHelp.Styles.FullKey.
		Bold(true).
		Foreground(lipgloss.Color("#cccccc"))
	m.keyHelp.Styles.FullDesc = m.keyHelp.Styles.FullDesc.
		Bold(false).
		Foreground(lipgloss.Color("#aaaaaa"))
	return m
}

// SetFullHelp switches between the small and the full help displays.
func (m *Model) SetFullHelp(b bool) {
	m.keyHelp.ShowAll = !m.keyHelp.ShowAll
}

// GetHeight returns the natural height of the help display.
func (m *Model) GetHeight() int {
	return lipgloss.Height(m.keyHelp.View(m.keyMap)) + m.style().GetVerticalFrameSize()
}

// Sets the width of the display.
func (m *Model) SetWidth(width int) {
	m.width = width
	m.keyHelp.Width = width - m.style().GetHorizontalFrameSize()
}

func (m *Model) style() lipgloss.Style {
	if m.keyHelp.ShowAll {
		return fullBoxStyle
	}
	return shortBoxStyle
}

// Init is run in the parent's Init(). (Currently a no-op.)
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update is run in the parent's Update(). (Currently a no-op.)
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	return nil
}

// View returns the help view.
func (m *Model) View() string {
	style := m.style()
	return style.
		Width(m.width - style.GetHorizontalBorderSize()).
		Render(m.keyHelp.View(m.keyMap))
}
