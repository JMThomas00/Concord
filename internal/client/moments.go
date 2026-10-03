package client

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Moments, seasons and glitches: more of the login stage's personality.
//
//   moments   a few moods have a theme of their own (the moment layer):
//             Midnight Jazz, Arcade, Film Noir, Synthwave
//   seasons   blossom in spring, fireflies on summer nights, a harvest
//             moon in autumn, frost in winter (the calendar's special days
//             take over on their day)
//   glitches  on some launches the screen glitches for a moment, now and
//             then, and snaps back

// moment is this launch's moment ("" or "none" for an ordinary one).
func (a *App) moment() string {
	m := a.pick(layerMoment)
	if m == "none" {
		return ""
	}
	return m
}

var momentNames = map[string]string{
	"jazz": "♪ MIDNIGHT JAZZ", "arcade": "◆ ARCADE", "noir": "▪ FILM NOIR", "synthwave": "▲ SYNTHWAVE",
}

// momentPurple recolours the grapes for a moment ("" for no change).
func (a *App) momentPurple(purple string) string {
	switch a.moment() {
	case "jazz":
		return mixOr("#5b7cfa", purple, .6)
	case "noir":
		return "#a0a0ac"
	case "synthwave":
		return mixOr("#ff4fd8", purple, .55)
	}
	return purple
}

// momentBanner is a moment's banner colouring, if it has one.
func (a *App) momentBanner(r, rows, c, cols int, t float64, pal loadingPalette) (string, bool) {
	y := float64(r) / float64(max(1, rows-1))
	switch a.moment() {
	case "jazz":
		return mix("#9fb4ff", "#3f56c9", 1-y), true
	case "noir":
		return mix("#e8e8ee", "#6a6a74", 1-y), true
	case "synthwave":
		stops := []string{"#ffe066", "#ff9f43", "#ff4fd8", "#9b5cff"}
		f := y * float64(len(stops)-1)
		i := min(int(f), len(stops)-2)
		return mix(stops[i+1], stops[i], f-float64(i)), true
	case "arcade":
		cols := []string{"#ff5555", "#ffd23f", "#4be36e", "#45d6ff"}
		return cols[(c/2+int(t*3))%len(cols)], true
	}
	return "", false
}

// momentAtmosphere adds a moment's touches to the background.
func (a *App) momentAtmosphere(g *fxGrid, t float64, pal loadingPalette) {
	seed := uint64(a.mood.seed)
	switch a.moment() {
	case "jazz": // notes drifting up
		for i := 0; i < g.w*g.h/220; i++ {
			y := float64(g.h) - math.Mod(t*(.7+cellHash(seed, i, 8, 1))+cellHash(seed, i, 8, 2)*float64(g.h), float64(g.h))
			x := cellHash(seed, i, 8, 3)*float64(g.w) + math.Sin(t+float64(i))*2
			g.set(int(y), int(x), []string{"♪", "♫", "♩"}[i%3], sgrFor(faint("#7d95ff", pal, .45), "", false), 1)
		}
	case "noir": // rain on the window
		for i := 0; i < g.w*g.h/70; i++ {
			y := math.Mod(t*(14+cellHash(seed, i, 9, 1)*8)+cellHash(seed, i, 9, 2)*float64(g.h), float64(g.h))
			x := cellHash(seed, i, 9, 3) * float64(g.w)
			g.set(int(y), int(x), "╱", sgrFor(faint("#c0c0c8", pal, .25), "", false), 1)
		}
	}
}

// paintMoment draws a moment's foreground touches: its name, and the
// arcade's scores and coin slot. Only empty rows are written on.
func (a *App) paintMoment(g *fxGrid, now time.Time) {
	m := a.moment()
	if m == "" {
		return
	}
	pal := a.loadingPalette()
	free := func(r int) bool {
		first, _ := g.contentSpan(r)
		return r >= 0 && r < g.h && first < 0
	}
	if name := momentNames[m]; free(1) {
		g.text(1, g.w-len([]rune(name))-3, name, sgrFor(faint(pal.fg, pal, .5), "", false))
	}
	switch m {
	case "arcade":
		if free(0) {
			hi := fmt.Sprintf("1UP  %06d     HI-SCORE  %06d", 0, a.coll().Launches*1000)
			g.text(0, (g.w-len(hi))/2, hi, sgrFor("#ffd23f", "", true))
		}
		if r := g.h - 4; free(r) && now.UnixMilli()/600%2 == 0 {
			coin := "INSERT COIN"
			g.text(r, (g.w-len(coin))/2, coin, sgrFor("#ff5555", "", true))
		}
	case "synthwave":
		synthGrid(g, now, pal)
	}
}

