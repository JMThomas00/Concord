package client

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// The loading screen plays for a few seconds on every launch, in this
// mood's style, while the client starts up for real (the status lines are
// a mix of what it's doing and jokes). Any key or click skips it. The very
// first launch gets a longer welcome sequence instead.

const (
	loadingDur      = 2600 * time.Millisecond
	firstLoadingDur = 3800 * time.Millisecond
)

type loadingState struct {
	kind  string // a layerLoading option, or "boot" for the first launch
	start time.Time
	dur   time.Duration
	seed  uint64
	lines []string
}

// startLoading begins this launch's loading screen, if it has one.
func (a *App) startLoading() {
	kind := a.pick(layerLoading)
	if kind == "" {
		return
	}
	dur := loadingDur
	first := a.coll().Launches <= 1
	if first {
		kind, dur = "boot", firstLoadingDur
	} else {
		a.discover(layerLoading, kind)
	}
	if kind == "bbs" {
		a.findEgg("bbs")
	}
	a.loading = &loadingState{kind: kind, start: time.Now(), dur: dur, seed: rng.Uint64(), lines: a.loadingLines(first)}
}

var loadingJokes = []string{
	"ripening grapes", "untangling the vines", "counting seeds", "chilling the wine",
	"reticulating stems", "aligning the grapevine", "warming up the CRT",
	"dusting off the modem", "teaching grapes to type", "pressing the grapes",
	"consulting the vintner", "polishing the bloom", "calibrating purple",
	"negotiating with the raisins", "rehearsing the banners", "defragmenting the vineyard",
	"tuning the light", "feeding the hamsters", "blowing on the cartridge",
	"reversing the polarity", "rolling the dice", "picking a mood",
}

// loadingLines is what the status line says, in order: what really
// happens at startup, with jokes in between.
func (a *App) loadingLines(first bool) []string {
	r := rand.New(rand.NewPCG(uint64(a.mood.seed), 99))
	jokes := append([]string(nil), loadingJokes...)
	r.Shuffle(len(jokes), func(i, j int) { jokes[i], jokes[j] = jokes[j], jokes[i] })
	theme := "default"
	if a.theme != nil && a.theme.Meta.Name != "" {
		theme = a.theme.Meta.Name
	}
	real := []string{
		"loading theme " + theme,
		fmt.Sprintf("polishing %d banners", len(banners)),
		"terminal graphics: " + termGraphics.best().String(),
		fmt.Sprintf("found %d server(s)", len(a.clientServers)),
		"today's mood is " + a.mood.code(),
	}
	if first {
		real = append([]string{"setting up ~/.concord"}, real...)
	}
	var out []string
	for i, l := range real {
		out = append(out, l)
		if i < len(jokes) && i%2 == 1 {
			out = append(out, jokes[i])
		}
	}
	return append(out, jokes[len(real)], "ready")
}

// skipLoading ends the loading screen early on any key or click.
func (a *App) skipLoading(msg tea.Msg) (bool, tea.Cmd) {
	if a.loading == nil {
		return false, nil
	}
	switch m := msg.(type) {
	case tea.KeyMsg:
	case tea.MouseMsg:
		if m.Action != tea.MouseActionPress {
			return true, nil
		}
	default:
		return false, nil
	}
	if a.count("skips") == 20 {
		a.unlock("impatient")
	}
	return true, a.endLoading()
}

// endLoading leaves the loading screen for the page underneath.
func (a *App) endLoading() tea.Cmd {
	a.loading = nil
	a.fx.stageAt = time.Now()
	a.fx.prevView = a.view
	a.fx.last = ""
	if a.view == ViewLogin || a.view == ViewRegister {
		return a.startBannerAnim()
	}
	return nil
}

// loadingPalette is the theme's colours, or Dracula's where the theme's
// can't be mixed (terminal-default's palette indexes).
type loadingPalette struct {
	purple, pink, fg, dim, dark, green, cyan, yellow, red, blue string
}

func (a *App) loadingPalette() loadingPalette {
	p := loadingPalette{"#bd93f9", "#ff79c6", "#f8f8f2", "#6272a4", "#21222c", "#50fa7b", "#8be9fd", "#f1fa8c", "#ff5555", "#3b4ba8"}
	if a.theme == nil {
		return p
	}
	c := a.theme.Colors
	for _, f := range []struct {
		dst *string
		src string
	}{{&p.purple, c.Purple}, {&p.pink, c.Pink}, {&p.fg, c.Foreground}, {&p.dim, c.Comment},
		{&p.green, c.Green}, {&p.cyan, c.Cyan}, {&p.yellow, c.Yellow}, {&p.red, c.Red}} {
		if _, ok := parseHex(f.src); ok {
			*f.dst = f.src
		}
	}
	if _, ok := parseHex(c.Background); ok {
		p.dark = c.Background
	}
	return p
}

