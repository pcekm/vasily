// Package table implements a table for displaying ping information for a list
// of hosts.
package table

import (
	"cmp"
	"fmt"
	"io"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pcekm/graphping/internal/pinger"
	"github.com/pcekm/graphping/internal/tui/help"
	"github.com/pcekm/graphping/internal/tui/nav"
	"github.com/pcekm/graphping/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// Minimum width for columns determined fractionally.
	minColWidth = 10

	// Duration at which a ping latency displays at maximum height.
	graphMax = 250 * time.Millisecond

	horizontalPadding = 1
)

var (
	defaultSort = []SortColumn{
		{ColumnID: ColIndex},
		{ColumnID: ColHost},
	}

	availSortColumns = []ColumnID{ColIndex, ColHost, ColAvgMs, ColJitter, ColPctLoss}
)

// SortColumn identifies a column to sort by.
type SortColumn struct {
	ColumnID
	Reverse bool
}

// ColumnID identifies a column.
type ColumnID int

// ColumnID values specified in the order they should appear in the table.
const (
	ColIndex ColumnID = iota
	ColHost
	ColResults
	ColAvgMs
	ColJitter
	ColPctLoss
)

func (c ColumnID) String() string {
	switch c {
	case ColIndex:
		return "ColIndex"
	case ColHost:
		return "ColHost"
	case ColResults:
		return "ColResults"
	case ColAvgMs:
		return "ColAvgMs"
	case ColJitter:
		return "ColJitter"
	case ColPctLoss:
		return "ColPctLoss"
	default:
		return fmt.Sprintf("(unknown:%d)", c)
	}
}

// Display returns a displayable title for this column.
func (c ColumnID) Display() string {
	spec := columnSpecs[slices.IndexFunc(columnSpecs, func(s columnSpec) bool { return s.ID == c })]
	return strings.TrimSpace(spec.Title)
}

// AvailColumns are the columns available for sorting.
func AvailColumns() []ColumnID {
	return append([]ColumnID{}, availSortColumns...)
}

// Describes a column.
type columnSpec struct {
	// ID is the column ID.
	ID ColumnID

	// Title is the title displayed at the top of the column.
	Title string

	// FixedWidth is fixed width for the column.
	FixedWidth int

	// ProportionalWidth, if nonzero, is the proportion of the available space
	// this column will use. (Minus the fixed width columns.)
	ProportionalWidth float64
}

var (
	columnSpecs = []columnSpec{
		{ID: ColIndex, Title: "Hop", FixedWidth: 3},
		{ID: ColHost, Title: "Host", ProportionalWidth: 2},
		{ID: ColResults, Title: "Results", ProportionalWidth: 3},
		{ID: ColAvgMs, Title: "AvgMs", FixedWidth: 5},
		{ID: ColJitter, Title: "Jitter", FixedWidth: 6},
		{ID: ColPctLoss, Title: " Loss", FixedWidth: 5},
	}

	headerStyleBase = lipgloss.NewStyle().
			Padding(0, horizontalPadding)
	cellStyleBase = lipgloss.NewStyle().
			Padding(0, horizontalPadding)

	bars     = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	statuses = map[pinger.ResultType]string{
		pinger.Waiting:     " ",
		pinger.Dropped:     "?",
		pinger.Duplicate:   "D",
		pinger.TTLExceeded: "T",
		pinger.Unreachable: "X",
	}
)

// Row holds information about pings to a single host.
type Row struct {
	RowKey

	// DisplayHost is the hostname or IP address to display.
	DisplayHost string

	// Pinger is the pinger for this host.
	Pinger *pinger.Pinger
}

func (r Row) cells() map[ColumnID]any {
	st := r.Pinger.Stats()
	return map[ColumnID]any{
		ColIndex:   r.Index,
		ColHost:    r.DisplayHost,
		ColResults: r.Pinger,
		ColAvgMs:   st.AvgLatency,
		ColJitter:  st.StdDev,
		ColPctLoss: 100 * st.PacketLoss(),
	}
}

