package sortselect

import (
	"fmt"
	"io"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pcekm/graphping/internal/tui/help"
	"github.com/pcekm/graphping/internal/tui/table"
)

var (
	normalStyle  = lipgloss.NewStyle()
	focusedStyle = normalStyle.
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)
)

type keyMap struct {
	list.KeyMap
	Toggle  key.Binding
	Accept  key.Binding
	Esc     key.Binding
	Reverse key.Binding
}

func (k *keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Accept, k.Esc, k.ShowFullHelp}
}

func (k *keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.CursorUp, k.CursorDown, k.NextPage, k.PrevPage, k.GoToStart, k.GoToEnd},
		{k.Toggle, k.Reverse, k.Accept, k.Esc, k.CloseFullHelp}}
}

var defaultKeyMap = keyMap{
	KeyMap: list.DefaultKeyMap(),
	Toggle: key.NewBinding(
		key.WithKeys("x", " "),
		key.WithHelp("x/space", "toggle"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "accept"),
	),
	Esc: key.NewBinding(
		key.WithKeys("esc", "q"),
		key.WithHelp("esc/q", "cancel"),
	),
	Reverse: key.NewBinding(
		key.WithKeys("-", "r"),
		key.WithHelp("-/r", "reverse"),
	),
}

type listItem struct {
	Col table.SortColumn
	sel int
}

func (i listItem) Title() string {
	return i.Col.Display()
}

func (i listItem) Selected() int {
	return i.sel
}

func (i *listItem) SetSelected(s int) {
	i.sel = s
}

func (i listItem) Reversed() bool {
	return i.Col.Reverse
}

func (i *listItem) SetReversed(b bool) {
	i.Col.Reverse = b
}

func (i listItem) FilterValue() string {
	return i.Col.Display()
}

type delegate struct{}

func (d delegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it := item.(*listItem)
	style := normalStyle
	if m.Index() == index {
		style = focusedStyle
	}
	sel := " "
	if it.Selected() > 0 {
		sel = fmt.Sprint(it.Selected())
	}
	rev := " "
	if it.Selected() > 0 {
		rev = "▲"
		if it.Reversed() {
			rev = "▼"
		}
	}
	fmt.Fprintf(w, style.Render("[%s] %s %s"), sel, rev, it.Title())
}

func (d delegate) Height() int {
	return 1
}

func (d delegate) Spacing() int {
	return 0
}

func (d delegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

// Done is a message generated when the user accepts the newly selected sort
// columns.
type Done struct {
	Columns []table.SortColumn
}

// Cancel is a message generated when the user cancels sort selection.
type Cancel struct{}

// Model gets the user to select sort columns.
type Model struct {
	list          list.Model
	help          *help.Model
	width, height int
	nSelected     int
}

// New creates a new Model.
func New(curSelected []table.SortColumn) *Model {
	var items []list.Item
	for _, col := range table.AvailColumns() {
		j := slices.IndexFunc(curSelected, func(c table.SortColumn) bool { return c.ColumnID == col })
		if j >= 0 {
			items = append(items, &listItem{Col: curSelected[j], sel: j + 1})
		} else {
			items = append(items, &listItem{Col: table.SortColumn{ColumnID: col}})
		}
	}

	delegate := delegate{}
	lst := list.New(items, delegate, 0, 0)
	lst.DisableQuitKeybindings()
	lst.SetFilteringEnabled(false)
	lst.SetShowStatusBar(false)
	lst.SetShowHelp(false)

	return &Model{
		list:      lst,
		help:      help.New(&defaultKeyMap),
		nSelected: len(curSelected),
	}
}

func (s *Model) Init() tea.Cmd {
	return nil
}

func (s *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(msg.Width, msg.Height)
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, defaultKeyMap.ShowFullHelp):
			s.help.SetFullHelp(true)
			s.updateSizes()
			return nil
		case key.Matches(msg, defaultKeyMap.CloseFullHelp):
			s.help.SetFullHelp(false)
			s.updateSizes()
			return nil
		case key.Matches(msg, defaultKeyMap.Reverse):
			return s.handleReverse()
		case key.Matches(msg, defaultKeyMap.Toggle):
			return s.handleKeyToggle()
		case key.Matches(msg, defaultKeyMap.Accept):
			return s.handleKeyAccept()
		case key.Matches(msg, defaultKeyMap.Esc):
			return func() tea.Msg { return Cancel{} }
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return cmd
}

func (s *Model) handleReverse() tea.Cmd {
	item := s.list.SelectedItem().(*listItem)
	item.SetReversed(!item.Reversed())
	return nil
}

func (s *Model) handleKeyToggle() tea.Cmd {
	item := s.list.SelectedItem().(*listItem)
	if item.Selected() == 0 {
		s.nSelected++
		item.SetSelected(s.nSelected)
	} else {
		for _, it := range s.list.Items() {
			it := it.(*listItem)
			if it.Selected() > item.Selected() {
				it.SetSelected(it.Selected() - 1)
			}
		}
		s.nSelected--
		item.SetSelected(0)
	}
	return nil
}

func (s *Model) handleKeyAccept() tea.Cmd {
	res := Done{}
	for _, item := range s.list.Items() {
		if item := item.(*listItem); item.Selected() > 0 {
			i := item.Selected() - 1
			res.Columns = slices.Grow(res.Columns, i+1)
			res.Columns = res.Columns[:i+1]
			res.Columns[i] = item.Col
		}
	}
	return func() tea.Msg { return res }
}

func (s *Model) resize(width, height int) {
	s.width = width
	s.height = height
	s.updateSizes()
}

func (s *Model) updateSizes() {
	s.help.SetWidth(s.width)
	hh := s.help.GetHeight()
	s.list.SetSize(s.width, s.height-hh)
}

func (s *Model) View() string {
	return lipgloss.JoinVertical(lipgloss.Top, s.list.View(), s.help.View())
}
