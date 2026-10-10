package client

import (
	"math"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The website's hero scenes (concord-site scenes.js), as terminal art. The
// calm ones are login backgrounds here (atmospheres); the busy ones are
// screensavers (screensaver_site.go), and the site's light/dark switches
// became loading screens and page transitions (loading_site.go,
// transitions_site.go). The brand rules carry over: grapes are always the
// purple emoji, and colours only ever go in glows, links and sparks. A glow
// is a soft background tint mixed from the theme's dark, so every scene
// follows the terminal's theme.

// Brand colours that aren't theme colours (concord-site scenes.js).
const (
	brandDeep  = "#6a3de8" // grape-deep: glows, juice
	brandJuice = "#2a145c" // the top of the juice
	brandWarm  = "#ffd66e" // the lightbulb's glow
	brandFlash = "#fffdf4" // a flashbang's white
	brandMoon  = "#e9e2fa"
	brandSun   = "#ffe7a8"
	brandNight = "#0c0a13"
)

// blend is from moved t of the way to to (mix takes t of its first colour,
// which reads backwards here).
func blend(from, to string, t float64) string { return mix(to, from, t) }

// sceneCanvas draws a scene in layers: background tints (glows, skies) and
// glyphs on top, which keep the tint under them. finish writes it to a grid.
type sceneCanvas struct {
	w, h int
	pal  loadingPalette
	bg   [][]string // background colour per cell ("" for none)
	cell [][]fxCell // glyph per cell (w == -1 for none)
}

func newSceneCanvas(w, h int, pal loadingPalette) *sceneCanvas {
	c := &sceneCanvas{w: w, h: h, pal: pal, bg: make([][]string, h), cell: make([][]fxCell, h)}
	for r := 0; r < h; r++ {
		c.bg[r] = make([]string, w)
		c.cell[r] = make([]fxCell, w)
		for x := range c.cell[r] {
			c.cell[r][x].w = -1
		}
	}
	return c
}

func (c *sceneCanvas) in(r, x int) bool { return r >= 0 && r < c.h && x >= 0 && x < c.w }

// tint sets a cell's background to col mixed k of the way out of the dark,
// keeping a stronger tint already there.
func (c *sceneCanvas) tint(r, x int, col string, k float64) {
	if !c.in(r, x) || k <= 0.02 {
		return
	}
	k = math.Min(1, k)
	if old := c.bg[r][x]; old != "" {
		// blend: the brighter of the two wins, roughly
		c.bg[r][x] = blend(old, col, k*0.6)
		return
	}
	c.bg[r][x] = blend(c.pal.dark, col, k)
}

// glow is a soft round light at (cx, cy) in cells: radius rx columns (rows
// count double, so it's round on screen), strength a at its centre.
func (c *sceneCanvas) glow(cx, cy, rx float64, col string, a float64) {
	if rx < 0.5 {
		return
	}
	for r := int(cy - rx/2 - 1); r <= int(cy+rx/2+1); r++ {
		for x := int(cx - rx - 1); x <= int(cx+rx+1); x++ {
			d := math.Hypot(float64(x)+0.5-cx, (float64(r)+0.5-cy)*2) / rx
			if d < 1 {
				f := 1 - d
				c.tint(r, x, col, a*f*f)
			}
		}
	}
}

// put draws one glyph (width 1) in fg.
func (c *sceneCanvas) put(r, x int, ch, fg string, bold bool) {
	if !c.in(r, x) {
		return
	}
	c.clearGlyph(r, x)
	c.cell[r][x] = fxCell{ch: ch, sgr: fg, w: 1}
	if bold {
		c.cell[r][x].sgr = "b" + fg
	}
}

// grape draws the grape emoji (two columns wide) at r, x.
func (c *sceneCanvas) grape(r, x int) {
	if !c.in(r, x) || !c.in(r, x+1) {
		return
	}
	c.clearGlyph(r, x)
	c.clearGlyph(r, x+1)
	c.cell[r][x] = fxCell{ch: "🍇", w: 2}
	c.cell[r][x+1] = fxCell{w: 0}
}

func (c *sceneCanvas) clearGlyph(r, x int) {
	row := c.cell[r]
	switch {
	case row[x].w == 0 && x > 0 && row[x-1].w == 2:
		row[x-1].w = -1
	case row[x].w == 2 && x+1 < c.w:
		row[x+1].w = -1
	}
	row[x].w = -1
}

// text writes a string of width-1 glyphs.
func (c *sceneCanvas) text(r, x int, s, fg string, bold bool) {
	for _, ch := range s {
		if ch != ' ' {
			c.put(r, x, string(ch), fg, bold)
		}
		x++
	}
}

// halfDisc fills a round shape two pixels per cell (half blocks), so it's
// round on screen: colorAt gives each pixel's colour, or false outside it.
// Pixel coordinates are in cells, y in half-rows.
func (c *sceneCanvas) halfDisc(cx, cy, rx float64, colorAt func(dx, dy float64) (string, bool)) {
	for r := int(cy - rx/2 - 1); r <= int(cy+rx/2+1); r++ {
		for x := int(cx - rx - 1); x <= int(cx+rx+1); x++ {
			if !c.in(r, x) {
				continue
			}
			dx := float64(x) + 0.5 - cx
			top, okT := colorAt(dx/rx, (float64(r)+0.25-cy)*2/rx)
			bot, okB := colorAt(dx/rx, (float64(r)+0.75-cy)*2/rx)
			switch {
			case okT && okB:
				c.put(r, x, "▀", top, false)
				c.bg[r][x] = bot
			case okT:
				c.put(r, x, "▀", top, false)
			case okB:
				c.put(r, x, "▄", bot, false)
			}
		}
	}
}

// finish writes the canvas into a grid of its size.
func (c *sceneCanvas) finish() *fxGrid {
	g := newGrid(c.w, c.h)
	for r := 0; r < c.h; r++ {
		for x := 0; x < c.w; x++ {
			cl := c.cell[r][x]
			bg := c.bg[r][x]
			switch {
			case cl.w > 0:
				fg, bold := cl.sgr, false
				if len(fg) > 0 && fg[0] == 'b' {
					fg, bold = fg[1:], true
				}
				if cl.w == 2 && bg == "" && x+1 < c.w {
					bg = c.bg[r][x+1]
				}
				g.set(r, x, cl.ch, sgrFor(fg, bg, bold), cl.w)
			case cl.w == 0:
				// the second half of a grape, drawn with it
			case bg != "":
				g.set(r, x, " ", sgrFor("", bg, false), 1)
			}
		}
	}
	return g
}

// --- stars and meteors, for the night skies ----------------------------------

// nightStars: twinkling stars over the upper part of the sky.
func (c *sceneCanvas) nightStars(t float64, seed uint64, density float64, a float64) {
	n := int(float64(c.w*c.h) / density)
	for i := 0; i < n; i++ {
		x := int(cellHash(seed, i, 7, 1) * float64(c.w))
		r := int(cellHash(seed, i, 7, 2) * float64(c.h) * 0.85)
		tw := 0.35 + 0.65*(0.5+0.5*math.Sin(t*(0.8+2.2*cellHash(seed, i, 7, 3))+cellHash(seed, i, 7, 4)*7))
		ch := "·"
		if cellHash(seed, i, 7, 5) < 0.12 {
			ch = "✦"
		} else if cellHash(seed, i, 7, 5) < 0.3 {
			ch = "⋆"
		}
		if c.cell[r][x].w < 0 {
			c.put(r, x, ch, faint(c.pal.fg, c.pal, 1-tw*0.8*a), false)
		}
	}
}

// meteor: now and then, a shooting star across the top of the sky.
func (c *sceneCanvas) meteor(t float64, seed uint64) {
	const every, dur = 17.0, 0.9
	k := math.Floor(t / every)
	at := cellHash(seed, int(k), 9, 1) * (every - dur)
	f := (t - k*every - at) / dur
	if f < 0 || f > 1 {
		return
	}
	x0 := (0.3 + 0.6*cellHash(seed, int(k), 9, 2)) * float64(c.w)
	y0 := (0.05 + 0.25*cellHash(seed, int(k), 9, 3)) * float64(c.h)
	hx, hy := x0-f*30, y0+f*6
	for i := 0; i < 7; i++ {
		x, y := hx+float64(i)*2, hy-float64(i)*0.4
		ch := "·"
		if i == 0 {
			ch = "•"
		}
		r, cx := int(y), int(x)
		if c.in(r, cx) && c.cell[r][cx].w < 0 {
			c.put(r, cx, ch, faint(c.pal.fg, c.pal, float64(i)/8+f*0.4), false)
		}
	}
}

// --- login backgrounds (atmospheres) ------------------------------------------

// atmosNetwork (Grape network): grapes drifting as the nodes of a network,
// joined by faint links while they're close, with packets running along
// them now and then.
func atmosNetwork(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	s := uint64(seed)
	n := max(6, min(16, w*h/450))
	xs, ys := make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		xs[i] = float64(w) * (0.5 + 0.47*math.Sin(t*(0.02+0.05*cellHash(s, i, 1, 1))+cellHash(s, i, 1, 2)*7))
		ys[i] = float64(h) * (0.5 + 0.45*math.Sin(t*(0.03+0.05*cellHash(s, i, 1, 3))+cellHash(s, i, 1, 4)*7))
	}
	link := float64(w) * 0.3
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dx, dy := xs[j]-xs[i], (ys[j]-ys[i])*2
			d := math.Hypot(dx, dy)
			if d >= link || d < 3 {
				continue
			}
			f := 1 - d/link
			col := faint(pal.purple, pal, 0.72-0.42*f)
			steps := int(d / 2)
			for k := 1; k < steps; k++ {
				u := float64(k) / float64(steps)
				r, x := int(ys[i]+(ys[j]-ys[i])*u), int(xs[i]+(xs[j]-xs[i])*u)
				if c.in(r, x) && c.cell[r][x].w < 0 {
					c.put(r, x, "·", col, false)
				}
			}
			// a packet on about a third of the links
			if cellHash(s, i*31+j, 2, 1) < 0.35 {
				u := math.Mod(t*0.25+cellHash(s, i*31+j, 2, 2), 1)
				col := pal.fg
				if cellHash(s, i*31+j, 2, 3) < 0.25 {
					col = pal.green
				}
				c.put(int(ys[i]+(ys[j]-ys[i])*u), int(xs[i]+(xs[j]-xs[i])*u), "•", faint(col, pal, 0.2), false)
			}
		}
	}
	for i := 0; i < n; i++ {
		c.grape(int(ys[i]), int(xs[i]))
	}
	return c.finish()
}

