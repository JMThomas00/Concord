package client

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestMoodCodeRoundTrip(t *testing.T) {
	for _, seed := range []uint32{0, 1, 12345, 1<<moodCodeBits - 1} {
		m := moodFromSeed(seed)
		code := m.code()
		if len(code) != 6 || code[0] != '#' {
			t.Fatalf("code %q", code)
		}
		back, ok := parseMoodCode(strings.ToLower(code))
		if !ok || back.seed != seed {
			t.Fatalf("%q read back as %d, %v", code, back.seed, ok)
		}
		for l, id := range m.picks {
			if back.picks[l] != id {
				t.Fatalf("layer %s: %s then %s", l, id, back.picks[l])
			}
		}
	}
	if _, ok := parseMoodCode("#U!!!!"); ok {
		t.Fatal("accepted nonsense")
	}
	// Look-alike letters are forgiven.
	a, _ := parseMoodCode("#0I1L0")
	b, _ := parseMoodCode("#O1I1O")
	if a.seed != b.seed {
		t.Fatal("look-alikes read differently")
	}
}

// Over many seeds every option turns up, and rarer tiers turn up less.
func TestMoodRarity(t *testing.T) {
	const n = 200000
	counts := map[moodLayer]map[string]int{}
	for s := uint32(0); s < n; s++ {
		m := moodFromSeed(s * 7919)
		for l, id := range m.picks {
			if counts[l] == nil {
				counts[l] = map[string]int{}
			}
			counts[l][id]++
		}
	}
	for _, l := range moodLayers {
		for _, o := range l.options {
			c := counts[l.id][o.id]
			if c == 0 {
				t.Fatalf("%s/%s never picked", l.id, o.id)
			}
			if o.rarity == legendary && c > n/legendaryOdds*2 {
				t.Fatalf("legendary %s/%s picked %d times in %d", l.id, o.id, c, n)
			}
		}
	}
}

func TestSurpriseLevels(t *testing.T) {
	a := &App{uiConfig: &UIConfig{}, mood: moodFromSeed(42)}
	a.mood.picks[layerAtmosphere] = "grapes"
	a.uiConfig.Display.Surprise = "calm"
	if a.pick(layerAtmosphere) != "none" || a.pick(layerTransition) != "slide" {
		t.Fatal("calm still busy")
	}
	a.uiConfig.Display.Surprise = "off"
	if a.pick(layerTransition) != "" || a.pick(layerLoading) != "" {
		t.Fatal("off still moving")
	}
	a.uiConfig.Display.Surprise = ""
	a.uiConfig.Display.DisablePanelAnimations = true
	if a.pick(layerTransition) != "" {
		t.Fatal("animations off but transitions on")
	}
	if nextSurprise(nextSurprise(nextSurprise(""))) != "" {
		t.Fatal("surprise doesn't cycle")
	}
}

func TestCollectionAndAchievements(t *testing.T) {
	dir := t.TempDir()
	cm := &ConfigManager{configFilePath: filepath.Join(dir, "config.json")}
	a := &App{configMgr: cm, uiConfig: &UIConfig{}}
	a.launched()
	if len(a.toasts) != 1 || a.toasts[0].title != "First Light" {
		t.Fatalf("toasts %+v", a.toasts)
	}
	a.discover(layerLogo, "golden")
	a.discover(layerLogo, "golden")
	if a.coll().seenCount(layerLogo) != 1 {
		t.Fatal("seen twice")
	}
	if _, ok := a.coll().Achievements["legendary"]; !ok {
		t.Fatal("legendary not unlocked")
	}
	a.discoverBanner(3)
	a.discoverBanner(1)
	a.discoverBanner(3)
	if !a.findEgg("konami") || a.findEgg("konami") {
		t.Fatal("egg found twice")
	}

	// It all survives a restart.
	b := &App{configMgr: cm}
	c := b.coll()
	if c.Launches != 1 || len(c.Banners) != 2 || c.Banners[0] != 1 || c.Eggs["konami"] == "" ||
		c.Achievements["egg_konami"] == "" {
		t.Fatalf("reloaded %+v", c)
	}
}

