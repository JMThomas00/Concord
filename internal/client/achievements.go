package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// achievement is one entry on Settings > About. A locked one shows its hint;
// a secret one stays hidden until it's earned.
type achievement struct {
	id     string
	name   string
	hint   string
	secret bool
}

var achievements = []achievement{
	{"first_light", "First Light", "Start Concord for the first time", false},
	{"regular", "Regular", "Start Concord 10 times", false},
	{"vintner", "Vintner", "Start Concord 100 times", false},
	{"legendary", "Once in a Purple Moon", "See something legendary", false},
	{"all_loading", "Seen It Load", "See every loading screen", false},
	{"all_logos", "Grape Expectations", "See every grape style", false},
	{"all_backgrounds", "Scenery", "See every background", false},
	{"banners_50", "Wordsmith", "See 50 different banners", false},
	{"banners_all", "Complete Works", "See every banner", false},
	{"shuffler", "Shuffler", "Shuffle the banner 25 times (Ctrl+R)", false},
	{"homebody", "Homebody", "Lock a mood you like", false},
	{"impatient", "Impatient", "Skip 20 loading screens", false},
	{"streak_3", "Three Days on the Vine", "Open Concord three days running", false},
	{"streak_7", "A Week of Wine", "Open Concord seven days running", false},
	{"streak_30", "A Month in the Cellar", "Open Concord thirty days running", false},
	{"night_owl", "Night Owl", "Start Concord between 2 and 5 a.m.", false},
	{"egg_hunter", "Egg Hunter", "Find five easter eggs", false},
	{"egg_konami", "Up Up Down Down", "", true},
	{"egg_wine", "Sommelier", "", true},
	{"egg_burst", "Grape Burst", "", true},
	{"egg_disco", "Saturday Night Grapes", "", true},
	{"egg_name", "That's Me!", "", true},
	{"egg_corner", "Right in the Corner", "", true},
	{"egg_screensaver", "Away From Keyboard", "", true},
	{"egg_bbs", "Carrier Detected", "", true},
	{"egg_halloween", "Spooky Grapes", "", true},
	{"egg_december", "Mulled Wine", "", true},
	{"egg_newyear", "Countdown", "", true},
	{"egg_april", "Upside Down", "", true},
	{"egg_birthday", "Many Happy Returns", "", true},
}

func findAchievement(id string) (achievement, bool) {
	for _, a := range achievements {
		if a.id == id {
			return a, true
		}
	}
	return achievement{}, false
}

// toast is an achievement announcement in the top-right corner.
type toast struct {
	label string // the first line; "" for an achievement
	title string
	shown time.Time // zero until it reaches the front of the queue
}

// unlock earns an achievement, once, and queues its toast.
func (a *App) unlock(id string) {
	ach, ok := findAchievement(id)
	if !ok {
		return
	}
	c := a.coll()
	if _, done := c.Achievements[id]; done {
		return
	}
	c.Achievements[id] = today()
	a.saveCollection()
	a.toasts = append(a.toasts, &toast{title: ach.name})
}

// currentToast is the toast to show now, retiring finished ones.
func (a *App) currentToast(now time.Time) *toast {
	for len(a.toasts) > 0 {
		t := a.toasts[0]
		if t.shown.IsZero() {
			t.shown = now
		}
		if now.Sub(t.shown) < toastTime {
			return t
		}
		a.toasts = a.toasts[1:]
	}
	return nil
}

// paintToast draws the toast over the top-right of a frame. Only the lines
// it covers are touched, so the rest of the frame (links, pictures) is
// left exactly as it was.
func (a *App) paintToast(frame string, t *toast, now time.Time) string {
	if a.width < 30 {
		return frame
	}
	accent := a.theme.Colors.Purple
	if accent == "" {
		accent = "#bd93f9"
	}
	label := "🏆 Achievement unlocked"
	if t.label != "" {
		label = t.label
	}
	w := max(runewidth.StringWidth(label), runewidth.StringWidth(t.title)) + 4
	if w > a.width-2 {
		w = a.width - 2
	}
	// Slide in from the right over the first 250ms, out over the last.
	age := now.Sub(t.shown)
	slide := 1.0
	if age < 250*time.Millisecond {
		slide = easeOutCubic(float64(age) / float64(250*time.Millisecond))
	} else if left := toastTime - age; left < 250*time.Millisecond {
		slide = easeOutCubic(float64(left) / float64(250*time.Millisecond))
	}
	box := lipgloss.NewStyle().Width(w).Padding(0, 2).
		Background(lipgloss.Color(a.theme.Colors.Background)).
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	lines := []string{
		box.Render(""),
		box.Foreground(lipgloss.Color(accent)).Bold(true).Render(label),
		box.Render(t.title),
		box.Render(""),
	}
	col := a.width - 1 - int(float64(w+1)*slide)
	return overlayLines(frame, 1, col, a.width, lines)
}

// overlayLines draws lines over frame starting at row, col, touching only
// those rows. A wide character cut in half by the overlay is blanked.
func overlayLines(frame string, row, col, width int, lines []string) string {
	rows := strings.Split(frame, "\n")
	for i, l := range lines {
		r := row + i
		if r < 0 || r >= len(rows) {
			continue
		}
		g := newGrid(width, 1)
		parseLine(rows[r], g.rows[0])
		over := newGrid(ansi.StringWidth(l), 1)
		parseLine(l, over.rows[0])
		for c, cell := range over.rows[0] {
			if cell.w != 0 {
				g.set(0, col+c, cell.ch, cell.sgr, cell.w)
			}
		}
		rows[r] = g.String()
	}
	return strings.Join(rows, "\n")
}

// achievementLine is one row of the About page's list.
func achievementLine(ach achievement, date string) string {
	if date != "" {
		return fmt.Sprintf("★ %s  %s", ach.name, date)
	}
	if ach.secret {
		return "☆ ???  (a secret)"
	}
	return fmt.Sprintf("☆ ???  %s", ach.hint)
}
