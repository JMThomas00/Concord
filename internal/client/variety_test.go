package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Every option of every layer draws the login screen at the right size,
// and the grapes keep their 35×21 footprint in every style.
func TestEveryMoodOptionRenders(t *testing.T) {
	a := newLoginTestApp(t, 120, 40, true)
	a.view = ViewLogin
	a.uiConfig = &UIConfig{}
	a.mood = moodFromSeed(9)
	a.fx.stageAt = time.Now().Add(-5 * time.Second)
	for _, l := range moodLayers {
		for _, o := range l.options {
			a.mood.picks[l.id] = o.id
			out := a.applyFx(a.view0())
			lines := strings.Split(out, "\n")
			if len(lines) != 40 {
				t.Fatalf("%s/%s: %d lines", l.id, o.id, len(lines))
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w != 120 {
					t.Fatalf("%s/%s: line %d is %d wide", l.id, o.id, i, w)
				}
			}
			if l.id == layerLogo {
				logo := strings.Split(a.renderGrapeLogo(), "\n")
				if len(logo) != 21 {
					t.Fatalf("logo %s: %d rows", o.id, len(logo))
				}
				for _, row := range logo {
					if w := ansi.StringWidth(row); w != 35 {
						t.Fatalf("logo %s: row %d wide", o.id, w)
					}
				}
			}
		}
		a.mood = moodFromSeed(9)
	}
}

// Banner colourings change colours, never the letters.
func TestBannerColouringsKeepTheArt(t *testing.T) {
	a := newLoginTestApp(t, 120, 40, true)
	a.mood = moodFromSeed(9)
	art := trimBannerArt(banners[0].Art)
	for _, o := range findLayer(layerBanner).options {
		a.mood.picks[layerBanner] = o.id
		if out, ok := a.colourBanner(art, time.Now()); ok && ansi.Strip(out) != art {
			t.Fatalf("%s changed the art", o.id)
		}
	}
}

func TestHSV(t *testing.T) {
	for h, want := range map[float64]string{0: "#ff0000", 120: "#00ff00", 240: "#0000ff"} {
		if got := hsvHex(h, 1, 1); got != want {
			t.Errorf("hue %v: %s, want %s", h, got, want)
		}
	}
}
