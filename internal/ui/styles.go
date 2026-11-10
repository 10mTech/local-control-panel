package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Catppuccin Mocha, to match herdr's theme.
var (
	cText     = lipgloss.Color("#cdd6f4")
	cSubtext  = lipgloss.Color("#a6adc8")
	cOverlay  = lipgloss.Color("#6c7086")
	cSurface1 = lipgloss.Color("#45475a")
	cSurface0 = lipgloss.Color("#313244")
	cCrust    = lipgloss.Color("#11111b")
	cMauve    = lipgloss.Color("#cba6f7")
	cBlue     = lipgloss.Color("#89b4fa")
	cGreen    = lipgloss.Color("#a6e3a1")
	cYellow   = lipgloss.Color("#f9e2af")
	cPeach    = lipgloss.Color("#fab387")
	cRed      = lipgloss.Color("#f38ba8")
	cTeal     = lipgloss.Color("#94e2d5")
	cSky      = lipgloss.Color("#89dceb")
)

var (
	sText     = lipgloss.NewStyle().Foreground(cText)
	sDim      = lipgloss.NewStyle().Foreground(cOverlay)
	sSub      = lipgloss.NewStyle().Foreground(cSubtext)
	sAccent   = lipgloss.NewStyle().Foreground(cMauve).Bold(true)
	sGreen    = lipgloss.NewStyle().Foreground(cGreen)
	sYellow   = lipgloss.NewStyle().Foreground(cYellow)
	sPeach    = lipgloss.NewStyle().Foreground(cPeach)
	sRed      = lipgloss.NewStyle().Foreground(cRed)
	sTeal     = lipgloss.NewStyle().Foreground(cTeal)
	sSky      = lipgloss.NewStyle().Foreground(cSky)
	sBlue     = lipgloss.NewStyle().Foreground(cBlue)
	sHeader   = lipgloss.NewStyle().Foreground(cOverlay).Bold(true)
	sBadge    = lipgloss.NewStyle().Foreground(cCrust).Background(cMauve).Bold(true).Padding(0, 1)
	sTabOn    = lipgloss.NewStyle().Foreground(cCrust).Background(cBlue).Bold(true).Padding(0, 1)
	sTabOff   = lipgloss.NewStyle().Foreground(cSubtext).Background(cSurface0).Padding(0, 1)
	sKey      = lipgloss.NewStyle().Foreground(cMauve).Bold(true)
	sSelected = lipgloss.NewStyle().Foreground(cText).Background(cSurface0).Bold(true)
	sBarText  = lipgloss.NewStyle().Foreground(cSubtext)
)

var activityStyle = map[string]lipgloss.Style{
	"active":    sGreen,
	"warm":      sBlue,
	"dormant":   sYellow,
	"abandoned": sRed,
}

// box draws a rounded window frame of exactly w×h cells with a title in the
// top edge and an optional note in the bottom edge, like a TUIOS window.
func box(title, note string, lines []string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	bc := lipgloss.NewStyle().Foreground(cSurface1)
	tc := sSub
	if focused {
		bc = lipgloss.NewStyle().Foreground(cMauve)
		tc = sAccent
	}
	inner := w - 2

	var b strings.Builder
	t := ansi.Truncate(title, max(0, inner-4), "…")
	b.WriteString(bc.Render("╭─ ") + tc.Render(t) + " ")
	b.WriteString(bc.Render(strings.Repeat("─", max(0, inner-3-ansi.StringWidth(t))) + "╮"))
	b.WriteString("\n")

	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(bc.Render("│") + fit(line, inner) + bc.Render("│") + "\n")
	}

	if note != "" {
		n := ansi.Truncate(note, max(0, inner-4), "…")
		b.WriteString(bc.Render("╰"+strings.Repeat("─", max(0, inner-3-ansi.StringWidth(n)))) + " " + sDim.Render(n) + " " + bc.Render("─╯"))
	} else {
		b.WriteString(bc.Render("╰" + strings.Repeat("─", inner) + "╯"))
	}
	return b.String()
}
