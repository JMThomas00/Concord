package client

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/concord-chat/concord/internal/themes"
)

// TestChromaHexRejectsBareAnsiIndices is a regression test for the root
// cause of a real, live crash (2026-09-11): the terminal-default theme
// deliberately uses bare ANSI palette indices ("0"-"15", e.g. green="2")
// for its color fields -- valid for lipgloss/termenv everywhere else in
// Concord's UI, but chroma's own style-entry parser only understands
// "#rrggbb"/"bg:#rrggbb"/bold/italic/underline/noinherit. chromaHex must
// filter these out (returning "") rather than passing them through, or
// they reach chroma.MustNewStyle and panic.
func TestChromaHexRejectsBareAnsiIndices(t *testing.T) {
	cases := map[string]string{
		"2":       "",
		"15":      "",
		"":        "",
		"#a3be8c": "#a3be8c",
	}
	for in, want := range cases {
		if got := chromaHex(in); got != want {
			t.Errorf("chromaHex(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRegisterChromaStyleForThemeNeverPanicsOnTerminalDefault reproduces
// the exact live crash: switching to the terminal-default theme (green="2")
// and opening Settings > Help & Guide previously panicked inside
// chroma.MustNewStyle with "invalid entry for NameFunction: unknown style
// element \"2\"" -- an unrecovered panic, not a returned error, so it took
// the whole TUI down. registerChromaStyleForTheme must never panic for any
// shipped theme, terminal-default included.
func TestRegisterChromaStyleForThemeNeverPanicsOnTerminalDefault(t *testing.T) {
	theme, err := themes.GetTheme("terminal-default")
	if err != nil {
		t.Fatalf("GetTheme(terminal-default): %v", err)
	}

	name := registerChromaStyleForTheme(theme)
	if name == "" {
		t.Error("expected a non-empty registered style name for terminal-default")
	}
}

// TestRenderHelpMarkdownDoesNotPanicWithTerminalDefaultTheme is the
// end-to-end version of the crash above: render the real Help & Guide
// document (which contains several ```bash/```toml fenced code blocks,
// see help_view.go) through the actual renderHelpMarkdown entry point
// under the terminal-default theme. An unrecovered panic here fails the
// whole test binary, exactly mirroring how the real bug crashed the shipped
// TUI (bubbletea's own panic recovery only prints a stack trace and exits,
// it doesn't keep the program running).
func TestRenderHelpMarkdownDoesNotPanicWithTerminalDefaultTheme(t *testing.T) {
	theme, err := themes.GetTheme("terminal-default")
	if err != nil {
		t.Fatalf("GetTheme(terminal-default): %v", err)
	}

	lines := renderHelpMarkdown(100, theme)
	if len(lines) == 0 {
		t.Error("expected renderHelpMarkdown to return rendered content, got nothing")
	}
}

// TestRegisterChromaStyleForThemeFollowsLaterThemeSwitches is a regression
// test for the second real bug found while diagnosing the crash: glamour's
// own chroma auto-registration path registers under one hardcoded name
// ("charm") and only the FIRST time ever in the process -- every later
// Concord theme switch's code-block colors would silently keep showing
// whichever theme rendered a code block first, never the new one.
// registerChromaStyleForTheme must give each theme its own distinct,
// always-current registered style.
func TestRegisterChromaStyleForThemeFollowsLaterThemeSwitches(t *testing.T) {
	dracula, err := themes.GetTheme("dracula")
	if err != nil {
		t.Fatalf("GetTheme(dracula): %v", err)
	}
	nord, err := themes.GetTheme("nord")
	if err != nil {
		t.Fatalf("GetTheme(nord): %v", err)
	}
	if dracula.Colors.Green == nord.Colors.Green {
		t.Skip("dracula and nord happen to share the same code color in this build; test can't distinguish them")
	}

	// Render dracula's code-block style first (simulating it being the
	// first theme ever to hit this code path in the process), then nord's.
	draculaName := registerChromaStyleForTheme(dracula)
	nordName := registerChromaStyleForTheme(nord)

	if draculaName == nordName {
		t.Fatalf("expected distinct registered style names per theme, both got %q", draculaName)
	}

	draculaStyle := chromaStyleEntries(dracula)[chroma.NameFunction]
	nordStyle := chromaStyleEntries(nord)[chroma.NameFunction]
	if draculaStyle == nordStyle {
		t.Error("expected NameFunction's chroma style entry to differ between dracula and nord")
	}
	if !strings.Contains(draculaStyle, dracula.Colors.Green) {
		t.Errorf("expected dracula's NameFunction entry to contain its own green (%q), got %q", dracula.Colors.Green, draculaStyle)
	}
	if !strings.Contains(nordStyle, nord.Colors.Green) {
		t.Errorf("expected nord's NameFunction entry to contain its own green (%q), got %q", nord.Colors.Green, nordStyle)
	}
}
