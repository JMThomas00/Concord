package client

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Easter eggs on the login stage (Concord - Login Experience Plan):
//
//   konami     ↑↑↓↓←→←→BA: the grapes invade, then retreat
//   wine       click the grapes ten times: they're pressed into wine
//   burst      type "grape" on Settings > About: grapes everywhere
//   calendar   Halloween, December, New Year's Eve, April 1st, and
//              Concord's birthday (1 February) each look different
//   night      after 2 a.m. the grapes doze; around dawn they catch the sunrise
//   corner     the bouncing-logo screensaver hits a corner exactly
//              (screensaver.go)
//
// Each is recorded in the collection the first time it's found.

type eggState struct {
	kind  string
	start time.Time
	dur   time.Duration
	seed  uint64
}

func (a *App) startEgg(kind string, dur time.Duration) {
	a.egg = &eggState{kind: kind, start: time.Now(), dur: dur, seed: rng.Uint64()}
	a.findEgg(kind)
}

func (a *App) eggPlaying(now time.Time) bool {
	if a.egg != nil && now.Sub(a.egg.start) >= a.egg.dur {
		a.egg = nil
	}
	return a.egg != nil
}

var konamiCode = []string{"up", "up", "down", "down", "left", "right", "left", "right", "b", "a"}

// watchEggKeys follows keys for the eggs typed on the stage and About.
func (a *App) watchEggKeys(msg tea.Msg) {
	k, ok := msg.(tea.KeyMsg)
	if !ok || a.surprise() == surpriseOff {
		return
	}
	key := strings.ToLower(k.String())
	a.eggKeys = append(a.eggKeys, key)
	if len(a.eggKeys) > len(konamiCode) {
		a.eggKeys = a.eggKeys[len(a.eggKeys)-len(konamiCode):]
	}
	if a.aboutShowing() && strings.HasSuffix(strings.Join(a.eggKeys, ""), "disco") {
		a.eggKeys = nil
		if a.uiConfig != nil {
			d := &a.uiConfig.Display
			d.Disco = !d.Disco
			a.saveDisplayConfig()
			msg := "Party's off. Next launch is back to normal."
			if d.Disco {
				a.findEgg("disco")
				msg = "Next time you open Concord, the login's a disco."
			}
			a.toasts = append(a.toasts, &toast{label: "🪩 Disco party", title: msg})
		}
		return
	}
	if a.aboutShowing() && strings.HasSuffix(strings.Join(a.eggKeys, ""), "grape") {
		a.eggKeys = nil
		a.startEgg("burst", 1800*time.Millisecond)
		return
	}
	if isStageView(a.view) && strings.Join(a.eggKeys, " ") == strings.Join(konamiCode, " ") {
		a.eggKeys = nil
		// The B and A went into the password box; take them back out.
		if v := a.loginPassword.Value(); strings.HasSuffix(strings.ToLower(v), "ba") {
			a.loginPassword.SetValue(v[:len(v)-2])
		}
		a.startEgg("konami", 4*time.Second)
	}
}

// watchGrapeClicks counts clicks on the grapes; ten in a row presses them.
func (a *App) watchGrapeClicks(msg tea.MouseMsg) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft || a.surprise() == surpriseOff {
		return
	}
	z := zone.Get("grape-logo")
	if z == nil || !z.InBounds(msg) {
		a.grapeClicks = nil
		return
	}
	now := time.Now()
	if n := len(a.grapeClicks); n > 0 && now.Sub(a.grapeClicks[n-1]) > 1500*time.Millisecond {
		a.grapeClicks = nil
	}
	a.grapeClicks = append(a.grapeClicks, now)
	if len(a.grapeClicks) >= 10 {
		a.grapeClicks = nil
		a.startEgg("wine", 3500*time.Millisecond)
	}
}

