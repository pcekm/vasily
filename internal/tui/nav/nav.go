// Package nav contains navigation-related commands.
package nav

import tea "github.com/charmbracelet/bubbletea"

// Screen is a screen that may be navigated to.
type Screen int

// Screens.
const (
	_ Screen = iota
	Main
	SortSelect
)

// GoMsg is a message to go to a given model.
type GoMsg struct {
	Screen
}

// Go returns a command to go to a given mode.
func Go(s Screen) tea.Cmd {
	return func() tea.Msg {
		return GoMsg{Screen: s}
	}
}
