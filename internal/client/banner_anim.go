package client

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Login/register banner intro animations. Each style decides, per character
// cell and animation progress p (0..1), whether that cell is hidden, drawn
// normally, drawn as a bright "leading edge", or shown as scrambled noise.
// Hidden cells render as spaces, so every frame has exactly the final art's
// dimensions and the centered layout never shifts.

var bannerAnimNames = []string{
	"typewriter", // reading-order reveal with a bright cursor trail
	"wipe",       // left-to-right curtain with a glowing edge
	"scanline",   // top-down rows, current row lit
	"dissolve",   // characters fade in at random
	"decode",     // scrambled glyphs settle into the logo
	"rain",       // columns fill top-down at staggered times
	"iris",       // reveal grows outward from the center
	"diagonal",   // sweep from the top-left corner
}

const (
	bannerAnimFrameDur = 33 * time.Millisecond
	bannerAnimFrames   = 32 // ~1s at 30fps
	bannerAnimEdge     = 0.10
)

const bannerNoiseGlyphs = "!@#$%&*<>/\\|=+?~^;:{}[]01"

type bannerCellKind uint8

const (
	cellHidden bannerCellKind = iota
	cellNormal
	cellEdge
	cellNoise
)

type bannerCell struct {
	r    rune
	kind bannerCellKind
}

type bannerAnimState struct {
	style int
	frame int
	seed  uint64
	gen   int
}

type bannerAnimTickMsg struct{ gen int }

func bannerAnimTick(gen int) tea.Cmd {
	return tea.Tick(bannerAnimFrameDur, func(time.Time) tea.Msg { return bannerAnimTickMsg{gen: gen} })
}

// startBannerAnim begins a new intro animation in a random style (never the
// same style twice in a row). A no-op when panel animations are disabled.
func (a *App) startBannerAnim() tea.Cmd {
	if a.uiConfig != nil && a.uiConfig.Display.DisablePanelAnimations {
		a.bannerAnim = nil
		return nil
	}
	style := rng.Intn(len(bannerAnimNames) - 1)
	if style >= a.lastBannerAnimStyle && a.lastBannerAnimStyle >= 0 {
		style++
	}
	a.lastBannerAnimStyle = style
	a.bannerAnimGen++
	a.bannerAnim = &bannerAnimState{style: style, seed: rng.Uint64(), gen: a.bannerAnimGen}
	return bannerAnimTick(a.bannerAnimGen)
}

// pickFittingBanner chooses a random banner other than last among those
// fits accepts; ok is false when there's no such banner.
func pickFittingBanner(last int, fits func(int) bool) (int, bool) {
	var candidates []int
	for i := range banners {
		if i != last && fits(i) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return last, false
	}
	return candidates[rng.Intn(len(candidates))], true
}

// setBanner switches to banner idx, remembering it so the next launch
// won't open on the same one.
func (a *App) setBanner(idx int) {
	a.banner, a.bannerIndex = banners[idx], idx
	a.discoverBanner(idx)
	if a.configMgr != nil {
		if cfg, err := a.configMgr.LoadAppConfig(); err == nil && cfg != nil {
			cfg.UI.LastBannerIndex = idx
			_ = a.configMgr.SaveAppConfig(cfg)
		}
	}
}

// shuffleBanner is the login-screen easter egg (Ctrl+R): a different banner
// that fits the logo box at the current terminal size, animated in.
func (a *App) shuffleBanner() tea.Cmd {
	a.nameBanner = "" // back to the 327
	if a.count("shuffles") == 25 {
		a.unlock("shuffler")
	}
	fits := func(int) bool { return true }
	if g, ok := a.currentLockup(); ok {
		fits = g.fits
	}
	if idx, ok := pickFittingBanner(a.bannerIndex, fits); ok {
		a.setBanner(idx)
	}
	return a.startBannerAnim()
}

// ensureBannerFits swaps in a fitting banner when the current one doesn't
// fit the logo box -- at startup (the banner is chosen before the terminal
// size is known), after a resize, or on arriving at login/register. Called
// after every Update.
func (a *App) ensureBannerFits() tea.Cmd {
	if a.width <= 0 || a.height <= 0 {
		return nil
	}
	g, ok := a.currentLockup()
	if !ok || g.fits(a.bannerIndex) {
		return nil
	}
	idx, found := pickFittingBanner(a.bannerIndex, g.fits)
	if !found {
		return nil // nothing fits a screen this small; the layout clips instead
	}
	a.setBanner(idx)
	return a.startBannerAnim()
}

func (a *App) handleBannerAnimTick(msg bannerAnimTickMsg) tea.Cmd {
	if a.bannerAnim == nil || msg.gen != a.bannerAnim.gen {
		return nil
	}
	a.bannerAnim.frame++
	if a.bannerAnim.frame >= bannerAnimFrames {
		a.bannerAnim = nil
		return nil
	}
	return bannerAnimTick(msg.gen)
}