// vinePath is one vine's points, grown cell by cell from a seed.
func vinePath(w, h int, seed uint64, k, steps int) [][2]float64 {
	var x, y, a float64
	switch k % 3 {
	case 0: // from the left edge
		x, y, a = 0, float64(h)*(0.25+0.6*cellHash(seed, k, 3, 1)), -0.3+0.4*cellHash(seed, k, 3, 2)
	case 1: // from the bottom
		x, y, a = float64(w)*(0.55+0.4*cellHash(seed, k, 3, 1)), float64(h), -math.Pi/2+0.5*(cellHash(seed, k, 3, 2)-0.5)
	default: // from the right edge
		x, y, a = float64(w), float64(h)*(0.3+0.6*cellHash(seed, k, 3, 1)), math.Pi+0.3-0.4*cellHash(seed, k, 3, 2)
	}
	pts := make([][2]float64, 0, steps)
	ph := cellHash(seed, k, 3, 3) * 100
	for i := 0; i < steps; i++ {
		a += math.Sin(float64(i)/14+ph)*0.09 + math.Sin(float64(i)/5+ph*3)*0.05
		x += math.Cos(a) * 0.9
		y += math.Sin(a) * 0.45
		pts = append(pts, [2]float64{x, y})
	}
	return pts
}

// atmosVine (Grapevine): vines grow in from the edges, put out leaves and
// fruit, and after a while fade and start again.
func atmosVine(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	const cycle = 48.0
	round := int(t / cycle)
	tt := math.Mod(t, cycle)
	s := uint64(seed) + uint64(round)*7919
	fade := 0.0
	if tt > cycle-4 {
		fade = (tt - (cycle - 4)) / 4
	}
	grown := int(tt * 9) // cells a second
	for k := 0; k < 4; k++ {
		pts := vinePath(w, h, s, k, min(grown, 420))
		for i := 1; i < len(pts); i++ {
			x, y := pts[i][0], pts[i][1]
			dx, dy := x-pts[i-1][0], (y-pts[i-1][1])*2
			ch := "─"
			switch ang := math.Atan2(dy, dx); {
			case math.Abs(math.Sin(ang)) > 0.8:
				ch = "│"
			case math.Abs(math.Sin(ang)) > 0.35:
				if (dx > 0) == (dy > 0) {
					ch = "╲"
				} else {
					ch = "╱"
				}
			}
			r, cx := int(y), int(x)
			if c.in(r, cx) && c.cell[r][cx].w < 0 {
				c.put(r, cx, ch, faint(pal.green, pal, 0.62+0.3*fade), false)
			}
			if i%23 == 0 { // a leaf to one side
				side := 1
				if (i/23)%2 == 0 {
					side = -1
				}
				c.put(r+side, cx, "❦", faint(pal.green, pal, 0.4+0.4*fade), false)
			}
			if i%37 == 18 && fade < 0.5 && i < len(pts)-6 { // a bunch, once the vine's past it
				c.grape(r+1, cx)
			}
		}
	}
	return c.finish()
}

