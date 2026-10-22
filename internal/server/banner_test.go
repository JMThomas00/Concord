package server

import "testing"

// The banner said "v0.1.0" whatever the version; it now shows the build's.
func TestBannerNameShowsTheBuildVersion(t *testing.T) {
	for version, want := range map[string]string{
		"0.1.4":  "Concord Server v0.1.4",
		"v0.1.4": "Concord Server v0.1.4",
		"dev":    "Concord Server (development build)",
		"":       "Concord Server (development build)",
	} {
		if got := BannerName("Concord Server", version); got != want {
			t.Errorf("BannerName(%q) = %q, want %q", version, got, want)
		}
	}
}
