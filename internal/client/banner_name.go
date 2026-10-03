package client

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Now and then (about one launch in eight) the banner spells the profile's
// own name instead of CONCORD, as if the machine is greeting you. The 327
// banners are pre-drawn art of one word, so names are drawn from a small
// pixel font, in one of several renderings.

// pixelFont is 5×5 glyphs, '#' for a lit pixel.
var pixelFont = map[rune][5]string{
	'A':  {" ### ", "#   #", "#####", "#   #", "#   #"},
	'B':  {"#### ", "#   #", "#### ", "#   #", "#### "},
	'C':  {" ####", "#    ", "#    ", "#    ", " ####"},
	'D':  {"#### ", "#   #", "#   #", "#   #", "#### "},
	'E':  {"#####", "#    ", "#### ", "#    ", "#####"},
	'F':  {"#####", "#    ", "#### ", "#    ", "#    "},
	'G':  {" ####", "#    ", "#  ##", "#   #", " ### "},
	'H':  {"#   #", "#   #", "#####", "#   #", "#   #"},
	'I':  {"#####", "  #  ", "  #  ", "  #  ", "#####"},
	'J':  {"  ###", "   # ", "   # ", "#  # ", " ##  "},
	'K':  {"#   #", "#  # ", "###  ", "#  # ", "#   #"},
	'L':  {"#    ", "#    ", "#    ", "#    ", "#####"},
	'M':  {"#   #", "## ##", "# # #", "#   #", "#   #"},
	'N':  {"#   #", "##  #", "# # #", "#  ##", "#   #"},
	'O':  {" ### ", "#   #", "#   #", "#   #", " ### "},
	'P':  {"#### ", "#   #", "#### ", "#    ", "#    "},
	'Q':  {" ### ", "#   #", "# # #", "#  # ", " ## #"},
	'R':  {"#### ", "#   #", "#### ", "#  # ", "#   #"},
	'S':  {" ####", "#    ", " ### ", "    #", "#### "},
	'T':  {"#####", "  #  ", "  #  ", "  #  ", "  #  "},
	'U':  {"#   #", "#   #", "#   #", "#   #", " ### "},
	'V':  {"#   #", "#   #", "#   #", " # # ", "  #  "},
	'W':  {"#   #", "#   #", "# # #", "## ##", "#   #"},
	'X':  {"#   #", " # # ", "  #  ", " # # ", "#   #"},
	'Y':  {"#   #", " # # ", "  #  ", "  #  ", "  #  "},
	'Z':  {"#####", "   # ", "  #  ", " #   ", "#####"},
	'0':  {" ### ", "#  ##", "# # #", "##  #", " ### "},
	'1':  {"  #  ", " ##  ", "  #  ", "  #  ", " ### "},
	'2':  {" ### ", "#   #", "  ## ", " #   ", "#####"},
	'3':  {"#### ", "    #", " ### ", "    #", "#### "},
	'4':  {"#  # ", "#  # ", "#####", "   # ", "   # "},
	'5':  {"#####", "#    ", "#### ", "    #", "#### "},
	'6':  {" ### ", "#    ", "#### ", "#   #", " ### "},
	'7':  {"#####", "    #", "   # ", "  #  ", "  #  "},
	'8':  {" ### ", "#   #", " ### ", "#   #", " ### "},
	'9':  {" ### ", "#   #", " ####", "    #", " ### "},
	' ':  {"   ", "   ", "   ", "   ", "   "},
	'-':  {"     ", "     ", " ### ", "     ", "     "},
	'_':  {"     ", "     ", "     ", "     ", "#####"},
	'.':  {"  ", "  ", "  ", "  ", "# "},
	'!':  {"# ", "# ", "# ", "  ", "# "},
	'?':  {" ### ", "#   #", "  ## ", "     ", "  #  "},
	'\'': {"# ", "# ", "  ", "  ", "  "},
}

