package client

import (
	"math"
	"math/rand/v2"
	"time"
)

// A transition plays when the login stage changes page. It works on whole
// frames: only cells that differ between the old page and the new one
// move, so the grapes and banner, which every page shares, stay put.
type transition struct {
	kind  string // a layerTransition option
	from  string // the old page's last frame
	start time.Time
	seed  uint64
}

func (t *transition) duration() time.Duration {
	switch t.kind {
	case "baud":
		return 700 * time.Millisecond
	case "crt", "teletext":
		return 600 * time.Millisecond
	}
	return 450 * time.Millisecond
}

// render draws the transition at progress p (0..1) from one frame to the next.
func (t *transition) render(from, to *fxGrid, p float64) *fxGrid {
	switch t.kind {
	case "decode":
		return transDecode(from, to, p, t.seed)
	case "teletext":
		return transTeletext(from, to, p)
	case "baud":
		return transBaud(from, to, p)
	case "crt":
		return transCRT(from, to, p, t.seed)
	case "plasma":
		return transPlasma(from, to, p)
	}
	return transSlide(from, to, p)
}

func sameCell(a, b fxCell) bool { return a == b }

func rowsDiffer(from, to *fxGrid, r int) bool {
	for c := range to.rows[r] {
		if !sameCell(from.rows[r][c], to.rows[r][c]) {
			return true
		}
	}
	return false
}

// putCell copies one cell (a whole wide character) into g.
func putCell(g *fxGrid, r, c int, cell fxCell) {
	if cell.w == 0 {
		return
	}
	g.set(r, c, cell.ch, cell.sgr, cell.w)
}

// transSlide: the changed rows slide in from the right one after another,
// easing to a stop, replacing the old ones top to bottom.
func transSlide(from, to *fxGrid, p float64) *fxGrid {
	out := to.clone()
	var changed []int
	for r := 0; r < to.h; r++ {
		if rowsDiffer(from, to, r) {
			changed = append(changed, r)
		}
	}
	stagger := 0.35 / math.Max(1, float64(len(changed)))
	const shift = 14
	for k, r := range changed {
		rp := (p - float64(k)*stagger) / 0.65
		if rp <= 0 {
			out.rows[r] = append([]fxCell(nil), from.rows[r]...)
			continue
		}
		if rp >= 1 {
			continue
		}
		off := int(math.Round((1 - easeOutCubic(rp)) * shift))
		row := make([]fxCell, to.w)
		for c := range row {
			row[c] = blankCell
		}
		out.rows[r] = row
		for c, cell := range to.rows[r] {
			putCell(out, r, c+off, cell)
		}
	}
	return out
}

var decodeGlyphs = []rune("!<>-_\\/[]{}=+*^?#%&@$░▒")

// transDecode: each changed cell flickers through random characters before
// settling, at its own moment.
func transDecode(from, to *fxGrid, p float64, seed uint64) *fxGrid {
	out := to.clone()
	r := rand.New(rand.NewPCG(seed, 1))
	frame := uint64(p * 30)
	for y := 0; y < to.h; y++ {
		for x := 0; x < to.w; x++ {
			at := r.Float64() * 0.7
			src, dst := from.rows[y][x], to.rows[y][x]
			if sameCell(src, dst) || dst.w == 0 {
				continue
			}
			switch {
			case p < at:
				out.set(y, x, " ", "", 1)
				if src.w != 0 {
					putCell(out, y, x, src)
				}
			case p < at+0.25 && !dst.isEmpty():
				g := decodeGlyphs[int(cellHash(seed+frame, y, x, 0)*float64(len(decodeGlyphs)))]
				out.set(y, x, string(g), dst.sgr, 1)
			}
		}
	}
	return out
}

// transTeletext: the new page arrives a row at a time, top to bottom, like
// a teletext page loading, with the rows still to come blank.
func transTeletext(from, to *fxGrid, p float64) *fxGrid {
	out := to.clone()
	upTo := int(p * float64(to.h))
	for r := upTo; r < to.h; r++ {
		if rowsDiffer(from, to, r) {
			blank := make([]fxCell, to.w)
			for c := range blank {
				blank[c] = blankCell
			}
			out.rows[r] = blank
		}
	}
	return out
}

