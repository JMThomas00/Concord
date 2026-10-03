package client

import (
	"image"
	"image/color"
	"math"
	"strings"

	zone "github.com/lrstanley/bubblezone"
)

// The legendary "photo" grapes: on a terminal that can show pictures
// (Kitty, Sixel, iTerm2) the grapes are drawn as a real picture, smoothly
// shaded, in place of the characters. The logo's cells are left blank and
// View puts the picture into them after every effect has run, the same way
// plugin pictures are drawn (plugin_images.go). Anywhere else this option
// falls back to the shaded grapes.

// canShowPhoto reports whether this terminal draws real pictures.
func (a *App) canShowPhoto() bool {
	switch a.graphicsProtocol() {
	case gfxKitty, gfxSixel, gfxITerm2:
		return true
	}
	return false
}

// photoGrapes reports whether this launch shows the picture grapes now.
func (a *App) photoGrapes() bool {
	return isStageView(a.view) && a.pick(layerLogo) == "photo" && a.pick(layerLight) != "disco"
}

// blankLogo is the logo's footprint, empty, for the picture to sit in.
func blankLogo() string {
	L := grapeLogos[grapeLogoSize]
	row := strings.Repeat(" ", L.cols)
	rows := make([]string, L.rows)
	for i := range rows {
		rows[i] = row
	}
	return zone.Mark("grape-logo", strings.Join(rows, "\n"))
}

// grapePhoto renders the grapes as a picture, one pixel per logo unit.
func (a *App) grapePhoto() image.Image {
	if a.photoCache != nil && a.photoKey == a.theme.Colors.Purple {
		return a.photoCache
	}
	a.photoKey = a.theme.Colors.Purple
	L := grapeLogos[grapeLogoSize]
	w, h := int(float64(L.cols)*L.cw), int(float64(L.rows)*L.ch)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	c := a.theme.Colors
	dark := parseHexColor(mixOr(c.Purple, "#000000", .25))
	mid := parseHexColor(c.Purple)
	light := parseHexColor(mixOr(c.Purple, "#ffffff", .55))
	leafCol := map[byte]color.NRGBA{'G': parseHexColor(c.Green), 'g': parseHexColor(mixOr(c.Green, "#000000", .65)),
		'O': parseHexColor(c.Orange), 'o': parseHexColor(mixOr(c.Orange, "#000000", .65))}
	lerp := func(a, b color.NRGBA, t float64) color.NRGBA {
		f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
		return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			lum, spec, ok := grapeSample(L, float64(x)+.5, float64(y)+.5, grapeLight0)
			if ok {
				col := lerp(dark, mid, math.Min(1, lum*1.4))
				if lum > .7 {
					col = lerp(mid, light, (lum-.7)/.3)
				}
				img.SetNRGBA(x, y, lerp(col, color.NRGBA{255, 255, 255, 255}, math.Min(1, spec*1.2)))
				continue
			}
			// The leaf and stem, from the traced characters' cells.
			r, cc := int(float64(y)/L.ch), int(float64(x)/L.cw)
			if r < L.rows && cc < L.cols && L.chars[r][cc] != ' ' {
				if col, ok := leafCol[L.tones[r][cc]]; ok {
					img.SetNRGBA(x, y, col)
				}
			}
		}
	}
	a.photoCache = img
	return img
}

// placePhoto puts the picture grapes into the finished frame, at the
// logo's place, and returns the frame. For Sixel and iTerm2 it adds a
// raster for paintRasterImages to draw.
func (a *App) placePhoto(out string) string {
	z := zone.Get("grape-logo")
	if z == nil || z.IsZero() {
		return out
	}
	L := grapeLogos[grapeLogoSize]
	lines := strings.Split(out, "\n")
	if z.StartY+L.rows > len(lines) {
		return out
	}
	store := a.assets()
	img := a.grapePhoto()
	key := "concord-grapes:" + a.theme.Colors.Purple
	switch proto := a.graphicsProtocol(); proto {
	case gfxKitty:
		id, transmit := store.kittyImage(key, img, L.cols, L.rows)
		for r := 0; r < L.rows; r++ {
			lines[z.StartY+r] = spliceCells(lines[z.StartY+r], z.StartX, L.cols, kittyCells(id, r, L.cols))
		}
		lines[0] = transmit + lines[0]
	case gfxSixel, gfxITerm2:
		blob := store.rasterBlob(proto, key, img, L.cols, L.rows)
		left, right := cutCells(lines[z.StartY], z.StartX)
		lines[z.StartY] = left + rasterMarker(len(a.paneRasters)) + right
		a.paneRasters = append(a.paneRasters, rasterImage{key: key, blob: blob, rows: L.rows, cols: L.cols})
	}
	return strings.Join(lines, "\n")
}