// synthGrid draws the neon floor along the bottom, on rows the page leaves
// empty, receding to a horizon.
func synthGrid(g *fxGrid, now time.Time, pal loadingPalette) {
	top := g.h
	for r := g.h - 1; r >= g.h-8 && r >= 0; r-- {
		if first, _ := g.contentSpan(r); first >= 0 {
			break
		}
		top = r
	}
	if g.h-top < 3 {
		return
	}
	scroll := math.Mod(float64(now.UnixMilli())/700, 1)
	cx := float64(g.w) / 2
	for r := top; r < g.h; r++ {
		depth := float64(r-top+1) / float64(g.h-top) // 0 far, 1 near
		col := faint(mix("#ff4fd8", "#9b5cff", depth), pal, .8-.5*depth)
		// Horizontal lines, closer together towards the horizon, sliding forward.
		if math.Mod(depth*4+scroll, 1) < .25 {
			g.text(r, 0, strings.Repeat("─", g.w), sgrFor(col, "", false))
			continue
		}
		for k := -12; k <= 12; k++ {
			x := cx + float64(k)*depth*float64(g.w)/14
			if x >= 0 && x < float64(g.w) {
				g.set(r, int(x), "│", sgrFor(col, "", false), 1)
			}
		}
	}
}

// --- seasons -----------------------------------------------------------------

// season is the time of year, for the stage's seasonal touches.
func season(now time.Time) string {
	switch now.Month() {
	case time.March, time.April, time.May:
		return "spring"
	case time.June, time.July, time.August:
		return "summer"
	case time.September, time.October, time.November:
		return "autumn"
	}
	return "winter"
}

// seasonAtmosphere adds the season's sparse touches to the background.
func (a *App) seasonAtmosphere(g *fxGrid, t float64, pal loadingPalette) {
	if a.surprise() != surpriseFull || a.calendar() != "" {
		return
	}
	now := moodClock()
	seed := uint64(a.mood.seed)
	switch season(now) {
	case "spring": // blossom drifting down
		for i := 0; i < g.w*g.h/400; i++ {
			y := math.Mod(t*(.6+cellHash(seed, i, 10, 1)*.8)+cellHash(seed, i, 10, 2)*float64(g.h), float64(g.h))
			x := cellHash(seed, i, 10, 3)*float64(g.w) + 3*math.Sin(t*.7+float64(i))
			g.set(int(y), int(x), []string{"✿", "❀", "·"}[i%3], sgrFor(faint("#ffb3d9", pal, .5), "", false), 1)
		}
	case "summer": // fireflies, after dark
		if h := now.Hour(); h >= 6 && h < 19 {
			return
		}
		for i := 0; i < g.w*g.h/300; i++ {
			x := cellHash(seed, i, 11, 1)*float64(g.w) + 4*math.Sin(t*.4+float64(i)*1.3)
			y := cellHash(seed, i, 11, 2)*float64(g.h) + 2*math.Cos(t*.3+float64(i))
			glow := (math.Sin(t*2.2+float64(i)*2.1) + 1) / 2
			if glow > .45 {
				g.set(int(y), int(x), "•", sgrFor(faint("#f6ff7a", pal, 1-glow), "", glow > .85), 1)
			}
		}
	case "autumn": // the harvest moon
		if !moonHours() {
			return
		}
		// A different phase each launch, from a thin crescent to full.
		phase := .45 + cellHash(seed, 0, 0, 16)*(2*math.Pi-.9)
		drawMoon(g, 2, g.w-moonCols-4, phase, pal)
	case "winter": // frost creeping in at the edges
		for i := 0; i < (g.w+g.h)*2/3; i++ {
			edge := cellHash(seed, i, 12, 1)
			var r, c int
			switch {
			case edge < .4:
				r, c = int(cellHash(seed, i, 12, 2)*2), int(cellHash(seed, i, 12, 3)*float64(g.w))
			case edge < .7:
				r, c = int(cellHash(seed, i, 12, 2)*float64(g.h)), int(cellHash(seed, i, 12, 3)*3)
			default:
				r, c = int(cellHash(seed, i, 12, 2)*float64(g.h)), g.w-1-int(cellHash(seed, i, 12, 3)*3)
			}
			twinkle := (math.Sin(t*1.5+float64(i)) + 1) / 2
			g.set(r, c, []string{"❄", "·", "*", "⁂"}[i%4], sgrFor(faint("#dff4ff", pal, .6-.4*twinkle), "", false), 1)
		}
	}
}

// --- glitches ----------------------------------------------------------------

const glitchDur = 380 * time.Millisecond

// glitchy reports whether this launch glitches now and then (about one
// launch in three).
func (a *App) glitchy() bool {
	return a.surprise() == surpriseFull && cellHash(uint64(a.mood.seed), 0, 0, 13) < .35
}

func (a *App) glitching(now time.Time) bool {
	return !a.fx.glitchAt.IsZero() && now.Sub(a.fx.glitchAt) < glitchDur
}