// atmosOrbit (Orbit): grapes circling a glowing grape, the near ones in
// front as grapes, the far ones behind as small purple points.
func atmosOrbit(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	s := uint64(seed)
	cx, cy := float64(w)*0.8, float64(h)*0.42
	R := math.Min(float64(w)*0.22, float64(h)*0.8)
	type body struct{ x, y, z float64 }
	var bodies []body
	n := max(10, min(18, w*h/350))
	for i := 0; i < n; i++ {
		r := (0.25 + 0.75*cellHash(s, i, 4, 1)) * R
		sq := 0.3 + 0.2*cellHash(s, i, 4, 2)
		tilt := -0.35 + 0.45*cellHash(s, i, 4, 3)
		dir := 1.0
		if cellHash(s, i, 4, 4) < 0.15 {
			dir = -1
		}
		a := cellHash(s, i, 4, 5)*2*math.Pi + dir*t*0.35*math.Pow(0.3*R/r, 1.5)
		ex, ey := math.Cos(a)*r, math.Sin(a)*r*sq
		bodies = append(bodies, body{cx + ex*math.Cos(tilt) - ey*math.Sin(tilt), cy + (ex*math.Sin(tilt)+ey*math.Cos(tilt))/2, math.Sin(a)})
	}
	sort.Slice(bodies, func(i, j int) bool { return bodies[i].z < bodies[j].z })
	c.glow(cx, cy, R*0.5, brandDeep, 0.55)
	c.glow(cx, cy, R*0.18, pal.purple, 0.35)
	sunDrawn := false
	for _, b := range bodies {
		if !sunDrawn && b.z > 0 {
			c.grape(int(cy), int(cx)-1)
			sunDrawn = true
		}
		if b.z > 0 {
			c.grape(int(b.y), int(b.x))
		} else {
			c.put(int(b.y), int(b.x), "•", faint(pal.purple, pal, 0.55-0.3*(b.z+1)), false)
		}
	}
	if !sunDrawn {
		c.grape(int(cy), int(cx)-1)
	}
	return c.finish()
}

