package client

import (
	"math"
	"sort"
	"strings"
	"time"
)

// Screensavers from the website's hero scenes (concord-site scenes.js): the
// busy ones, which would crowd a login form. Each is a function of the time
// since it started, except the bouncing grapes, which keep a position.

// saverWarp (Warp speed): flying through a field of grapes, the far ones
// specks, the near ones grapes.
func saverWarp(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	cx := float64(g.w) * (0.55 + 0.08*math.Sin(el*0.21))
	cy := float64(g.h) * (0.45 + 0.08*math.Sin(el*0.17+1))
	type star struct{ x, y, z float64 }
	var stars []star
	for i := 0; i < 110; i++ {
		z := 1 - math.Mod(el*0.22+cellHash(seed, i, 1, 1), 1)
		f := 1 / math.Max(z, 0.05)
		x := cx + (cellHash(seed, i, 1, 2)*2-1)*f*float64(g.w)*0.08
		y := cy + (cellHash(seed, i, 1, 3)*2-1)*f*float64(g.h)*0.08
		stars = append(stars, star{x, y, z})
	}
	sort.Slice(stars, func(i, j int) bool { return stars[i].z > stars[j].z })
	for _, s := range stars {
		switch {
		case s.z > 0.65:
			c.put(int(s.y), int(s.x), "·", faint(pal.purple, pal, 0.3+0.5*s.z), false)
		case s.z > 0.35:
			c.put(int(s.y), int(s.x), "•", faint(pal.purple, pal, 0.25), false)
		default:
			c.grape(int(s.y), int(s.x))
		}
	}
	*g = *c.finish()
}

// saverHarvest (Harvest): grapes fall and pile up along the bottom, each
// rolling to the lower side of where it lands; then the bins empty and it
// starts over.
func saverHarvest(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	const cycle, every, grav = 75.0, 0.22, 40.0
	round := int(el / cycle)
	t := math.Mod(el, cycle)
	s := seed + uint64(round)*104729
	cols := g.w / 2
	if cols < 2 {
		return
	}
	heights := make([]int, cols)
	n := min(int(math.Floor((cycle-12)/every)), cols*(g.h*2/3))
	drain := 0.0
	if t > cycle-5 {
		drain = (t - (cycle - 5)) * 12 // the floor opens
	}
	for i := 0; i < n; i++ {
		ti := float64(i) * every
		if ti > t {
			break
		}
		k := int(cellHash(s, i, 2, 1) * float64(cols))
		// roll to the lowest of the neighbours
		for _, d := range []int{-1, 1, -2, 2} {
			if j := k + d; j >= 0 && j < cols && heights[j] < heights[k]-1 {
				k = j
			}
		}
		floor := float64(g.h - 1 - heights[k])
		y := 0.5 * grav * (t - ti) * (t - ti)
		if y >= floor {
			y = floor
			heights[k]++
		}
		c.grape(int(y+drain), k*2)
	}
	*g = *c.finish()
}

// saverSnake (Grape snake): chains of grapes wandering the screen.
func saverSnake(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	for k := 0; k < 3; k++ {
		p1, p2 := cellHash(seed, k, 3, 1)*7, cellHash(seed, k, 3, 2)*7
		speed := 0.7 + 0.3*cellHash(seed, k, 3, 3)
		at := func(s float64) (int, int) {
			x := float64(g.w) * (0.5 + 0.44*math.Sin(0.37*s+p1)*math.Cos(0.11*s+p2))
			y := float64(g.h) * (0.5 + 0.42*math.Sin(0.29*s+p2))
			return int(y), int(x)
		}
		for i := 15; i >= 0; i-- {
			r, x := at(el*speed - float64(i)*0.1)
			c.grape(r, x)
		}
	}
	*g = *c.finish()
}

