package client

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// More loading screens: old machines starting up (a DOS prompt, the
// Concord 64, the happy-grapes start-up), and the vine growing its grapes.

// typed returns the first n characters of s (by rune), for typing effects.
func typed(s string, n int) string {
	rs := []rune(s)
	return string(rs[:max(0, min(n, len(rs)))])
}

// loadDOS: a DOS prompt runs CONCORD.EXE, drivers load, a progress bar fills.
func loadDOS(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette, a *App) {
	grey, white := sgrFor("#aaaaaa", "", false), sgrFor("#ffffff", "", true)
	version := strings.TrimPrefix(a.clientVersion, "v")
	if version == "" {
		version = "0.1.0"
	}
	chars := int(el * 28) // typing speed
	lines := []struct{ text, sgr string }{
		{`C:\>cd \CONCORD`, grey},
		{`C:\CONCORD>concord.exe`, grey},
		{"", grey},
		{"Concord v" + version + "  (C) 1996-2026 The Concord Vineyard", white},
		{"", grey},
	}
	row := 1
	for _, l := range lines {
		if chars <= 0 {
			break
		}
		g.text(row, 2, typed(l.text, chars), l.sgr)
		chars -= len([]rune(l.text)) + 3
		row++
	}
	if chars > 0 {
		drivers := []string{"HIMEM.GRP", "VINE.SYS", "TRELLIS.DRV", "PRESS.EXE", "CELLAR.386", "MOUSE.COM", "BUNCH.DLL"}
		n := min(len(ls.lines), int(p/.85*float64(len(ls.lines)))+1)
		for i := 0; i < n && row < g.h-4; i++ {
			g.text(row, 2, fmt.Sprintf("  %-12s %s", drivers[i%len(drivers)], ls.lines[i]), grey)
			g.text(row, 52, "ok", sgrFor(pal.green, "", true))
			row++
		}
		const barW = 40
		filled := int(p * barW)
		g.text(g.h-3, 2, "["+strings.Repeat("▓", filled)+strings.Repeat("░", barW-filled)+fmt.Sprintf("] %3d%%", int(p*100)), white)
	}
	if int(el*2)%2 == 0 {
		g.text(g.h-2, 2, "_", grey)
	}
}

// loadC64: the Concord 64 powers up in its blue, types LOAD and RUN.
func loadC64(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette, a *App) {
	border, screen, ink := "#7c70da", "#3e31a2", "#a59cf0"
	for r := 0; r < g.h; r++ {
		for c := 0; c < g.w; c++ {
			bg := screen
			if r < 2 || r >= g.h-2 || c < 6 || c >= g.w-6 {
				bg = border
			}
			g.set(r, c, " ", sgrFor("", bg, false), 1)
		}
	}
	s := sgrFor(ink, screen, true)
	x, row := 8, 3
	put := func(text string) {
		if row < g.h-3 {
			g.text(row, x, padTo(text, g.w-14), s)
		}
		row++
	}
	put("    **** CONCORD 64 BASIC V2 ****")
	put("")
	put(fmt.Sprintf(" 64K RAM SYSTEM  %d BASIC BYTES FREE", 38911+a.coll().Launches))
	put("")
	put("READY.")
	chars := int((el - .6) * 14)
	if chars > 0 {
		cmd := `LOAD"CONCORD",8,1`
		put(typed(cmd, chars))
		if chars > len(cmd)+2 {
			put("")
			put("SEARCHING FOR CONCORD")
			n := min(len(ls.lines), int((p-.35)/.5*float64(len(ls.lines)))+1)
			for i := 0; i < n && p > .35; i++ {
				put("LOADING " + strings.ToUpper(strings.TrimPrefix(ls.lines[i], "loading ")))
			}
			if p > .88 {
				put("READY.")
				put(typed("RUN", int((p-.88)*60)))
			}
		}
	}
	if int(el*2.5)%2 == 0 && row < g.h-3 {
		g.text(row, x, "█", sgrFor(ink, screen, false))
	}
}

var happyGrapes = []string{
	"┌──────────────┐",
	"│ ┌──────────┐ │",
	"│ │  ●    ●  │ │",
	"│ │    🍇    │ │",
	"│ │  ╰────╯  │ │",
	"│ └──────────┘ │",
	"│        ▬▬    │",
	"└──────────────┘",
}