// atmosGalaxy (Grape galaxy): a slow three-armed spiral of purple points,
// a few of them grapes, round a soft glow.
func atmosGalaxy(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	s := uint64(seed)
	cx, cy := float64(w)*0.72, float64(h)*0.44
	R := math.Min(float64(w)*0.3, float64(h)*1.1)
	c.glow(cx, cy, R*0.45, brandDeep, 0.5)
	c.glow(cx, cy, R*0.14, pal.purple, 0.4)
	tints := []string{pal.purple, pal.purple, pal.purple, pal.pink, pal.cyan}
	for i := 0; i < 160; i++ {
		f := math.Pow(cellHash(s, i, 5, 1), 0.7)
		arm := i % 3
		a := float64(arm)*2*math.Pi/3 + f*3.4 + (cellHash(s, i, 5, 2)-0.5)*0.7 + t*0.1*(1-f*0.6)
		ex, ey := math.Cos(a)*f*R, math.Sin(a)*f*R*0.5
		x, y := cx+ex*0.94-ey*0.34, cy+(ex*0.34+ey*0.94)/2
		r, xc := int(y), int(x)
		if i%17 == 0 && f < 0.75 {
			c.grape(r, xc)
			continue
		}
		ch := "·"
		if f < 0.35 {
			ch = "•"
		} else if f < 0.6 {
			ch = "∙"
		}
		c.put(r, xc, ch, faint(tints[i%len(tints)], c.pal, 0.25+0.6*f), false)
	}
	return c.finish()
}