// renderBanner draws the current banner, mid-animation if one is running.
func (a *App) renderBanner() string {
	art := trimBannerArt(a.banner.Art)
	if a.nameBannerFits() {
		art = a.nameBanner // hello, you
		a.findEgg("name")
	}
	if a.calendar() == "april" {
		art = flipArt(art) // April 1st
	}
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	if a.bannerAnim == nil {
		if coloured, ok := a.colourBanner(art, time.Now()); ok {
			return coloured
		}
		return base.Render(art)
	}
	p := easeOutCubic(float64(a.bannerAnim.frame+1) / float64(bannerAnimFrames))
	grid := bannerAnimCells(art, a.bannerAnim.style, p, a.bannerAnim.seed, a.bannerAnim.frame)

	styles := map[bannerCellKind]lipgloss.Style{
		cellNormal: base,
		cellEdge:   lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true),
		cellNoise:  lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)),
	}
	var out strings.Builder
	for i, row := range grid {
		if i > 0 {
			out.WriteByte('\n')
		}
		// Style runs of same-kind cells together rather than per character.
		for j := 0; j < len(row); {
			k := j
			var run strings.Builder
			for k < len(row) && row[k].kind == row[j].kind {
				run.WriteRune(row[k].r)
				k++
			}
			if st, ok := styles[row[j].kind]; ok {
				out.WriteString(st.Render(run.String()))
			} else {
				out.WriteString(run.String())
			}
			j = k
		}
	}
	return out.String()
}

func easeOutCubic(t float64) float64 {
	if t >= 1 {
		return 1
	}
	return 1 - math.Pow(1-t, 3)
}

// cellHash gives each (row, col, salt) a stable pseudo-random value in [0,1).
func cellHash(seed uint64, r, c, salt int) float64 {
	x := seed ^ uint64(r)*0x9E3779B97F4A7C15 ^ uint64(c)*0xC2B2AE3D27D4EB4F ^ uint64(salt)*0x165667B19E3779F9
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return float64(x>>11) / float64(1<<53)
}

// band maps a cell's reveal position t (0 = first, 1 = last) to its kind at
// progress p: revealed behind the moving edge, lit on it, hidden ahead of it.
func band(t, p float64) bannerCellKind {
	edge := p * (1 + bannerAnimEdge)
	switch {
	case t < edge-bannerAnimEdge:
		return cellNormal
	case t < edge:
		return cellEdge
	default:
		return cellHidden
	}
}

// bannerAnimCells computes one frame of a banner animation. Space characters
// are always left as spaces; at p == 1 every style yields the plain art.
func bannerAnimCells(art string, style int, p float64, seed uint64, frame int) [][]bannerCell {
	lines := strings.Split(art, "\n")
	grid := make([][]rune, len(lines))
	rows, cols := len(lines), 1
	total := 0
	for i, l := range lines {
		grid[i] = []rune(l)
		if len(grid[i]) > cols {
			cols = len(grid[i])
		}
		for _, ch := range grid[i] {
			if ch != ' ' {
				total++
			}
		}
	}
	if total == 0 {
		total = 1
	}

	out := make([][]bannerCell, rows)
	order := 0
	for r, line := range grid {
		out[r] = make([]bannerCell, len(line))
		for c, ch := range line {
			if ch == ' ' {
				out[r][c] = bannerCell{r: ' ', kind: cellHidden}
				continue
			}
			kind := cellNormal
			if p < 1 {
				kind = cellKindFor(style, r, c, rows, cols, order, total, p, seed)
			}
			order++
			cell := bannerCell{r: ch, kind: kind}
			switch kind {
			case cellHidden:
				cell.r = ' '
			case cellNoise:
				g := int(cellHash(seed, r, c, frame+7) * float64(len(bannerNoiseGlyphs)))
				cell.r = rune(bannerNoiseGlyphs[g])
			}
			out[r][c] = cell
		}
	}
	return out
}

func cellKindFor(style, r, c, rows, cols, order, total int, p float64, seed uint64) bannerCellKind {
	fr, fc := float64(r), float64(c)
	switch bannerAnimNames[style] {
	case "typewriter":
		return band(float64(order)/float64(total), p)
	case "wipe":
		return band(fc/float64(cols), p)
	case "scanline":
		return band(fr/float64(rows), p)
	case "dissolve":
		return band(cellHash(seed, r, c, 1), p)
	case "decode":
		if cellHash(seed, r, c, 2) < p {
			return cellNormal
		}
		return cellNoise
	case "rain":
		// Each column starts falling at a random delay, then fills top-down.
		delay := cellHash(seed, 0, c, 3) * 0.45
		q := (p - delay) / 0.55
		return band(fr/float64(rows), math.Max(0, math.Min(1, q)))
	case "iris":
		dx := (fc - float64(cols)/2) / (float64(cols) / 2)
		dy := (fr - float64(rows)/2) / (float64(rows) / 2)
		return band(math.Min(1, math.Hypot(dx, dy)/math.Sqrt2), p)
	case "diagonal":
		return band((fc+fr*3)/(float64(cols)+float64(rows)*3), p)
	}
	return cellNormal
}