// paintEgg draws the egg that's playing over the frame.
func (a *App) paintEgg(g *fxGrid, now time.Time) {
	e := a.egg
	p := float64(now.Sub(e.start)) / float64(e.dur)
	pal := a.loadingPalette()
	switch e.kind {
	case "konami":
		eggInvasion(g, p, pal)
	case "burst":
		eggBurst(g, now.Sub(e.start).Seconds(), e.seed)
	case "wine":
		if z := zone.Get("grape-logo"); z != nil && !z.IsZero() {
			eggWine(g, z.StartX, z.StartY, z.EndX, z.EndY, p, pal, a.mood.code())
		}
	case "confetti":
		eggConfetti(g, now.Sub(e.start).Seconds(), e.seed, pal)
	}
}

// eggInvasion: grapes march in from every edge, hold the screen, and leave.
func eggInvasion(g *fxGrid, p float64, pal loadingPalette) {
	reach := 0.0
	switch {
	case p < .4:
		reach = easeOutCubic(p / .4)
	case p < .65:
		reach = 1
	default:
		reach = 1 - easeOutCubic((p-.65)/.35)
	}
	maxD := float64(min(g.w/2, g.h))
	for r := 0; r < g.h; r++ {
		for c := 0; c+1 < g.w; c += 2 {
			edge := float64(min(min(r, g.h-1-r)*2, min(c, g.w-1-c)))
			if edge < reach*maxD+1 {
				g.set(r, c, "🍇", "", 2)
			}
		}
	}
	if p > .35 && p < .7 {
		msg := "  ALL YOUR BASE ARE BELONG TO GRAPE  "
		clearBox(g, g.h/2-1, len(msg)+2, 3)
		centerText(g, g.h/2, msg, sgrFor(pal.yellow, "", true))
	}
}

// eggBurst: grapes thrown out of the middle of the screen, falling away.
func eggBurst(g *fxGrid, t float64, seed uint64) {
	cx, cy := float64(g.w)/2, float64(g.h)/2
	for i := 0; i < 48; i++ {
		ang := cellHash(seed, i, 0, 1) * 2 * math.Pi
		speed := 20 + cellHash(seed, i, 0, 2)*45
		x := cx + math.Cos(ang)*speed*t
		y := cy + math.Sin(ang)*speed*t*.5 + 18*t*t // gravity
		g.set(int(y), int(x), "🍇", "", 2)
	}
}

var wineGlass = []string{
	`\            /`,
	` \          / `,
	`  \        /  `,
	`   \      /   `,
	`    \____/    `,
	`      ||      `,
	`      ||      `,
	`   ________   `,
}

// eggWine: the grapes squash down into the bottom of their space, then a
// glass fills with wine where they were.
func eggWine(g *fxGrid, x0, y0, x1, y1 int, p float64, pal loadingPalette, code string) {
	h := y1 - y0 + 1
	if p < .25 {
		// Squash: the logo's rows crowd into fewer and fewer at the bottom.
		k := 1 - .75*easeOutCubic(p/.25)
		src := make([][]fxCell, h)
		for r := 0; r < h; r++ {
			src[r] = append([]fxCell(nil), g.rows[y0+r][x0:x1+1]...)
		}
		nh := max(1, int(float64(h)*k))
		for r := 0; r < h; r++ {
			row := g.rows[y0+r]
			for c := x0; c <= x1; c++ {
				row[c] = blankCell
			}
			if i := r - (h - nh); i >= 0 {
				copy(row[x0:x1+1], src[i*h/nh])
			}
		}
		return
	}
	for r := y0; r <= y1; r++ {
		for c := x0; c <= x1; c++ {
			g.clearAt(r, c)
		}
	}
	gw := len([]rune(wineGlass[0]))
	gx := x0 + (x1-x0+1-gw)/2
	gy := y0 + (h-len(wineGlass))/2
	glass := sgrFor(pal.fg, "", false)
	fill := math.Min(1, (p-.25)/.45)
	wine := sgrFor("#8b1a4a", "", true)
	for i, line := range wineGlass {
		g.text(gy+i, gx, line, glass)
		// The bowl (rows 0 to 3) fills from the bottom up.
		if i < 4 && float64(4-i) <= fill*4+.01 {
			rs := []rune(line)
			for c := range rs {
				if c > i && c < len(rs)-1-i {
					g.set(gy+i, gx+c, "▓", wine, 1)
				}
			}
		}
	}
	if p > .7 {
		label := "Vintage " + code
		g.text(gy+len(wineGlass)+1, x0+(x1-x0+1-len([]rune(label)))/2, label, sgrFor(pal.pink, "", true))
	}
}