// atmosRows (Vineyard rows): a field of points that ripples like a
// vineyard in the wind, a grape on each crest.
func atmosRows(w, h int, t float64, pal loadingPalette) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	for r, row := 1, 0; r < h; r, row = r+3, row+1 {
		for x := (row % 2) * 4; x < w-1; x += 8 {
			v := math.Sin(float64(x)*0.07+t*0.8)*0.6 + math.Cos(float64(r)*0.32+t*0.55)*0.4
			switch {
			case v > 0.82:
				c.grape(r, x)
			case v > 0.35:
				c.put(r, x, "•", faint(pal.purple, pal, 0.45-0.2*v), false)
			case v > -0.3:
				c.put(r, x, "∙", faint(pal.purple, pal, 0.65), false)
			default:
				c.put(r, x, "·", faint(pal.dim, pal, 0.6), false)
			}
		}
	}
	return c.finish()
}

// atmosStarlings (Starlings): a murmuration of grapes, the flock turning,
// stretching and folding as it crosses the sky.
func atmosStarlings(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	s := uint64(seed)
	fx := func(tt float64) (float64, float64) {
		return float64(w) * (0.5 + 0.38*math.Sin(tt*0.09)), float64(h) * (0.42 + 0.28*math.Sin(tt*0.14+1))
	}
	x0, y0 := fx(t)
	x1, y1 := fx(t + 0.5)
	head := math.Atan2((y1-y0)*2, x1-x0)
	rx := float64(w) * 0.11 * (1 + 0.45*math.Sin(t*0.23))
	ry := float64(h) * 0.13 * (1 + 0.35*math.Cos(t*0.19))
	n := max(14, min(26, w*h/260))
	for i := 0; i < n; i++ {
		phi := cellHash(s, i, 6, 1)*2*math.Pi + 0.4*math.Sin(t*0.5+float64(i))
		rho := math.Sqrt(cellHash(s, i, 6, 2))
		ox, oy := math.Cos(phi)*rho*rx, math.Sin(phi)*rho*ry*2
		x := x0 + ox*math.Cos(head) - oy*math.Sin(head)
		y := y0 + (ox*math.Sin(head)+oy*math.Cos(head))/2
		if i%3 == 0 {
			c.grape(int(y), int(x))
		} else {
			c.put(int(y), int(x), "•", faint(pal.purple, pal, 0.35+0.3*cellHash(s, i, 6, 3)), false)
		}
	}
	return c.finish()
}

// bulbGeom is where the Lightbulb hangs: its cord's column, the row the
// bulb's top hangs at, and its swing (columns off the cord's line).
func bulbGeom(w, h int, t float64) (col, top, swing int) {
	col = min(w-8, int(float64(w)*0.8))
	top = max(2, h*14/100)
	swing = int(math.Round(math.Sin(t*0.7) * 1.2))
	return
}

var bulbArt = []string{
	"   ▐█▌   ",
	"   ▐█▌   ",
	"  ╭┘ └╮  ",
	" ╱ │ │ ╲ ",
	"│  ╰∿╯  │",
	"│       │",
	" ╲     ╱ ",
	"  ╰───╯  ",
}

