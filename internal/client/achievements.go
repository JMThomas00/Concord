package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	{"first_words", "First Words", "Send a message", false},
	{"msgs_100", "Chatterbox", "Send 100 messages", false},
	{"msgs_1000", "Town Crier", "Send 1,000 messages", false},
	{"msgs_10000", "The Whole Vineyard Heard", "Send 10,000 messages", false},
	{"night_shift", "Night Shift", "Chat between 2 and 5 a.m.", false},
	{"voice_1", "Warm Voice", "Spend an hour in voice", false},
	{"voice_10", "Regular Caller", "Spend ten hours in voice", false},
	{"voice_100", "Radio Host", "Spend a hundred hours in voice", false},
	{"launch_500", "Grand Cru", "Start Concord 500 times", false},
	{"banners_150", "Font Fancier", "See 150 different banners", false},
	{"first_win", "First Victory", "Win a game in a plugin channel", false},
	{"wins_10", "Seasoned Player", "Win ten games", false},
	{"wins_50", "Grandmaster of the Vineyard", "Win fifty games", false},
	{"beat_computer", "Beat the Machine", "Beat the computer at a game", false},
	{"night_owl", "Night Owl", "See the moon (it rises after midnight)", false},
	{"go_to_bed", "Go to Bed!", "Catch the grapes dozing", false},
	{"egg_hunter", "Egg Hunter", "Find five easter eggs", false},
	{"egg_konami", "Up Up Down Down", "", true},
	{"egg_wine", "Sommelier", "", true},
	{"egg_burst", "Grape Burst", "", true},
	{"egg_disco", "Saturday Night Grapes", "", true},
	{"egg_name", "That's Me!", "", true},
	{"egg_slash_grape", "Make It Rain", "", true},
	{"egg_slash_disco", "Dance Floor", "", true},
	{"egg_slash_party", "Party Animal", "", true},
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
	a.toasts = append(a.toasts, &toast{title: ach.name, created: time.Now()})
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

// achievementTiers marks the counting achievements bronze, silver or gold.
var achievementTiers = map[string]string{
	"regular": "🥉", "vintner": "🥈", "launch_500": "🥇",
	"banners_50": "🥉", "banners_150": "🥈", "banners_all": "🥇",
	"msgs_100": "🥉", "msgs_1000": "🥈", "msgs_10000": "🥇",
	"voice_1": "🥉", "voice_10": "🥈", "voice_100": "🥇",
	"streak_3": "🥉", "streak_7": "🥈", "streak_30": "🥇",
	"first_win": "🥉", "wins_10": "🥈", "wins_50": "🥇",
}

// secretDone says what was done to earn a secret achievement, shown once
// it's earned (until then it stays a secret).
var secretDone = map[string]string{
	"egg_konami":      "Entered the Konami code",
	"egg_wine":        "Pressed the grapes into wine",
	"egg_burst":       "Typed grape on the About page",
	"egg_disco":       "Started a disco party",
	"egg_name":        "Saw your own name as the banner",
	"egg_slash_grape": "Found the hidden /grape command",
	"egg_slash_disco": "Found the hidden /disco command",
	"egg_slash_party": "Found the hidden /party command",
	"egg_corner":      "The bouncing logo hit a corner",
	"egg_screensaver": "Waited long enough for a screensaver",
	"egg_bbs":         "Dialled up the Concord BBS",
	"egg_halloween":   "Opened Concord on Halloween",
	"egg_december":    "Opened Concord in December",
	"egg_newyear":     "Opened Concord at the new year",
	"egg_april":       "Opened Concord on April Fools' Day",
	"egg_birthday":    "Opened Concord on its birthday",
}

// description says what an achievement is for.
func (ach achievement) description() string {
	if d := secretDone[ach.id]; d != "" {
		return d
	}
	return ach.hint
}
