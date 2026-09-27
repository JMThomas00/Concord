package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

type noopMsg struct{}

func grapeFrameText(frame [][]grapeCell) string {
	var b strings.Builder
	for _, row := range frame {
		for _, c := range row {
			b.WriteByte(c.ch)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// 35 columns must stay no taller than the login logo slot, or the grapes
// could never show beside the banner.
func TestGrapeLogoFitsLoginSlot(t *testing.T) {
	g := grapeLogos[grapeLogoSize]
	if g.cols != grapeLogoSize || len(g.chars) != g.rows || len(g.tones) != g.rows {
		t.Fatalf("inconsistent generated data: cols %d rows %d chars %d tones %d", g.cols, g.rows, len(g.chars), len(g.tones))
	}
	if g.rows > maxBannerHeight {
		t.Errorf("grape logo is %d rows, taller than the %d-row logo slot", g.rows, maxBannerHeight)
	}
}

// Moving the light re-shades the grapes but never the traced leaf and stem.
func TestGrapeShadingFollowsTheLight(t *testing.T) {
	lit := shadeGrapeLogo(grapeLogoSize, grapeLight0)
	dark := shadeGrapeLogo(grapeLogoSize, grapeDark)
	if grapeFrameText(lit) == grapeFrameText(dark) {
		t.Fatal("lighting from the opposite side produced an identical frame")
	}
	cells := grapeCells(grapeLogoSize)
	for r, row := range lit {
		for c := range row {
			if cells[r*grapeLogoSize+c].grape {
				continue
			}
			if lit[r][c] != dark[r][c] {
				t.Fatalf("leaf/stem cell (%d,%d) changed with the light", r, c)
			}
		}
	}
}

func TestGrapeLightStartsOnLoginAndStopsElsewhere(t *testing.T) {
	a := newLoginTestApp(t, 200, 55, true)
	a.view = ViewLogin

	a.Update(noopMsg{})
	gl := a.grapeLight
	if gl == nil {
		t.Fatal("expected the grape light to start on the login screen")
	}
	if gl.cur != grapeDark {
		t.Error("the light should power on from the dark side")
	}

	before := gl.cur
	a.Update(grapeTickMsg{gen: gl.gen - 1})
	if gl.cur != before {
		t.Error("a stale tick moved the light")
	}
	for i := 0; i < 30; i++ {
		a.Update(grapeTickMsg{gen: gl.gen})
	}
	dist := func(p, q [3]float64) float64 {
		return (p[0]-q[0])*(p[0]-q[0]) + (p[1]-q[1])*(p[1]-q[1]) + (p[2]-q[2])*(p[2]-q[2])
	}
	if dist(gl.cur, grapeLight0) >= dist(grapeDark, grapeLight0) {
		t.Error("ticks should carry the light toward its resting position")
	}

	a.view = ViewMain
	a.Update(noopMsg{})
	if a.grapeLight != nil {
		t.Error("the light should stop once the logo is off screen")
	}
}

func TestGrapeLightStaysOffWithAnimationsDisabled(t *testing.T) {
	a := newLoginTestApp(t, 200, 55, true)
	a.view = ViewLogin
	a.uiConfig = &UIConfig{}
	a.uiConfig.Display.DisablePanelAnimations = true
	if _, cmd := a.Update(noopMsg{}); a.grapeLight != nil || cmd != nil {
		t.Error("no animation should run with animations disabled")
	}
	if !strings.Contains(ansi.Strip(a.renderGrapeLogo()), ";##:") {
		t.Error("the logo should still render, statically lit")
	}
}

// The grapes sit beside the banner when there's room, and drop out (rather
// than clip) on a screen too narrow for both.
func TestLoginShowsGrapesBesideBannerWhenTheyFit(t *testing.T) {
	narrowest := banners[0]
	for _, b := range banners {
		if strings.TrimSpace(b.Art) != "" && ansi.StringWidth(trimBannerArt(b.Art)) < ansi.StringWidth(trimBannerArt(narrowest.Art)) {
			narrowest = b
		}
	}

	wide := newLoginTestApp(t, 200, 55, true)
	wide.banner = narrowest
	if !strings.Contains(ansi.Strip(wide.renderLoginView()), ";##:") {
		t.Error("expected the grape logo beside the banner on a wide screen")
	}

	// Too narrow for even the smallest banner plus the 35-column grapes.
	narrow := newLoginTestApp(t, 45, 55, true)
	narrow.banner = narrowest
	if strings.Contains(ansi.Strip(narrow.renderLoginView()), ";##:") {
		t.Error("the grape logo should be left out when it can't fit")
	}

	// Too short for the full 21-row slot: left out rather than clipped.
	short := newLoginTestApp(t, 200, 30, true)
	short.banner = narrowest
	if strings.Contains(ansi.Strip(short.renderLoginView()), ";##:") {
		t.Error("the grape logo should be left out when the logo slot is shorter than it")
	}
}

func TestAboutPageShowsGrapes(t *testing.T) {
	a := newLayoutTestApp(t, 200, 55)
	if !strings.Contains(ansi.Strip(a.renderAboutContent(140, 50)), ";##:") {
		t.Error("expected the grape logo on a roomy About page")
	}
	if strings.Contains(ansi.Strip(a.renderAboutContent(60, 50)), ";##:") {
		t.Error("the grape logo should be left out of a narrow About page")
	}
}

func TestMixHex(t *testing.T) {
	if got, ok := mixHex("#ff0000", "#0000ff", .25); !ok || got != "#4000bf" {
		t.Errorf("mixHex = %q %v, want #4000bf", got, ok)
	}
	if _, ok := mixHex("5", "#000000", .5); ok {
		t.Error("ANSI palette indices can't be mixed and should report !ok")
	}
}