// atmosBulb (Lightbulb): a clear incandescent bulb hanging on its cord,
// lighting the corner of the screen, with grapes circling it like moths.
// When the pull chain has been pulled (transitions_site.go), it's dark and
// the moths doze at the bottom of the screen until it flickers back on.
func atmosBulb(w, h int, t float64, seed uint32, pal loadingPalette, sincePop float64) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	s := uint64(seed)
	col, top, swing := bulbGeom(w, h, t)
	on := 1.0
	asleep := 0.0
	const darkFor = 14.0
	if sincePop >= 0 && sincePop < darkFor+1 {
		switch {
		case sincePop < darkFor:
			on, asleep = 0, math.Min(1, sincePop/2)
		default: // flickering back on
			k := sincePop - darkFor
			if math.Mod(k*7, 1) < 0.5 || k > 0.75 {
				on = 1
			} else {
				on = 0
			}
			asleep = 1 - k
		}
	}
	bx := col + swing - len([]rune(bulbArt[0]))/2
	gx, gy := float64(col+swing), float64(top+4)
	if on > 0 {
		c.glow(gx, gy, 26, brandWarm, 0.3*on)
		c.glow(gx, gy, 9, "#fff4d6", 0.45*on)
	}
	for r := 0; r < top; r++ { // the cord
		x := col + int(math.Round(float64(swing)*float64(r)/float64(top)))
		c.put(r, x, "│", faint(pal.dim, pal, 0.3), false)
	}
	glass := faint(pal.fg, pal, 0.45)
	filament := faint(pal.dim, pal, 0.2)
	if on > 0 {
		glass = blend(pal.fg, brandWarm, 0.3)
		filament = "#fff4c6"
	}
	for i, line := range bulbArt {
		for j, ch := range []rune(line) {
			if ch == ' ' {
				continue
			}
			fg := glass
			switch {
			case i < 2:
				fg = blend("#8a8d98", "#a3a4ae", float64(j%2))
			case ch == '∿':
				fg = filament
			case i == 4 && (ch == '╰' || ch == '╯'):
				fg = faint(pal.dim, pal, 0.1)
			}
			c.put(top+i, bx+j, string(ch), fg, ch == '∿' && on > 0)
		}
	}
	// the moths
	n := max(5, min(9, w*h/700))
	for i := 0; i < n; i++ {
		dir := 1.0
		if i%2 == 1 {
			dir = -1
		}
		a := cellHash(s, i, 8, 1)*7 + t*(0.7+0.6*cellHash(s, i, 8, 2))*dir
		rx := 7 + 9*cellHash(s, i, 8, 3) + 2*math.Sin(t*1.3+float64(i))
		ry := 3 + 3*cellHash(s, i, 8, 4) + math.Sin(t*1.7+float64(i)*2)
		x, y := gx+math.Cos(a)*rx, gy+math.Sin(a)*ry
		if asleep > 0 { // drifting down to doze along the bottom
			fx := gx + (cellHash(s, i, 8, 5)-0.5)*float64(w)*0.5
			x = x + (fx-x)*asleep
			y = y + (float64(h-1)-y)*asleep
			if asleep >= 1 && math.Mod(t*0.5+float64(i)*0.37, 3) < 1.2 && i%2 == 0 {
				k := math.Mod(t*0.5+float64(i)*0.37, 3) / 1.2
				c.put(h-2-int(k*3), int(x)+2+int(k*2), "z", faint(pal.purple, pal, k*0.7), true)
			}
		}
		c.grape(int(y), int(x))
	}
	return c.finish()
}

// atmosMoon (Moonrise): a night sky with the moon up, stars twinkling, the
// odd shooting star, and grapes drifting past like clouds.
func atmosMoon(w, h int, t float64, seed uint32, pal loadingPalette) *fxGrid {
	c := newSceneCanvas(w, h, pal)
	s := uint64(seed)
	c.nightStars(t, s, 70, 1)
	c.meteor(t, s)
	mx, my := float64(w)*0.82, float64(h)*0.24
	mr := math.Max(4, float64(h)*0.16)
	c.drawMoon(mx, my, mr, 1)
	for i := 0; i < 4; i++ {
		span := float64(w + 8)
		x := math.Mod(t*(0.6+0.8*cellHash(s, i, 10, 1))+cellHash(s, i, 10, 2)*span, span) - 4
		y := float64(h) * (0.25 + 0.6*cellHash(s, i, 10, 3))
		c.grape(int(y), int(x))
	}
	return c.finish()
}

