package client

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// Terminals don't agree with each other, or with us, on how wide some emoji
// are: skin-toned ones, ones joined from several (🐻‍❄️), ones with a
// presentation selector (☺️). Windows Terminal draws some wider than we
// measure, so a line holding one ran past the window's edge, wrapped, and
// pushed everything below it down a row (broken borders, the message box
// drawn twice). So every frame is pinned (View, pinScreen): after each wide
// or joined character the terminal is told which column to carry on from,
// so whatever width it gives the emoji, the rest of the line lands where
// the layout put it.

// cha moves the cursor to column col (0-based).
func cha(col int) string { return fmt.Sprintf("\x1b[%dG", col+1) }

// ech erases n cells from the cursor in the current colours, without
// moving it. Zero width to anything measuring the line (Bubbletea truncates
// lines it measures wider than the window, so filling the cells with
// spaces first cut the ends off lines with emoji in them).
func ech(n int) string { return fmt.Sprintf("\x1b[%dX", n) }

// pinScreen pins every line of a frame (see the top of this file).
func pinScreen(frame string) string {
	lines := strings.Split(frame, "\n")
	for i, l := range lines {
		lines[i] = pinWide(l, 0)
	}
	return strings.Join(lines, "\n")
}

// pinWide pins s, a line starting at column start: after each wide or
// joined character, its cells are erased first (so a terminal drawing it
// narrower leaves no unpainted gap) and the cursor is sent to where we
// measured it to end. Escape sequences pass through; a cursor-to-column
// already in s (an overlay pinned on its own) moves the count along with
// it. A line with nothing to pin comes back as it was.
func pinWide(s string, start int) string {
	if !needsPin(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 32)
	col := start
	for s != "" {
		c := s[0]
		switch {
		case c == 0x1b: // an escape sequence: copy it whole
			n := 1
			if len(s) > 1 && s[1] == '[' {
				n = 2
				for n < len(s) && (s[n] < 0x40 || s[n] > 0x7e) {
					n++
				}
				n = min(n+1, len(s))
				if s[n-1] == 'G' { // cursor to column: follow it
					if v, err := strconv.Atoi(s[2 : n-1]); err == nil {
						col = v - 1
					}
				}
			}
			b.WriteString(s[:n])
			s = s[n:]
		case c < 0x80: // plain ASCII: one column
			b.WriteByte(c)
			col++
			s = s[1:]
		default:
			cluster, rest, w, _ := uniseg.FirstGraphemeClusterInString(s, -1)
			if w > 1 || utf8.RuneCountInString(cluster) > 1 {
				b.WriteString(ech(max(w, 1)) + cluster + cha(col+w))
			} else {
				b.WriteString(cluster)
			}
			col += w
			s = rest
		}
	}
	return b.String()
}

// needsPin reports whether s has anything pinWide would pin: a character
// that isn't one column on its own (wide, or a joiner, selector or mark
// that combines). Most lines hold only ASCII and box drawing, and skip the
// grapheme work. A narrow character that a following variation selector
// makes an emoji (☺️) is caught by the selector, which the table calls one
// column wide.
func needsPin(s string) bool {
	for i := 0; i < len(s); {
		if s[i] < 0x80 {
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if runewidth.RuneWidth(r) != 1 || r >= 0xFE00 && r <= 0xFE0F {
			return true
		}
		i += size
	}
	return false
}
