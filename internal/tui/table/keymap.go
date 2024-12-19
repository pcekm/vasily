package table

import "github.com/charmbracelet/bubbles/key"

var defaultKeyMap = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	PgUp: key.NewBinding(
		key.WithKeys("pgup", "left", "h"),
		key.WithHelp("↑/h/pgup", "prev page"),
	),
	PgDn: key.NewBinding(
		key.WithKeys("pgdn", "right", "l"),
		key.WithHelp("→/l/pgdn", "next page"),
	),
	Home: key.NewBinding(
		key.WithKeys("home", "g"),
		key.WithHelp("g/home", "go to start"),
	),
	End: key.NewBinding(
		key.WithKeys("end", "G"),
		key.WithHelp("G/end", "go to end"),
	),
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sorting"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	// TODO: Should this be a global keymap?
	Help: key.NewBinding(
		key.WithKeys("f1", "?"),
		key.WithHelp("?", "help"),
	),
}

type keyMap struct {
	Up   key.Binding
	Down key.Binding
	PgUp key.Binding
	PgDn key.Binding
	Home key.Binding
	End  key.Binding
	Sort key.Binding
	Quit key.Binding
	Help key.Binding
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PgUp, k.PgDn, k.Home, k.End},
		{k.Sort, k.Help, k.Quit},
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Help, k.Quit,
	}
}