// eggConfetti: coloured flecks tumbling down (a birthday, a new year,
// a corner hit).
func eggConfetti(g *fxGrid, t float64, seed uint64, pal loadingPalette) {
	colours := []string{pal.pink, pal.purple, pal.cyan, pal.green, pal.yellow}
	flecks := []string{"▪", "▫", "•", "◆", "▴", "✦"}
	n := g.w * g.h / 25
	for i := 0; i < n; i++ {
		x := cellHash(seed, i, 3, 1)*float64(g.w) + math.Sin(t*2+float64(i))*2
		y := cellHash(seed, i, 3, 2)*float64(g.h)*-1 + t*(6+cellHash(seed, i, 3, 3)*10)
		if y < 0 || y >= float64(g.h) {
			continue
		}
		g.set(int(y), int(x), flecks[i%len(flecks)], sgrFor(colours[i%len(colours)], "", true), 1)
	}
}

// calendar is today's special date on the stage, if any.
func (a *App) calendar() string {
	if a.surprise() != surpriseFull {
		return ""
	}
	now := moodClock()
	switch m, d := now.Month(), now.Day(); {
	case m == time.October && d == 31:
		return "halloween"
	case m == time.December && d == 31, m == time.January && d == 1:
		return "newyear"
	case m == time.December && d >= 1:
		return "december"
	case m == time.April && d == 1:
		return "april"
	case m == time.February && d == 1:
		return "birthday"
	}
	return ""
}

// calendarAtmosphere is the background for a special date, or nil.
func (a *App) calendarAtmosphere(now time.Time, t float64) *fxGrid {
	pal := a.loadingPalette()
	w, h := a.width, a.height
	switch a.calendar() {
	case "halloween":
		// Wisps of purple fog drifting across (sparse, so the form never
		// sits in a cut-out of it).
		g := newGrid(w, h)
		for i := 0; i < w*h/90; i++ {
			x := math.Mod(cellHash(uint64(a.mood.seed), i, 7, 1)*float64(w)+t*(1+cellHash(uint64(a.mood.seed), i, 7, 2)*2), float64(w))
			y := cellHash(uint64(a.mood.seed), i, 7, 3) * float64(h)
			g.set(int(y), int(x), []string{"~", "∽", "≈"}[i%3], sgrFor(faint(pal.purple, pal, .35), "", false), 1)
		}
		spooky := atmosGrapes(w, h, t, a.mood.seed)
		for i, row := range spooky.rows {
			for c, cell := range row {
				if cell.ch == "🍇" {
					switch (i + c) % 3 {
					case 0:
						g.set(i, c, "🎃", "", 2)
					case 1:
						g.set(i, c, "👻", "", 2)
					default:
						g.set(i, c, "🍇", "", 2)
					}
				}
			}
		}
		return g
	case "december":
		g := newGrid(w, h)
		snow := sgrFor(faint("#ffffff", pal, .55), "", false)
		for i := 0; i < w*h/60; i++ {
			y := math.Mod(t*(1.5+cellHash(uint64(a.mood.seed), i, 4, 1)*2)+cellHash(uint64(a.mood.seed), i, 4, 2)*float64(h), float64(h))
			x := cellHash(uint64(a.mood.seed), i, 4, 3)*float64(w) + math.Sin(t+float64(i))*1.5
			g.set(int(y), int(x), []string{"❄", "*", "·"}[i%3], snow, 1)
		}
		// String lights along the top, twinkling.
		bulbs := []string{pal.red, pal.green, pal.yellow, pal.cyan, pal.pink}
		for c := 2; c < w-2; c += 4 {
			col := bulbs[(c/4)%len(bulbs)]
			if int(t*2+float64(c))%5 == 0 {
				col = faint(col, pal, .35)
			}
			g.set(0, c, "●", sgrFor(col, "", true), 1)
		}
		return g
	case "newyear", "birthday":
		g := newGrid(w, h)
		eggConfetti(g, math.Mod(t, 8), uint64(a.mood.seed), pal)
		return g
	}
	return nil
}

