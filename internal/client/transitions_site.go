package client

import (
	"math"
	"time"
)

// Page transitions from the website's light and dark switches (concord-site
// appearance.js): a flashbang's white-out, an old CRT's power cut, a flood
// of grape juice, and the Lightbulb's pull chain (which plays instead of
// the mood's transition whenever the Lightbulb background is showing).

// siteTransitionDur is how long these take; the rest are in duration().
func siteTransitionDur(kind string) (time.Duration, bool) {
	switch kind {
	case "flashbang":
		return 900 * time.Millisecond, true
	case "powercut":
		return 1100 * time.Millisecond, true
	case "juice":
		return 1300 * time.Millisecond, true
	case "pullchain":
		return 1500 * time.Millisecond, true
	}
	return 0, false
}

// wholeScreen reports a transition that redraws the whole screen on
// purpose, shared rows (the banner, the grapes) and all.
func wholeScreen(kind string) bool {
	switch kind {
	case "crt", "modem", "flashbang", "powercut", "juice", "pullchain":
		return true
	}
	return false
}

// pullChainPop is when in the pull chain the filament pops (and the page
// changes): a fraction of its duration.
const pullChainPop = 0.72

// prepare gives the transition the theme's colours.
func (t *transition) prepare(pal loadingPalette) { t.pal = pal }

func (t *transition) renderSite(from, to *fxGrid, p float64) (*fxGrid, bool) {
	switch t.kind {
	case "flashbang":
		return transFlash(from, to, p, t.seed, t.pal), true
	case "powercut":
		return transPowerCut(from, to, p, t.seed), true
	case "juice":
		return transJuice(from, to, p, t.seed, t.pal), true
	case "pullchain":
		return transPullChain(from, to, p, t.seed, t.pal, t.bulbCol, t.bulbTop), true
	}
	return nil, false
}

// transFlash (Flashbang): the old page washes out to white, and the new
// one comes back out of the white.
func transFlash(from, to *fxGrid, p float64, seed uint64, pal loadingPalette) *fxGrid {
	switch {
	case p < 0.25:
		return washOut(from, easeOutCubic(p/0.25), pal)
	case p < 0.42:
		return washOut(to, 1, pal)
	}
	return washOut(to, 1-easeOutCubic((p-0.42)/0.58), pal)
}

// washOut is g blended k of the way to a flashbang's white: every cell's
// background, and its text (which fades into the white).
func washOut(g *fxGrid, k float64, pal loadingPalette) *fxGrid {
	out := g.clone()
	if k <= 0 {
		return out
	}
	bg := blend(pal.dark, brandFlash, 0.95*k)
	empty := sgrFor("", bg, false)
	text := sgrFor("", bg, false) // faint: the text keeps its colours
	if k > 0.3 {
		text = sgrFor(blend(pal.fg, brandFlash, k), bg, false)
	}
	for r := range out.rows {
		for c, cell := range out.rows[r] {
			switch {
			case cell.w == 0:
				out.rows[r][c].sgr = cell.sgr + text
			case cell.isEmpty():
				out.rows[r][c] = fxCell{ch: " ", sgr: empty, w: 1}
			default:
				out.rows[r][c].sgr = cell.sgr + text
			}
		}
	}
	return out
}

// transPowerCut (Power cut): the old page squashes to a bright line, the
// line to a dot, the dot fades; a beat of black, then the new page opens
// out from the middle with a flicker.
func transPowerCut(from, to *fxGrid, p float64, seed uint64) *fxGrid {
	h, w := to.h, to.w
	out := newGrid(w, h)
	mid := float64(h) / 2
	line := sgrFor("#ffffff", "", true)
	switch {
	case p < 0.2: // squash
		k := easeOutCubic(p / 0.2)
		half := mid * (1 - k)
		for r := 0; r < h; r++ {
			if math.Abs(float64(r)+0.5-mid) < half {
				for c := 0; c < w; c++ {
					putCell(out, r, c, from.rows[r][c])
				}
			}
		}
		edge := int(mid - half)
		if half < 1 {
			out.text(int(mid), 0, repeatRune('━', w), line)
		} else if edge >= 0 && edge < h {
			out.text(edge, 0, repeatRune('▁', w), line)
			out.text(min(h-1, int(mid+half)), 0, repeatRune('▔', w), line)
		}
	case p < 0.4: // the line pulls in to a dot
		k := easeOutCubic((p - 0.2) / 0.2)
		lw := max(1, int(float64(w)*(1-k)))
		out.text(int(mid), (w-lw)/2, repeatRune('━', lw), line)
	case p < 0.62: // the dot fades
		k := (p - 0.4) / 0.22
		out.set(int(mid), w/2, "•", sgrFor(blend("#ffffff", "#000000", k), "", true), 1)
	case p < 0.7: // black
	default: // power back on
		k := easeOutCubic((p - 0.7) / 0.3)
		half := mid * k
		flick := cellHash(seed, int(p*40), 0, 22) < 0.25*(1-k)
		for r := 0; r < h; r++ {
			if math.Abs(float64(r)+0.5-mid) < half && !flick {
				for c := 0; c < w; c++ {
					putCell(out, r, c, to.rows[r][c])
				}
			}
		}
		if k < 0.97 {
			glowLine := sgrFor(blend("#c8d2ff", "#000000", k), "", false)
			out.text(max(0, int(mid-half)), 0, repeatRune('▔', w), glowLine)
			out.text(min(h-1, int(mid+half)), 0, repeatRune('▁', w), glowLine)
		}
	}
	return out
}

