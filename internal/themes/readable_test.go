package themes

import (
	"strings"
	"testing"
)

// Every theme, once loaded, is readable: body text at 4.5:1 against
// everything it's drawn on, secondary text (accents, dim text, names,
// statuses) at 3:1, and selection bars that show against the background.
// Some themes' own palettes fell short (Solarized Light's selection bar was
// 1.14:1, its yellow 1.9:1).
func TestEveryThemeIsReadable(t *testing.T) {
	for _, name := range ListAvailableThemes() {
		th, err := GetTheme(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		c, s := th.Colors, th.Semantic
		if _, ok := parseHex(c.Background); !ok {
			continue // follows the terminal's own colours
		}
		check := func(what, fg, bg string, min float64) {
			f, ok1 := parseHex(fg)
			b, ok2 := parseHex(bg)
			if !ok1 || !ok2 {
				return
			}
			if r := contrast(f, b); r < min {
				t.Errorf("%s: %s %s on %s is %.2f:1, want %.1f", name, what, fg, bg, r, min)
			}
		}
		surfaces := map[string]string{
			"background": c.Background, "chat_bg": s.ChatBg, "sidebar_bg": s.SidebarBg,
			"sidebar_selected": s.SidebarSelected, "selection": c.Selection, "input_bg": s.InputBg,
		}
		for _, bar := range []string{"sidebar_selected", "selection"} {
			check("foreground on "+bar, c.Foreground, surfaces[bar], minTextContrast)
			check(bar+" bar", surfaces[bar], c.Background, minBarContrast)
		}
		check("foreground", c.Foreground, c.Background, minTextContrast)
		check("chat_fg", s.ChatFg, s.ChatBg, minTextContrast)
		check("sidebar_fg", s.SidebarFg, s.SidebarBg, minTextContrast)
		check("input_fg", s.InputFg, s.InputBg, minTextContrast)
		check("sidebar_fg on sidebar_selected", s.SidebarFg, s.SidebarSelected, minTextContrast)
		check("chat_fg on selection", s.ChatFg, c.Selection, minTextContrast)
		pages := []string{"background", "chat_bg", "sidebar_bg"}
		// The palette's colours are drawn on the pages and on selection
		// (key hints, the status bar); comment and cyan on the input too.
		accents := map[string]string{
			"comment": c.Comment, "red": c.Red, "orange": c.Orange, "yellow": c.Yellow, "green": c.Green,
			"cyan": c.Cyan, "purple": c.Purple, "pink": c.Pink,
		}
		for an, a := range accents {
			for _, sn := range append(pages, "selection") {
				check(an+" on "+sn, a, surfaces[sn], minAccentContrast)
			}
		}
		check("comment on input_bg", c.Comment, s.InputBg, minAccentContrast)
		check("inline code (green on current_line)", c.Green, c.CurrentLine, minAccentContrast)
		check("cyan on input_bg", c.Cyan, s.InputBg, minAccentContrast)
		secondary := map[string]string{
			"chat_timestamp": s.ChatTimestamp, "chat_username_self": s.ChatUsernameSelf,
			"chat_username_other": s.ChatUsernameOther, "chat_mention": s.ChatMention,
			"status_online": s.StatusOnline, "status_idle": s.StatusIdle, "status_dnd": s.StatusDND,
			"status_offline": s.StatusOffline, "error": s.Error, "warning": s.Warning,
			"success": s.Success, "info": s.Info,
		}
		for an, a := range secondary {
			for _, sn := range pages {
				check(an+" on "+sn, a, surfaces[sn], minAccentContrast)
			}
		}
	}
}

// A theme that already reads well comes through exactly as written.
func TestReadableColoursAreLeftAlone(t *testing.T) {
	want := draculaTheme()
	got := GetDefaultTheme()
	if got.Colors.Foreground != want.Colors.Foreground || got.Colors.Background != want.Colors.Background ||
		got.Colors.Green != want.Colors.Green || got.Semantic.SidebarSelected != want.Semantic.SidebarSelected {
		t.Fatalf("Dracula's readable colours changed: %+v", got.Colors)
	}
}

// A colour that's too faint keeps its hue: Solarized Light's yellow
// darkens, it doesn't turn grey.
func TestAFaintColourKeepsItsHue(t *testing.T) {
	th, err := GetTheme("solarized_light")
	if err != nil {
		t.Fatal(err)
	}
	y, _ := parseHex(th.Colors.Yellow)
	if strings.EqualFold(th.Colors.Yellow, "#B58900") || !(y.r > y.g && y.g > y.b) {
		t.Fatalf("yellow %s: want a darker yellow", th.Colors.Yellow)
	}
}