// paintCalendarText adds the words for a special date: the countdown to
// midnight on New Year's Eve, or happy birthday.
func (a *App) paintCalendarText(g *fxGrid, now time.Time) {
	pal := a.loadingPalette()
	switch a.calendar() {
	case "newyear":
		if now.Month() == time.December && now.Hour() == 23 {
			left := time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, now.Location()).Sub(now)
			msg := "  " + strings.TrimSuffix(left.Truncate(time.Second).String(), "0s") + " to midnight  "
			if left < time.Minute {
				msg = "  " + left.Truncate(time.Second).String() + "!  "
			}
			g.text(1, g.w-len(msg)-2, msg, sgrFor(pal.yellow, "", true))
		} else if now.Month() == time.January {
			msg := "  Happy New Year!  "
			g.text(1, g.w-len(msg)-2, msg, sgrFor(pal.yellow, "", true))
		}
	case "birthday":
		msg := "  Happy birthday, Concord!  "
		g.text(1, g.w-len(msg)-2, msg, sgrFor(pal.pink, "", true))
	}
}

// flipArt turns banner art upside down (April 1st).
func flipArt(art string) string {
	turn := map[rune]rune{'(': ')', ')': '(', '[': ']', ']': '[', '{': '}', '}': '{', '<': '>', '>': '<',
		'_': '‾', '‾': '_', '.': '˙', ',': '`', '`': ',', '\'': ',', 'v': '^', '^': 'v', 'V': 'Λ', 'Λ': 'V',
		'▀': '▄', '▄': '▀', '▘': '▗', '▗': '▘', '▝': '▖', '▖': '▝', '┌': '┘', '┘': '┌', '┐': '└', '└': '┐',
		'╔': '╝', '╝': '╔', '╗': '╚', '╚': '╗'}
	lines := strings.Split(art, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, len([]rune(l)))
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		rs := []rune(l)
		for len(rs) < w {
			rs = append(rs, ' ')
		}
		flipped := make([]rune, w)
		for j, r := range rs {
			if t, ok := turn[r]; ok {
				r = t
			}
			flipped[w-1-j] = r
		}
		out[len(lines)-1-i] = strings.TrimRight(string(flipped), " ")
	}
	return strings.Join(out, "\n")
}

// paintDoze floats a few z's up from the grapes in the small hours.
func (a *App) paintDoze(g *fxGrid, now time.Time) {
	if h := moodClock().Hour(); h < 2 || h >= 5 || a.surprise() != surpriseFull {
		return
	}
	z := zone.Get("grape-logo")
	if z == nil || z.IsZero() {
		return
	}
	pal := a.loadingPalette()
	t := float64(now.UnixMilli()%3000) / 3000
	for i, ch := range []string{"z", "z", "Z"} {
		p := math.Mod(t+float64(i)/3, 1)
		r := z.StartY + 3 - int(p*4)
		c := z.EndX - 6 + i*2 + int(math.Sin(p*6)*1.5)
		if r >= 0 && r < g.h && c >= 0 && c < g.w && g.rows[r][c].isEmpty() {
			g.set(r, c, ch, sgrFor(faint(pal.fg, pal, .3+.5*(1-p)), "", false), 1)
		}
	}
}

// dozing reports the small hours, when the stage redraws for the z's.
func (a *App) dozing() bool {
	h := moodClock().Hour()
	return h >= 2 && h < 5 && a.surprise() == surpriseFull
}

// sunrise reports early morning, when the grapes catch the dawn.
func sunrise() bool {
	h := moodClock().Hour()
	return h >= 5 && h < 7
}
