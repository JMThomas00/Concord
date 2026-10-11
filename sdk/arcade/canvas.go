package arcade

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// cell is one character cell. A pixel cell holds four fine pixels, top
// left, top right, bottom left, bottom right, each a role ("" =
// see-through); a half-block pixel is two of them side by side.
type cell struct {
	ch     string
	fg, bg string
	bold   bool
	px     bool
	q      [4]string
	skip   bool // the right half of a wide character
}

// Canvas is a grid of character cells the size of a pane. Coordinates are
// cells; pixel coordinates (py) count half-rows, two to a cell. Fine
// pixels (FinePx) also halve the columns: four to a cell.
type Canvas struct {
	W, H   int
	pal    Palette
	cells  []cell
	styles map[styleKey]string // SGR strings already built, for String
}

type styleKey struct {
	fg, bg string
	bold   bool
}

// New makes a blank canvas.
func New(w, h int, pal Palette) *Canvas {
	c := &Canvas{W: max(0, w), H: max(0, h), pal: pal, styles: map[styleKey]string{}}
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
	c.FinePx(2*x, py, role)
	c.FinePx(2*x+1, py, role)
}

// FinePx sets one fine pixel: half-column fx, half-row fy. Fine pixels are
// drawn with quadrant characters (▘▝▖▗▚▞▙▛▜▟), twice the detail of Px
// across but half as wide, so art for them is drawn twice as wide. A cell
// shows at most two colours, so keep fine art to one colour on a
// background; a third colour in one cell is drawn in the second's place.
func (c *Canvas) FinePx(fx, fy int, role string) {
	if fx < 0 || fy < 0 {
		return
	}
	cl := c.at(fx/2, fy/2)
	if cl == nil {
		return
	}
	if !cl.px {
		bg := cl.bg
		*cl = cell{px: true, bg: bg}
	}
	cl.q[(fy%2)*2+fx%2] = role
}

// FineSprite draws pixel art in fine pixels from half-column fx and
// half-row fy: one string per row, one rune per fine pixel, looked up in
// pal (runes not in pal are see-through).
func (c *Canvas) FineSprite(fx, fy int, rows []string, pal map[rune]string) {
	for j, row := range rows {
		i := 0
		for _, k := range row {
			if role, ok := pal[k]; ok && role != "" {
				c.FinePx(fx+i, fy+j, role)
			}
			i++
		}
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
// style only when it changes. Every line is the full width: Concord centres
// each line of a frame on its own, so lines of different lengths would
// shift against each other.
func (c *Canvas) String() string {
	var b strings.Builder
	b.Grow(c.W * c.H * 3)
	for y := 0; y < c.H; y++ {
		row := c.cells[y*c.W : (y+1)*c.W]
		last := len(row) - 1
		cur, curBg := "", false
		var lastKey styleKey
		lastSGR, haveLast := "", false
		for x := 0; x <= last; x++ {
			cl := row[x]
			if cl.skip {
				continue
			}
			ch, fg, bg, bold := c.glyph(cl)
			if ch == " " && bg == "" && !curBg {
				b.WriteByte(' ') // a plain space shows no colour: keep the current style
				continue
			}
			k := styleKey{fg, bg, bold}
			if !haveLast || k != lastKey { // runs of one look are common: skip the lookup
				lastKey, lastSGR, haveLast = k, c.style(fg, bg, bold), true
			}
			sgr := lastSGR
			if sgr != cur {
				b.WriteString("\x1b[0")
				if sgr != "" {
					b.WriteString(";" + sgr)
				}
				b.WriteString("m")
				cur, curBg = sgr, bg != "" && c.pal.code(bg, true) != "49"
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

// quadrants are the block characters for each set of lit quadrants: bit 1
// top left, 2 top right, 4 bottom left, 8 bottom right.
var quadrants = [16]string{" ", "▘", "▝", "▀", "▖", "▌", "▞", "▛", "▗", "▚", "▐", "▜", "▄", "▙", "▟", "█"}

// glyph turns a pixel cell into a block character: the first role found
// (top left first) is drawn in the foreground, everything else is the
// background -- a second role, or the cell's own background where nothing
// is drawn. Half-block cells come out as ▀ ▄ █ exactly as they always have.
func (c *Canvas) glyph(cl cell) (ch, fg, bg string, bold bool) {
	if !cl.px {
		return cl.ch, cl.fg, cl.bg, cl.bold
	}
	a, b := "", ""
	for _, r := range cl.q {
		switch {
		case r == "" || r == a || r == b:
		case a == "":
			a = r
		case b == "":
			b = r
		}
	}
	if a == "" {
		return " ", "fg", cl.bg, false
	}
	bits := 0
	for i, r := range cl.q {
		if r == a {
			bits |= 1 << i
		}
	}
	if b == "" {
		b = cl.bg
	}
	return quadrants[bits], a, b, false
}

// style is the SGR parameters for a cell's look. Built once per look per
// canvas: String asks for every cell, and in client code (WebAssembly in
// an interpreter) building them each time made a frame take ~200 ms.
func (c *Canvas) style(fg, bg string, bold bool) string {
	k := styleKey{fg, bg, bold}
	if s, ok := c.styles[k]; ok {
		return s
	}
	var parts []string
	if bold {
		parts = append(parts, "1")
	}
	if fg != "" && fg != "default" {
		if s := c.pal.code(fg, false); s != "39" {
			parts = append(parts, s)
		}
	}
	if bg != "" {
		if s := c.pal.code(bg, true); s != "49" {
			parts = append(parts, s)
		}
	}
	s := strings.Join(parts, ";")
	if c.styles == nil {
		c.styles = map[styleKey]string{}
	}
	c.styles[k] = s
	return s
}

// Shade sets the background of the cells in a w x h box at (x, y),
// keeping what's drawn in them: a chalkboard, a highlighted square.
func (c *Canvas) Shade(x, y, w, h int, bg string) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			if cl := c.at(i, j); cl != nil && !cl.skip {
				cl.bg = bg
			}
		}
	}
}
