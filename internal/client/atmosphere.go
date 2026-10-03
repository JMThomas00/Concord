package client

import (
	"math"
	"strings"
	"time"
)

// Atmospheres are the login stage's backgrounds, drawn behind the page and
// kept clear of its text (fxGrid.underlay). Each is a pure function of the
// time since the stage appeared, so it costs nothing between frames.

// atmosphereMoving reports whether this launch's background animates.
func (a *App) atmosphereMoving() bool {
	if a.pick(layerLight) == "disco" {
		return true // the reflections sweep round
	}
	switch a.pick(layerAtmosphere) {
	case "", "none", "dots":
		return false
	}
	return true
}

// renderAtmosphere draws the background for now, or nil for none.
func (a *App) renderAtmosphere(now time.Time) *fxGrid {
	t := now.Sub(a.fx.stageAt).Seconds()
	pal := a.loadingPalette()
	bg := a.baseAtmosphere(a.pick(layerAtmosphere), t, pal)
	if a.pick(layerLight) == "disco" {
		if bg == nil {
			bg = newGrid(a.width, a.height)
		}
		discoReflections(bg, t, pal)
	}
	return bg
}

func (a *App) baseAtmosphere(kind string, t float64, pal loadingPalette) *fxGrid {
	switch kind {
	case "grapes":
		return atmosGrapes(a.width, a.height, t, a.mood.seed)
	case "stars":
		return atmosStars(a.width, a.height, t, a.mood.seed, pal)
	case "dots":
		return atmosDots(a.width, a.height, pal)
	case "leaves":
		return atmosLeaves(a.width, a.height, t, a.mood.seed, pal)
	case "lava":
		return atmosLava(a.width, a.height, t, a.mood.seed, pal)
	}
	return nil
}

// atmosGrapes: a few grapes drifting slowly up the screen, swaying, like
// the concord-site hero. Emoji can't be dimmed, so there are few of them.
func atmosGrapes(w, h int, t float64, seed uint32) *fxGrid {
	g := newGrid(w, h)
	n := max(3, w*h/450)
	for i := 0; i < n; i++ {
		x0 := cellHash(uint64(seed), i, 0, 1) * float64(w-4)
		speed := 0.6 + cellHash(uint64(seed), i, 0, 2)*0.9 // rows per second
		phase := cellHash(uint64(seed), i, 0, 3) * float64(h+4)
		sway := cellHash(uint64(seed), i, 0, 4) * 2 * math.Pi
		y := float64(h+2) - math.Mod(t*speed+phase, float64(h+4))
		x := x0 + 2*math.Sin(t*0.5+sway)
		g.set(int(y), int(x)+1, "🍇", "", 2)
	}
	return g
}

// faint mixes a colour most of the way into the dark, so backgrounds stay
// in the background.
func faint(col string, pal loadingPalette, k float64) string { return mix(pal.dark, col, k) }

// atmosStars: a slow warp through a braille starfield, stars brightening
// as they near the edges.
func atmosStars(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	g := newGrid(w, h)
	cx, cy := float64(w)/2, float64(h)/2
	dots := []string{"⠁", "⠂", "⠄", "⠈", "⠐", "⠠", "⡀", "⢀"}
	n := w * h / 40
	for i := 0; i < n; i++ {
		ang := cellHash(uint64(seed), i, 0, 1) * 2 * math.Pi
		speed := 0.04 + cellHash(uint64(seed), i, 0, 2)*0.08
		d := math.Mod(t*speed+cellHash(uint64(seed), i, 0, 3), 1) // 0 at the centre, 1 at the edge
		d = d * d
		x := cx + math.Cos(ang)*d*cx*1.1
		y := cy + math.Sin(ang)*d*cy*1.1
		ch := dots[i%len(dots)]
		if d > .55 {
			ch = "·"
		}
		if d > .85 {
			ch = "•"
		}
		g.set(int(y), int(x), ch, sgrFor(faint(pal.fg, pal, .15+.6*d), "", false), 1)
	}
	return g
}

// atmosDots: a still dot grid, like graph paper.
func atmosDots(w, h int, pal loadingPalette) *fxGrid {
	g := newGrid(w, h)
	s := sgrFor(faint(pal.dim, pal, .45), "", false)
	for r := 1; r < h; r += 2 {
		for c := 2; c < w; c += 4 {
			g.set(r, c, "·", s, 1)
		}
	}
	return g
}