// transBaud: the new page is typed out in reading order at modem speed,
// behind a block cursor.
func transBaud(from, to *fxGrid, p float64) *fxGrid {
	out := to.clone()
	type pos struct{ r, c int }
	var cells []pos
	for r := 0; r < to.h; r++ {
		for c := 0; c < to.w; c++ {
			if !sameCell(from.rows[r][c], to.rows[r][c]) && to.rows[r][c].w != 0 && !to.rows[r][c].isEmpty() {
				cells = append(cells, pos{r, c})
			}
		}
	}
	shown := int(p * float64(len(cells)))
	for r := 0; r < to.h; r++ {
		if rowsDiffer(from, to, r) {
			for c := range out.rows[r] {
				out.rows[r][c] = blankCell
			}
		}
	}
	for i, q := range cells {
		if i < shown {
			putCell(out, q.r, q.c, to.rows[q.r][q.c])
		} else if i == shown {
			out.set(q.r, q.c, "█", to.rows[q.r][q.c].sgr, 1)
			break
		}
	}
	return out
}

// transCRT: an old television changing channel. The old picture collapses
// to a bright line, a flash of static, and the new one opens out.
func transCRT(from, to *fxGrid, p float64, seed uint64) *fxGrid {
	h := to.h
	mid := h / 2
	out := newGrid(to.w, h)
	line := sgrFor("#ffffff", "", true)
	switch {
	case p < 0.4: // collapse
		half := int((1 - easeOutCubic(p/0.4)) * float64(h) / 2)
		for r := mid - half; r <= mid+half && r < h; r++ {
			if r >= 0 {
				out.rows[r] = append([]fxCell(nil), from.rows[r]...)
			}
		}
		if half == 0 {
			out.text(mid, 0, repeatRune('─', to.w), line)
		}
	case p < 0.55: // static
		static := []string{"░", "▒", "▓", " ", " "}
		dim := sgrFor("#6b6b7b", "", false)
		frame := uint64(p * 60)
		for r := 0; r < h; r++ {
			for c := 0; c < to.w; c++ {
				s := static[int(cellHash(seed+frame, r, c, 0)*float64(len(static)))]
				out.set(r, c, s, dim, 1)
			}
		}
	default: // open out
		half := int(easeOutCubic((p-0.55)/0.45) * float64(h) / 2)
		for r := mid - half; r <= mid+half && r < h; r++ {
			if r >= 0 {
				out.rows[r] = append([]fxCell(nil), to.rows[r]...)
			}
		}
	}
	return out
}

// transPlasma: a diagonal band of shifting purple and pink sweeps across,
// leaving the new page behind it.
func transPlasma(from, to *fxGrid, p float64) *fxGrid {
	out := to.clone()
	const band = 10.0
	span := float64(to.w) + float64(to.h)*2 + band
	edge := p * span
	shades := []string{"░", "▒", "▓", "█", "▓", "▒"}
	for r := 0; r < to.h; r++ {
		for c := 0; c < to.w; c++ {
			if sameCell(from.rows[r][c], to.rows[r][c]) {
				continue
			}
			d := float64(c) + float64(r)*2
			switch {
			case d >= edge:
				out.set(r, c, " ", "", 1)
				putCell(out, r, c, from.rows[r][c])
			case d >= edge-band:
				v := (math.Sin(float64(c)*0.35+p*9) + math.Sin(float64(r)*0.6-p*7)) / 2
				col, _ := mixHex("#ff79c6", "#bd93f9", (v+1)/2)
				i := int((edge - d) / band * float64(len(shades)))
				out.set(r, c, shades[min(i, len(shades)-1)], sgrFor(col, "", false), 1)
			}
		}
	}
	return out
}

func repeatRune(r rune, n int) string {
	b := make([]rune, max(n, 0))
	for i := range b {
		b[i] = r
	}
	return string(b)
}
