// Package table implements a table for displaying ping information for a list
// of hosts.
package table

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pcekm/graphping/internal/pinger"

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

type columnID int

// columnID values specified in the order they should appear in the table.
const (
	colIndex columnID = iota
	colHost
	colResults
	colAvgMs
	colJitter
	colPctLoss
)

func (c columnID) String() string {
	switch c {
	case colIndex:
		return "colIndex"
	case colHost:
		return "colHost"
	case colResults:
		return "colResults"
	case colAvgMs:
		return "colAvgMs"
	case colJitter:
		return "colJitter"
	case colPctLoss:
		return "colPctLoss"
	default:
		return fmt.Sprintf("(unknown:%d)", c)
	}
}

// Describes a column.
type columnSpec struct {
	// ID is the column ID.
	ID columnID

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
		{ID: colIndex, Title: "Hop", FixedWidth: 3},
		{ID: colHost, Title: "Host", ProportionalWidth: 2},
		{ID: colResults, Title: "Results", ProportionalWidth: 3},
		{ID: colAvgMs, Title: "AvgMs", FixedWidth: 5},
		{ID: colJitter, Title: "Jitter", FixedWidth: 6},
		{ID: colPctLoss, Title: " Loss", FixedWidth: 5},
	}

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#cccccc")).
			Background(lipgloss.Color("#1F326F")).
			Padding(0, horizontalPadding)
	cellStyle = lipgloss.NewStyle().
			Padding(0, horizontalPadding)

	latencyColors = []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("#3abb46")).Inline(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#6faa1e")).Inline(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#8d9800")).Inline(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#a18400")).Inline(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#ae7006")).Inline(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#b45d21")).Inline(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#b34a34")).Inline(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#ab3c45")).Inline(true),
	}
	statusErrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cccccc")).
			Background(lipgloss.Color("#ab3c45")).
			Inline(true)

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

func (r Row) cells() map[columnID]any {
	st := r.Pinger.Stats()
	return map[columnID]any{
		colIndex:   r.Index,
		colHost:    r.DisplayHost,
		colResults: r.Pinger,
		colAvgMs:   st.AvgLatency,
		colJitter:  st.StdDev,
		colPctLoss: 100 * st.PacketLoss(),
	}
}

func cmpRows(a, b Row) int {
	return cmpRowKeys(a.RowKey, b.RowKey)
}

// RowKey uniquely identifies a row.
type RowKey struct {
	// Group is used to group related pings, such as all the hosts in a path.
	Group string

	// Index is the numeric index of the row.
	Index int
}

func cmpRowKeys(a, b RowKey) int {
	if a.Group < b.Group {
		return -1
	} else if a.Group > b.Group {
		return 1
	}
	return cmp.Compare(a.Index, b.Index)
}

// Model contains the table information.
type Model struct {
	ready     bool
	vp        viewport.Model
	colWidths []int
	rows      []Row
}

// New makes an empty ping result table with headers.
func New() *Model {
	return &Model{
		colWidths: make([]int, len(columnSpecs)),
	}
}

func (t *Model) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var vpCmd tea.Cmd
	t.vp, vpCmd = t.vp.Update(msg)
	cmds = append(cmds, vpCmd)
	return tea.Batch(cmds...)
}

// SetSize sets the table size. It must be called at least once in order for
// anything to be displayed.
func (t *Model) SetSize(width, height int) {
	if !t.ready {
		t.vp = viewport.New(width, height-1)
		t.ready = true
	}
	t.vp.Width = width
	t.vp.Height = height - 1
	t.recalcColumnWidths()
}

func (t *Model) recalcColumnWidths() {
	fixedTot := 0
	propTot := 0.0
	for _, c := range columnSpecs {
		fixedTot += cellStyle.GetHorizontalPadding()
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
	slices.SortStableFunc(t.rows, cmpRows)
	t.updateRows()
}

// UpdateRow updates an existing row.
func (t *Model) UpdateRow(r RowKey) tea.Cmd {
	// TODO: Actually just update one row?
	t.updateRows()
	return nil
}

func (t *Model) updateRows() {
	if !t.ready {
		return
	}
	lines := make([]string, len(t.rows))
	for i, r := range t.rows {
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
	out.WriteString(cellStyle.Width(width + cellStyle.GetHorizontalPadding()).Render(s))
}

func (t *Model) renderLatencies(width int, p *pinger.Pinger) string {
	chars := make([]string, width)
	i := 0
	for _, r := range p.RevResults() {
		frac := math.Min(1, float64(r.Latency)/float64(graphMax))
		barIdx := int(frac * float64(len(bars)-1))
		c := latencyColors[barIdx].Render(bars[barIdx])
		if r.Type != pinger.Success {
			c = statuses[r.Type]
			if r.Type != pinger.Waiting {
				c = statusErrStyle.Render(c)
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
		sb.WriteString(headerStyle.Width(width + 2*horizontalPadding).Render(rpad(width, c.Title)))
	}
	return sb.String()
}

func (t *Model) View() string {
	if !t.ready {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Top, t.headerView(), t.vp.View())
}
