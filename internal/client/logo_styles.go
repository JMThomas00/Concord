package client

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/themes"
)

// Grape styles (the mood's logo layer). Every style draws the same eight
// spheres from grape_logo_data.go in the same 35×21 cells, under the same
// moving light, so the stage never shifts; only the drawing changes.
//
//   shaded     the ASCII shading (grape_logo.go)
//   pixel      half-blocks, two pixels per cell, like the concord-site
//   braille    2×4 dots per cell, shading by ordered dithering
//   dotmatrix  LED dots, bigger for brighter
//   wireframe  rotating meridians and parallels (legendary)
//   golden     the shading in gold, with the odd sparkle (legendary)

// grapeSample shades one point of the logo (in logo pixels): its
// brightness, its specular highlight, and whether a grape is there.
func grapeSample(L grapeLogoData, x, y float64, light [3]float64) (lum, spec float64, ok bool) {
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
		return 0, 0, false
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
	hv := norm3([3]float64{light[0], light[1], light[2] + 1})
	d := math.Max(0, n[0]*light[0]+n[1]*light[1]+n[2]*light[2])
	spec = math.Pow(math.Max(0, n[0]*hv[0]+n[1]*hv[1]+n[2]*hv[2]), 28)
	rim := math.Min(1, math.Max(0, (n[2]-.14)/.4))
	lum = math.Min(1, (.34+.66*d)*rim*(1-.9*occ)+.9*spec)
	return lum, spec, true
}

// grapeNormal is the surface normal of the front grape at a point, and
// which grape it is.
func grapeNormal(L grapeLogoData, x, y float64) (n [3]float64, which int) {
	which = -1
	bz := math.Inf(-1)
	for k, g := range L.grapes {
		dx, dy := (x-g[0])/g[2], (y-g[1])/g[2]
		if d := dx*dx + dy*dy; d < 1 {
			nz := math.Sqrt(1 - d)
			if z := g[3] + g[2]*nz; z > bz {
				bz, which, n = z, k, [3]float64{dx, dy, nz}
			}
		}
	}
	return n, which
}

// grapeTones is the logo's palette as colours: dark, mid, light, highlight.
func (a *App) grapeTones() [4]string {
	c := a.theme.Colors
	purple := a.grapePurple()
	if a.pick(layerLogo) == "golden" {
		return [4]string{"#6b4e00", "#b8860b", "#ffd700", "#fff6c8"}
	}
	t := [4]string{c.Comment, purple, purple, c.Foreground}
	if m, ok := mixHex(purple, c.Background, .42); ok {
		t[0] = m
	}
	if m, ok := mixHex(purple, c.Background, .70); ok {
		t[1] = m
	}
	if m, ok := mixHex(purple, c.Foreground, .30); ok {
		t[3] = m
	}
	return t
}

// toneFor picks the palette colour for a brightness.
func toneFor(t [4]string, lum, spec float64) string {
	switch {
	case spec > .5:
		return t[3]
	case lum > .6:
		return t[2]
	case lum > .3:
		return t[1]
	}
	return t[0]
}

// leafCell is the leaf and stem's own character at a cell, styled.
func (a *App) leafCell(L grapeLogoData, r, c int) string {
	ch := L.chars[r][c]
	if ch == ' ' {
		return " "
	}
	style, ok := a.grapeToneColors()[map[byte]string{'G': "g2", 'g': "g1", 'O': "o2", 'o': "o1"}[L.tones[r][c]]]
	if !ok {
		return string(ch)
	}
	return style.Render(string(ch))
}

// renderLogoStyle draws the logo in one of the non-ASCII styles.
func (a *App) renderLogoStyle(style string, light [3]float64) string {
	L := grapeLogos[grapeLogoSize]
	tones := a.grapeTones()
	now := time.Now()
	var b strings.Builder
	for r := 0; r < L.rows; r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		for c := 0; c < L.cols; c++ {
			x0, y0 := float64(c)*L.cw, float64(r)*L.ch
			var cell string
			switch style {
			case "pixel":
				cell = pixelCell(L, x0, y0, light, tones)
			case "braille":
				cell = brailleCell(L, x0, y0, light, tones, r, c)
			case "dotmatrix":
				cell = dotCell(L, x0, y0, light, tones)
			case "wireframe":
				cell = wireCell(L, x0, y0, now, a.theme.Colors)
			}
			if cell == "" {
				cell = a.leafCell(L, r, c)
			}
			b.WriteString(cell)
		}
	}
	return b.String()
}

func fg(col string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(col)) }