// drawMoon: a lavender moon with craters and a soft halo.
func (c *sceneCanvas) drawMoon(x, y, r, a float64) {
	c.glow(x, y, r*2.6, "#d6c8ff", 0.22*a)
	craters := [][3]float64{{-.35, -.15, .22}, {.25, .3, .16}, {.3, -.35, .1}, {-.1, .45, .09}}
	c.halfDisc(x, y, r, func(dx, dy float64) (string, bool) {
		if dx*dx+dy*dy > 1 {
			return "", false
		}
		col := blend(brandMoon, "#cbbcec", (dx+dy+1.4)/2.8)
		for _, k := range craters {
			if math.Hypot(dx-k[0], dy-k[1]) < k[2] {
				col = blend(col, "#9680c8", 0.45)
			}
		}
		return blend(c.pal.dark, col, a), true
	})
}

// drawCorona: the eclipse's corona round (x, y). flare brightens it (the
// moment of totality).
func (c *sceneCanvas) drawCorona(x, y, r, t, a, flare float64) {
	c.glow(x, y, r*(3.2+flare), "#d6c4ff", 0.22*a)
	for i := 0; i < 12; i++ {
		ang := float64(i)/12*2*math.Pi + math.Sin(t/2.4+float64(i))*0.12
		l := r * (1.5 + 0.5*math.Sin(t/0.9+float64(i)*1.7) + flare*0.6)
		c.glow(x+math.Cos(ang)*l*0.55, y+math.Sin(ang)*l*0.55/2, r*0.55, "#f0e6ff", 0.14*a)
	}
	c.glow(x, y, r*1.25, "#fffaee", 0.45*a)
}

// The eclipsing grapes are the logo's own model (grape_logo_data.go): its
// spheres for the berries, its leaf and stem from the logo's top rows. In
// units of the body's radius, a point maps to logo pixels around the logo's
// middle (the logo's pixels are square, like the half-block pixels here).
const (
	eclipseLogoCX, eclipseLogoCY = 223.0, 268.0
	eclipseLogoScale             = 268.0 // logo pixels per unit
)

// grapeModelAt is the logo model at (dx, dy): whether it's in the bunch,
// the surface normal there (z toward the viewer), and whether it's the leaf
// or stem (whose normal is flat, edge in its x: 1 at its outline).
func grapeModelAt(dx, dy float64) (in bool, n [3]float64, leaf bool) {
	L := grapeLogos[grapeLogoSize]
	x, y := eclipseLogoCX+dx*eclipseLogoScale, eclipseLogoCY+dy*eclipseLogoScale
	bz := math.Inf(-1)
	for _, g := range L.grapes {
		ex, ey := (x-g[0])/g[2], (y-g[1])/g[2]
		if d := ex*ex + ey*ey; d < 1 {
			z := math.Sqrt(1 - d)
			if depth := g[3] + g[2]*z; depth > bz {
				bz, n = depth, [3]float64{ex, ey, z}
			}
		}
	}
	if !math.IsInf(bz, -1) {
		return true, n, false
	}
	// the leaf and stem: the logo's coloured cells, with an edge where a
	// neighbouring cell is empty
	toneAt := func(x, y float64) bool {
		r, c := int(math.Floor(y/L.ch)), int(math.Floor(x/L.cw))
		return r >= 0 && r < len(L.tones) && c >= 0 && c < len(L.tones[r]) && L.tones[r][c] != ' '
	}
	if !toneAt(x, y) {
		return false, n, false
	}
	e := 0.0
	for _, o := range [][2]float64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		if !toneAt(x+o[0]*L.cw*0.5, y+o[1]*L.ch*0.35) {
			e = 1
		}
	}
	return true, [3]float64{e, 0, 0}, true
}

