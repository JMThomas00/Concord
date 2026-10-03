package client

import (
	"math"
	"time"
)

// Atmospheres are the login stage's backgrounds, drawn behind the page and
// kept clear of its text (fxGrid.underlay). Each is a pure function of the
// time since the stage appeared, so it costs nothing between frames.

// atmosphereMoving reports whether this launch's background animates.
func (a *App) atmosphereMoving() bool {
	switch a.pick(layerAtmosphere) {
	case "", "none", "dots":
		return false
	}
	return true
}

// renderAtmosphere draws the background for now, or nil for none.
func (a *App) renderAtmosphere(now time.Time) *fxGrid {
	kind := a.pick(layerAtmosphere)
	t := now.Sub(a.fx.stageAt).Seconds()
	switch kind {
	case "grapes":
		return atmosGrapes(a.width, a.height, t, a.mood.seed)
	}
	return nil
}

// atmosGrapes: a few grapes drifting slowly up the screen, swaying, like
// the concord-site hero. Emoji can't be dimmed, so there are few of them.
func atmosGrapes(w, h int, t float64, seed uint32) *fxGrid {
	g := newGrid(w, h)
	n := max(3, w*h/450)
	for i := 0; i < n; i++ {
		x0 := cellHash(uint64(seed), i, 0, 1) * float64(w-4)
		speed := 0.6 + cellHash(uint64(seed), i, 0, 2)*0.9 // rows per second
		phase := cellHash(uint64(seed), i, 0, 3) * float64(h+4)
		sway := cellHash(uint64(seed), i, 0, 4) * 2 * math.Pi
		y := float64(h+2) - math.Mod(t*speed+phase, float64(h+4))
		x := x0 + 2*math.Sin(t*0.5+sway)
		g.set(int(y), int(x)+1, "🍇", "", 2)
	}
	return g
}
