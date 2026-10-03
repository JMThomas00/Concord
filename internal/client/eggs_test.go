package client

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func eggApp(t *testing.T) *App {
	a := newLoginTestApp(t, 120, 40, true)
	a.view = ViewLogin
	a.uiConfig = &UIConfig{}
	a.configMgr = &ConfigManager{configFilePath: filepath.Join(t.TempDir(), "config.json")}
	a.mood = moodFromSeed(4)
	return a
}

func pressKeys(a *App, keys ...string) {
	for _, k := range keys {
		var m tea.KeyMsg
		switch k {
		case "up":
			m = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			m = tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			m = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			m = tea.KeyMsg{Type: tea.KeyRight}
		default:
			m = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		a.Update(m)
	}
}

func TestKonamiCode(t *testing.T) {
	a := eggApp(t)
	a.loginPassword.Focus()
	pressKeys(a, konamiCode...)
	if a.egg == nil || a.egg.kind != "konami" {
		t.Fatal("no invasion")
	}
	if strings.Contains(a.loginPassword.Value(), "ba") {
		t.Fatalf("the code's letters stayed in the password: %q", a.loginPassword.Value())
	}
	if a.coll().Achievements["egg_konami"] == "" {
		t.Fatal("not recorded")
	}
	// It draws, at full size, at every stage.
	for _, p := range []float64{.1, .5, .9} {
		a.egg.start = time.Now().Add(-time.Duration(p * float64(a.egg.dur)))
		if lines := strings.Split(a.View(), "\n"); len(lines) != 40 || ansi.StringWidth(lines[0]) != 120 {
			t.Fatalf("at %.1f: %d lines", p, len(lines))
		}
	}
}

func TestEggsOffWhenSurpriseIsOff(t *testing.T) {
	a := eggApp(t)
	a.uiConfig.Display.Surprise = "off"
	pressKeys(a, konamiCode...)
	if a.egg != nil {
		t.Fatal("egg with Surprise Me off")
	}
	moodClock = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local) }
	defer func() { moodClock = time.Now }()
	if a.calendar() != "" {
		t.Fatal("calendar with Surprise Me off")
	}
}

func TestCalendar(t *testing.T) {
	a := eggApp(t)
	defer func() { moodClock = time.Now }()
	for date, want := range map[string]string{
		"2026-10-31": "halloween", "2026-12-12": "december", "2026-12-31": "newyear",
		"2027-01-01": "newyear", "2027-04-01": "april", "2027-02-01": "birthday", "2027-03-03": "",
	} {
		d, _ := time.ParseInLocation("2006-01-02", date, time.Local)
		moodClock = func() time.Time { return d.Add(23 * time.Hour) }
		if got := a.calendar(); got != want {
			t.Errorf("%s: %q, want %q", date, got, want)
		}
		a.fx.stageAt = time.Now().Add(-3 * time.Second)
		if lines := strings.Split(a.View(), "\n"); len(lines) != 40 {
			t.Fatalf("%s: %d lines", date, len(lines))
		}
	}
}

func TestFlipArt(t *testing.T) {
	// Turned round, the short line ends up on the right.
	if got := flipArt("ab_\n(c"); got != " c)\n‾ba" {
		t.Fatalf("got %q", got)
	}
}

func TestScreensaverStartsWhenIdleAndAnyKeyEndsIt(t *testing.T) {
	a := eggApp(t)
	a.lastInput = time.Now()
	a.handleIdleCheck()
	if a.saver != nil {
		t.Fatal("saver too soon")
	}
	a.lastInput = time.Now().Add(-saverAfter)
	a.handleIdleCheck()
	if a.saver == nil {
		t.Fatal("no saver")
	}
	for _, kind := range saverKinds {
		a.saver.kind = kind
		for i := 0; i < 5; i++ {
			a.saver.last = time.Now().Add(-100 * time.Millisecond)
			if lines := strings.Split(a.View(), "\n"); len(lines) != 40 || ansi.StringWidth(lines[39]) != 120 {
				t.Fatalf("%s: %d lines", kind, len(lines))
			}
		}
	}
	before := a.loginPassword.Value()
	pressKeys(a, "x")
	if a.saver != nil || a.loginPassword.Value() != before {
		t.Fatal("the key didn't just wake the screen")
	}
}

func TestBounceCelebratesAnExactCorner(t *testing.T) {
	a := eggApp(t)
	a.saver = &saverState{kind: "bounce", x: 1, y: 1, dx: -20, dy: -20, last: time.Now()}
	g := newGrid(a.width, a.height)
	a.saverBounce(g, .1, a.loadingPalette())
	if a.coll().Eggs["corner"] == "" {
		t.Fatal("corner hit not celebrated")
	}
}

func TestTenClicksMakeWine(t *testing.T) {
	a := eggApp(t)
	a.View() // places the grapes' zone
	time.Sleep(20 * time.Millisecond)
	z := waitForZone(t, "grape-logo")
	if z == nil {
		t.Fatal("no grapes zone")
	}
	for i := 0; i < 10; i++ {
		a.watchGrapeClicks(tea.MouseMsg{X: z.StartX + 5, Y: z.StartY + 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	if a.egg == nil || a.egg.kind != "wine" {
		t.Fatal("no wine")
	}
	for _, p := range []float64{.1, .5, .9} {
		a.egg.start = time.Now().Add(-time.Duration(p * float64(a.egg.dur)))
		if lines := strings.Split(a.View(), "\n"); len(lines) != 40 {
			t.Fatalf("at %.1f: %d lines", p, len(lines))
		}
	}
}
