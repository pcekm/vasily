// Package table implements a table for displaying ping information for a list
// of hosts.
package table

import (
	"cmp"
	"fmt"
	"math"
	"slices"
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
	// Title is the title displayed at the top of the column.
	Title string

	// Width may be int or float64. If int, the column will be exactly
	// that wide. If float64, the column will take up that fraction of the
	// remaining space on the line. The fractions should probably add to 1.0.
	Width any
}

var (
	columnSpecs = map[columnID]columnSpec{
		colIndex:   {Title: "Hop", Width: 3},
		colHost:    {Title: "Host", Width: 1.0 / 3.0},
		colResults: {Title: "Results", Width: 2.0 / 3.0},
		colAvgMs:   {Title: "AvgMs", Width: 5},
		colJitter:  {Title: "Jitter", Width: 6},
		colPctLoss: {Title: " Loss", Width: 5},
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

// View returns views for each cell in the table row.
func (r Row) View(cols []columnSpec, chartWidth int) string {
	st := r.Pinger.Stats()
	idx := "-"
	if r.Index != 0 {
		idx = fmt.Sprintf("%2d.", r.Index)
	}
	render := func(i int, s string) string {
		width := cols[i].Width.(int)
		if columnID(i) == colResults {
		}
		if lipgloss.Width(s) > width {
			s = s[:width-1] + "…"
		}
		return cellStyle.
			Width(width + cellStyle.GetHorizontalPadding()).
			Render(s)
	}
	views := []string{
		render(0, idx),
		render(1, r.DisplayHost),
		render(2, r.latencyChart(chartWidth)),
		render(3, fmt.Sprintf("%5d", st.AvgLatency.Milliseconds())),
		render(4, fmt.Sprintf("%6d", st.StdDev.Milliseconds())),
		render(5, fmt.Sprintf("%4.0f%%", 100*st.PacketLoss())),
	}
	return strings.Join(views, "")
}

func (r Row) latencyChart(chartWidth int) string {
	chars := make([]string, chartWidth)
	i := 0
	for _, r := range r.Pinger.RevResults() {
		frac := math.Min(1, float64(r.Latency)/float64(graphMax))
		barIdx := int(frac * float64(len(bars)-1))
		c := latencyColors[barIdx].Render(bars[barIdx])
		if r.Type != pinger.Success {
			c = statuses[r.Type]
			if r.Type != pinger.Waiting {
				c = statusErrStyle.Render(c)
			}
		}
		charIdx := chartWidth - i - 1
		if charIdx < 0 {
			break
		}
		chars[charIdx] = c
		i++
	}
	return strings.Join(chars, "")
}

func cmpRows(a, b Row) int {
	return cmpRowKeys(a.RowKey, b.RowKey)
}

// AddRow is a message to add a new row.
type AddRow struct {
	// Row is the new row to add.
	Row Row
}

// RowUpdated is a message that a row has been updated.
type RowUpdated struct {
	// Key is the row key that was updated.
	Key RowKey
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
	ready          bool
	vp             viewport.Model
	fixedWidth     int
	latencyColumns int
	columns        []columnSpec
	rows           []Row
}

// New makes an empty ping result table with headers.
func New() *Model {
	// Add up all fixed space.
	fixedWidth := 2 * horizontalPadding * len(columnSpecs) // Each column has horizontalPadding fore and aft
	for _, c := range columnSpecs {
		if w, ok := c.Width.(int); ok {
			fixedWidth += w
		}
	}
	return &Model{
		fixedWidth: fixedWidth,
	}
}

func (t *Model) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case AddRow:
		cmds = append(cmds, t.handleAddRow(msg))
	case RowUpdated:
		cmds = append(cmds, t.handleRowUpdated(msg))
	}
	var vpCmd tea.Cmd
	t.vp, vpCmd = t.vp.Update(msg)
	cmds = append(cmds, vpCmd)
	return tea.Batch(cmds...)
}

// SetSize sets the table size. It must be called at least once in order for
// anything to be displayed.
func (t *Model) SetSize(width, height int) {
	if !t.ready {
		t.columns = make([]columnSpec, len(columnSpecs))
		t.vp = viewport.New(width, height-1)
		t.ready = true
	}
	t.vp.Width = width
	t.vp.Height = height - 1
	availSpace := float64(width - t.fixedWidth)
	for id, spec := range columnSpecs {
		t.columns[id] = spec
		switch w := spec.Width.(type) {
		case int:
			t.columns[id].Width = w
		case float64:
			t.columns[id].Width = int(math.Round(math.Max(minColWidth, w*availSpace)))
		}
		if id == colResults {
			t.latencyColumns = t.columns[id].Width.(int)
		}
	}
}

func (t *Model) handleAddRow(ar AddRow) tea.Cmd {
	t.rows = append(t.rows, ar.Row)
	slices.SortStableFunc(t.rows, cmpRows)
	t.updateRows()
	return nil
}

func (t *Model) handleRowUpdated(_ RowUpdated) tea.Cmd {
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
		lines[i] = r.View(t.columns, t.latencyColumns)
	}
	t.vp.SetContent(strings.Join(lines, "\n"))
}

func (t *Model) headerView() string {
	titles := make([]string, len(t.columns))
	for i, c := range t.columns {
		width, _ := c.Width.(int)
		titles[i] = headerStyle.Width(width + 2*horizontalPadding).Render(c.Title)
	}
	return strings.Join(titles, "")
}

func (t *Model) View() string {
	if !t.ready {
		return ""
	}
	return t.headerView() + "\n" + t.vp.View()
}