// nameBitmap lays a name out as rows of pixels, or false when it has a
// character the font doesn't have.
func nameBitmap(name string) ([5]string, bool) {
	var rows [5]string
	for i, r := range strings.ToUpper(name) {
		g, ok := pixelFont[r]
		if !ok {
			return rows, false
		}
		for y := range rows {
			if i > 0 {
				rows[y] += " "
			}
			rows[y] += g[y]
		}
	}
	return rows, true
}

var nameRenderers = []string{"half", "block", "shadow", "dots", "braille", "hash"}

// renderNameArt draws a name's pixels in one style.
func renderNameArt(name, style string) (string, bool) {
	px, ok := nameBitmap(name)
	if !ok || strings.TrimSpace(name) == "" {
		return "", false
	}
	on := func(y, x int) bool { return y >= 0 && y < 5 && x >= 0 && x < len(px[y]) && px[y][x] == '#' }
	w := len(px[0])
	var out []string
	switch style {
	case "half": // two pixel rows per line
		for y := 0; y < 5; y += 2 {
			var b strings.Builder
			for x := 0; x < w; x++ {
				switch t, u := on(y, x), on(y+1, x); {
				case t && u:
					b.WriteRune('█')
				case t:
					b.WriteRune('▀')
				case u:
					b.WriteRune('▄')
				default:
					b.WriteRune(' ')
				}
			}
			out = append(out, b.String())
		}
	case "shadow": // solid letters with a shade behind them
		for y := 0; y < 6; y++ {
			var b strings.Builder
			for x := 0; x <= w; x++ {
				switch {
				case on(y, x):
					b.WriteRune('█')
				case on(y-1, x-1):
					b.WriteRune('░')
				default:
					b.WriteRune(' ')
				}
			}
			out = append(out, b.String())
		}
	case "braille": // eight pixels per character: tiny
		for y := 0; y < 5; y += 4 {
			var b strings.Builder
			for x := 0; x < w; x += 2 {
				var bits rune
				for dy := 0; dy < 4; dy++ {
					for dx := 0; dx < 2; dx++ {
						if on(y+dy, x+dx) {
							bits |= brailleBits[dy][dx]
						}
					}
				}
				b.WriteRune(0x2800 + bits)
			}
			out = append(out, b.String())
		}
	default: // one character per pixel
		ch := map[string]rune{"block": '█', "dots": '●', "hash": '#'}[style]
		for y := 0; y < 5; y++ {
			var b strings.Builder
			for x := 0; x < w; x++ {
				if on(y, x) {
					b.WriteRune(ch)
				} else {
					b.WriteRune(' ')
				}
			}
			out = append(out, b.String())
		}
	}
	return trimBannerArt(strings.Join(out, "\n")), true
}

// nameCanBeDrawn reports whether every letter of a name is in the font.
func nameCanBeDrawn(name string) bool {
	for _, r := range strings.ToUpper(name) {
		if _, ok := pixelFont[r]; !ok || unicode.IsControl(r) {
			return false
		}
	}
	return name != ""
}

// pickNameBanner decides, once a launch, whether the banner is your name.
func (a *App) pickNameBanner() {
	a.nameBanner = ""
	if a.surprise() != surpriseFull || a.localIdentity == nil || !nameCanBeDrawn(a.localIdentity.Alias) {
		return
	}
	if cellHash(uint64(a.mood.seed), 0, 0, 14) >= 1.0/8 {
		return
	}
	style := nameRenderers[int(cellHash(uint64(a.mood.seed), 0, 0, 15)*float64(len(nameRenderers)))]
	a.nameBanner, _ = renderNameArt(a.localIdentity.Alias, style)
}

// nameBannerFits reports whether the name banner fits the logo box now.
func (a *App) nameBannerFits() bool {
	if a.nameBanner == "" {
		return false
	}
	g, ok := a.currentLockup()
	if !ok {
		return false
	}
	lines := strings.Split(a.nameBanner, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	return w <= g.boxW && len(lines) <= g.slot
}
