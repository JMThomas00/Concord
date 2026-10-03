package client

import (
	"testing"

	"github.com/concord-chat/concord/internal/grapes"
)

// The installer's grapes (internal/grapes) are a copy of these; they must
// shade the same, light for light.
func TestInstallerGrapesMatchTheClients(t *testing.T) {
	if grapes.Size != grapeLogoSize || grapes.Rows != grapeLogos[grapeLogoSize].rows {
		t.Fatal("the installer's logo is a different size")
	}
	for _, light := range [][3]float64{grapeLight0, grapeDark, norm3([3]float64{.3, -.2, .9}), grapes.Orbit(2)} {
		want := shadeGrapeLogo(grapeLogoSize, light)
		got := grapes.Frame(light)
		for r := range want {
			for c := range want[r] {
				if got[r][c].Ch != want[r][c].ch || got[r][c].Tone != want[r][c].tone {
					t.Fatalf("light %v, row %d col %d: %q/%s, want %q/%s", light, r, c, got[r][c].Ch, got[r][c].Tone, want[r][c].ch, want[r][c].tone)
				}
			}
		}
	}
}