// saverFireworks (Fireworks): rockets rise and burst into grapes and sparks.
func saverFireworks(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	const every, rise, burst, grav = 1.4, 1.1, 2.6, 7.0
	sparkCols := []string{pal.purple, pal.fg, pal.green, pal.pink, pal.yellow}
	for k := int((el - rise - burst) / every); k <= int(el/every); k++ {
		if k < 0 {
			continue
		}
		t0 := float64(k)*every + cellHash(seed, k, 4, 1)*0.6
		age := el - t0
		if age < 0 || age > rise+burst {
			continue
		}
		bx := float64(g.w) * (0.12 + 0.76*cellHash(seed, k, 4, 2))
		by := float64(g.h) * (0.15 + 0.35*cellHash(seed, k, 4, 3))
		if age < rise { // the rocket and its trail
			f := easeOutCubic(age / rise)
			y := float64(g.h) - (float64(g.h)-by)*f
			for i := 0; i < 4; i++ {
				c.put(int(y)+i, int(bx), "│", faint(pal.green, pal, 0.2+0.2*float64(i)), i == 0)
			}
			continue
		}
		tau := age - rise
		if tau < 0.35 {
			c.glow(bx, by, 10*(0.5+tau*3), pal.purple, 0.5*(1-tau/0.35))
		}
		n := 10 + int(cellHash(seed, k, 4, 4)*6)
		for i := 0; i < n; i++ {
			a := float64(i)/float64(n)*2*math.Pi + cellHash(seed, k*31+i, 4, 5)*0.3
			v := 5 + 4*cellHash(seed, k*31+i, 4, 6)
			drag := (1 - math.Exp(-tau*1.6)) / 1.6
			x := bx + math.Cos(a)*v*drag*2
			y := by + math.Sin(a)*v*drag + 0.5*grav*0.4*tau*tau
			if tau < burst-0.5 || math.Mod(tau*8, 1) < 0.5 { // they blink out at the end
				c.grape(int(y), int(x))
			}
		}
		for i := 0; i < 18; i++ {
			a := cellHash(seed, k*53+i, 5, 1) * 2 * math.Pi
			v := 3 + 9*cellHash(seed, k*53+i, 5, 2)
			drag := (1 - math.Exp(-tau*2.5)) / 2.5
			life := 1 - tau/1.4
			if life <= 0 {
				break
			}
			ch := "✦"
			if life < 0.5 {
				ch = "·"
			}
			c.put(int(by+math.Sin(a)*v*drag+grav*0.3*tau*tau), int(bx+math.Cos(a)*v*drag*2), ch,
				faint(sparkCols[i%len(sparkCols)], pal, 1-life), false)
		}
	}
	*g = *c.finish()
}

// saverPop (Pop): grapes rising like bubbles, each popping into a splash.
func saverPop(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	n := max(10, g.w*g.h/140)
	span := float64(g.h + 6)
	for i := 0; i < n; i++ {
		v := 2.5 + 4*cellHash(seed, i, 6, 1)
		pos := el*v + cellHash(seed, i, 6, 2)*span
		round := int(pos / span)
		y := float64(g.h+3) - math.Mod(pos, span)
		x := cellHash(seed, i*977+round, 6, 3)*float64(g.w-4) + 1.5*math.Sin(el*0.9+float64(i))
		popAt := float64(g.h) * (0.08 + 0.6*cellHash(seed, i*977+round, 6, 4))
		if y > popAt {
			c.grape(int(y), int(x))
			continue
		}
		since := (popAt - y) / v // seconds since it popped
		if since > 0.45 {
			continue
		}
		rad := 1 + since*8
		for k := 0; k < 8; k++ {
			a := float64(k) / 8 * 2 * math.Pi
			ch := "°"
			if since > 0.25 {
				ch = "·"
			}
			c.put(int(popAt+math.Sin(a)*rad/2), int(x+1+math.Cos(a)*rad), ch, faint(pal.purple, pal, since*1.6), false)
		}
	}
	*g = *c.finish()
}

// juiceDepth is how far down the juice reaches in column x: a rolling band
// (base, in rows) with drips hanging from it.
func juiceDepth(x, base float64, t float64, drips []juiceDrip) float64 {
	y := base + 1.2*math.Sin(x*0.06+t/1.7) + 0.7*math.Sin(x*0.17-t/1.1)
	for _, d := range drips {
		if k := (x - d.x) / d.w; math.Abs(k) < 1 {
			y = math.Max(y, base+d.len*math.Sqrt(1-k*k))
		}
	}
	return y
}

type juiceDrip struct{ x, w, len float64 }

// paintJuice fills the juice down to each column's depth, deep purple
// from the top, the edge in half blocks so it's smooth.
func (c *sceneCanvas) paintJuice(depth func(x int) float64, a float64) {
	for x := 0; x < c.w; x++ {
		d := depth(x)
		full := int(d)
		for r := 0; r < full && r < c.h; r++ {
			k := float64(r) / math.Max(1, d)
			col := blend(brandJuice, brandDeep, k)
			c.bg[r][x] = blend(c.pal.dark, col, 0.95*a)
		}
		if full >= 0 && full < c.h && d-float64(full) > 0.25 {
			c.put(full, x, "▀", blend(c.pal.dark, brandDeep, 0.95*a), false)
		}
	}
}

