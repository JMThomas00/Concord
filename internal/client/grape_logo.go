package client

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// The shaded ASCII grape logo, ported from the concord-site project
// (github.com/Anthoneyq/concord-site, app.js). The eight grapes are spheres
// shaded per character like donut.c, so a light can move across them; the
// leaf and stem are fixed characters traced from the logo image
// (tools/grapelogo generates the data). The light powers on from the dark
// side when the logo appears, then drifts in a slow orbit; clicking or
// dragging steers it toward the pointer.

type grapeLogoData struct {
	cols, rows int
	cw, ch     float64
	grapes     [][4]float64 // x, y, radius, depth in logo pixels
	chars      []string
	tones      []string
}

const (
	grapeLogoSize    = 35 // columns; 21 rows, the login logo slot's height
	grapeRamp        = ".,-~:;=!*#$@"
	grapeFrameDur    = 33 * time.Millisecond
	grapeIdleAfter   = 2500 * time.Millisecond
	grapeOrbitPeriod = 2600 * time.Millisecond
	grapeEase        = 0.1719 // the site's 0.09 per 60fps frame, at 30fps
)

var (
	grapeLight0 = norm3([3]float64{-.42, -.52, .8})
	grapeDark   = norm3([3]float64{.5, .6, -.7})
)

type grapeCell struct {
	grape         bool
	x, y, z       float64 // surface normal
	occ, coverage float64
	ch            byte
	tone          string
}

var grapeCellCache = map[int][]grapeCell{}

func norm3(v [3]float64) [3]float64 {
	m := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	if m == 0 {
		m = 1
	}
	return [3]float64{v[0] / m, v[1] / m, v[2] / m}
}

// grapeCells samples each character cell SX×SY times against the spheres
// (nearest surface wins; a grape in front casts a contact shadow on the
// one behind) and averages the hits into one normal per cell.
func grapeCells(size int) []grapeCell {
	if cells, ok := grapeCellCache[size]; ok {
		return cells
	}
	L := grapeLogos[size]
	const sx, sy = 3, 5
	toneOf := map[byte]string{'G': "g2", 'g': "g1", 'O': "o2", 'o': "o1"}
	type hit struct{ n, x, y, z, o float64 }
	cells := make([]grapeCell, 0, L.rows*L.cols)
	for r := 0; r < L.rows; r++ {
		for c := 0; c < L.cols; c++ {
			hits := make([]hit, len(L.grapes))
			for j := 0; j < sy; j++ {
				for i := 0; i < sx; i++ {
					x := (float64(c) + (float64(i)+.5)/sx) * L.cw
					y := (float64(r) + (float64(j)+.5)/sy) * L.ch
					best, bz := -1, math.Inf(-1)
					var n [3]float64
					for k, g := range L.grapes {
						dx, dy := (x-g[0])/g[2], (y-g[1])/g[2]
						if d := dx*dx + dy*dy; d < 1 {
							nz := math.Sqrt(1 - d)
							if z := g[3] + g[2]*nz; z > bz {
								bz, best, n = z, k, [3]float64{dx, dy, nz}
							}
						}
					}
					if best < 0 {
						continue
					}
					occ := 0.0
					for k, g := range L.grapes {
						if k == best || g[3] <= L.grapes[best][3] {
							continue
						}
						if d := math.Hypot(x-g[0], y-g[1]) / g[2]; d < 1.25 {
							occ = math.Max(occ, 1-(d-1)/.25)
						}
					}
					h := &hits[best]
					h.n++
					h.x += n[0]
					h.y += n[1]
					h.z += n[2]
					h.o += occ
				}
			}
			// Majority grape; ties go to the lower index (JS stable sort order).
			best, total := -1, 0.0
			for k := range hits {
				total += hits[k].n
				if hits[k].n > 0 && (best < 0 || hits[k].n > hits[best].n) {
					best = k
				}
			}
			coverage := total / (sx * sy)
			if best >= 0 && coverage >= .3 {
				h := hits[best]
				m := math.Sqrt(h.x*h.x + h.y*h.y + h.z*h.z)
				cells = append(cells, grapeCell{grape: true, x: h.x / m, y: h.y / m, z: h.z / m, occ: h.o / h.n, coverage: coverage})
				continue
			}
			ch := L.chars[r][c]
			cells = append(cells, grapeCell{ch: ch, tone: toneOf[L.tones[r][c]]})
		}
	}
	grapeCellCache[size] = cells
	return cells
}

