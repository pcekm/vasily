package tui

import "github.com/charmbracelet/bubbles/key"

var defaultKeyMap = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Suspend: key.NewBinding(
		key.WithKeys("ctrl+z"),
		key.WithHelp("ctrl+z", "suspend"),
	),
	Log: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "toggle log"),
	),
	Help: key.NewBinding(
		key.WithKeys("f1", "h"),
		key.WithHelp("h", "help"),
	),
}

type keyMap struct {
	Quit    key.Binding
	Suspend key.Binding
	Log     key.Binding
	Help    key.Binding
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Help, k.Log, k.Quit}}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Help, k.Quit,
	}
}