func mix(a, b string, t float64) string {
	if s, ok := mixHex(a, b, math.Max(0, math.Min(1, t))); ok {
		return s
	}
	return a
}

// renderLoading draws the loading screen at now.
func (a *App) renderLoading(now time.Time) string {
	ls := a.loading
	el := now.Sub(ls.start).Seconds()
	p := math.Min(1, el/ls.dur.Seconds())
	g := newGrid(a.width, a.height)
	pal := a.loadingPalette()
	switch ls.kind {
	case "matrix":
		loadMatrix(g, el, p, ls, pal)
	case "lava":
		loadLava(g, el, p, ls, pal)
	case "bios":
		loadBIOS(g, el, p, ls, pal, a)
	case "teletext":
		loadTeletext(g, el, p, ls, pal, a, now)
	case "crt":
		loadCRT(g, el, p, ls, pal)
	case "bbs":
		loadBBS(g, el, p, ls, pal, a)
	case "boot":
		loadBoot(g, el, p, ls, pal)
	default:
		loadCalm(g, el, p, ls, pal)
	}
	return g.String()
}

// statusAt is the status line showing at progress p.
func (ls *loadingState) statusAt(p float64) string {
	i := int(p * float64(len(ls.lines)))
	return ls.lines[min(i, len(ls.lines)-1)]
}

// centerText writes s centred on row r.
func centerText(g *fxGrid, r int, s, sgr string) {
	g.text(r, (g.w-runewidth.StringWidth(s))/2, s, sgr)
}

// clearBox blanks a w×h box centred at row r, for a caption to sit in.
func clearBox(g *fxGrid, r, w, h int) {
	c0 := (g.w - w) / 2
	for y := r; y < r+h; y++ {
		for x := c0; x < c0+w; x++ {
			if y >= 0 && y < g.h && x >= 0 && x < g.w {
				g.clearAt(y, x)
			}
		}
	}
}

// caption is the shared title, progress bar and status line, centred at row r.
func caption(g *fxGrid, r int, p float64, ls *loadingState, pal loadingPalette) {
	clearBox(g, r-1, 44, 6)
	centerText(g, r, "C O N C O R D", sgrFor(pal.purple, "", true))
	const barW = 30
	filled := int(p * barW)
	bar := strings.Repeat("━", filled)
	g.text(r+2, (g.w-barW)/2, bar, sgrFor(pal.pink, "", false))
	g.text(r+2, (g.w-barW)/2+filled, strings.Repeat("─", barW-filled), sgrFor(pal.dim, "", false))
	centerText(g, r+3, ls.statusAt(p)+"…", sgrFor(pal.dim, "", false))
}

// --- grape rain --------------------------------------------------------------

var rainGlyphs = []rune("ｦｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ0123456789:.=*+<>")

// loadMatrix: grape Matrix rain. Each falling column has a grape for a
// head and a trail of characters fading from pink into the dark.
func loadMatrix(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	for k := 0; k*2+1 < g.w; k++ {
		if cellHash(ls.seed, k, 0, 1) > 0.65 {
			continue
		}
		speed := 7 + cellHash(ls.seed, k, 0, 2)*14
		length := 5 + int(cellHash(ls.seed, k, 0, 3)*12)
		span := float64(g.h + length + 4)
		head := math.Mod(el*speed+cellHash(ls.seed, k, 0, 4)*span, span) - 2
		hy := int(head)
		for i := length; i >= 1; i-- {
			y := hy - i
			if y < 0 || y >= g.h {
				continue
			}
			ch := rainGlyphs[int(cellHash(ls.seed, k*997+y, int(el*6+float64(y)*0.3), 5)*float64(len(rainGlyphs)))]
			t := float64(i) / float64(length)
			col := mix(pal.pink, pal.purple, 1-t*2)
			if t > 0.5 {
				col = mix(pal.dark, pal.purple, (t-0.5)*2)
			}
			g.set(y, k*2, string(ch), sgrFor(col, "", i == 1), 1)
		}
		g.set(hy, k*2, "🍇", "", 2)
	}
	caption(g, g.h/2-2, p, ls, pal)
}

// --- lava lamp ---------------------------------------------------------------