// transJuice (Juice spill): grape juice floods down over the old page in
// drips, and drains away downwards showing the new one.
func transJuice(from, to *fxGrid, p float64, seed uint64, pal loadingPalette) *fxGrid {
	h, w := to.h, to.w
	var drips []juiceDrip
	for i, n := 0, max(8, w/8); i < n; i++ {
		drips = append(drips, juiceDrip{
			x:   (float64(i) + cellHash(seed, i, 13, 1)) * float64(w) / float64(n),
			w:   1 + cellHash(seed, i, 13, 2)*2,
			len: 2 + 6*cellHash(seed, i, 13, 3),
		})
	}
	span := float64(h) + 10
	cover := -8 + span*easeOutCubic(p/0.55)                  // the flood's front
	drain := -8 + span*easeInOutCubic(math.Max(0, p-0.45)/0.55) // the back, where the new page shows
	out := from.clone()
	deep := func(r int, d float64) string {
		return blend(pal.dark, blend(brandJuice, brandDeep, float64(r)/math.Max(1, d)), 0.95)
	}
	for c := 0; c < w; c++ {
		front := juiceDepth(float64(c), cover, p*3, drips)
		back := -1.0
		if p > 0.45 {
			back = juiceDepth(float64(c), drain, p*3+1, drips)
		}
		for r := 0; r < h; r++ {
			y := float64(r)
			switch {
			case y < back:
				out.clearAt(r, c)
				putCell(out, r, c, to.rows[r][c])
			case y < front:
				out.set(r, c, " ", sgrFor("", deep(r, front), false), 1)
			case y < front+0.75 && y >= back:
				out.set(r, c, "▀", sgrFor(deep(r, front), "", false), 1)
			}
		}
	}
	return out
}

// transPullChain (Pull chain): a pull chain drops beside the Lightbulb and
// is yanked; the lights flicker, the filament pops in a spray of sparks, and
// the new page is there in the dark (the bulb stays off a while: the
// atmosphere knows from fx.bulbPopAt).
func transPullChain(from, to *fxGrid, p float64, seed uint64, pal loadingPalette, bulbCol, bulbTop int) *fxGrid {
	var out *fxGrid
	switch {
	case p < 0.45:
		out = from.clone()
	case p < pullChainPop: // the flicker: the page goes dark and comes back
		if int(p*30)%3 == 0 {
			out = newGrid(to.w, to.h)
		} else {
			out = from.clone()
		}
	default:
		out = to.clone()
	}
	glassY := bulbTop + 4
	x := bulbCol + 6
	if p < pullChainPop {
		full := float64(glassY)
		drop := easeOutCubic(math.Min(1, p/0.25))
		l := full * drop
		if p > 0.3 && p < 0.45 { // the yank
			l += math.Sin((p-0.3)/0.15*math.Pi) * 3
		}
		chain := sgrFor(blend(pal.dark, "#8a8d98", 0.9), "", false)
		for r := 0; r < int(l); r++ {
			out.set(r, x, "┊", chain, 1)
		}
		out.set(int(l), x, "▼", sgrFor(blend(pal.dark, "#5c5f6b", 0.95), "", true), 1)
	}
	if p >= pullChainPop && p < pullChainPop+0.2 { // the pop
		k := (p - pullChainPop) / 0.2
		for i := 0; i < 14; i++ {
			a := cellHash(seed, i, 14, 1) * 2 * math.Pi
			v := 3 + 6*cellHash(seed, i, 14, 2)
			col := "#fffad2"
			if k > 0.5 {
				col = "#ffbe5a"
			}
			out.set(glassY+int(math.Sin(a)*v*k*0.5+3*k*k), bulbCol+int(math.Cos(a)*v*k), "*", sgrFor(blend(pal.dark, col, 1-k*0.6), "", true), 1)
		}
	}
	return out
}