// pixelCell: the top and bottom halves of the cell as two pixels.
func pixelCell(L grapeLogoData, x0, y0 float64, light [3]float64, tones [4]string) string {
	top, ts, tok := grapeSample(L, x0+L.cw/2, y0+L.ch/4, light)
	bot, bs, bok := grapeSample(L, x0+L.cw/2, y0+L.ch*3/4, light)
	switch {
	case tok && bok:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(toneFor(tones, top, ts))).
			Background(lipgloss.Color(toneFor(tones, bot, bs))).Render("▀")
	case tok:
		return fg(toneFor(tones, top, ts)).Render("▀")
	case bok:
		return fg(toneFor(tones, bot, bs)).Render("▄")
	}
	return ""
}

// bayer4 is a 4×4 ordered-dither threshold matrix.
var bayer4 = [4][4]float64{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

// brailleBits maps a dot at (column, row) in a cell to its braille bit.
var brailleBits = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

// brailleCell: eight dots per cell, lit by dithering the brightness.
func brailleCell(L grapeLogoData, x0, y0 float64, light [3]float64, tones [4]string, r, c int) string {
	var bits rune
	any := false
	sum, best := 0.0, 0.0
	n := 0
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			lum, spec, ok := grapeSample(L, x0+(float64(dx)+.5)*L.cw/2, y0+(float64(dy)+.5)*L.ch/4, light)
			if !ok {
				continue
			}
			any = true
			sum += lum
			best = math.Max(best, spec)
			n++
			threshold := (bayer4[(r*4+dy)%4][(c*2+dx)%4] + .5) / 16
			if lum*1.15 > threshold {
				bits |= brailleBits[dy][dx]
			}
		}
	}
	if !any {
		return ""
	}
	if bits == 0 {
		return " "
	}
	return fg(toneFor(tones, sum/float64(n), best)).Render(string(0x2800 + bits))
}

// dotCell: an LED that's bigger the brighter its spot.
func dotCell(L grapeLogoData, x0, y0 float64, light [3]float64, tones [4]string) string {
	lum, spec, ok := grapeSample(L, x0+L.cw/2, y0+L.ch/2, light)
	if !ok {
		return ""
	}
	dots := []string{"·", "∙", "•", "●"}
	return fg(toneFor(tones, lum, spec)).Render(dots[min(3, int(lum*4))])
}

// wireCell: each grape as a wireframe globe turning slowly, drawn in
// braille: its outline, three meridians and two parallels.
func wireCell(L grapeLogoData, x0, y0 float64, now time.Time, c themes.ThemeColors) string {
	spin := float64(now.UnixMilli()%12000) / 12000 * 2 * math.Pi
	var bits rune
	any := false
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			n, which := grapeNormal(L, x0+(float64(dx)+.5)*L.cw/2, y0+(float64(dy)+.5)*L.ch/4)
			if which < 0 {
				continue
			}
			any = true
			// Turn the globe about its vertical axis.
			lon := math.Atan2(n[0], n[2]) + spin + float64(which)
			lat := math.Asin(math.Max(-1, math.Min(1, n[1])))
			on := n[2] < .22 || // the outline
				math.Abs(math.Sin(lon*1.5)) < .16 || // meridians
				math.Abs(math.Sin(lat*3)) < .12 // parallels
			if on {
				bits |= brailleBits[dy][dx]
			}
		}
	}
	if !any {
		return ""
	}
	if bits == 0 {
		return " "
	}
	return fg(c.Cyan).Render(string(0x2800 + bits))
}

// sparkle puts a couple of glints on the golden grapes, moving on every
// few hundred milliseconds.
func sparkle(rows [][]grapeCell) {
	var lit [][2]int
	for r, row := range rows {
		for c, cell := range row {
			if cell.tone == "p2" || cell.tone == "p3" {
				lit = append(lit, [2]int{r, c})
			}
		}
	}
	if len(lit) == 0 {
		return
	}
	beat := uint64(time.Now().UnixMilli() / 350)
	for i := 0; i < 2; i++ {
		p := lit[int(cellHash(beat, i, 0, 9)*float64(len(lit)))]
		rows[p[0]][p[1]] = grapeCell{ch: '*', tone: "sp"}
	}
}

// grapePurple is the grapes' colour now: the theme's purple, flushed red
// at a problem (stage.go), or cycling through colours under the disco light.
func (a *App) grapePurple() string {
	c := a.theme.Colors
	purple := c.Purple
	if _, ok := parseHex(purple); ok && a.pick(layerLight) == "disco" {
		purple = hsvHex(math.Mod(float64(time.Now().UnixMilli())/12, 360), .45, .97)
	}
	if sunrise() && a.surprise() == surpriseFull {
		if m, ok := mixHex(c.Orange, purple, .25); ok {
			purple = m // the grapes catch the dawn
		}
	}
	if tint, success := a.reactionTint(time.Now()); tint > 0 && !success {
		if m, ok := mixHex(c.Red, purple, tint*.8); ok {
			purple = m
		}
	}
	return purple
}