// loadLava: a lava lamp in half-blocks (two pixels per cell, so blobs are
// round). Blobs of wax rise, merge and split; one reaching the top pops
// into a grape.
func loadLava(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	W, H := float64(g.w), float64(g.h*2)
	type blob struct{ x, y, r float64 }
	var blobs []blob
	n := 7 + g.w/30
	for i := 0; i < n; i++ {
		speed := 2.5 + cellHash(ls.seed, i, 0, 1)*4 // pixels per second
		r := 2.5 + cellHash(ls.seed, i, 0, 2)*3
		travel := H * 1.15
		y := H + r - math.Mod(el*speed+cellHash(ls.seed, i, 0, 3)*travel, travel)
		x := W*(0.1+0.8*cellHash(ls.seed, i, 0, 4)) + math.Sin(el*0.7+float64(i)*1.9)*W*0.04
		if y < H*0.18 {
			// Popped: a grape floats on up where the blob was.
			g.set(int(y/2), int(x)-1, "🍇", "", 2)
			continue
		}
		blobs = append(blobs, blob{x, y, r * 1.2})
	}
	lit := func(x, y float64) bool {
		f := 0.0
		if y > H-3 {
			f += (y - (H - 3)) / 2 // the pool of wax at the bottom
		}
		for _, b := range blobs {
			dx, dy := x-b.x, y-b.y
			f += b.r * b.r / (dx*dx + dy*dy + 0.01)
		}
		return f > 1
	}
	colour := func(y float64) string { return mix(pal.pink, pal.purple, y/H) }
	for r := 0; r < g.h; r++ {
		for c := 0; c < g.w; c++ {
			if g.rows[r][c].w != 1 || !g.rows[r][c].isEmpty() {
				continue
			}
			top, bot := lit(float64(c), float64(r*2)), lit(float64(c), float64(r*2+1))
			switch {
			case top && bot:
				g.set(r, c, "▀", sgrFor(colour(float64(r*2)), colour(float64(r*2+1)), false), 1)
			case top:
				g.set(r, c, "▀", sgrFor(colour(float64(r*2)), "", false), 1)
			case bot:
				g.set(r, c, "▄", sgrFor(colour(float64(r*2+1)), "", false), 1)
			}
		}
	}
	caption(g, g.h/3-2, p, ls, pal)
}

// --- 90s PC start-up ---------------------------------------------------------

var biosGrape = []string{
	"   ,;,   ",
	"  (@@@)  ",
	" (@@@@@) ",
	"  (@@@)  ",
	"   (@)   ",
}

// loadBIOS: a 1990s PC's power-on self test, counting up to 640K of grapes.
func loadBIOS(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette, a *App) {
	grey, white := sgrFor("#aaaaaa", "", false), sgrFor("#ffffff", "", true)
	ok := sgrFor(pal.green, "", true)
	version := strings.TrimPrefix(a.clientVersion, "v")
	if version == "" {
		version = "0.1.0"
	}
	g.text(1, 2, "Concord Modular BIOS v"+version+", An Energy Grape Ally", white)
	g.text(2, 2, "Copyright (C) 1996-2026, The Concord Vineyard", grey)
	if g.w > 70 {
		for i, l := range biosGrape {
			g.text(1+i, g.w-len(l)-3, l, sgrFor(pal.purple, "", true))
		}
		g.text(6, g.w-len(biosGrape[0])-3, "ENERGY GRAPE", sgrFor(pal.green, "", true))
	}
	g.text(4, 2, "Main Processor : Grape-Core(tm) @ 4.77 MHz", grey)
	mem := int(math.Min(1, p/0.35) * 640)
	g.text(5, 2, fmt.Sprintf("Memory Testing : %dK", mem), grey)
	if p >= 0.35 {
		g.text(5, 19+len(fmt.Sprint(mem))+2, "GRAPES OK", ok)
	}
	row := 7
	for i, l := range ls.lines {
		at := 0.35 + 0.6*float64(i)/float64(len(ls.lines))
		if p < at || row >= g.h-3 {
			break
		}
		label := strings.ToUpper(l[:1]) + l[1:] + " "
		g.text(row, 2, label+strings.Repeat(".", max(2, 46-len(label))), grey)
		g.text(row, max(50, len(label)+5), "OK", ok)
		row++
	}
	if int(el*2)%2 == 0 {
		g.text(row, 2, "_", grey)
	}
	g.text(g.h-2, 2, "Press DEL to enter SETUP, any other key to skip", grey)
	g.text(g.h-1, 2, moodClock().Format("01/02/2006")+"-CONCORD-GRAPE-"+strings.TrimPrefix(a.mood.code(), "#"), grey)
}

// --- teletext ----------------------------------------------------------------