// Notifications stack, newest at the bottom, as far up as a third of the
// window; older ones wait above and are counted. They stay until dismissed
// (the bottom one with Ctrl+X); a flourish goes by itself.
func TestToastStack(t *testing.T) {
	a := &App{uiConfig: &UIConfig{}, width: 120, height: 40}
	fit := a.toastFit()
	if fit < 3 {
		t.Fatalf("only %d fit in 40 rows", fit)
	}
	for i := 0; i < fit+2; i++ {
		a.toasts = append(a.toasts, &toast{title: fmt.Sprint(i)})
	}
	now := time.Now()
	vis, waiting := a.visibleToasts(now)
	if len(vis) != fit || waiting != 2 || vis[len(vis)-1].title != fmt.Sprint(fit+1) {
		t.Fatalf("visible %d, waiting %d, newest %q", len(vis), waiting, vis[len(vis)-1].title)
	}
	top := int(vis[0].row(now.Add(time.Second)))
	if top < a.height/3 {
		t.Fatalf("the stack reaches row %d, above a third of the window", top)
	}
	if later, _ := a.visibleToasts(now.Add(time.Hour)); len(later) != fit {
		t.Fatal("notifications went away by themselves")
	}
	newest := a.currentToast(now)
	if !a.dismissBottomToast() || a.currentToast(now) == newest || len(a.toasts) != fit+1 {
		t.Fatal("Ctrl+X didn't dismiss the bottom one")
	}
	// The rest drop down: the new bottom one slides to the bottom slot.
	vis, waiting = a.visibleToasts(now.Add(time.Second))
	bottom := vis[len(vis)-1]
	if waiting != 1 || bottom.toRow != float64(a.height-2-a.toastRows()+1) {
		t.Fatalf("waiting %d, bottom heading for row %v", waiting, bottom.toRow)
	}
	a.toasts = []*toast{{brief: true, title: "flourish"}}
	a.visibleToasts(now)
	if left, _ := a.visibleToasts(now.Add(briefToastTime + time.Millisecond)); len(left) != 0 {
		t.Fatal("a flourish stayed")
	}
}

func TestOverlayLinesTouchesOnlyItsRows(t *testing.T) {
	frame := "aaaaaaaaaa\nbbbbbbbbbb\ncccccccccc"
	out := overlayLines(frame, 1, 6, 10, []string{"\x1b[1mXY\x1b[0m"})
	lines := strings.Split(out, "\n")
	if lines[0] != "aaaaaaaaaa" || lines[2] != "cccccccccc" {
		t.Fatalf("other rows changed: %q", out)
	}
	if ansi.Strip(lines[1]) != "bbbbbbXYbb" {
		t.Fatalf("row %q", ansi.Strip(lines[1]))
	}
}

// Every transition ends on the new page and keeps rows the pages share.
func TestTransitionsKeepSharedRows(t *testing.T) {
	from := parseFrame("  GRAPES  \n  old     \n  page    ", 10, 3)
	to := parseFrame("  GRAPES  \n  new one \n          ", 10, 3)
	for _, o := range findLayer(layerTransition).options {
		tr := &transition{kind: o.id, seed: 7}
		for _, p := range []float64{0.1, 0.5, 0.9} {
			g := tr.render(from, to, p)
			if g.w != 10 || g.h != 3 {
				t.Fatalf("%s: size %dx%d", o.id, g.w, g.h)
			}
			// Some redraw the whole screen on purpose (the CRT, dial-up, and
			// the website's flashbang, power cut and juice).
			if !wholeScreen(o.id) && ansi.Strip(g.String())[:10] != "  GRAPES  " {
				t.Fatalf("%s at %.1f moved the shared row: %q", o.id, p, g.String())
			}
		}
		if got := tr.render(from, to, 0.999).String(); o.id != "baud" && o.id != "crt" &&
			ansi.Strip(got) != ansi.Strip(to.String()) {
			t.Fatalf("%s doesn't end on the new page: %q", o.id, ansi.Strip(got))
		}
	}
}

// The shaded grapes are the default: about four launches in five.
func TestShadedGrapesAreTheDefault(t *testing.T) {
	const n = 50000
	shaded := 0
	for s := uint32(0); s < n; s++ {
		if moodFromSeed(s * 7919).picks[layerLogo] == "shaded" {
			shaded++
		}
	}
	if frac := float64(shaded) / n; frac < .75 || frac > .85 {
		t.Fatalf("shaded on %.0f%% of launches, want about 80%%", frac*100)
	}
}