// loadMac: the little computer with the happy grapes, then the welcome
// box with its progress bar, like a 1990s Mac starting up.
func loadMac(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	desk, ink := "#c8c8c8", "#202020"
	for r := 0; r < g.h; r++ {
		for c := 0; c < g.w; c++ {
			g.set(r, c, " ", sgrFor("", desk, false), 1)
		}
	}
	s := sgrFor(ink, desk, false)
	if p < .35 {
		top := (g.h - len(happyGrapes)) / 2
		for i, l := range happyGrapes {
			c0 := (g.w - 16) / 2
			g.text(top+i, c0, l, s)
		}
		return
	}
	const bw = 48
	top, c0 := g.h/2-4, (g.w-bw)/2
	box := sgrFor(ink, "#ffffff", false)
	for r := 0; r < 8; r++ {
		g.text(top+r, c0, strings.Repeat(" ", bw), box)
	}
	g.text(top, c0, "┌"+strings.Repeat("─", bw-2)+"┐", box)
	g.text(top+7, c0, "└"+strings.Repeat("─", bw-2)+"┘", box)
	for r := 1; r < 7; r++ {
		g.text(top+r, c0, "│", box)
		g.text(top+r, c0+bw-1, "│", box)
	}
	title := "Welcome to Concord"
	g.text(top+2, c0+(bw-len(title))/2, title, sgrFor(ink, "#ffffff", true))
	const barW = bw - 10
	q := (p - .35) / .65
	filled := int(q * barW)
	g.text(top+4, c0+5, strings.Repeat("█", filled), sgrFor("#5a5a5a", "#ffffff", false))
	g.text(top+4, c0+5+filled, strings.Repeat("░", barW-filled), sgrFor("#bbbbbb", "#ffffff", false))
	status := strings.ToUpper(ls.statusAt(p)[:1]) + ls.statusAt(p)[1:] + "…"
	g.text(top+5, c0+(bw-len([]rune(status)))/2, status, sgrFor("#606060", "#ffffff", false))
}

// loadVine: the logo grows. The leaf and stem draw themselves, then the
// grapes pop on one by one, top first.
func (a *App) loadVine(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	L := grapeLogos[grapeLogoSize]
	rows := shadeGrapeLogo(grapeLogoSize, grapeLight0)
	styles := a.grapeToneColors()
	order := make([]int, len(L.grapes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return L.grapes[order[i]][1] < L.grapes[order[j]][1] })
	popAt := map[int]float64{}
	for k, idx := range order {
		popAt[idx] = .35 + .5*float64(k)/float64(len(order))
	}
	top := max(0, (g.h-L.rows)/2-3)
	c0 := (g.w - L.cols) / 2
	if c0 < 0 {
		loadCalm(g, el, p, ls, pal) // too narrow for the vine
		return
	}
	stem := int(math.Min(1, p/.35) * float64(L.rows*L.cols))
	for r := 0; r < L.rows; r++ {
		var b strings.Builder
		for c := 0; c < L.cols; c++ {
			cell := rows[r][c]
			px, py := (float64(c)+.5)*L.cw, (float64(r)+.5)*L.ch
			_, which := grapeNormal(L, px, py)
			if which < 0 && strings.HasPrefix(cell.tone, "p") {
				// A grape's edge whose middle misses the sphere: it belongs to
				// the nearest grape.
				best := math.Inf(1)
				for k, gr := range L.grapes {
					if d := math.Hypot(px-gr[0], py-gr[1]) - gr[2]; d < best {
						best, which = d, k
					}
				}
			}
			show := false
			ch := string(cell.ch)
			if which < 0 {
				show = r*L.cols+c < stem // the leaf and stem, drawn in reading order
			} else if p >= popAt[which] {
				show = true
				if p < popAt[which]+.04 && cell.ch != ' ' {
					ch = "o" // just popped
				}
			}
			if !show || cell.ch == ' ' {
				b.WriteByte(' ')
				continue
			}
			if st, ok := styles[cell.tone]; ok {
				b.WriteString(st.Render(ch))
			} else {
				b.WriteString(ch)
			}
		}
		if top+r < g.h {
			parseLine(b.String(), g.rows[top+r][c0:])
		}
	}
	if r := top + L.rows + 2; r+3 < g.h {
		caption(g, r, p, ls, pal)
	}
}
