package client

import (
	"fmt"
	"strings"
	"time"
)

// Vintage labels, the daily fortune, launch streaks and the almanac: small
// touches that make each launch feel like it belongs to you.

var estates = []string{
	"Château Concord", "Domaine du Terminal", "Vineyard 80×24", "Clos de la Console",
	"Maison Phosphore", "Cave du Curseur", "Tenuta ANSI", "Bodega Baud",
}

var varietals = map[string]string{
	"shaded": "Cabernet ASCII", "pixel": "Pixel Noir", "braille": "Braille Blanc",
	"dotmatrix": "Dot Matrix Merlot", "wireframe": "Wireframe Riesling", "golden": "Golden Reserve",
}

var tastingNotes = map[moodLayer]map[string]string{
	layerAtmosphere: {"grapes": "ripe grapes", "stars": "starlight", "dots": "graph paper",
		"leaves": "autumn leaves", "none": "quiet evenings"},
	layerBanner: {"solid": "classic purple", "gradient": "dusk", "shimmer": "sparkle", "bbs": "ANSI art",
		"green": "green phosphor", "rainbow": "candy floss", "amber": "warm CRT glass"},
	layerLight: {"orbit": "a gentle orbit", "breathe": "a slow breath", "disco": "mirror balls"},
	layerTransition: {"slide": "a smooth", "decode": "a cryptic", "teletext": "a teletext", "baud": "a 2400-baud",
		"modem": "a dial-up", "crt": "a flickering", "plasma": "a molten", "curl": "a page-turning",
		"blinds": "a shuttered", "dissolve": "a fizzy"},
}

// label is the mood's wine label: estate, year, varietal and code, then a
// tasting note made from its layers.
func (m mood) label(year int) (name, notes string) {
	estate := estates[int(m.seed)%len(estates)]
	varietal := varietals[m.picks[layerLogo]]
	if varietal == "" {
		varietal = "House Red"
	}
	name = fmt.Sprintf("%s %d %s %s", estate, year, varietal, m.code())
	a := tastingNotes[layerAtmosphere][m.picks[layerAtmosphere]]
	b := tastingNotes[layerBanner][m.picks[layerBanner]]
	l := tastingNotes[layerLight][m.picks[layerLight]]
	fin := tastingNotes[layerTransition][m.picks[layerTransition]]
	if fin == "" {
		fin = "a long"
	}
	notes = fmt.Sprintf("Notes of %s and %s, over %s, with %s finish.", a, b, l, fin)
	return name, notes
}

// fortunes: one each launch on the login stage, real tips mixed with grape lore.
var fortunes = []string{
	"Ctrl+R on the login screen shuffles the banner. There are 327.",
	"A grape a day keeps the lag away.",
	"Alt+M highlights messages; A copies an attachment's ID.",
	"Good things come to those who wait. Grapes become wine.",
	"/theme changes the colours. Settings > Theme previews them.",
	"Your mood code is on Settings > About. Lock one you love.",
	"Tab moves between panels; Esc leaves a plugin pane.",
	"The vine that bends doesn't break.",
	"Ctrl+B manages your servers.",
	"Even raisins were grapes once. Be kind to yourself.",
	"/nick changes how you appear on a server.",
	"Today is a good day to prune something.",
	"Voice levels: ←/→ on a member in voice changes their volume.",
	"Every vintage is different. So is every launch.",
	"/attach shares a file straight from your computer.",
	"A bunch is stronger than a single grape.",
	"Click the grapes. Then keep clicking.",
	"The best conversations ferment slowly.",
	"Settings > Display > Surprise Me turns all this down, if you like.",
	"Ripe ideas fall when they're ready.",
	"Plugins live in their own channels: games, boards, bots.",
	"In vino veritas. In Concord, chat.",
	"The grapes follow your mouse. Try it.",
	"Old modems sang. Today's just hum.",
	"Some things are only here on certain days.",
	"Hold space on the loading screen to watch it slowly.",
	"Trellis your thoughts before you post them.",
	"Grape expectations lead to grape results.",
	"Ctrl+P on the login screen switches profile.",
	"There are secrets on the About page.",
}

// pickFortune chooses this launch's fortune, never the last launch's.
func (a *App) pickFortune() {
	c := a.coll()
	i := rng.Intn(len(fortunes))
	if last := c.Counters["fortune"] - 1; i == last {
		i = (i + 1 + rng.Intn(len(fortunes)-1)) % len(fortunes)
	}
	c.Counters["fortune"] = i + 1
	a.saveCollection()
	a.fortune = fortunes[i]
}

// paintFortune puts today's fortune (and the launch streak) along the
// bottom of the login stage, where nothing else is.
func (a *App) paintFortune(g *fxGrid) {
	if a.surprise() != surpriseFull || g.h < 20 || a.moment() == "synthwave" { // the neon floor has the bottom rows
		return
	}
	r := g.h - 2
	if first, _ := g.contentSpan(r); first >= 0 {
		return
	}
	pal := a.loadingPalette()
	if a.fortune == "" {
		return
	}
	line := "🍇 " + a.fortune
	if s := a.coll().Streak; s >= 2 {
		line += fmt.Sprintf("   ·   %d-day streak", s)
	}
	w := 0
	for _, ch := range line {
		w += runeCells(ch)
	}
	if w > g.w-4 {
		return
	}
	g.text(r, (g.w-w)/2, line, sgrFor(faint(pal.fg, pal, .55), "", false))
}

func runeCells(r rune) int {
	if r >= 0x1F300 {
		return 2
	}
	return 1
}

// updateStreak counts consecutive days Concord was opened.
func (c *Collection) updateStreak(now time.Time) {
	today := now.Format("2006-01-02")
	if c.FirstLaunch == "" {
		c.FirstLaunch = today
	}
	switch c.LastDay {
	case today:
		return
	case now.AddDate(0, 0, -1).Format("2006-01-02"):
		c.Streak++
	default:
		c.Streak = 1
	}
	c.LastDay = today
	c.BestStreak = max(c.BestStreak, c.Streak)
}

// almanac is the Profiles page's line about this computer's vineyard.
func (a *App) almanac() string {
	c := a.coll()
	days := 1
	if t, err := time.ParseInLocation("2006-01-02", c.FirstLaunch, time.Local); err == nil {
		days = int(moodClock().Sub(t).Hours()/24) + 1
	}
	parts := []string{fmt.Sprintf("Day %d of the vineyard", days)}
	if a.configMgr != nil {
		n := len(a.configMgr.GetClientServers())
		parts = append(parts, fmt.Sprintf("%d server%s tended", n, plural(n)))
	}
	if m := c.Counters["messages"]; m > 0 {
		parts = append(parts, fmt.Sprintf("%s messages pressed", commas(m)))
	}
	return strings.Join(parts, " · ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// commas writes 1204 as "1,204".
func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
