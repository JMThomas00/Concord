package client

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNameBannerEveryStyle(t *testing.T) {
	for _, style := range nameRenderers {
		art, ok := renderNameArt("gh0st", style)
		if !ok || strings.TrimSpace(ansi.Strip(art)) == "" {
			t.Fatalf("%s: nothing drawn", style)
		}
	}
	if _, ok := renderNameArt("Zoë", "half"); ok {
		t.Fatal("drew a letter the font doesn't have")
	}
	if !nameCanBeDrawn("Amy-2") || nameCanBeDrawn("") {
		t.Fatal("nameCanBeDrawn")
	}
}

func TestStreaks(t *testing.T) {
	c := newCollection()
	day := time.Date(2026, 10, 1, 9, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		c.updateStreak(day.AddDate(0, 0, i))
		c.updateStreak(day.AddDate(0, 0, i).Add(time.Hour)) // twice the same day
	}
	if c.Streak != 3 || c.FirstLaunch != "2026-10-01" {
		t.Fatalf("streak %d, first %s", c.Streak, c.FirstLaunch)
	}
	c.updateStreak(day.AddDate(0, 0, 9))
	if c.Streak != 1 || c.BestStreak != 3 {
		t.Fatalf("after a gap: streak %d, best %d", c.Streak, c.BestStreak)
	}
}

func TestLabelAndFortune(t *testing.T) {
	name, notes := moodFromSeed(77).label(2026)
	if !strings.Contains(name, "2026") || !strings.Contains(name, moodFromSeed(77).code()) || !strings.HasPrefix(notes, "Notes of") {
		t.Fatalf("label %q / %q", name, notes)
	}
	d := time.Date(2026, 5, 5, 0, 0, 0, 0, time.Local)
	if fortuneOf(d) != fortuneOf(d.Add(20*time.Hour)) {
		t.Fatal("the fortune changed within a day")
	}
	if commas(1204) != "1,204" || commas(12) != "12" || commas(1234567) != "1,234,567" {
		t.Fatal("commas")
	}
}

// Every moment, in every season, draws the login screen at full size.
func TestMomentsAndSeasonsRender(t *testing.T) {
	a := eggApp(t)
	defer func() { moodClock = time.Now }()
	a.fx.stageAt = time.Now().Add(-4 * time.Second)
	for _, month := range []time.Month{time.April, time.July, time.October, time.January} {
		d := time.Date(2026, month, 15, 22, 0, 0, 0, time.Local)
		moodClock = func() time.Time { return d }
		for _, o := range findLayer(layerMoment).options {
			a.mood.picks[layerMoment] = o.id
			lines := strings.Split(a.View(), "\n")
			if len(lines) != 40 || ansi.StringWidth(lines[39]) != 120 {
				t.Fatalf("%s in %s: %d lines", o.id, month, len(lines))
			}
		}
	}
}

func TestGlitchKeepsTheFrameSize(t *testing.T) {
	g := parseFrame(strings.Repeat("some page text here\n", 10), 30, 10)
	for ms := 0; ms < 400; ms += 50 {
		glitch(g, time.Duration(ms)*time.Millisecond, 42)
		for _, row := range strings.Split(g.String(), "\n") {
			if w := ansi.StringWidth(row); w != 30 {
				t.Fatalf("row %d wide", w)
			}
		}
	}
}

// Space slows the loading screen down instead of skipping it.
func TestSpaceIsSlowMotion(t *testing.T) {
	a := eggApp(t)
	start := time.Now().Add(-time.Second)
	a.loading = &loadingState{kind: "calm", start: start, dur: loadingDur, lines: []string{"x"}}
	a.loading.advance(start.Add(500 * time.Millisecond))
	a.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if a.loading == nil {
		t.Fatal("space skipped the loading screen")
	}
	before := a.loading.elapsed
	a.loading.advance(a.loading.lastTick.Add(400 * time.Millisecond))
	if got := a.loading.elapsed - before; got != 100*time.Millisecond {
		t.Fatalf("400ms of slow motion moved the clock %v", got)
	}
}

func TestMazeCarvesSolvesAndWalks(t *testing.T) {
	m := newMaze(31, 15)
	for len(m.stack) > 0 {
		m.carve(7)
	}
	m.solve()
	if len(m.path) < 2 || m.path[0] != m.w+1 || m.path[len(m.path)-1] != (m.h-2)*m.w+m.w-2 {
		t.Fatalf("no way through: %d steps", len(m.path))
	}
	for i := 1; i < len(m.path); i++ {
		if d := abs(m.path[i] - m.path[i-1]); d != 1 && d != m.w {
			t.Fatal("the path jumps")
		}
	}
}

func TestBunchesPlaysOnAndClearsLines(t *testing.T) {
	b := &bunchState{}
	b.spawn(5)
	for i := 0; i < 20000; i++ {
		b.tick(5)
	}
	if b.lines == 0 {
		t.Fatal("the computer never cleared a line")
	}
}

func TestLifeSettlesAndStartsOver(t *testing.T) {
	l := newLife(20, 10, 3)
	for i := 0; i < 700 && !l.settled(); i++ {
		l.step()
	}
	if !l.settled() {
		t.Fatal("never settled")
	}
}
