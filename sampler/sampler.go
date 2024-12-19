// Command sampler prints a theme color sampler.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
	"github.com/pcekm/graphping/internal/tui/theme"
)

func main() {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		log.Fatal("Error: not a terminal.")
	}

	profiles := []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI}
	for _, p := range profiles {
		printSamples(p, theme.Default)
	}
}

func printSamples(prof termenv.Profile, th theme.Theme) {
	lipgloss.SetColorProfile(prof)

	co := theme.Default.Colors
	samples := []struct {
		text   string
		fg, bg lipgloss.TerminalColor
	}{
		{"Surface", co.OnSurface, co.Surface},
		{"OnSurfaceVariant", co.OnSurfaceVariant, co.Surface},
		{"Primary", co.OnPrimary, co.Primary},
		{"Secondary", co.OnSecondary, co.Secondary},
		{"Error", co.OnError, co.Error},
	}

	width, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		log.Fatalf("GetSize: %v", err)
	}

	var profileName string
	switch prof {
	case termenv.TrueColor:
		profileName = "TrueColor: "
	case termenv.ANSI256:
		profileName = "ANSI256:   "
	case termenv.ANSI:
		profileName = "ANSI:      "
	}

	profileTile := lipgloss.PlaceVertical(3, lipgloss.Center, profileName)

	curWidth := lipgloss.Width(profileTile)
	soFar := []string{profileTile}
	for _, s := range samples {
		samp := sample(s.text, s.fg, s.bg)
		size := lipgloss.Width(samp)
		if curWidth+size > width {
			fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, soFar...))
			curWidth = 0
			soFar = soFar[:0]
		}
		curWidth += size
		soFar = append(soFar, samp)
	}

	grad := gradient(th.Heatmap)
	size := lipgloss.Width(grad)
	if size+curWidth > width {
		fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, soFar...))
		curWidth = size
		soFar = soFar[:0]
	}
	soFar = append(soFar, grad)
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, soFar...))
}

// Returns a color sample with the given text and colors.
func sample(text string, fg, bg lipgloss.TerminalColor) string {
	style := lipgloss.NewStyle().
		Foreground(fg).
		Background(bg).
		Padding(1)
	return style.Render(text)
}

// Returns a gradient sample.
func gradient(gr theme.Heatmap) string {
	const n = 7
	style := lipgloss.NewStyle().
		Width(2).
		Height(3)

	row := make([]string, n)
	for i := range n {
		row[i] = style.Background(gr.At(float64(i) / (n - 1))).Render("")
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, row...)
}
