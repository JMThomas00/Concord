package client

import (
	"math"
	"strings"
)

// Loading screens from the website's light and dark switches (concord-site
// appearance.js). The terminal's own theme decides light or dark, so here
// they're just shows: each plays out, then the usual title, bar and status
// line settle in underneath.

// fillSky tints every cell of the canvas: col, k of the way out of the dark.
func (c *sceneCanvas) fillSky(col string, k float64) {
	if k <= 0.02 {
		return
	}
	v := blend(c.pal.dark, col, math.Min(1, k))
	for r := range c.bg {
		for x := range c.bg[r] {
			c.bg[r][x] = v
		}
	}
}

// arcGrape is the tossed grape's position at f (0..1): an arc from one edge
// to (x1, y1), rising h rows on the way.
func arcGrape(w int, left bool, x1, y1, y0, h, f float64) (float64, float64) {
	x0 := -2.0
	if !left {
		x0 = float64(w) + 2
	}
	return x0 + (x1-x0)*f, y0 + (y1-y0)*f - math.Sin(f*math.Pi)*h
}

// loadFlashbang (Flashbang): a grape is tossed in, goes off, and the
// screen whites out, then fades back as the shrapnel falls.
func loadFlashbang(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	const toss, bang = 0.8, 0.8
	left := cellHash(ls.seed, 0, 11, 1) < 0.5
	x1, y1 := float64(g.w)*0.5, float64(g.h)*0.4
	if el < toss {
		x, y := arcGrape(g.w, left, x1, y1, float64(g.h)*0.8, float64(g.h)*0.3, el/toss)
		c.grape(int(y), int(x))
		*g = *c.finish()
		return
	}
	k := el - bang
	white := 0.0
	switch {
	case k < 0.12:
		white = k / 0.12
	case k < 0.45:
		white = 1
	default:
		white = 1 - easeOutCubic((k-0.45)/1.4)
	}
	c.fillSky(brandFlash, white*0.97)
	c.glow(x1, y1, float64(g.w)*(0.15+0.5*easeOutCubic(k/0.4)), brandWarm, 0.6*(1-easeOutCubic(k/1.6)))
	if k < 2.6 { // the shrapnel
		for i := 0; i < 26; i++ {
			a := cellHash(ls.seed, i, 11, 2) * 2 * math.Pi
			v := 10 + 22*cellHash(ls.seed, i, 11, 3)
			drag := (1 - math.Exp(-k*2)) / 2
			c.grape(int(y1+math.Sin(a)*v*drag*0.5-4*drag+9*k*k), int(x1+math.Cos(a)*v*drag))
		}
	}
	*g = *c.finish()
	if k > 1.3 {
		caption(g, g.h/2-2, p, ls, pal)
	}
}

// loadEclipse (Eclipse): a grape rolls across the sun; the glow of the day
// shrinks away, the stars come out and the corona flares at totality.
func loadEclipse(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	sx, sy := float64(g.w)*0.5, float64(g.h)*0.27
	r := math.Max(3.5, float64(g.h)*0.13)
	roll := easeOutCubic((el - 0.4) / 1.6)
	bx := sx - (sx+r*3)*(1-roll)
	cover := math.Max(0, 1-math.Abs(bx-sx)/(2*r))
	c.glow(sx, sy, float64(g.w)*0.6*(1-cover), "#ffecbe", 0.35*(1-cover)*math.Min(1, el/0.4))
	c.nightStars(el, ls.seed, 60, cover)
	if cover < 1 {
		c.drawSun(sx, sy, r, math.Min(1, el/0.4)*(1-cover*0.2))
	}
	if cover > 0.55 {
		k := (cover - 0.55) / 0.45
		flare := 0.0
		if el > 2 {
			flare = math.Sin(math.Min(1, (el-2)/0.6)*math.Pi) * 0.8
		}
		c.drawEclipse(sx, sy, r, el, k*k, flare)
	}
	c.drawDisc(bx, sy, r, math.Min(1, el/0.4), 0.25*cover)
	*g = *c.finish()
	if cover > 0.9 { // once the day's gone, so its box doesn't cut the sky
		caption(g, g.h*2/3-2, p, ls, pal)
	}
}

