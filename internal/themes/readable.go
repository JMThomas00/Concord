package themes

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Contrast floors (WCAG 2 ratios) every theme is held to when it loads:
// body text, secondary text (accents, dim text, timestamps, names,
// statuses), and a selection bar against the background, so the row it
// marks can be seen.
const (
	minTextContrast   = 4.5
	minAccentContrast = 3.0
	minBarContrast    = 1.3
)

// MakeReadable nudges the colours that would be hard to read, keeping
// their hue: a text colour too close to what it's drawn on moves towards
// black (light themes) or white (dark ones). Selection bars go first: one
// that text can't be read on moves back towards the page (so the theme's
// own colours can stay), and one too faint to see moves away from it.
// Colours that already pass are left exactly as the theme has them.
// Themes without hex colours (terminal-default follows the terminal's
// own) are left alone.
func (t *Theme) MakeReadable() {
	c, s := &t.Colors, &t.Semantic
	bg, ok := parseHex(c.Background)
	if !ok {
		return
	}
	ink := rgb{0, 0, 0} // the way text moves to stand out
	if bg.luminance() < 0.18 {
		ink = rgb{1, 1, 1}
	}
	pages := []string{c.Background, s.ChatBg, s.SidebarBg}
	accents := []*string{&c.Comment, &c.Red, &c.Orange, &c.Yellow, &c.Green, &c.Cyan, &c.Purple, &c.Pink}

	raise(&c.Foreground, ink, minTextContrast, pages...)
	fitBar(&s.SidebarSelected, c.Background, ink,
		need{&c.Foreground, minTextContrast}, need{&s.SidebarFg, minTextContrast})
	onSelection := []need{{&c.Foreground, minTextContrast}} // key hints, the status bar, chips
	for _, a := range accents {
		onSelection = append(onSelection, need{a, minAccentContrast})
	}
	fitBar(&c.Selection, c.Background, ink, onSelection...)
	fitBar(&c.CurrentLine, c.Background, ink, need{&c.Green, minAccentContrast}) // inline code in messages

	raise(&c.Foreground, ink, minTextContrast, s.SidebarSelected, c.Selection)
	raise(&s.ChatFg, ink, minTextContrast, append(pages, c.Selection)...)
	raise(&s.SidebarFg, ink, minTextContrast, s.SidebarBg, s.SidebarSelected)
	raise(&s.InputFg, ink, minTextContrast, s.InputBg)
	for _, a := range accents {
		raise(a, ink, minAccentContrast, append(pages, c.Selection)...)
	}
	raise(&c.Comment, ink, minAccentContrast, s.InputBg) // the input's placeholder and hints
	raise(&c.Cyan, ink, minAccentContrast, s.InputBg)
	raise(&c.Green, ink, minAccentContrast, c.CurrentLine)
	for _, col := range []*string{
		&s.ChatTimestamp, &s.ChatUsernameSelf, &s.ChatUsernameOther, &s.ChatMention,
		&s.StatusOnline, &s.StatusIdle, &s.StatusDND, &s.StatusOffline,
		&s.Error, &s.Warning, &s.Success, &s.Info,
	} {
		raise(col, ink, minAccentContrast, pages...)
	}
}

// need is a colour drawn on a bar, and the contrast it needs there.
type need struct {
	col *string
	min float64
}

// raise moves *col towards ink until it reaches min contrast against every
// (hex) background given.
func raise(col *string, ink rgb, min float64, bgs ...string) {
	c, ok := parseHex(*col)
	if !ok {
		return
	}
	var against []rgb
	for _, b := range bgs {
		if v, ok := parseHex(b); ok {
			against = append(against, v)
		}
	}
	reads := func(c rgb) bool {
		for _, b := range against {
			if contrast(c, b) < min {
				return false
			}
		}
		return true
	}
	if reads(c) {
		return
	}
	for f := 0.02; f < 1; f += 0.02 {
		if m, _ := parseHex(c.mix(ink, f).hex()); reads(m) {
			*col = m.hex()
			return
		}
	}
	*col = ink.hex()
}

// fitBar keeps a selection bar between the background and the text on
// it: a bar the text can't be read on moves back towards the page, as far
// as it can while still showing, and one too faint to see moves away.
func fitBar(bar *string, page string, ink rgb, needs ...need) {
	b, ok1 := parseHex(*bar)
	p, ok2 := parseHex(page)
	if !ok1 || !ok2 {
		return
	}
	readable := func(b rgb) bool {
		for _, n := range needs {
			if c, ok := parseHex(*n.col); ok && contrast(c, b) < n.min {
				return false
			}
		}
		return true
	}
	start := b
	for i := 1; !readable(b) && i <= 50; i++ {
		next, _ := parseHex(start.mix(p, float64(i)/50).hex()) // as it will be written
		if contrast(next, p) < minBarContrast {
			break
		}
		b = next
	}
	for i := 1; contrast(b, p) < minBarContrast && i <= 50; i++ {
		b, _ = parseHex(b.mix(ink, 0.02).hex())
	}
	if b != start {
		*bar = b.hex()
	}
}

type rgb struct{ r, g, b float64 } // 0-1, sRGB

func parseHex(s string) (rgb, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[0] != '#' {
		return rgb{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return rgb{}, false
	}
	return rgb{float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255}, true
}

func (c rgb) hex() string {
	b := func(x float64) int { return int(math.Round(math.Max(0, math.Min(1, x)) * 255)) }
	return fmt.Sprintf("#%02X%02X%02X", b(c.r), b(c.g), b(c.b))
}

func (c rgb) mix(o rgb, f float64) rgb {
	return rgb{c.r + (o.r-c.r)*f, c.g + (o.g-c.g)*f, c.b + (o.b-c.b)*f}
}

// luminance is WCAG 2's relative luminance.
func (c rgb) luminance() float64 {
	lin := func(x float64) float64 {
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
}

func contrast(a, b rgb) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
