package themes

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestTerminalDefaultThemeIsRegistered guards against the theme file existing
// on disk but never actually being reachable through the normal theme
// lookup path (as it wasn't, until this test was added: it had no test
// coverage and nothing referenced it by name outside its own TOML file).
func TestTerminalDefaultThemeIsRegistered(t *testing.T) {
	names := ListAvailableThemes()
	found := false
	for _, n := range names {
		if n == "terminal-default" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("terminal-default not present in ListAvailableThemes(): %v", names)
	}

	th, err := GetTheme("terminal-default")
	if err != nil {
		t.Fatalf("GetTheme(%q) failed: %v", "terminal-default", err)
	}
	if th.Meta.Name == "" {
		t.Error("expected terminal-default theme to have a Meta.Name")
	}
}

// TestTerminalDefaultThemeDefersToTerminalColors is the real regression
// test for the "follow the OS/terminal theme live" requirement: it proves
// that terminal-default's empty-string fields (background/foreground) emit
// no color escape codes at all -- letting whatever the terminal is
// currently configured to show through unmodified -- and that its bare
// ANSI-index fields emit the raw 16-color SGR codes (30-37/90-97 foreground,
// 40-47/100-107 background) rather than a fixed, pre-resolved RGB value.
// Those SGR codes are exactly what a terminal emulator re-maps whenever its
// own palette changes (e.g. an Omarchy OS theme switch), so this is what
// makes the theme "live" rather than merely "look plausible at startup."
func TestTerminalDefaultThemeDefersToTerminalColors(t *testing.T) {
	th, err := GetTheme("terminal-default")
	if err != nil {
		t.Fatalf("GetTheme failed: %v", err)
	}

	if th.Colors.Background != "" {
		t.Errorf("expected empty background (inherit terminal default), got %q", th.Colors.Background)
	}
	if th.Colors.Foreground != "" {
		t.Errorf("expected empty foreground (inherit terminal default), got %q", th.Colors.Foreground)
	}

	var buf bytes.Buffer
	r := lipgloss.NewRenderer(&buf)
	r.SetColorProfile(termenv.ANSI)

	// An empty color must not emit any SGR sequence at all.
	out := lipgloss.NewStyle().Renderer(r).
		Background(lipgloss.Color(th.Colors.Background)).
		Foreground(lipgloss.Color(th.Colors.Foreground)).
		Render("x")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("expected no ANSI escape codes for empty-string colors, got %q", out)
	}

	// A bare ANSI index (e.g. "1" for red) must emit the raw 16-color SGR
	// code (31), not a resolved hex/RGB sequence -- that's what lets the
	// terminal's own current palette decide the actual color.
	out = lipgloss.NewStyle().Renderer(r).
		Foreground(lipgloss.Color(th.Colors.Red)).
		Render("x")
	if !strings.Contains(out, "\x1b[31m") {
		t.Errorf("expected raw ANSI SGR code 31 (red) for bare index color %q, got %q", th.Colors.Red, out)
	}
	if strings.Contains(out, "#") {
		t.Errorf("expected no pre-resolved hex color in output, got %q", out)
	}
}