// loadMoonrise (Moonrise): the sun sets at the bottom of a dusk sky, night
// comes down from the top, the stars come out and the moon rises.
func loadMoonrise(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	dusk := easeOutCubic(el / 1.2)
	night := easeOutCubic((el - 1.4) / 1.4)
	for r := 0; r < g.h; r++ {
		y := float64(r) / float64(g.h)
		col := blend("#281648", "#e88060", y*y) // dusk purple high, sunset orange low
		k := (0.25 + 0.75*y) * dusk * (1 - night)
		for x := 0; x < g.w; x++ {
			c.tint(r, x, col, k*0.85)
		}
	}
	sunR := math.Max(3, float64(g.h)*0.1)
	if el < 1.6 {
		f := easeOutCubic(el / 1.4)
		c.drawSun(float64(g.w)*0.5, float64(g.h)*0.4+(float64(g.h)*0.6+sunR*2)*f*f, sunR, 1)
	}
	if el > 1.1 {
		c.nightStars(el, ls.seed, 60, math.Min(1, (el-1.1)/0.8))
		mr := math.Max(3.5, float64(g.h)*0.12)
		mx, my := float64(g.w)*0.72, float64(g.h)*0.24
		k := easeOutCubic((el - 1.3) / 1.4)
		c.drawMoon(mx, float64(g.h)+mr*2-(float64(g.h)+mr*2-my)*k, mr, math.Min(1, (el-1.3)/0.3))
	}
	*g = *c.finish()
	if el > 2.2 {
		caption(g, g.h*2/3-2, p, ls, pal)
	}
}

// loadJuice (Juice spill): the shaded grapes drop in and burst; juice
// sprays out and sticks, floods the screen in drips, then drains to a band
// across the top that keeps dripping.
func (a *App) loadJuice(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	const boom = 0.6
	gx, gy := float64(g.w)*0.5, float64(g.h)*0.4
	if el < boom { // the drop
		logo := strings.Split(a.renderGrapeLogo(), "\n")
		f := el / boom
		top := int(-float64(len(logo)) + (gy+float64(len(logo))/2)*f*f)
		*g = *c.finish()
		for r, line := range logo {
			row := parseFrame(line, grapeLogoSize, 1).rows[0]
			for x, cell := range row {
				if cell.w > 0 && !cell.isEmpty() {
					g.set(top+r, int(gx)-grapeLogoSize/2+x, cell.ch, cell.sgr, cell.w)
				}
			}
		}
		return
	}
	f := el - boom
	if f < 0.35 {
		c.glow(gx, gy, float64(g.w)*(0.1+0.4*easeOutCubic(f/0.35)), pal.purple, 0.85*(1-f/0.35))
	}
	// the spray: it flies out, slows and sticks
	for i := 0; i < 40; i++ {
		ang := cellHash(ls.seed, i, 12, 1) * 2 * math.Pi
		v := 8 + 30*cellHash(ls.seed, i, 12, 2)
		k := 1 - math.Exp(-f*5)
		x, y := gx+math.Cos(ang)*v*k, gy+math.Sin(ang)*v*k*0.5+f*0.8
		c.put(int(y), int(x), "●", blend(pal.dark, brandDeep, 0.92), false)
	}
	if f < 1.6 { // bits of grape, falling
		for i := 0; i < 14; i++ {
			ang := cellHash(ls.seed, i, 12, 3) * 2 * math.Pi
			v := 10 + 16*cellHash(ls.seed, i, 12, 4)
			c.grape(int(gy+math.Sin(ang)*v*f*0.5-5*f+12*f*f), int(gx+math.Cos(ang)*v*f))
		}
	}
	// the flood: down past the bottom, then back up to a band that drips
	var drips []juiceDrip
	for i, n := 0, max(8, g.w/8); i < n; i++ {
		drips = append(drips, juiceDrip{
			x:   (float64(i) + cellHash(ls.seed, i, 12, 5)) * float64(g.w) / float64(n),
			w:   1 + cellHash(ls.seed, i, 12, 6)*2,
			len: 2 + 6*cellHash(ls.seed, i, 12, 7) + math.Max(0, f-2)*1.2*cellHash(ls.seed, i, 12, 8),
		})
	}
	band := float64(g.h) * 0.2
	level := -6.0
	switch ff := f - 0.15; {
	case ff < 0:
	case ff < 1.1:
		level = -6 + (float64(g.h)+12)*easeOutCubic(ff/1.1)
	case ff < 1.5:
		level = float64(g.h) + 6
	default:
		level = band + (float64(g.h)+6-band)*(1-easeOutCubic((ff-1.5)/0.9))
	}
	c.paintJuice(func(x int) float64 { return juiceDepth(float64(x), level, el, drips) }, 1)
	*g = *c.finish()
	if f > 2.2 {
		caption(g, g.h*3/5-2, p, ls, pal)
	}
}
