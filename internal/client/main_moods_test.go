package client

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

func TestTypingVerbsAreSteadyAndOccasional(t *testing.T) {
	a := eggApp(t)
	grape := 0
	for i := 0; i < 600; i++ {
		id := uuid.New()
		v := a.typingVerb(id)
		if v != a.typingVerb(id) {
			t.Fatal("the verb changed between calls")
		}
		if v != "" {
			grape++
		}
	}
	if grape < 120 || grape > 280 {
		t.Fatalf("%d of 600 typists got a grape verb, want about a third", grape)
	}
	a.uiConfig.Display.Surprise = "calm"
	if a.typingVerb(uuid.New()) != "" && a.typingVerb(uuid.New()) != "" {
		t.Fatal("grape verbs with Surprise Me calm")
	}
}

func TestMessagesCountAndCelebrate(t *testing.T) {
	a := eggApp(t)
	server := uuid.New()
	a.onMessageSent(server)
	if a.coll().Achievements["first_words"] == "" {
		t.Fatal("no First Words")
	}
	if len(a.toasts) == 0 || a.egg == nil || a.egg.kind != "confetti" {
		t.Fatal("first message on a server not celebrated")
	}
	before := len(a.toasts)
	a.onMessageSent(server)
	if len(a.toasts) != before {
		t.Fatal("celebrated the second message too")
	}
}

func TestFirstTimeInVoiceNeedsABaseline(t *testing.T) {
	a := eggApp(t)
	a.onVoiceJoin(uuid.New(), "amy", true) // the very first: just the baseline
	if len(a.toasts) != 0 {
		t.Fatal("greeted with no baseline")
	}
	ben := uuid.New()
	a.onVoiceJoin(ben, "ben", true)
	if len(a.toasts) != 1 || !strings.Contains(a.toasts[0].title, "ben") {
		t.Fatalf("toasts %+v", a.toasts)
	}
	a.onVoiceJoin(ben, "ben", true)
	if len(a.toasts) != 1 {
		t.Fatal("greeted twice")
	}
}

func TestChannelBirthdayOnceADay(t *testing.T) {
	a := eggApp(t)
	defer func() { moodClock = time.Now }()
	moodClock = func() time.Time { return time.Date(2027, 3, 4, 12, 0, 0, 0, time.Local) }
	ch := &models.Channel{ID: uuid.New(), Name: "general", CreatedAt: time.Date(2026, 3, 4, 9, 0, 0, 0, time.Local)}
	a.channelBirthday(ch)
	a.channelBirthday(ch)
	if len(a.toasts) != 1 || !strings.Contains(a.toasts[0].title, "1 year old") {
		t.Fatalf("toasts %+v", a.toasts)
	}
}

func TestMoodCommandAdoptsACode(t *testing.T) {
	a := eggApp(t)
	ch := NewCommandHandler(a)
	if _, err := ch.Execute(&Command{Name: "mood", Args: []string{"#k7q2m"}}); err != nil {
		t.Fatal(err)
	}
	if a.uiConfig.Display.MoodLock != "#K7Q2M" {
		t.Fatalf("lock %q", a.uiConfig.Display.MoodLock)
	}
	if _, err := ch.Execute(&Command{Name: "mood", Args: []string{"nope!"}}); err == nil {
		t.Fatal("accepted a bad code")
	}
	if _, err := ch.Execute(&Command{Name: "collection"}); err == nil {
		t.Fatal("posted with no channel open")
	}
}

func TestMainWindowEggsDraw(t *testing.T) {
	a := eggApp(t)
	for _, kind := range []string{"rain", "party", "confetti"} {
		a.view = ViewMain
		a.egg = &eggState{kind: kind, start: time.Now().Add(-time.Second), dur: 3 * time.Second, seed: 9}
		g := parseFrame("", 120, 40)
		a.paintEgg(g, time.Now())
		if strings.TrimSpace(g.String()) == "" {
			t.Fatalf("%s drew nothing", kind)
		}
	}
}

func TestTypingSitsInTheChatBorder(t *testing.T) {
	box := "╭────────────────────╮\n│ hi                 │\n╰────────────────────╯"
	out := embedBorderBottom(box, "amy is typing...", lipglossPlain())
	last := strings.Split(out, "\n")[2]
	if !strings.HasPrefix(last, "╰─ amy is typing...") || !strings.HasSuffix(last, "╯") {
		t.Fatalf("bottom border %q", last)
	}
	if embedBorderBottom(box, "", lipglossPlain()) != box {
		t.Fatal("an empty indicator changed the border")
	}
}

func TestMessageNavigationSkipsSystemLines(t *testing.T) {
	msgs := []*MessageDisplay{{AuthorName: "amy"}, {IsSystem: true}, {AuthorName: "System"}, {AuthorName: "ben"}, {IsSystem: true}}
	if lastSelectable(msgs) != 3 {
		t.Fatalf("lastSelectable = %d", lastSelectable(msgs))
	}
}

func TestMoonPhases(t *testing.T) {
	pal := (&App{theme: eggApp(t).theme}).loadingPalette()
	for _, phase := range []float64{.5, math.Pi / 2, math.Pi, 3 * math.Pi / 2, 2*math.Pi - .5} {
		g := newGrid(20, 10)
		drawMoon(g, 0, 0, phase, pal)
		if strings.TrimSpace(ansi.Strip(g.String())) == "" {
			t.Fatalf("phase %.2f drew nothing", phase)
		}
	}
}

func lipglossPlain() lipgloss.Style { return lipgloss.NewStyle() }

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		have, latest string
		newer        bool
	}{
		{"v0.1.0", "v0.1.1", true}, {"0.1.0", "v0.2.0", true}, {"v0.1.0", "v0.1.0", false},
		{"v0.2.0", "v0.1.9", false}, {"dev", "v0.1.0", false}, {"v1.0.0", "v1.0.1-rc1", true},
	} {
		if got := newerVersion(c.have, c.latest); got != c.newer {
			t.Errorf("%s → %s: %v", c.have, c.latest, got)
		}
	}
}

func TestUpdatesPageAndHiddenCommands(t *testing.T) {
	a := eggApp(t)
	a.view = ViewUpdates
	a.updates = &updateState{}
	a.clientVersion = "0.1.0"
	a.handleUpdateChecked(updateCheckedMsg{latest: "v9.9.9", url: "https://example.com/r"})
	if out := ansi.Strip(a.View()); !strings.Contains(out, "v9.9.9") || !strings.Contains(out, "newer Concord is out") {
		t.Fatalf("updates page:\n%s", out)
	}
	a.handleUpdatesKey(keyOf("esc"))
	if a.view != ViewLogin {
		t.Fatal("Esc didn't go back")
	}
	ch := NewCommandHandler(a)
	if _, err := ch.Execute(&Command{Name: "party"}); err != nil || a.egg == nil || a.coll().Eggs["slash_party"] == "" {
		t.Fatal("/party")
	}
	for _, c := range allSlashCommands() {
		if c.Name == "grape" || c.Name == "disco" || c.Name == "party" {
			t.Fatalf("/%s is listed in /help", c.Name)
		}
	}
}
