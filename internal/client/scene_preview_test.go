package client

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestScenePreview writes frames of the login stage's scenes as ANSI text,
// for looking at them outside a terminal (one file per frame). It runs only
// when CONCORD_SCENE_PREVIEW names a folder:
//
//	CONCORD_SCENE_PREVIEW=/tmp/scenes go test ./internal/client -run TestScenePreview
func TestScenePreview(t *testing.T) {
	dir := os.Getenv("CONCORD_SCENE_PREVIEW")
	if dir == "" {
		t.Skip("set CONCORD_SCENE_PREVIEW to a folder to write scene frames")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	os.MkdirAll(dir, 0o755)
	write := func(name, frame string) {
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(frame), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const w, h = 150, 42
	times := []float64{3, 11}

	// Login backgrounds, behind the real login screen.
	for _, o := range findLayer(layerAtmosphere).options {
		for _, at := range times {
			a := newLoginTestApp(t, w, h, true)
			a.view = ViewLogin
			a.mood = moodFromSeed(12345)
			a.mood.picks[layerAtmosphere] = o.id
			a.mood.picks[layerMoment] = "none"
			a.fx.stageAt = time.Now().Add(-time.Duration(at * float64(time.Second)))
			write(fmt.Sprintf("atmos-%s-%02.0f", o.id, at), a.applyFx(a.view0()))
		}
	}

	pal := (&App{}).loadingPalette()

	// Screensavers.
	for _, kind := range saverKinds {
		for _, at := range []float64{2, 6} {
			a := newLoginTestApp(t, w, h, true)
			a.view = ViewLogin
			a.mood = moodFromSeed(12345)
			start := time.Now().Add(-time.Duration(at * float64(time.Second)))
			a.saver = &saverState{kind: kind, start: start, last: start, seed: 99, x: 3, y: 2, dx: 14, dy: 5}
			write(fmt.Sprintf("saver-%s-%02.0f", kind, at), a.renderSaver(time.Now()))
		}
	}

	// Loading screens.
	for _, o := range findLayer(layerLoading).options {
		for _, at := range []float64{0.6, 1.2, 2.5, 4} {
			a := newLoginTestApp(t, w, h, true)
			a.mood = moodFromSeed(12345)
			start := time.Now().Add(-time.Duration(at * float64(time.Second)))
			a.loading = &loadingState{kind: o.id, start: start, dur: loadingDur, seed: 7, lines: a.loadingLines(false)}
			write(fmt.Sprintf("loading-%s-%03.0f", o.id, at*10), a.renderLoading(time.Now()))
		}
	}

	// Page transitions: the login page into the register page.
	from := func() *fxGrid {
		a := newLoginTestApp(t, w, h, false)
		a.view = ViewLogin
		return parseFrame(a.view0(), w, h)
	}()
	to := func() *fxGrid {
		a := newLoginTestApp(t, w, h, false)
		a.view = ViewRegister
		return parseFrame(a.view0(), w, h)
	}()
	for _, o := range append(findLayer(layerTransition).options, moodOption{id: "pullchain"}) {
		for _, p := range []float64{0.15, 0.35, 0.6, 0.85} {
			tr := &transition{kind: o.id, seed: 3, w: w, h: h}
			tr.prepare(pal)
			tr.bulbCol, tr.bulbTop, _ = bulbGeom(w, h, 0)
			write(fmt.Sprintf("trans-%s-%02.0f", o.id, p*100), tr.render(from, to, p).String())
		}
	}
}
