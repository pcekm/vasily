// Package theme contains theme-related code. This is influenced by Material
// Design, although that analogy only goes so far in a text UI.
package theme

import (
	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
)

// Default contains the default theme.
var Default = Theme{
	Text: Text{
		Normal: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
				Light: "#333333",
				Dark:  "#AAAAAA",
			}),
		Important: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
				Dark:  "#DDDDDD",
				Light: "#000000",
			}).
			Bold(true),
		Unimportant: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
				Light: "#666666",
				Dark:  "#999999",
			}),
	},
	Colors: Colors{
		Primary:     lipgloss.Color("#1F326F"),
		OnPrimary:   lipgloss.Color("#CCCCCC"),
		Secondary:   lipgloss.Color("#788AC4"),
		OnSecondary: lipgloss.Color("#000000"),
		Error:       lipgloss.Color("#881522"),
		OnError:     lipgloss.Color("#CCCCCC"),
	},
	Heatmap: Gradient{
		Low:  "#3abb46",
		High: "#ab3c45",
	},
}

// Theme contains common styles for use throughout the program.
type Theme struct {
	Text    Text
	Colors  Colors
	Heatmap Heatmap
}

// Text contains common text styles.
type Text struct {
	Normal      lipgloss.Style
	Important   lipgloss.Style
	Unimportant lipgloss.Style
}

// Colors contains some common colors that recur through the theme.
type Colors struct {
	Primary     lipgloss.TerminalColor
	OnPrimary   lipgloss.TerminalColor
	Secondary   lipgloss.TerminalColor
	OnSecondary lipgloss.TerminalColor
	Error       lipgloss.TerminalColor
	OnError     lipgloss.TerminalColor
}

// Heatmap maps a fraction in the interval [0, 1] to a color.
type Heatmap interface {
	At(v float64) lipgloss.TerminalColor
}

// Creates a colorful.Color from a hex string or returns primary red so that the
// mistake (hopefully) stands out.
func hexColor(s string) colorful.Color {
	c, err := colorful.Hex(s)
	if err != nil {
		return colorful.Color{R: 1}
	}
	return c
}

// Gradient contains a color gradient representing a fraction from 0 to 1.
type Gradient struct {
	Low  string
	High string
}

// For returns the color for the given value. The value must be in the interval
// [0, 1].
func (h Gradient) At(v float64) lipgloss.TerminalColor {
	cold := hexColor(h.Low)
	hot := hexColor(h.High)
	c := cold.BlendHcl(hot, v)
	return lipgloss.Color(c.Hex())
}
