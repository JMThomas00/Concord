package arcade

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// cell is one character cell. A pixel cell holds two pixels, top and
// bottom, each a role ("" = see-through).
type cell struct {
	ch       string
	fg, bg   string
	bold     bool
	px       bool
	top, bot string
	skip     bool // the right half of a wide character
}

// Canvas is a grid of character cells the size of a pane. Coordinates are
// cells; pixel coordinates (py) count half-rows, two to a cell.
type Canvas struct {
	W, H  int
	pal   Palette
	cells []cell
}

// New makes a blank canvas.
func New(w, h int, pal Palette) *Canvas {
	c := &Canvas{W: max(0, w), H: max(0, h), pal: pal}
	c.cells = make([]cell, c.W*c.H)
	for i := range c.cells {
		c.cells[i] = cell{ch: " ", fg: "fg"}
	}
	return c
}

func (c *Canvas) at(x, y int) *cell {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return nil
	}
	return &c.cells[y*c.W+x]
}

// Text writes s at (x, y) and returns the column after it. Characters past
// the edge are dropped. bg "" leaves the background alone.
func (c *Canvas) Text(x, y int, s, fg, bg string, bold bool) int {
	for _, r := range s {
		ch := string(r)
		w := ansi.StringWidth(ch)
		if w == 0 {
			continue
		}
		if w == 2 && x+1 >= c.W {
			break
		}
		if cl := c.at(x, y); cl != nil {
			*cl = cell{ch: ch, fg: fg, bg: bg, bold: bold}
			if w == 2 {
				if n := c.at(x+1, y); n != nil {
					*n = cell{skip: true}
				}
			}
		}
		x += w
	}
	return x
}

// TextWidth is how many cells s takes.
func TextWidth(s string) int { return ansi.StringWidth(s) }

// Center writes s centred on row y.
func (c *Canvas) Center(y int, s, fg, bg string, bold bool) int {
	return c.Text((c.W-TextWidth(s))/2, y, s, fg, bg, bold)
}

// CenterIn writes s centred in the columns [x, x+w).
func (c *Canvas) CenterIn(x, w, y int, s, fg, bg string, bold bool) int {
	return c.Text(x+max(0, (w-TextWidth(s))/2), y, s, fg, bg, bold)
}

// Fill repeats ch n times from (x, y).
func (c *Canvas) Fill(x, y, n int, ch, fg, bg string) {
	for i := 0; i < n; i++ {
		c.Text(x+i, y, ch, fg, bg, false)
	}
}

// Px sets one pixel: column x, half-row py.
func (c *Canvas) Px(x, py int, role string) {
	if py < 0 {
		return
	}
	cl := c.at(x, py/2)
	if cl == nil {
		return
	}
	if !cl.px {
		bg := cl.bg
		*cl = cell{px: true, bg: bg}
	}
	if py%2 == 0 {
		cl.top = role
	} else {
		cl.bot = role
	}
}

// Sprite draws pixel art: one string per pixel row, one rune per pixel,
// looked up in pal (runes not in pal are see-through). Each pixel becomes a
// scale x scale block.
func (c *Canvas) Sprite(x, py int, rows []string, pal map[rune]string, scale int) {
	scale = max(1, scale)
	for j, row := range rows {
		i := 0
		for _, k := range row {
			if role, ok := pal[k]; ok && role != "" {
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						c.Px(x+i*scale+sx, py+j*scale+sy, role)
					}
				}
			}
			i++
		}
	}
}

// SpriteWidth is the width in cells of rows at scale.
func SpriteWidth(rows []string, scale int) int {
	w := 0
	for _, r := range rows {
		w = max(w, len([]rune(r)))
	}
	return w * max(1, scale)
}

// Flip mirrors pixel art left to right.
func Flip(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		rs := []rune(r)
		for a, b := 0, len(rs)-1; a < b; a, b = a+1, b-1 {
			rs[a], rs[b] = rs[b], rs[a]
		}
		out[i] = string(rs)
	}
	return out
}

// Box draws a Concord pane: rounded corners, the title set into the top
// border.
func (c *Canvas) Box(x, y, w, h int, role, title, titleRole string) {
	if w < 2 || h < 2 {
		return
	}
	c.Text(x, y, "╭"+strings.Repeat("─", w-2)+"╮", role, "", false)
	for i := 1; i < h-1; i++ {
		c.Text(x, y+i, "│", role, "", false)
		c.Text(x+w-1, y+i, "│", role, "", false)
	}
	c.Text(x, y+h-1, "╰"+strings.Repeat("─", w-2)+"╯", role, "", false)
	if title != "" {
		c.Text(x+2, y, " "+title+" ", titleRole, "", true)
	}
}

// Key is one footer hint: the key and what it does.
type Key struct{ Key, Does string }

// Keys draws footer key chips the Concord way, the key in bold cyan on the
// current-line colour, from column x on row y.
func (c *Canvas) Keys(x, y int, keys ...Key) int {
	for _, k := range keys {
		x = c.Text(x, y, " "+k.Key+" ", "cyan", "line", true)
		x = c.Text(x+1, y, k.Does, "dim", "", false) + 2
	}
	return x
}

// String renders the canvas as lines of text with SGR colours, writing a
// style only when it changes and dropping trailing blank cells.
func (c *Canvas) String() string {
	var b strings.Builder
	for y := 0; y < c.H; y++ {
		row := c.cells[y*c.W : (y+1)*c.W]
		last := len(row) - 1
		for last >= 0 && blank(row[last]) {
			last--
		}
		cur := ""
		for x := 0; x <= last; x++ {
			cl := row[x]
			if cl.skip {
				continue
			}
			ch, fg, bg, bold := c.glyph(cl)
			sgr := c.style(fg, bg, bold)
			if sgr != cur {
				b.WriteString("\x1b[0")
				if sgr != "" {
					b.WriteString(";" + sgr)
				}
				b.WriteString("m")
				cur = sgr
			}
			b.WriteString(ch)
		}
		if cur != "" {
			b.WriteString("\x1b[0m")
		}
		if y < c.H-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func blank(cl cell) bool {
	return !cl.skip && !cl.px && cl.ch == " " && cl.bg == ""
}

// glyph turns a pixel cell into a half-block character.
func (c *Canvas) glyph(cl cell) (ch, fg, bg string, bold bool) {
	if !cl.px {
		return cl.ch, cl.fg, cl.bg, cl.bold
	}
	t, u := cl.top, cl.bot
	switch {
	case t == "" && u == "":
		return " ", "fg", cl.bg, false
	case t == u:
		return "█", t, cl.bg, false
	case u == "":
		return "▀", t, cl.bg, false
	case t == "":
		return "▄", u, cl.bg, false
	}
	return "▀", t, u, false
}

func (c *Canvas) style(fg, bg string, bold bool) string {
	var parts []string
	if bold {
		parts = append(parts, "1")
	}
	if fg != "" && fg != "default" {
		if s := c.pal.Get(fg).sgr(false); s != "39" {
			parts = append(parts, s)
		}
	}
	if bg != "" {
		if s := c.pal.Get(bg).sgr(true); s != "49" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ";")
}