// atmosLeaves: grape leaves drifting down, rocking as they fall.
func atmosLeaves(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	g := newGrid(w, h)
	shapes := []string{"❦", "❧", "☙"}
	n := max(4, w*h/300)
	for i := 0; i < n; i++ {
		speed := 0.8 + cellHash(uint64(seed), i, 1, 2)*1.2
		phase := cellHash(uint64(seed), i, 1, 3) * float64(h+4)
		y := math.Mod(t*speed+phase, float64(h+4)) - 2
		x := cellHash(uint64(seed), i, 1, 1)*float64(w) + 4*math.Sin(t*0.8+float64(i))
		col := faint(pal.green, pal, .35+.35*cellHash(uint64(seed), i, 1, 4))
		if cellHash(uint64(seed), i, 1, 5) < .25 {
			col = faint("#ffb86c", pal, .5) // an autumn one
		}
		g.set(int(y), int(x), shapes[i%len(shapes)], sgrFor(col, "", false), 1)
	}
	return g
}

// atmosLava: a lava lamp's glow along the bottom of the screen.
func atmosLava(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	g := newGrid(w, h)
	const rows = 6
	top := h - rows
	H := float64(rows * 2)
	type blob struct{ x, y, r float64 }
	var blobs []blob
	for i := 0; i < 5+w/25; i++ {
		x := float64(w)*cellHash(uint64(seed), i, 2, 1) + math.Sin(t*.3+float64(i))*6
		y := H*.55 + math.Sin(t*(.25+.2*cellHash(uint64(seed), i, 2, 2))+float64(i)*2)*H*.45
		blobs = append(blobs, blob{x, y, 2 + cellHash(uint64(seed), i, 2, 3)*2.5})
	}
	lit := func(x, y float64) bool {
		f := 0.0
		if y > H-2 {
			f += (y - (H - 2)) / 1.5
		}
		for _, b := range blobs {
			dx, dy := x-b.x, y-b.y
			f += b.r * b.r / (dx*dx + dy*dy + .01)
		}
		return f > 1
	}
	colour := func(y float64) string { return faint(mix(pal.pink, pal.purple, y/H), pal, .45) }
	for r := 0; r < rows; r++ {
		for c := 0; c < w; c++ {
			tp, bt := lit(float64(c), float64(r*2)), lit(float64(c), float64(r*2+1))
			switch {
			case tp && bt:
				g.set(top+r, c, "▀", sgrFor(colour(float64(r*2)), colour(float64(r*2+1)), false), 1)
			case tp:
				g.set(top+r, c, "▀", sgrFor(colour(float64(r*2)), "", false), 1)
			case bt:
				g.set(top+r, c, "▄", sgrFor(colour(float64(r*2+1)), "", false), 1)
			}
		}
	}
	return g
}

// drawAccents adds the mood's frame accents: faint hairlines along the top
// and bottom, or HUD corner brackets with the mood code. Only empty cells
// are drawn on.
func (a *App) drawAccents(g *fxGrid) {
	kind := a.pick(layerAccent)
	if kind == "" || kind == "none" || g.w < 20 || g.h < 6 {
		return
	}
	pal := a.loadingPalette()
	s := sgrFor(faint(pal.dim, pal, .55), "", false)
	put := func(r, c int, text string) {
		for _, ch := range text {
			if c >= 0 && c < g.w && g.rows[r][c].isEmpty() {
				g.set(r, c, string(ch), s, 1)
			}
			c++
		}
	}
	switch kind {
	case "hairlines":
		line := strings.Repeat("─", g.w-8)
		put(0, 4, line)
		put(g.h-1, 4, line)
	case "brackets":
		const arm = "────"
		put(0, 1, "┌"+arm)
		put(0, g.w-6, arm+"┐")
		put(g.h-1, 1, "└"+arm)
		put(g.h-1, g.w-6, arm+"┘")
		put(1, 3, "CONCORD // "+a.mood.code())
	}
}

// discoReflections: spots of coloured light thrown off the mirror balls,
// sweeping round the room.
func discoReflections(g *fxGrid, t float64, pal loadingPalette) {
	cx, cy := float64(g.w)*.3, float64(g.h)*.45
	cols := []string{pal.pink, pal.cyan, pal.yellow, pal.green, pal.purple, "#ffffff"}
	for i := 0; i < 64; i++ {
		ang := float64(i)*2.399 + t*.55 // golden-angle spread, turning
		rad := 6 + float64(i%9)*float64(g.w)/14
		x := cx + math.Cos(ang)*rad
		y := cy + math.Sin(ang)*rad*.45
		ch := []string{"•", "✦", "✦", "∙"}[i%4]
		col := cols[(i+int(t*2))%len(cols)]
		if r, c := int(y), int(x); r >= 0 && r < g.h && c >= 0 && c < g.w && g.rows[r][c].isEmpty() {
			g.set(r, c, ch, sgrFor(col, "", i%4 == 1), 1)
		}
	}
}
