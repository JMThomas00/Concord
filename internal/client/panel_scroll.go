package client

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// panelScroll is a sidebar panel's (channel list, members) scroll position.
// Only two things move it: the mouse wheel, and a change of selection,
// which scrolls just far enough to bring the new selection into view.
// Wheel scrolling never changes the selection -- in the channel list that
// would switch the open channel.
type panelScroll struct {
	offset  int
	lastSel string // selection the offset was last adjusted to show
}

const panelWheelStep = 3

func (s *panelScroll) wheel(up bool) {
	if up {
		s.offset = max(0, s.offset-panelWheelStep)
	} else {
		s.offset += panelWheelStep // clamped at the next render
	}
}

// scrollPanel returns the visible rows of a panel's content lines. When the
// content overflows, a one-column scrollbar takes the rightmost of width
// columns. [selStart, selEnd) is the selected item's line range (selStart
// < 0 for none); it's brought into view whenever selKey changes. This runs
// during rendering because only the renderer knows each item's line
// position; its one side effect is updating s.
func (a *App) scrollPanel(s *panelScroll, lines []string, width, visible, selStart, selEnd int, selKey string) string {
	if visible < 1 {
		visible = 1
	}
	if len(lines) <= visible {
		s.offset = 0
		s.lastSel = selKey
		return strings.Join(lines, "\n")
	}

	if selKey != s.lastSel {
		s.lastSel = selKey
		if selStart >= 0 {
			if selStart < s.offset {
				s.offset = selStart
			} else if selEnd > s.offset+visible {
				s.offset = min(selStart, selEnd-visible)
			}
		}
	}
	s.offset = max(0, min(s.offset, len(lines)-visible))

	thumbPos, thumbSize := helpScrollbarThumb(s.offset, len(lines), visible)
	trackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Selection))
	thumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	contentWidth := max(1, width-1)

	out := make([]string, visible)
	for i := 0; i < visible; i++ {
		line := ansi.Truncate(lines[s.offset+i], contentWidth, "")
		if pad := contentWidth - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		glyph := trackStyle.Render("│")
		if i >= thumbPos && i < thumbPos+thumbSize {
			glyph = thumbStyle.Render("┃")
		}
		out[i] = line + glyph
	}
	return strings.Join(out, "\n")
}
