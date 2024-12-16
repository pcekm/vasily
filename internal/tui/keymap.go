package tui

import "github.com/charmbracelet/bubbles/key"

var defaultKeyMap = keyMap{
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sorting"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Suspend: key.NewBinding(
		key.WithKeys("ctrl+z"),
		key.WithHelp("ctrl+z", "suspend"),
	),
	Help: key.NewBinding(
		key.WithKeys("f1", "?"),
		key.WithHelp("?", "help"),
	),
}

type keyMap struct {
	Sort    key.Binding
	Toggle  key.Binding
	Quit    key.Binding
	Suspend key.Binding
	Help    key.Binding
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Sort, k.Help, k.Quit}}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Help, k.Quit,
	}
}
