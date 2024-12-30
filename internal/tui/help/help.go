// Package help displays a help screen.
package help

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pcekm/vasily/internal/tui/theme"
)

type Model struct {
	keyMap  help.KeyMap
	keyHelp help.Model
	theme   *theme.Theme
	width   int
}

func New(theme *theme.Theme, km help.KeyMap) *Model {
	m := &Model{
		keyMap:  km,
		keyHelp: help.New(),
		theme:   theme,
	}
	m.keyHelp.Styles.Ellipsis = theme.Text.Unimportant.
		Inherit(m.keyHelp.Styles.Ellipsis)

	m.keyHelp.Styles.FullKey = theme.Text.Important.
		Inherit(m.keyHelp.Styles.FullKey)
	m.keyHelp.Styles.FullDesc = theme.Text.Normal.
		Inherit(m.keyHelp.Styles.FullDesc)
	m.keyHelp.Styles.FullSeparator = theme.Text.Unimportant.
		Inherit(m.keyHelp.Styles.FullSeparator)

	m.keyHelp.Styles.ShortKey = theme.Text.Normal.
		Inherit(m.keyHelp.Styles.ShortKey)
	m.keyHelp.Styles.ShortDesc = theme.Text.Unimportant.
		Inherit(m.keyHelp.Styles.ShortDesc)
	m.keyHelp.Styles.ShortSeparator = theme.Text.Unimportant.
		Inherit(m.keyHelp.Styles.ShortSeparator)
	return m
}

// FullHelp determines if the full help is displayed.
func (m *Model) FullHelp() bool {
	return m.keyHelp.ShowAll
}

// SetFullHelp switches between the small and the full help displays.
func (m *Model) SetFullHelp(b bool) {
	m.keyHelp.ShowAll = b
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
		return m.theme.Base.
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(m.theme.Colors.OnSurfaceVariant).
			Padding(0, 1)
	}
	return m.theme.Base.
		Padding(0, 1).
		AlignHorizontal(lipgloss.Right)
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