// loadTeletext: a teletext page being found and filled in, row by row.
func loadTeletext(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette, a *App, now time.Time) {
	black := "#000000"
	page := 100
	if p < 0.25 {
		page = 100 + int(p/0.25*99)%100 // the page counter searching
		if page == 100 {
			page = 101
		}
	}
	head := fmt.Sprintf(" P100  CONCORD %03d  %s", page, now.Format("Mon 02 Jan  15:04:05"))
	g.text(0, 0, padTo(head, g.w), sgrFor("#ffffff", black, false))
	if p < 0.25 {
		return
	}
	const bandW = 40
	band := func(s string) string {
		pad := (bandW - runewidth.StringWidth(s)) / 2
		return padTo(strings.Repeat(" ", pad)+s, bandW)
	}
	entry := func(label, value string, page int) string {
		s := fmt.Sprintf("%-10s %s ", label, value)
		return s + strings.Repeat(".", max(2, bandW-4-runewidth.StringWidth(s))) + fmt.Sprintf(" %d", page)
	}
	rows := []struct {
		text, fg, bg string
		gap          int // blank rows before it
	}{
		{band(""), "#ffff00", "#0000ff", 1},
		{band(concordBlock[0]), "#ffff00", "#0000ff", 0},
		{band(concordBlock[1]), "#ffff00", "#0000ff", 0},
		{band(""), "#ffff00", "#0000ff", 0},
		{entry("GRAPE NEWS", "", 101), "#00ffff", "", 2},
		{entry("WEATHER", "purple, 100% grapes", 102), "#00ff00", "", 1},
		{entry("SERVERS", fmt.Sprintf("%d known", len(a.clientServers)), 103), "#ffffff", "", 1},
		{entry("MOOD", a.mood.code(), 104), "#ff00ff", "", 1},
		{strings.ToUpper(ls.statusAt(p)), "#ffff00", "", 2},
	}
	shown := int((p - 0.25) / 0.6 * float64(len(rows)))
	c0 := max(0, (g.w-bandW)/2)
	r := 1
	for i, row := range rows {
		r += row.gap
		if i > shown || r >= g.h-1 {
			break
		}
		g.text(r, c0, row.text, sgrFor(row.fg, row.bg, true))
		r++
	}
	keys := []struct{ label, bg string }{{"News", "#ff0000"}, {"Sport", "#00ff00"}, {"Grapes", "#ffff00"}, {"Index", "#00ffff"}}
	qw := g.w / 4
	for i, k := range keys {
		g.text(g.h-1, i*qw, padTo(" "+k.label, qw), sgrFor("#000000", k.bg, true))
	}
}

func padTo(s string, w int) string {
	if n := runewidth.StringWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return runewidth.Truncate(s, w, "")
}

// --- CRT warm-up -------------------------------------------------------------

// concordBlock is CONCORD in a two-row block font.
var concordBlock = []string{
	"█▀▀ █▀█ █▄ █ █▀▀ █▀█ █▀█ █▀▄",
	"█▄▄ █▄█ █ ▀█ █▄▄ █▄█ █▀▄ █▄▀",
}

// gradientText writes s at r, c shading each character across from a to b.
func gradientText(g *fxGrid, r, c int, s, a, b string, bold bool) {
	rs := []rune(s)
	for i, ch := range rs {
		if ch == ' ' {
			continue
		}
		g.set(r, c+i, string(ch), sgrFor(mix(a, b, 1-float64(i)/float64(max(1, len(rs)-1))), "", bold), 1)
	}
}

// loadCRT: an old monitor warming up. A dot, a line, then the picture opens
// out, with a brighter band rolling down it.
func loadCRT(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	mid, cx := g.h/2, g.w/2
	white := sgrFor("#ffffff", "", true)
	switch {
	case p < 0.12:
		g.set(mid, cx, "●", white, 1)
	case p < 0.3:
		half := int((p - 0.12) / 0.18 * float64(g.w) / 2)
		g.text(mid, cx-half, strings.Repeat("━", half*2), white)
	default:
		open := easeOutCubic((p - 0.3) / 0.25)
		half := int(open * float64(g.h) / 2)
		pic := newGrid(g.w, g.h)
		gradientText(pic, mid-3, (g.w-len([]rune(concordBlock[0])))/2, concordBlock[0], pal.purple, pal.pink, true)
		gradientText(pic, mid-2, (g.w-len([]rune(concordBlock[1])))/2, concordBlock[1], pal.purple, pal.pink, true)
		caption(pic, mid+1, p, ls, pal)
		clearBox(pic, mid+1, 44, 1) // the caption's title: the block letters stand in
		roll := int(math.Mod(el*8, float64(g.h+6))) - 3
		for r := mid - half; r <= mid+half; r++ {
			if r < 0 || r >= g.h {
				continue
			}
			g.rows[r] = append([]fxCell(nil), pic.rows[r]...)
			if r >= roll && r < roll+2 {
				for c := range g.rows[r] {
					if g.rows[r][c].isEmpty() {
						g.set(r, c, "░", sgrFor(mix(pal.dark, pal.dim, 0.35), "", false), 1)
					}
				}
			}
		}
	}
}