// shadeGrapeLogo returns one frame as rows of (character, tone) for light L.
func shadeGrapeLogo(size int, L [3]float64) [][]grapeCell {
	logo := grapeLogos[size]
	cells := grapeCells(size)
	hv := norm3([3]float64{L[0], L[1], L[2] + 1})
	out := make([][]grapeCell, logo.rows)
	for r := range out {
		out[r] = make([]grapeCell, logo.cols)
		for c := range out[r] {
			cell := cells[r*logo.cols+c]
			if !cell.grape {
				out[r][c] = grapeCell{ch: cell.ch, tone: cell.tone}
				if cell.ch == ' ' {
					out[r][c].tone = ""
				}
				continue
			}
			d := math.Max(0, cell.x*L[0]+cell.y*L[1]+cell.z*L[2])
			sp := math.Pow(math.Max(0, cell.x*hv[0]+cell.y*hv[1]+cell.z*hv[2]), 28)
			rim := math.Min(1, math.Max(0, (cell.z-.14)/.4)) // edges fall dark so each grape reads as its own circle
			lum := math.Min(1, (.34+.66*d)*rim*(1-.9*cell.occ)*math.Min(1, cell.coverage*1.3)+.9*sp)
			ch := grapeRamp[int(math.Floor(lum*float64(len(grapeRamp)-1)+.5))]
			tone := "p0"
			switch {
			case sp > .5:
				tone = "p3"
			case lum > .6:
				tone = "p2"
			case lum > .3:
				tone = "p1"
			}
			out[r][c] = grapeCell{ch: ch, tone: tone}
		}
	}
	return out
}

// --- animation state -------------------------------------------------------

type grapeLightState struct {
	cur, to [3]float64
	steered time.Time // last pointer steer; the idle orbit waits grapeIdleAfter
	started time.Time
	gen     int
}

type grapeTickMsg struct{ gen int }

func grapeTick(gen int) tea.Cmd {
	return tea.Tick(grapeFrameDur, func(time.Time) tea.Msg { return grapeTickMsg{gen: gen} })
}

// grapeLogoShowing reports whether a screen currently displays the grape
// logo: login/register, or Settings > About.
func (a *App) grapeLogoShowing() bool {
	if a.loading != nil {
		return false // it powers on when the loading screen ends
	}
	return isStageView(a.view)
}

// syncGrapeLight starts the light (powering on from dark) when the logo
// comes into view and stops it when it leaves. Called after every Update.
func (a *App) syncGrapeLight() tea.Cmd {
	animated := a.uiConfig == nil || !a.uiConfig.Display.DisablePanelAnimations
	if !a.grapeLogoShowing() || !animated {
		a.grapeLight = nil
		return nil
	}
	if a.grapeLight != nil {
		return nil
	}
	a.grapeGen++
	now := time.Now()
	a.grapeLight = &grapeLightState{cur: grapeDark, to: grapeLight0, steered: now, started: now, gen: a.grapeGen}
	return grapeTick(a.grapeGen)
}

func (a *App) handleGrapeTick(msg grapeTickMsg) tea.Cmd {
	gl := a.grapeLight
	if gl == nil || msg.gen != gl.gen {
		return nil
	}
	now := time.Now()
	if now.Sub(gl.steered) > grapeIdleAfter {
		t := float64(now.Sub(gl.started)) / float64(grapeOrbitPeriod)
		switch a.pick(layerLight) {
		case "breathe": // a slow rise and fall
			gl.to = norm3([3]float64{grapeLight0[0], grapeLight0[1] + .38*math.Sin(t*.6), grapeLight0[2] + .15*math.Cos(t*.6)})
		case "disco": // jumping about to the beat
			beat := uint64(now.UnixMilli() / 320)
			gl.to = norm3([3]float64{cellHash(beat, 0, 0, 1)*1.6 - .8, cellHash(beat, 0, 0, 2)*1.4 - .7, .6})
		default: // orbit
			gl.to = norm3([3]float64{grapeLight0[0] + .32*math.Cos(t), grapeLight0[1] + .22*math.Sin(t), grapeLight0[2]})
		}
	}
	for i := range gl.cur {
		gl.cur[i] += (gl.to[i] - gl.cur[i]) * grapeEase
	}
	gl.cur = norm3(gl.cur)
	return grapeTick(gl.gen)
}

// steerGrapeLight points the light at a click or drag, relative to the
// logo's center (rows count double: a character cell is twice as tall).
func (a *App) steerGrapeLight(msg tea.MouseMsg) {
	gl := a.grapeLight
	if gl == nil {
		return
	}
	z := zone.Get("grape-logo")
	if z == nil || z.IsZero() {
		return
	}
	cx := float64(z.StartX+z.EndX) / 2
	cy := float64(z.StartY+z.EndY) / 2
	width := float64(z.EndX - z.StartX + 1)
	gl.to = norm3([3]float64{float64(msg.X) - cx, (float64(msg.Y) - cy) * 2, width * .55})
	gl.steered = time.Now()
}