// drawDisc: the eclipsing grapes, backlit: each berry a dark purple sphere,
// faintly lit from the front like the logo, its edge glowing where it curves
// away, which outlines every grape, even one in front of another. rim (0..1)
// is how much light leaks round.
func (c *sceneCanvas) drawDisc(x, y, r, a, rim float64) {
	core := blend(c.pal.dark, "#160d28", 0.92*a)
	leafCore := blend(c.pal.dark, "#0f1a14", 0.92*a)
	c.halfDisc(x, y, r, func(dx, dy float64) (string, bool) {
		in, n, leaf := grapeModelAt(dx, dy)
		if !in {
			return "", false
		}
		if leaf {
			return blend(leafCore, "#d8f0e0", n[0]*0.5*rim*a), true
		}
		fill := math.Max(0, n[0]*grapeLight0[0]+n[1]*grapeLight0[1]+n[2]*grapeLight0[2])
		col := blend(core, brandDeep, 0.35*fill*fill*a) // the grape's own purple, barely lit
		edge := math.Pow(math.Max(0, (1-n[2]-0.3)/0.7), 1.3)
		return blend(col, "#e6dcff", math.Min(1, edge*rim*1.5)*a), true
	})
}

// drawSun: the sun with its glow.
func (c *sceneCanvas) drawSun(x, y, r, a float64) {
	c.glow(x, y, r*3.6, "#ffecbe", 0.3*a)
	c.halfDisc(x, y, r, func(dx, dy float64) (string, bool) {
		if dx*dx+dy*dy > 1 {
			return "", false
		}
		return blend(c.pal.dark, blend("#fffdf0", brandSun, math.Hypot(dx+0.2, dy+0.2)), a), true
	})
}

// sinceBulbPop is how long since the pull chain popped the bulb (negative:
// it hasn't, or not lately).
func (a *App) sinceBulbPop(now time.Time) float64 {
	if a.fx.bulbPopAt.IsZero() {
		return -1
	}
	return now.Sub(a.fx.bulbPopAt).Seconds()
}

// sceneObject is the screen area a background's centrepiece takes up on the
// right (the moon, the lightbulb and its glow, the orbit's and the galaxy's
// cores), for the banner to keep clear of; false for a background without one.
func sceneObject(kind string, w, h int) (x0, y0, x1, y1 int, ok bool) {
	fw, fh := float64(w), float64(h)
	switch kind {
	case "moonrise":
		mx, my, mr := fw*0.82, fh*0.24, math.Max(4, fh*0.16)
		return int(mx - mr - 2), int(my - mr/2 - 1), int(mx + mr + 2), int(my + mr/2 + 1), true
	case "lightbulb":
		col, top, _ := bulbGeom(w, h, 0)
		return col - 10, 0, col + 10, top + len(bulbArt) + 2, true
	case "orbit":
		cx, cy := fw*0.8, fh*0.42
		R := math.Min(fw*0.22, fh*0.8) * 0.5
		return int(cx - R), int(cy - R/2), int(cx + R), int(cy + R/2), true
	case "galaxy":
		cx, cy := fw*0.72, fh*0.44
		R := math.Min(fw*0.3, fh*1.1) * 0.45
		return int(cx - R), int(cy - R/2), int(cx + R), int(cy + R/2), true
	}
	return 0, 0, 0, 0, false
}

// sceneClearance returns a test for whether banner i, where the login stage
// puts it, stays clear of this launch's background object (always true when
// there isn't one). The placement is worked out once, for all the banners.
func (a *App) sceneClearance() func(i int) bool {
	all := func(int) bool { return true }
	x0, y0, x1, y1, ok := sceneObject(a.pick(layerAtmosphere), a.width, a.height)
	if !ok {
		return all
	}
	var below string
	var stable int
	switch a.view {
	case ViewLogin:
		below, stable = a.loginFormBlock()
	case ViewRegister:
		below, stable = a.registerFormBlock()
	default:
		return all
	}
	g := a.logoLockupFor(stable)
	p := a.lockupPlace(g, lipgloss.Width(below), lipgloss.Height(below), stable)
	return func(i int) bool {
		if i < 0 || i >= len(bannerDims) {
			return true
		}
		bw, bh := min(bannerDims[i][0], g.boxW), min(bannerDims[i][1], g.slot)
		bx0, by0 := p.bannerCol, p.bannerBottom-bh
		return bx0+bw+1 < x0 || bx0 > x1 || p.bannerBottom <= y0 || by0 > y1
	}
}

// anyBanner reports whether any banner passes ok.
func (a *App) anyBanner(ok func(int) bool) bool {
	for i := range banners {
		if ok(i) {
			return true
		}
	}
	return false
}