// saverJuice (Grape juice): juice across the top of the screen, dripping:
// drips stretch, fall as drops, and splash at the bottom.
func saverJuice(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	c := newSceneCanvas(g.w, g.h, pal)
	base := float64(g.h) * 0.2
	n := max(8, g.w/9)
	var drips []juiceDrip
	for i := 0; i < n; i++ {
		x := (float64(i) + 0.2 + 0.6*cellHash(seed, i, 7, 1)) * float64(g.w) / float64(n)
		maxLen := 3 + 7*cellHash(seed, i, 7, 2)
		v := 0.6 + 1.2*cellHash(seed, i, 7, 3)
		ph := cellHash(seed, i, 7, 4) * maxLen
		l := math.Mod(el*v+ph, maxLen)
		drips = append(drips, juiceDrip{x, 1 + cellHash(seed, i, 7, 5)*1.5, l})
		// the last drop, falling from where the drip let go
		since := l / v
		top := base + maxLen
		y := top + 0.5*30*since*since
		if y < float64(g.h-1) {
			c.put(int(y), int(x), "●", blend(pal.dark, "#7648e4", 0.95), false)
		} else if spl := since - math.Sqrt(2*(float64(g.h-1)-top)/30); spl < 0.6 {
			r := 1 + spl*6
			c.text(g.h-1, int(x-r), "‿", faint(pal.purple, pal, spl), false)
			c.text(g.h-1, int(x+r), "‿", faint(pal.purple, pal, spl), false)
		}
	}
	c.paintJuice(func(x int) float64 { return juiceDepth(float64(x), base, el, drips) }, 1)
	*g = *c.finish()
}

// saverRain (Grape rain): the terminal code rain, a grape at each head.
func saverRain(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	rainColumns(g, el, seed, pal)
}

// saverGrapeBounce (the website's Screensaver): the shaded grapes waiting
// for the corner, their glow changing colour off each wall; an exact corner
// throws a shower of grapes.
func (a *App) saverGrapeBounce(g *fxGrid, now time.Time, dt float64, pal loadingPalette) {
	s := a.saver
	logo := strings.Split(a.renderGrapeLogo(), "\n")
	lw, lh := grapeLogoSize, len(logo)
	maxX, maxY := float64(g.w-lw), float64(g.h-lh)
	if maxX < 1 || maxY < 1 {
		a.saverBounce(g, dt, pal)
		return
	}
	s.x += s.dx * dt
	s.y += s.dy * 0.5 * dt
	hitX, hitY := false, false
	if s.x <= 0 || s.x >= maxX {
		s.x, s.dx, hitX = math.Max(0, math.Min(maxX, s.x)), -s.dx, true
	}
	if s.y <= 0 || s.y >= maxY {
		s.y, s.dy, hitY = math.Max(0, math.Min(maxY, s.y)), -s.dy, true
	}
	if hitX || hitY {
		s.colour++
	}
	if hitX && hitY {
		s.cornerAt, s.cornerX, s.cornerY = now, s.x+float64(lw)/2, s.y+float64(lh)/2
		a.findEgg("corner")
	}
	glows := []string{pal.purple, pal.cyan, pal.pink, pal.green, "#ffb86c", pal.yellow}
	c := newSceneCanvas(g.w, g.h, pal)
	c.glow(s.x+float64(lw)/2, s.y+float64(lh)/2, float64(lw)*0.9, glows[s.colour%len(glows)], 0.3)
	if since := now.Sub(s.cornerAt).Seconds(); !s.cornerAt.IsZero() && since < 2.5 {
		for i := 0; i < 24; i++ {
			ang := cellHash(s.seed, i, 8, 1) * 2 * math.Pi
			v := 6 + 14*cellHash(s.seed, i, 8, 2)
			c.grape(int(s.cornerY+math.Sin(ang)*v*since*0.5+6*since*since), int(s.cornerX+math.Cos(ang)*v*since))
		}
	}
	*g = *c.finish()
	for r, line := range logo {
		row := parseFrame(line, lw, 1).rows[0]
		for x, cell := range row {
			if cell.w > 0 && !cell.isEmpty() {
				g.set(int(s.y)+r, int(s.x)+x, cell.ch, cell.sgr, cell.w)
			}
		}
	}
}