// maybeGlitch starts a glitch when one's due (from the idle check).
func (a *App) maybeGlitch(now time.Time) {
	if !a.glitchy() || !isStageView(a.view) || a.loading != nil {
		return
	}
	if a.fx.nextGlitch.IsZero() {
		a.fx.nextGlitch = now.Add(time.Duration(15+rng.Intn(30)) * time.Second)
		return
	}
	if now.After(a.fx.nextGlitch) {
		a.fx.glitchAt = now
		a.fx.nextGlitch = now.Add(time.Duration(25+rng.Intn(50)) * time.Second)
	}
}

// glitch tears the frame: bands of rows slip sideways, one splits into
// red and cyan, one repeats the row above, changing every few frames.
func glitch(g *fxGrid, age time.Duration, seed uint64) {
	beat := seed + uint64(age/(60*time.Millisecond))
	for b := 0; b < 4; b++ {
		top := int(cellHash(beat, b, 0, 1) * float64(g.h))
		height := 1 + int(cellHash(beat, b, 0, 2)*3)
		shift := int(cellHash(beat, b, 0, 3)*12) - 6
		for r := top; r < top+height && r < g.h; r++ {
			row := g.rows[r]
			moved := make([]fxCell, len(row))
			for c := range moved {
				moved[c] = blankCell
			}
			out := &fxGrid{w: g.w, h: 1, rows: [][]fxCell{moved}}
			for c, cell := range row {
				if cell.w == 0 {
					continue
				}
				if b == 0 && !cell.isEmpty() { // the colour split
					split := "#ff3b5c"
					if c%2 == 0 {
						split = "#33e0ff"
					}
					cell.sgr = sgrFor(split, "", true)
				}
				putCell(out, 0, c+shift, cell)
			}
			g.rows[r] = moved
		}
	}
	if r := 1 + int(cellHash(beat, 9, 0, 4)*float64(g.h-1)); r < g.h {
		g.rows[r] = append([]fxCell(nil), g.rows[r-1]...) // the tear
	}
}

// The moon is ASCII art: shaded with characters like the grapes, craters
// drawn as o's, its rim softened with the partial-coverage characters an
// artist would use, and the dark side just a faint dotted outline.
const (
	moonRows = 8  // tall
	moonCols = 18 // wide (cells are about twice as tall as wide)
)

var moonRamp = []rune(".:-=+*#%")

var moonCraters = [][3]float64{{-.35, -.3, .17}, {.28, .12, .2}, {-.08, .5, .13}, {.48, -.38, .1}, {-.55, .2, .1}}

// drawMoon draws the moon at phase (radians: 0 new, π full; waxing lit on
// the right, waning on the left) with its top-left cell at row, col.
func drawMoon(g *fxGrid, row, col int, phase float64, pal loadingPalette) {
	k := math.Cos(phase)
	lit := func(x, y float64) bool {
		s := math.Sqrt(math.Max(0, 1-y*y))
		return (phase <= math.Pi && x > s*k) || (phase > math.Pi && x < -s*k)
	}
	for cy := 0; cy < moonRows; cy++ {
		for cx := 0; cx < moonCols; cx++ {
			inside, litN, lum := 0, 0, 0.0
			for j := 0; j < 3; j++ {
				for i := 0; i < 2; i++ {
					x := (float64(cx)+(float64(i)+.5)/2)/moonCols*2 - 1
					y := (float64(cy)+(float64(j)+.5)/3)/moonRows*2 - 1
					if d := x*x + y*y; d <= 1 {
						inside++
						if lit(x, y) {
							litN++
							lum += math.Sqrt(1 - d) // brighter in the middle
						}
					}
				}
			}
			if inside == 0 {
				continue
			}
			x := (float64(cx)+.5)/moonCols*2 - 1
			y := (float64(cy)+.5)/moonRows*2 - 1
			edge := inside < 4
			if litN*2 < inside {
				// The dark side: a faint dotted rim, the way earthshine shows it.
				if edge || (cx+cy)%3 == 0 {
					g.set(row+cy, col+cx, ".", sgrFor(faint("#9aa0c8", pal, .72), "", false), 1)
				}
				continue
			}
			lum /= float64(litN)
			ch := moonRamp[min(len(moonRamp)-1, int(lum*float64(len(moonRamp))))]
			colour := mixOr("#f6ecd0", "#a8996f", .35+lum*.65)
			for _, c := range moonCraters {
				if dx, dy := x-c[0], (y-c[1])*.9; dx*dx+dy*dy < c[2]*c[2] {
					ch = 'o'
					if dx*dx+dy*dy < c[2]*c[2]/3 {
						ch = 'O'
					}
					colour = mixOr(colour, "#8a7d5c", .5)
				}
			}
			if edge {
				switch {
				case y < -.5:
					ch = '.'
				case y > .5:
					ch = '\''
				default:
					ch = ':'
				}
			}
			g.set(row+cy, col+cx, string(ch), sgrFor(colour, "", false), 1)
		}
	}
}