func (r Row) sortKeys() map[ColumnID]any {
	st := r.Pinger.Stats()
	return map[ColumnID]any{
		ColIndex: r.Index,
		ColHost:  r.DisplayHost,
		// Not sortable:
		// ColResults: r.Pinger,
		ColAvgMs:   st.AvgLatency,
		ColJitter:  st.StdDev,
		ColPctLoss: 100 * st.PacketLoss(),
	}
}

// RowKey uniquely identifies a row.
// TODO: Is this necessary now? Can it be rolled into Row?
type RowKey struct {
	// Group is used to group related pings, such as all the hosts in a path.
	Group string

	// Index is the numeric index of the row.
	Index int
}

// Model contains the table information.
type Model struct {
	theme         *theme.Theme
	ready         bool
	width, height int
	vp            viewport.Model
	colWidths     []int
	rows          []Row
	sortCols      []SortColumn
	help          *help.Model
}

// New makes an empty ping result table with headers.
func New(theme *theme.Theme) *Model {
	return &Model{
		theme:     theme,
		colWidths: make([]int, len(columnSpecs)),
		sortCols:  append([]SortColumn{}, defaultSort...),
		help:      help.New(theme, defaultKeyMap),
	}
}

func (t *Model) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd = t.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		cmd = t.handleWindowSizeMsg(msg)
	}

	var vpCmd tea.Cmd
	t.vp, vpCmd = t.vp.Update(msg)
	return tea.Batch(cmd, vpCmd)
}

func (t *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	// Reset full help display after any keypress.
	origHelp := t.help.FullHelp()
	t.help.SetFullHelp(false)
	t.updateSizes()

	var cmd tea.Cmd
	switch {
	case key.Matches(msg, defaultKeyMap.Quit):
		cmd = tea.Quit
	case key.Matches(msg, defaultKeyMap.Sort):
		cmd = nav.Go(nav.SortSelect)
	case key.Matches(msg, defaultKeyMap.Help):
		t.help.SetFullHelp(!origHelp)
		t.updateSizes()
	}

	return cmd
}

func (t *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	t.width, t.height = msg.Width, msg.Height
	t.updateSizes()
	return nil
}

func (t *Model) updateSizes() {
	t.help.SetWidth(t.width)
	hh := t.help.GetHeight()
	if !t.ready {
		t.vp = viewport.New(t.width, t.height-hh-1)
		t.ready = true
	}
	t.vp.Width = t.width
	t.vp.Height = t.height - hh - 1
	t.recalcColumnWidths()
}

// Sort returns the current sort columns.
func (t *Model) Sort() []SortColumn {
	return append([]SortColumn{}, t.sortCols...)
}

// SetSort sets the columns to sort the table by. Use without args to restore
// the default.
func (t *Model) SetSort(cols ...SortColumn) {
	if len(cols) == 0 {
		t.sortCols = append([]SortColumn{}, defaultSort...)
		return
	}
	t.sortCols = cols
}

func cmpKey(a, b any, reverse bool) (res int) {
	defer func() {
		if reverse {
			res = -res
		}
	}()
	switch a := a.(type) {
	case int:
		b := b.(int)
		return cmp.Compare(a, b)
	case string:
		b := b.(string)
		return cmp.Compare(a, b)
	case time.Duration:
		b := b.(time.Duration)
		return cmp.Compare(a, b)
	case float64:
		b := b.(float64)
		return cmp.Compare(a, b)
	}
	log.Panicf("Unhandled sort key type %T", a)
	return 0
}

func (t *Model) cmpRows(a, b Row) int {
	for _, col := range t.sortCols {
		keyA := a.sortKeys()[col.ColumnID]
		keyB := b.sortKeys()[col.ColumnID]
		if res := cmpKey(keyA, keyB, col.Reverse); res != 0 {
			return res
		}
	}
	return 0
}

func (t *Model) recalcColumnWidths() {
	fixedTot := 0
	propTot := 0.0
	for _, c := range columnSpecs {
		fixedTot += t.cellStyle().GetHorizontalPadding()
		if c.FixedWidth != 0 {
			fixedTot += c.FixedWidth
		} else {
			propTot += c.ProportionalWidth
		}
	}
	avail := float64(t.vp.Width - fixedTot)
	for i, c := range columnSpecs {
		if c.FixedWidth != 0 {
			t.colWidths[i] = c.FixedWidth
		} else {
			t.colWidths[i] = int(math.Round(c.ProportionalWidth / propTot * avail))
		}
	}
}

