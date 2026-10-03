package client

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Banner colourings (the mood's banner layer), applied once the banner's
// intro animation has finished.
//
//   solid      the theme's purple (renderBanner's own)
//   gradient   purple into pink, left to right
//   shimmer    the gradient, with a band of light sweeping across now and then
//   bbs        1990s ANSI art: cyan, blue and magenta top to bottom
//   green      green phosphor, with scanlines
//   rainbow    every colour, drifting
//   amber      amber phosphor, with scanlines (legendary)

// colourBanner colours the art in this mood's banner colouring; ok is false
// for solid (or a theme whose colours can't be mixed).
func (a *App) colourBanner(art string, now time.Time) (string, bool) {
	style := a.pick(layerBanner)
	if a.pick(layerLight) == "disco" {
		style = "rainbow" // the party
	}
	pal := a.loadingPalette()
	if _, ok := parseHex(a.theme.Colors.Purple); !ok || style == "" || style == "solid" {
		return "", false
	}
	lines := strings.Split(art, "\n")
	width := 1
	for _, l := range lines {
		width = max(width, len([]rune(l)))
	}
	t := now.Sub(time.Time{}).Seconds()
	colourAt := func(r, c int) string {
		x := float64(c) / float64(width)
		y := float64(r) / float64(max(1, len(lines)-1))
		switch style {
		case "gradient":
			return mix(pal.pink, pal.purple, x)
		case "shimmer":
			base := mix(pal.pink, pal.purple, x)
			// A band crosses every six seconds, leaning with the rows.
			band := math.Mod(t/6, 1)*1.6 - .3
			if d := math.Abs(x + y*.15 - band); d < .06 {
				return mix("#ffffff", base, 1-d/.06)
			}
			return base
		case "bbs":
			switch {
			case y < .34:
				return "#55ffff"
			case y < .67:
				return "#5555ff"
			}
			return "#aa00aa"
		case "green", "amber":
			col := "#33ff66"
			if style == "amber" {
				col = "#ffb000"
			}
			if r%2 == 1 {
				col = mix(col, pal.dark, .6) // scanlines
			}
			return col
		case "rainbow":
			h := math.Mod(x*360+t*40, 360)
			return hsvHex(h, .55, 1)
		}
		return pal.purple
	}
	var b strings.Builder
	for r, l := range lines {
		if r > 0 {
			b.WriteByte('\n')
		}
		for c, ch := range []rune(l) {
			if ch == ' ' {
				b.WriteRune(ch)
				continue
			}
			b.WriteString(sgrFor(colourAt(r, c), "", true))
			b.WriteRune(ch)
			b.WriteString("\x1b[0m")
		}
	}
	return b.String(), true
}

// hsvHex is a hue (degrees), saturation and value as "#rrggbb".
func hsvHex(h, s, v float64) string {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	var r, g, b float64
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := v - c
	return fmt.Sprintf("#%02x%02x%02x", int((r+m)*255+.5), int((g+m)*255+.5), int((b+m)*255+.5))
}