// MouseHoverFilter is installed via tea.WithFilter in main.go. Bubbletea
// calls model.View() unconditionally after every single Update() call,
// regardless of what Update() actually did -- so with All Motion Tracking
// enabled (main.go's tea.WithMouseAllMotion, needed for the grape logo to
// follow the cursor at all on Linux/macOS terminals), every pixel of mouse
// movement would otherwise trigger a full Update+render pass. A filter is
// the only hook that runs before Update/View both, so it's the only place
// that can actually drop these events rather than merely skip some of the
// work they'd otherwise cause.
//
// A plain hover (motion, no button held) has no effect anywhere else in the
// app -- every other mouse handler either only acts on a Press, or (the Help
// scrollbar drag) explicitly requires Button == Left -- so it's safe to
// steer the light directly here and drop the message entirely. The logo
// itself is animated by its own bounded 30fps tick (grapeTick), completely
// decoupled from raw mouse-event rate, so nothing is lost by not forcing an
// extra render for every single one of these.
func MouseHoverFilter(model tea.Model, msg tea.Msg) tea.Msg {
	m, ok := msg.(tea.MouseMsg)
	if !ok || m.Action != tea.MouseActionMotion || m.Button != tea.MouseButtonNone {
		return msg
	}
	if a, ok := model.(*App); ok {
		a.steerGrapeLight(m)
	}
	return nil
}

// --- rendering -------------------------------------------------------------

// mixHex blends two "#rrggbb" colors (t of a); ok is false for anything
// else (e.g. terminal-default's ANSI palette indices, which can't be mixed).
func mixHex(a, b string, t float64) (string, bool) {
	pa, oka := parseHex(a)
	pb, okb := parseHex(b)
	if !oka || !okb {
		return "", false
	}
	var out [3]int
	for i := range out {
		out[i] = int(math.Round(t*float64(pa[i]) + (1-t)*float64(pb[i])))
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2]), true
}

func parseHex(s string) ([3]int, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return [3]int{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return [3]int{}, false
	}
	return [3]int{int(v >> 16), int(v >> 8 & 0xff), int(v & 0xff)}, true
}

// grapeToneColors maps tones to theme colors, blended like the site's CSS
// (color-mix in srgb), with plain theme colors where blending isn't possible.
func (a *App) grapeToneColors() map[string]lipgloss.Style {
	c := a.theme.Colors
	// Reacting (stage.go): the grapes flush red at a problem; the leaf
	// glows at a success.
	c.Purple = a.grapePurple()
	if tint, success := a.reactionTint(time.Now()); tint > 0 && success {
		if m, ok := mixHex(c.Foreground, c.Green, tint*.55); ok {
			c.Green = m
		}
	}
	pick := func(fallback string, mixA, mixB string, t float64) lipgloss.Style {
		col := fallback
		if m, ok := mixHex(mixA, mixB, t); ok {
			col = m
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(col))
	}
	if a.pick(layerLogo) == "golden" {
		t := a.grapeTones()
		return map[string]lipgloss.Style{
			"p3": lipgloss.NewStyle().Foreground(lipgloss.Color(t[3])).Bold(true),
			"p2": lipgloss.NewStyle().Foreground(lipgloss.Color(t[2])),
			"p1": lipgloss.NewStyle().Foreground(lipgloss.Color(t[1])),
			"p0": lipgloss.NewStyle().Foreground(lipgloss.Color(t[0])),
			"g2": lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green)),
			"g1": pick(c.Green, c.Green, c.Background, .60),
			"o2": lipgloss.NewStyle().Foreground(lipgloss.Color(c.Orange)),
			"o1": pick(c.Orange, c.Orange, c.Background, .60),
			"sp": lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true),
		}
	}
	return map[string]lipgloss.Style{
		"p3": pick(c.Foreground, c.Purple, c.Foreground, .30).Bold(true),
		"p2": lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)),
		"p1": pick(c.Purple, c.Purple, c.Background, .70),
		"p0": pick(c.Comment, c.Purple, c.Background, .42),
		"g2": lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green)),
		"g1": pick(c.Green, c.Green, c.Background, .60),
		"o2": lipgloss.NewStyle().Foreground(lipgloss.Color(c.Orange)),
		"o1": pick(c.Orange, c.Orange, c.Background, .60),
	}
}

// renderGrapeLogo draws the logo at the current light (the resting light
// when no animation is running), zone-marked so clicks can steer it.
func (a *App) renderGrapeLogo() string {
	light := grapeLight0
	if a.grapeLight != nil {
		light = a.grapeLight.cur
	}
	switch style := a.pick(layerLogo); style {
	case "pixel", "braille", "dotmatrix", "wireframe":
		return zone.Mark("grape-logo", a.renderLogoStyle(style, light))
	}
	styles := a.grapeToneColors()
	shaded := shadeGrapeLogo(grapeLogoSize, light)
	if a.pick(layerLogo) == "golden" {
		sparkle(shaded)
	}
	var b strings.Builder
	for i, row := range shaded {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j := 0; j < len(row); {
			k := j
			var run strings.Builder
			for k < len(row) && row[k].tone == row[j].tone {
				run.WriteByte(row[k].ch)
				k++
			}
			if st, ok := styles[row[j].tone]; ok {
				b.WriteString(st.Render(run.String()))
			} else {
				b.WriteString(run.String())
			}
			j = k
		}
	}
	return zone.Mark("grape-logo", b.String())
}