// AddRow adds a new row.
func (t *Model) AddRow(r Row) {
	t.rows = append(t.rows, r)
	t.UpdateRows()
}

// UpdateRows updates all of the rows in the table with the latest ping data.
func (t *Model) UpdateRows() {
	if !t.ready {
		return
	}
	slices.SortStableFunc(t.rows, t.cmpRows)
	lines := make([]string, len(t.rows))
	for i, r := range t.rows {
		// Collapse index numbers.
		if i > 0 && r.Index == t.rows[i-1].Index {
			r.Index = 0
		}
		lines[i] = t.renderRow(r)
	}
	t.vp.SetContent(strings.Join(lines, "\n"))
}

// Left-pads s out to i spaces. Enough spaces will be added to the left of s to make
// it at least length i.
func lpad(i int, s string) string {
	n := i - len(s)
	if n < 0 {
		return s[:i-1] + "…"
	}
	return strings.Repeat(" ", n) + s
}

// Right-pads s out to i spaces. Enough spaces will be added to the left of s to make
// it at least length i.
func rpad(i int, s string) string {
	n := i - len(s)
	if n < 0 {
		return s[:i-1] + "…"
	}
	return s + strings.Repeat(" ", n)
}

func (t *Model) renderRow(r Row) string {
	cells := r.cells()
	var sb strings.Builder
	for i, c := range columnSpecs {
		// A special case for zero index numbers.
		if c.ID == ColIndex && cells[c.ID] == 0 {
			t.renderCell("", t.colWidths[i], &sb)
			continue
		}
		t.renderCell(cells[c.ID], t.colWidths[i], &sb)
	}
	return sb.String()
}

func (t *Model) renderCell(v any, width int, out io.StringWriter) {
	var s string
	switch v := v.(type) {
	case string:
		s = rpad(width, v)
	case time.Duration:
		s = lpad(width, strconv.FormatInt(v.Milliseconds(), 10))
	case int:
		s = lpad(width, strconv.Itoa(v))
	case float64:
		s = lpad(width, fmt.Sprintf("%.0f%%", v))
	case *pinger.Pinger:
		s = t.renderLatencies(width, v)
	}
	out.WriteString(t.cellStyle().Width(width + t.cellStyle().GetHorizontalPadding()).Render(s))
}

func (t *Model) renderLatencies(width int, p *pinger.Pinger) string {
	chars := slices.Repeat([]string{" "}, width)
	i := 0
	for _, r := range p.RevResults() {
		frac := math.Min(1, float64(r.Latency)/float64(graphMax))
		barIdx := int(frac * float64(len(bars)-1))
		c := t.theme.Text.Normal.
			Foreground(t.theme.Heatmap.At(frac)).
			Render(bars[barIdx])
		if r.Type != pinger.Success {
			c = statuses[r.Type]
			if r.Type != pinger.Waiting {
				c = t.errStyle().Render(c)
			}
		}
		charIdx := width - i - 1
		if charIdx < 0 {
			break
		}
		chars[charIdx] = c
		i++
	}
	return strings.Join(chars, "")
}

func (t *Model) headerView() string {
	var sb strings.Builder
	for i, c := range columnSpecs {
		width := t.colWidths[i]
		sb.WriteString(t.headerStyle().Width(width + 2*horizontalPadding).Render(rpad(width, c.Title)))
	}
	return sb.String()
}

func (t *Model) headerStyle() lipgloss.Style {
	return headerStyleBase.Inherit(t.theme.Text.Important).
		Foreground(t.theme.Colors.OnPrimary).
		Background(t.theme.Colors.Primary)
}

func (t *Model) cellStyle() lipgloss.Style {
	return cellStyleBase.Inherit(t.theme.Text.Normal)
}

func (t *Model) errStyle() lipgloss.Style {
	return t.theme.Text.Normal.
		Foreground(t.theme.Colors.OnError).
		Background(t.theme.Colors.Error)
}

func (t *Model) View() string {
	if !t.ready {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Top, t.headerView(), t.vp.View(), t.help.View())
}
