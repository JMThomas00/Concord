package client

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

func newLoadingTestApp(t *testing.T) *App {
	a := newLoginTestApp(t, 100, 30, true)
	a.view = ViewLogin
	a.configMgr = &ConfigManager{configFilePath: filepath.Join(t.TempDir(), "config.json")}
	a.mood = moodFromSeed(5)
	return a
}

// Every style draws a full screen at every stage, without panicking.
func TestLoadingScreensRender(t *testing.T) {
	a := newLoadingTestApp(t)
	kinds := []string{"boot"}
	for _, o := range findLayer(layerLoading).options {
		kinds = append(kinds, o.id)
	}
	for _, kind := range kinds {
		for _, p := range []float64{0, 0.1, 0.3, 0.5, 0.7, 0.9, 1} {
			a.loading = &loadingState{kind: kind, dur: loadingDur, seed: 3, lines: a.loadingLines(kind == "boot")}
			a.loading.start = time.Now().Add(-time.Duration(p * float64(loadingDur)))
			out := a.View()
			lines := strings.Split(out, "\n")
			if len(lines) != 30 {
				t.Fatalf("%s at %.1f: %d lines", kind, p, len(lines))
			}
			for _, l := range lines {
				if w := ansi.StringWidth(l); w != 100 {
					t.Fatalf("%s at %.1f: line %d wide", kind, p, w)
				}
			}
		}
	}
}

func TestFirstLaunchGetsTheWelcome(t *testing.T) {
	a := newLoadingTestApp(t)
	a.uiConfig = &UIConfig{}
	a.launched()
	a.startLoading()
	if a.loading == nil || a.loading.kind != "boot" || a.loading.dur != firstLoadingDur {
		t.Fatalf("first launch: %+v", a.loading)
	}
	a.launched()
	a.startLoading()
	if a.loading.kind == "boot" {
		t.Fatal("second launch got the welcome again")
	}
	a.uiConfig.Display.Surprise = "off"
	a.loading = nil
	a.startLoading()
	if a.loading != nil {
		t.Fatal("loading screen with Surprise Me off")
	}
}

// Any key skips the loading screen and isn't passed on to the page.
func TestAnyKeySkipsLoading(t *testing.T) {
	a := newLoadingTestApp(t)
	a.uiConfig = &UIConfig{}
	a.loading = &loadingState{kind: "calm", start: time.Now(), dur: loadingDur, lines: []string{"x"}}
	a.Update(tea.MouseMsg{Action: tea.MouseActionMotion})
	if a.loading == nil {
		t.Fatal("mouse motion skipped it")
	}
	before := a.loginPassword.Value()
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if a.loading != nil {
		t.Fatal("key didn't skip")
	}
	if a.loginPassword.Value() != before {
		t.Fatal("the skipping key reached the form")
	}
	if a.coll().Counters["skips"] != 1 {
		t.Fatal("skip not counted")
	}
}

func TestLoadingEndsByItself(t *testing.T) {
	a := newLoadingTestApp(t)
	a.loading = &loadingState{kind: "calm", start: time.Now().Add(-loadingDur), dur: loadingDur, lines: []string{"x"}}
	a.fx.gen = 1
	a.handleFxTick(fxTickMsg{gen: 1})
	if a.loading != nil {
		t.Fatal("still loading")
	}
}

func TestConnectingScreen(t *testing.T) {
	a := newLoadingTestApp(t)
	a.view = ViewMain
	a.connecting = &connectingState{
		start:   time.Now().Add(-time.Second),
		servers: []*ClientServerInfo{{Name: "RedOak"}, {Name: "Sequoia"}},
		readyAt: map[uuid.UUID]time.Duration{},
	}
	out := ansi.Strip(a.renderConnecting(time.Now()))
	for _, want := range []string{"C O N N E C T I N G", "RedOak", "Sequoia", "offline"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if a.connectingSettled(500 * time.Millisecond) {
		t.Fatal("settled too soon")
	}
	if !a.connectingSettled(connectingMax) {
		t.Fatal("never settles")
	}
	if !a.skipConnecting(tea.KeyMsg{Type: tea.KeyEnter}) || a.connecting != nil {
		t.Fatal("key didn't skip")
	}
}

func TestLandingEndsOnTheMainWindow(t *testing.T) {
	main := parseFrame("top\nmiddle\nbottom", 10, 3)
	pal := (&App{theme: newLoginTestApp(t, 10, 3, true).theme}).loadingPalette()
	if got := landing(main, 1, pal).String(); got != main.String() {
		t.Fatalf("got %q", got)
	}
	mid := ansi.Strip(landing(main, .3, pal).String())
	if !strings.Contains(mid, "top") || strings.Contains(mid, "bottom") || !strings.Contains(mid, "━") {
		t.Fatalf("mid-sweep: %q", mid)
	}
}
