package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Concord's default look (the Dracula theme) with a grape lean.
const (
	cFg      = "#F8F8F2"
	cComment = "#6272A4"
	cDim     = "#44475A"
	cPurple  = "#BD93F9"
	cPink    = "#FF79C6"
	cGreen   = "#50FA7B"
	cOrange  = "#FFB86C"
	cRed     = "#FF5555"
	cYellow  = "#F1FA8C"
	cCyan    = "#8BE9FD"
)

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

var (
	sTitle   = fg(cPurple).Bold(true)
	sText    = fg(cFg)
	sDim     = fg(cComment)
	sGood    = fg(cGreen)
	sBad     = fg(cRed)
	sAccent  = fg(cPink)
	sKey     = lipgloss.NewStyle().Foreground(lipgloss.Color(cFg)).Background(lipgloss.Color(cDim)).Padding(0, 1)
	sCode    = fg(cCyan)
	sWarm    = fg(cOrange)
	sYellow  = fg(cYellow)
	sSection = fg(cPink).Bold(true)
)

// grapeStyles colour the logo's tones (internal/grapes).
var grapeStyles = map[string]lipgloss.Style{
	"p3": fg("#E9DBFF").Bold(true),
	"p2": fg(cPurple),
	"p1": fg("#8A6BBE"),
	"p0": fg("#4B3D6B"),
	"g2": fg(cGreen),
	"g1": fg("#2E8F49"),
	"o2": fg(cOrange),
	"o1": fg("#9C6E40"),
}

// banner is CONCORD in the client's "ANSI Shadow" banner.
var banner = []string{
	" ██████╗ ██████╗ ███╗   ██╗ ██████╗ ██████╗ ██████╗ ██████╗ ",
	"██╔════╝██╔═══██╗████╗  ██║██╔════╝██╔═══██╗██╔══██╗██╔══██╗",
	"██║     ██║   ██║██╔██╗ ██║██║     ██║   ██║██████╔╝██║  ██║",
	"██║     ██║   ██║██║╚██╗██║██║     ██║   ██║██╔══██╗██║  ██║",
	"╚██████╗╚██████╔╝██║ ╚████║╚██████╗╚██████╔╝██║  ██║██████╔╝",
	" ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚═════╝ ",
}

// renderBanner draws the banner with a purple-to-pink sweep, showing only
// the first `reveal` columns (a wipe) — all of it when reveal < 0. shift
// moves the gradient along, so it can shimmer.
func renderBanner(reveal int, shift float64) string {
	var b strings.Builder
	for i, line := range banner {
		if i > 0 {
			b.WriteByte('\n')
		}
		runes := []rune(line)
		for j, r := range runes {
			if reveal >= 0 && j >= reveal {
				b.WriteByte(' ')
				continue
			}
			if r == ' ' {
				b.WriteByte(' ')
				continue
			}
			t := math.Mod(float64(j)/float64(len(runes))+shift, 2)
			if t > 1 {
				t = 2 - t
			}
			col := mix(cPurple, cPink, t)
			if r != '█' { // the shadow
				col = mix(col, "#282A36", .55)
			}
			b.WriteString(fg(col).Render(string(r)))
		}
	}
	return b.String()
}

// mix blends two #rrggbb colours.
func mix(a, b string, t float64) string {
	var ar, ag, ab, br, bg, bb int
	fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab)
	fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb)
	l := func(x, y int) int { return int(float64(x) + (float64(y)-float64(x))*t + .5) }
	return fmt.Sprintf("#%02x%02x%02x", l(ar, br), l(ag, bg), l(ab, bb))
}

// gradientBar is a progress bar that ripens from purple to pink.
func gradientBar(width int, frac float64) string {
	frac = math.Max(0, math.Min(1, frac))
	full := int(frac * float64(width))
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < full {
			b.WriteString(fg(mix(cPurple, cPink, float64(i)/float64(max(1, width-1)))).Render("█"))
		} else {
			b.WriteString(fg(cDim).Render("░"))
		}
	}
	return b.String()
}

// theme is the forms' look.
func theme() *huh.Theme {
	t := huh.ThemeDracula()
	purple, pink, green, comment := lipgloss.Color(cPurple), lipgloss.Color(cPink), lipgloss.Color(cGreen), lipgloss.Color(cComment)
	t.Focused.Base = t.Focused.Base.BorderForeground(purple)
	t.Focused.Title = t.Focused.Title.Foreground(purple).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(comment)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(pink).SetString("❯ ")
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(pink).SetString("❯ ")
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(green).SetString("🍇 ")
	t.Focused.UnselectedPrefix = t.Focused.UnselectedPrefix.Foreground(comment).SetString(" ○ ")
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(green)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(pink)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(pink)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("#282A36")).Background(purple).Bold(true)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(pink)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(pink)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Title = t.Blurred.Title.Foreground(comment).Bold(false)
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()
	return t
}

// center places s in the middle of a w×h area.
func center(w, h int, s string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)
}

// truncate shortens s to w cells.
func truncate(s string, w int) string { return ansi.Truncate(s, w, "…") }
