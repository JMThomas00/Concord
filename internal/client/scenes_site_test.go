package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Every scene from the website draws at any terminal size and any moment
// without panicking, and every frame is exactly the screen's size.
func TestSiteScenesDrawAtAnySize(t *testing.T) {
	sizes := [][2]int{{12, 5}, {40, 12}, {80, 24}, {150, 42}, {260, 70}}
	times := []float64{0, 0.3, 1.1, 2.4, 4.9, 13, 47, 61, 300}
	check := func(name string, w, h int, frame string) {
		t.Helper()
		lines := strings.Split(frame, "\n")
		if len(lines) != h {
			t.Fatalf("%s at %dx%d: %d rows", name, w, h, len(lines))
		}
		for i, l := range lines {
			if got := runewidth.StringWidth(ansi.Strip(l)); got != w {
				t.Fatalf("%s at %dx%d: row %d is %d wide", name, w, h, i, got)
			}
		}
	}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		a := newLoginTestApp(t, w, h, true)
		a.view = ViewLogin
		a.mood = moodFromSeed(777)
		pal := a.loadingPalette()
		for _, at := range times {
			for _, o := range findLayer(layerAtmosphere).options {
				if g := a.baseAtmosphere(o.id, at, pal); g != nil {
					check("atmosphere "+o.id, w, h, g.String())
				}
			}
			for _, pop := range []float64{-1, 0.5, 5, 14.3, 20} {
				check("lightbulb", w, h, atmosBulb(w, h, at, 9, pal, pop).String())
			}
			for _, kind := range saverKinds {
				start := time.Now().Add(-time.Duration(at * float64(time.Second)))
				a.saver = &saverState{kind: kind, start: start, last: start, seed: 5, x: 3, y: 2, dx: 14, dy: 5}
				check("saver "+kind, w, h, a.renderSaver(time.Now()))
				a.saver = nil
			}
			for _, kind := range []string{"flashbang", "eclipse", "moonrise", "juice"} {
				start := time.Now().Add(-time.Duration(at * float64(time.Second)))
				a.loading = &loadingState{kind: kind, start: start, dur: loadingDur, seed: 3, lines: []string{"Pressing grapes"}}
				check("loading "+kind, w, h, a.renderLoading(time.Now()))
				a.loading = nil
			}
		}
		from, to := newGrid(w, h), newGrid(w, h)
		from.text(h/2, 0, "the old page", "")
		to.text(h/2, 0, "the new page", "")
		for _, kind := range []string{"flashbang", "powercut", "juice", "pullchain"} {
			for p := 0.0; p <= 1; p += 0.05 {
				tr := &transition{kind: kind, seed: 4, w: w, h: h}
				tr.prepare(pal)
				tr.bulbCol, tr.bulbTop, _ = bulbGeom(w, h, 0)
				check("transition "+kind, w, h, tr.render(from, to, p).String())
			}
		}
	}
}

// With the Lightbulb lit, a page change plays the pull chain, which turns
// the bulb off for a while; a second change while it's dark doesn't.
func TestPullChainTurnsTheBulbOff(t *testing.T) {
	a := newLoginTestApp(t, 150, 42, false)
	a.view = ViewLogin
	a.mood = moodFromSeed(777)
	a.mood.picks[layerAtmosphere] = "lightbulb"
	a.mood.picks[layerTransition] = "slide"
	a.syncFx()
	a.fx.last = "a frame"
	a.view = ViewRegister
	a.syncFx()
	if a.fx.trans == nil || a.fx.trans.kind != "pullchain" {
		t.Fatalf("expected the pull chain, got %+v", a.fx.trans)
	}
	if a.sinceBulbPop(time.Now().Add(2*time.Second)) <= 0 {
		t.Fatal("the bulb never goes off")
	}
	a.fx.trans = nil
	a.fx.last = "a frame"
	a.view = ViewLogin
	a.syncFx()
	if a.fx.trans == nil || a.fx.trans.kind != "slide" {
		t.Fatalf("with the bulb already off, expected the mood's transition, got %+v", a.fx.trans)
	}
}