// --- BBS dial-up -------------------------------------------------------------

// loadBBS (rare): dialling a 1990s bulletin board, typed out at modem
// speed, with the welcome screen in ANSI art.
func loadBBS(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette, a *App) {
	grey, white := sgrFor("#aaaaaa", "", false), sgrFor("#ffffff", "", true)
	script := []struct{ text, sgr string }{
		{"ATZ", grey}, {"OK", white}, {"ATDT 1-800-GRAPES", grey}, {"RINGING", grey},
		{"CONNECT 2400/ARQ/V42BIS", sgrFor(pal.green, "", true)}, {"", grey},
	}
	budget := int(p * 1.25 * 260) // characters typed so far
	row := 1
	for _, l := range script {
		if budget <= 0 {
			break
		}
		n := min(budget, len(l.text))
		g.text(row, 2, l.text[:n], l.sgr)
		budget -= len(l.text) + 4
		row++
	}
	if budget <= 0 {
		return
	}
	c0 := (g.w - len([]rune(concordBlock[0]))) / 2
	r0 := row + 1
	for i, line := range concordBlock {
		n := max(0, min(budget, len([]rune(line))))
		gradientText(g, r0+i, c0, string([]rune(line)[:n]), pal.cyan, pal.purple, true)
		budget -= len([]rune(line))
	}
	if budget > 0 {
		g.text(r0+2, c0, strings.Repeat("▀", min(budget, 28)), sgrFor(pal.dim, "", false))
		budget -= 28
	}
	lines := []string{
		fmt.Sprintf("Welcome, caller #%d!", a.coll().Launches),
		"Node 1 of 1 · 2400 baud · mood " + a.mood.code(),
		"",
		"Press any key to continue...",
	}
	for i, l := range lines {
		if budget <= 0 {
			break
		}
		n := min(budget, len([]rune(l)))
		s := white
		if i == 3 {
			s = sgrFor(pal.yellow, "", true)
		}
		centerText(g, r0+4+i, string([]rune(l)[:n])+strings.Repeat(" ", len([]rune(l))-n), s)
		budget -= len([]rune(l))
	}
}

// --- first launch ------------------------------------------------------------

// loadBoot: the first launch's welcome. Start-up lines type out, then
// CONCORD builds in, letter by letter.
func loadBoot(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	ok := sgrFor(pal.green, "", true)
	grey := sgrFor(pal.dim, "", false)
	n := min(len(ls.lines), int(p/0.6*float64(len(ls.lines)))+1)
	top := max(1, g.h/2-len(ls.lines)/2-4)
	for i := 0; i < n; i++ {
		r := top + i
		if r >= g.h {
			break
		}
		g.text(r, 4, "[", grey)
		g.text(r, 5, "  OK  ", ok)
		g.text(r, 11, "] "+ls.lines[i], grey)
	}
	if p < 0.6 {
		return
	}
	q := (p - 0.6) / 0.3
	c0 := (g.w - len([]rune(concordBlock[0]))) / 2
	r0 := top + len(ls.lines) + 2
	for i, line := range concordBlock {
		rs := []rune(line)
		k := min(len(rs), int(q*float64(len(rs))))
		gradientText(g, r0+i, c0, string(rs[:k]), pal.purple, pal.pink, true)
	}
	if q >= 1 {
		centerText(g, r0+3, "Welcome to Concord.", sgrFor(pal.fg, "", true))
	}
}

// --- calm --------------------------------------------------------------------

// loadCalm: just the name and three breathing dots.
func loadCalm(g *fxGrid, el, p float64, ls *loadingState, pal loadingPalette) {
	mid := g.h/2 - 1
	centerText(g, mid, "c o n c o r d", sgrFor(pal.purple, "", true))
	c0 := g.w/2 - 3
	for i := 0; i < 3; i++ {
		v := (math.Sin(el*4-float64(i)*0.9) + 1) / 2
		g.set(mid+2, c0+i*3, "●", sgrFor(mix(pal.pink, pal.dark, v), "", false), 1)
	}
	centerText(g, mid+4, ls.statusAt(p), sgrFor(pal.dim, "", false))
}
