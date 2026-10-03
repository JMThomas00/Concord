package client

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// The login experience's effects (loading screens, page transitions,
// backgrounds, easter eggs; see fx.go) work on finished frames rather than
// on each page's own code: a frame is parsed into a grid of styled cells,
// changed, and written back. So any page gets every effect for free.

// fxCell is one terminal cell: its SGR state, its text, and its width. A
// wide character (an emoji) is one cell of width 2 followed by a
// placeholder of width 0.
type fxCell struct {
	sgr string // the SGR escape sequences in force ("" for plain)
	ch  string // what's drawn; "" for a wide character's second half
	w   int
}

var blankCell = fxCell{ch: " ", w: 1}

// fxGrid is a w×h frame of cells.
type fxGrid struct {
	w, h int
	rows [][]fxCell
}

func newGrid(w, h int) *fxGrid {
	g := &fxGrid{w: w, h: h, rows: make([][]fxCell, h)}
	for r := range g.rows {
		row := make([]fxCell, w)
		for c := range row {
			row[c] = blankCell
		}
		g.rows[r] = row
	}
	return g
}

// parseFrame reads a rendered frame into a w×h grid: SGR styling is kept,
// other escape sequences are dropped, and anything outside the grid is cut.
func parseFrame(s string, w, h int) *fxGrid {
	g := newGrid(w, h)
	lines := strings.Split(s, "\n")
	for r := 0; r < h && r < len(lines); r++ {
		parseLine(lines[r], g.rows[r])
	}
	return g
}

func parseLine(line string, row []fxCell) {
	sgr := ""
	col := 0
	for i := 0; i < len(line) && col < len(row); {
		if line[i] == 0x1b {
			n, seq, isSGR := escapeAt(line, i)
			if isSGR {
				if seq == "\x1b[m" || seq == "\x1b[0m" {
					sgr = ""
				} else {
					sgr += seq
				}
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		i += size
		w := runewidth.RuneWidth(r)
		if w == 0 {
			// A combining mark or variation selector: part of the cell before.
			if col > 0 {
				prev := col - 1
				if row[prev].w == 0 && prev > 0 {
					prev--
				}
				row[prev].ch += string(r)
			}
			continue
		}
		if col+w > len(row) {
			break
		}
		row[col] = fxCell{sgr: sgr, ch: string(r), w: w}
		if w == 2 {
			row[col+1] = fxCell{sgr: sgr, w: 0}
		}
		col += w
	}
}

// escapeAt measures the escape sequence at s[i]: its length, its text, and
// whether it's SGR (CSI ... m).
func escapeAt(s string, i int) (int, string, bool) {
	if i+1 >= len(s) {
		return 1, "", false
	}
	switch s[i+1] {
	case '[': // CSI: parameters, then a final byte 0x40–0x7e
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j >= len(s) {
			return len(s) - i, "", false
		}
		return j + 1 - i, s[i : j+1], s[j] == 'm'
	case ']', 'P', '_': // OSC, DCS, APC: up to BEL or ST
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1 - i, "", false
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2 - i, "", false
			}
		}
		return len(s) - i, "", false
	}
	return 2, "", false
}

// String writes the grid back as a frame.
func (g *fxGrid) String() string {
	var b strings.Builder
	for r, row := range g.rows {
		if r > 0 {
			b.WriteByte('\n')
		}
		cur := ""
		for _, c := range row {
			if c.w == 0 {
				continue
			}
			if c.sgr != cur {
				if cur != "" {
					b.WriteString("\x1b[0m")
				}
				b.WriteString(c.sgr)
				cur = c.sgr
			}
			b.WriteString(c.ch)
		}
		if cur != "" {
			b.WriteString("\x1b[0m")
		}
	}
	return b.String()
}

// set draws ch (a single cell's text, width w) at r, c with sgr, keeping
// wide characters whole.
func (g *fxGrid) set(r, c int, ch, sgr string, w int) {
	if r < 0 || r >= g.h || c < 0 || c+w > g.w {
		return
	}
	g.clearAt(r, c)
	if w == 2 {
		g.clearAt(r, c+1)
	}
	g.rows[r][c] = fxCell{sgr: sgr, ch: ch, w: w}
	if w == 2 {
		g.rows[r][c+1] = fxCell{sgr: sgr, w: 0}
	}
}

// clearAt blanks a cell, and the other half of a wide character it's part of.
func (g *fxGrid) clearAt(r, c int) {
	row := g.rows[r]
	switch {
	case row[c].w == 0 && c > 0 && row[c-1].w == 2:
		row[c-1] = blankCell
	case row[c].w == 2 && c+1 < g.w:
		row[c+1] = blankCell
	}
	row[c] = blankCell
}

// text writes a plain string at r, c in one style.
func (g *fxGrid) text(r, c int, s, sgr string) {
	for _, ch := range s {
		w := runewidth.RuneWidth(ch)
		if w == 0 {
			continue
		}
		g.set(r, c, string(ch), sgr, w)
		c += w
	}
}

// isEmpty reports whether a cell shows nothing: a space with no background.
func (c fxCell) isEmpty() bool {
	return c.w == 1 && c.ch == " " && !strings.Contains(c.sgr, "48;") && !strings.Contains(c.sgr, "[4")
}

// contentSpan is the first and last non-empty column of row r, or -1, -1.
func (g *fxGrid) contentSpan(r int) (int, int) {
	first, last := -1, -1
	for c, cell := range g.rows[r] {
		if cell.w != 0 && !cell.isEmpty() {
			if first < 0 {
				first = c
			}
			last = c + max(cell.w, 1) - 1
		}
	}
	return first, last
}

// underlay puts bg behind g wherever g shows nothing, keeping clear of g's
// content: pad columns either side of each row's text, and rows above and
// below. Text is never drawn over.
func (g *fxGrid) underlay(bg *fxGrid, padCols, padRows int) {
	type span struct{ first, last int }
	spans := make([]span, g.h)
	for r := range spans {
		f, l := g.contentSpan(r)
		spans[r] = span{f, l}
	}
	for r := 0; r < g.h && r < bg.h; r++ {
		// The keep-out on this row is the widest content within padRows.
		lo, hi := -1, -1
		for d := -padRows; d <= padRows; d++ {
			if rr := r + d; rr >= 0 && rr < g.h && spans[rr].first >= 0 {
				if lo < 0 || spans[rr].first < lo {
					lo = spans[rr].first
				}
				if spans[rr].last > hi {
					hi = spans[rr].last
				}
			}
		}
		if lo >= 0 {
			lo, hi = lo-padCols, hi+padCols
		}
		for c := 0; c < g.w && c < bg.w; c++ {
			b := bg.rows[r][c]
			if b.w == 0 || b.isEmpty() || (lo >= 0 && c+b.w-1 >= lo && c <= hi) {
				continue
			}
			if !g.rows[r][c].isEmpty() || (b.w == 2 && (c+1 >= g.w || !g.rows[r][c+1].isEmpty())) {
				continue
			}
			g.set(r, c, b.ch, b.sgr, b.w)
		}
	}
}

// clone copies the grid.
func (g *fxGrid) clone() *fxGrid {
	out := &fxGrid{w: g.w, h: g.h, rows: make([][]fxCell, g.h)}
	for r, row := range g.rows {
		out.rows[r] = append([]fxCell(nil), row...)
	}
	return out
}
