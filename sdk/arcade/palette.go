// Package arcade is the Concord Arcade standard for game plugins: a
// character-cell canvas with half-block pixels, the shaded pixel-font logo,
// seven-segment digits, colour roles taken from each viewer's Concord theme,
// the pass-and-draft reward system (Pit Passes, Gold Stars: each game names
// its own) and a generated chiptune sound kit.
//
// Screens are drawn on a Canvas the size of the viewer's pane and returned
// from a pane model's View as Canvas.String(). Everything is plain text with
// SGR colours, so it works in any terminal Concord runs in.
package arcade

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JMThomas00/Concord/sdk/wire"
)

// Color is one terminal colour: true colour, an ANSI palette index, or the
// terminal's own default.
type Color struct {
	kind    int // 0 default, 1 rgb, 2 ansi
	r, g, b uint8
	n       int
}

// RGB reports whether the colour is true colour, and its components.
func (c Color) RGB() (r, g, b uint8, ok bool) { return c.r, c.g, c.b, c.kind == 1 }

// ParseColor reads "#rrggbb", an ANSI index "0"-"15", or "" (default).
func ParseColor(s string) Color {
	s = strings.TrimSpace(s)
	if len(s) == 7 && s[0] == '#' {
		if v, err := strconv.ParseUint(s[1:], 16, 32); err == nil {
			return Color{kind: 1, r: uint8(v >> 16), g: uint8(v >> 8), b: uint8(v)}
		}
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n < 256 {
		return Color{kind: 2, n: n}
	}
	return Color{}
}

// Mix blends a toward b by t (0 = a, 1 = b). Only true colours can be
// mixed; otherwise it returns fallback.
func Mix(a, b Color, t float64, fallback Color) Color {
	if a.kind != 1 || b.kind != 1 {
		return fallback
	}
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return Color{kind: 1, r: m(a.r, b.r), g: m(a.g, b.g), b: m(a.b, b.b)}
}

func (c Color) sgr(bg bool) string {
	switch c.kind {
	case 1:
		if bg {
			return fmt.Sprintf("48;2;%d;%d;%d", c.r, c.g, c.b)
		}
		return fmt.Sprintf("38;2;%d;%d;%d", c.r, c.g, c.b)
	case 2:
		switch {
		case c.n < 8 && bg:
			return strconv.Itoa(40 + c.n)
		case c.n < 8:
			return strconv.Itoa(30 + c.n)
		case c.n < 16 && bg:
			return strconv.Itoa(100 + c.n - 8)
		case c.n < 16:
			return strconv.Itoa(90 + c.n - 8)
		case bg:
			return "48;5;" + strconv.Itoa(c.n)
		default:
			return "38;5;" + strconv.Itoa(c.n)
		}
	}
	if bg {
		return "49"
	}
	return "39"
}

// Paints are the colours a player can choose for their piece or car. They
// are theme roles, so they follow each viewer's theme. Purple is never a
// paint: it is the brand colour, kept for the grapes.
var Paints = []string{"cyan", "pink", "green", "orange", "yellow", "red", "fg"}

// PaintNames are the paints' display names.
var PaintNames = map[string]string{
	"cyan": "ICE BLUE", "pink": "BUBBLEGUM", "green": "LIME LEAF", "orange": "ORANGE CRUSH",
	"yellow": "LEMON DROP", "red": "CHERRY", "fg": "SILVER",
}

// dracula is the reference look, used for anything a theme leaves out.
var dracula = map[string]string{
	"background": "#282a36", "foreground": "#f8f8f2", "comment": "#6272a4", "current_line": "#44475a",
	"purple": "#bd93f9", "pink": "#ff79c6", "green": "#50fa7b", "yellow": "#f1fa8c",
	"orange": "#ffb86c", "red": "#ff5555", "cyan": "#8be9fd",
}

// Palette maps colour roles to the viewer's colours.
//
// Base roles come straight from the Concord theme: bg, fg, comment, line,
// purple, pink, green, yellow, orange, red, cyan. Derived roles:
//
//	hi      purple lightened: the top of the logo, grape shine
//	shadow  purple darkened: the logo's drop shadow
//	ghost   comment darkened: unlit segments, bulbs and locked silhouettes
//	dim     quiet text
//	tire    near black
//	hub     wheel hubs
//	<paint>D a darker shade of each paint, e.g. "cyanD"
//	<colour>B a background tint of purple and each paint, e.g. "greenB" (a
//	         chalkboard), falling back to "line" where colours can't mix
type Palette struct {
	roles map[string]Color
	codes map[string][2]string // each role's SGR parameters as foreground and background, worked out once
}

// NewPalette builds the roles from a viewer's theme (nil: Dracula).
func NewPalette(t *wire.PaneTheme) Palette {
	src := map[string]string{}
	for k, v := range dracula {
		src[k] = v
	}
	if t != nil {
		for k, v := range t.Palette {
			src[k] = v
		}
	}
	get := func(k string) Color { return ParseColor(src[k]) }
	p := Palette{roles: map[string]Color{
		"bg": get("background"), "fg": get("foreground"), "comment": get("comment"), "line": get("current_line"),
		"purple": get("purple"), "pink": get("pink"), "green": get("green"), "yellow": get("yellow"),
		"orange": get("orange"), "red": get("red"), "cyan": get("cyan"),
	}}
	r := p.roles
	if r["line"].kind == 0 && src["current_line"] == "" {
		r["line"] = ParseColor(src["selection"])
	}
	black := Color{kind: 1}
	brightBlack := Color{kind: 2, n: 8}
	r["hi"] = Mix(r["purple"], r["fg"], .45, r["purple"])
	r["shadow"] = Mix(r["purple"], r["bg"], .72, brightBlack)
	r["ghost"] = Mix(r["comment"], r["bg"], .62, brightBlack)
	r["dim"] = Mix(r["fg"], r["bg"], .42, r["comment"])
	r["tire"] = Mix(r["bg"], black, .55, Color{kind: 2, n: 0})
	r["hub"] = Mix(r["comment"], r["fg"], .3, r["comment"])
	for _, paint := range Paints {
		r[paint+"D"] = Mix(r[paint], r["bg"], .45, r[paint])
	}
	for _, col := range append([]string{"purple"}, Paints...) {
		r[col+"B"] = Mix(r[col], r["bg"], .8, r["line"])
	}
	p.codes = make(map[string][2]string, len(r))
	for role, c := range r {
		p.codes[role] = [2]string{c.sgr(false), c.sgr(true)}
	}
	return p
}

// code is a role's SGR parameters as a foreground ("38;2;…") or background,
// the default ("39" / "49") for an unknown role.
func (p Palette) code(role string, bg bool) string {
	i := 0
	if bg {
		i = 1
	}
	if c, ok := p.codes[role]; ok {
		return c[i]
	}
	return p.Get(role).sgr(bg)
}

// Get returns a role's colour (the default colour for an unknown role).
func (p Palette) Get(role string) Color { return p.roles[role] }
