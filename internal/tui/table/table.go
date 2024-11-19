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

	"github.com/pcekm/graphping/internal/ping/pinger"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	// Minimum width for columns determined fractionally.
	minColWidth = 10

	// Duration at which a ping latency displays at maximum height.
	graphMax = 250 * time.Millisecond
)

type columnID int

// columnID values specified in the order they should appear in the table.
const (
	colIndex columnID = iota
	colHost
	colResults
	colAvgMs
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
	case colPctLoss:
		return "colPctLoss"
	default:
		return fmt.Sprintf("(unknown:%d)", c)
	}
}

// Describes a column.
type columnSpec struct {
	Title string

	// Width may be int or float64. If int, the colum will be exactly
	// that wide. If float64, the column will take up that fraction of the
	// remaining space on the line. The fractions should probably add to 1.0.
	Width any
}

var (
	columns = map[columnID]columnSpec{
		colIndex:   {Title: "Hop", Width: 3},
		colHost:    {Title: "Host", Width: 1.0 / 3.0},
		colResults: {Title: "Results", Width: 2.0 / 3.0},
		colAvgMs:   {Title: "AvgMs", Width: 5},
		colPctLoss: {Title: "%Loss", Width: 5},
	}

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

// CellViews returns views for each cell in the table row.
func (r Row) CellViews(chartWidth int) table.Row {
	st := r.Pinger.Stats()
	idx := "-"
	if r.Index != 0 {
		idx = fmt.Sprintf("%2d.", r.Index)
	}
	return table.Row{
		idx,
		r.DisplayHost,
		r.latencyChart(chartWidth),
		fmt.Sprintf("%5d", st.AvgLatency.Milliseconds()),
		fmt.Sprintf("%4.0f%%", 100*st.PacketLoss()),
	}
}

func (r Row) latencyChart(chartWidth int) string {
	chars := make([]string, chartWidth)
	i := 0
	for _, r := range r.Pinger.RevResults() {
		frac := math.Min(1, float64(r.Latency)/float64(graphMax))
		c := bars[int(frac*float64(len(bars)-1))]
		if r.Type != pinger.Success {
			c = statuses[r.Type]
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
	table          table.Model
	fixedWidth     int
	latencyColumns int
	rows           []Row
}

// New makes an empty ping result table with headers.
func New() *Model {
	// Add up all fixed space.
	fixedWidth := 2 * len(columns) // Each column has has one space fore and aft
	for _, c := range columns {
		if w, ok := c.Width.(int); ok {
			fixedWidth += w
		}
	}

	// Make the table columns (all with 0 widths).
	cols := make([]table.Column, len(columns))
	for id, c := range columns {
		cols[id] = table.Column{Title: c.Title}
	}

	tab := table.New(table.WithColumns(cols))
	tab.SetCursor(-1)

	return &Model{
		table:      tab,
		fixedWidth: fixedWidth,
	}
}

func (t *Model) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var tblCmd tea.Cmd
	t.table, tblCmd = t.table.Update(msg)
	cmds = append(cmds, tblCmd)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.handleResize(msg)
	case AddRow:
		cmds = append(cmds, t.handleAddRow(msg))
	case RowUpdated:
		cmds = append(cmds, t.handleRowUpdated(msg))
	}
	return tea.Batch(cmds...)
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

func (t *Model) handleResize(msg tea.WindowSizeMsg) {
	// Weirdly, not handled by table.Update.
	t.table.SetWidth(msg.Width)
	t.table.SetHeight(msg.Height)
	availSpace := float64(t.table.Width() - t.fixedWidth)
	tCols := t.table.Columns()
	for id, spec := range columns {
		switch w := spec.Width.(type) {
		case int:
			tCols[id].Width = w
		case float64:
			tCols[id].Width = int(math.Round(math.Max(minColWidth, w*availSpace)))
		}
		if id == colResults {
			t.latencyColumns = tCols[id].Width
		}
	}
	t.table.SetColumns(tCols)
}

func (t *Model) updateRows() {
	latencyWidth := t.table.Columns()[colResults].Width
	rows := make([]table.Row, len(t.rows))
	for i, r := range t.rows {
		rows[i] = r.CellViews(latencyWidth)
	}
	t.table.SetRows(rows)
}

func (t *Model) View() string {
	return t.table.View()
}
