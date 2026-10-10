package client

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/themes"
)

// TestRenderHelpMarkdownUsesThemeColors is a regression test for the core
// item 3a(a) defect: renderHelpMarkdown used to hardcode
// glamour.WithStylePath("dark") -- one of glamour's own bundled palettes,
// completely independent of the active Concord theme. It must now actually
// use theme.Colors, and switching themes must change the rendered output.
func TestRenderHelpMarkdownUsesThemeColors(t *testing.T) {
	dracula, err := themes.GetTheme("dracula")
	if err != nil {
		t.Fatalf("GetTheme(dracula): %v", err)
	}
	nord, err := themes.GetTheme("nord")
	if err != nil {
		t.Fatalf("GetTheme(nord): %v", err)
	}
	if dracula.Colors.Purple == nord.Colors.Purple {
		t.Skip("dracula and nord happen to share the same heading color in this build; test can't distinguish them")
	}

	draculaOut := strings.Join(renderHelpMarkdown(100, dracula), "\n")
	nordOut := strings.Join(renderHelpMarkdown(100, nord), "\n")

	if draculaOut == nordOut {
		t.Error("expected rendered output to differ between two themes with different heading colors -- rendering doesn't look theme-derived")
	}
}

// sgrSeq matches one ANSI CSI SGR escape sequence, e.g. "\x1b[38;2;1;2;3m" or "\x1b[0m".
var sgrSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// lineEndsMidStyle reports whether a line's last SGR sequence (if any) is
// something other than a reset -- meaning the line's own captured text
// leaves color/style "open" rather than self-terminated. If a caller then
// independently wraps just this one line (as renderHelpContent's per-line
// lipgloss.Width().Render() does) and appends unrelated content right after
// it (the scrollbar glyph), an open style here would visually bleed into
// that unrelated content.
func lineEndsMidStyle(line string) bool {
	matches := sgrSeq.FindAllString(line, -1)
	if len(matches) == 0 {
		return false
	}
	last := matches[len(matches)-1]
	return last != "\x1b[0m" && last != "\x1b[m"
}

// TestHelpMarkdownFencedCodeBlockLinesAreSelfTerminated is the Phase
// 4(c) empirical test: render a fenced code block through the real
// configured glamour renderer, split exactly as renderHelpMarkdown does,
// and check whether any individual line leaves SGR state open at its end
// -- the scenario that would bleed a code block's background/foreground
// into the scrollbar column renderHelpContent appends immediately after
// each line (line.go's `middleBuf.WriteString(line + markedGlyphs[i])`).
// Per the plan: only build a normalizer/reflow fix if this actually finds
// corruption -- it currently does not (glamour resets styling per line
// internally), so no fix beyond this documenting test is needed.
func TestHelpMarkdownFencedCodeBlockLinesAreSelfTerminated(t *testing.T) {
	dracula, err := themes.GetTheme("dracula")
	if err != nil {
		t.Fatalf("GetTheme(dracula): %v", err)
	}

	// helpMarkdown's own "Hosting Your Own Server" section already has a
	// real fenced bash block; render the whole document at a realistic
	// narrow Settings-panel width so wrapping is actually exercised.
	lines := renderHelpMarkdown(70, dracula)
	if len(lines) == 0 {
		t.Fatal("expected renderHelpMarkdown to return non-empty output")
	}

	var openLines []int
	for i, line := range lines {
		if lineEndsMidStyle(line) {
			openLines = append(openLines, i)
		}
	}
	if len(openLines) > 0 {
		t.Errorf("found %d line(s) whose SGR style is left open at line end (not reset): indices %v -- "+
			"independently re-rendering/appending after these lines (as renderHelpContent's per-line "+
			"scrollbar-column logic does) could bleed styling into unrelated content", len(openLines), openLines)
	}
}

// TestHelpContentPerLineRewrapDoesNotAlterANSIBalance simulates
// renderHelpContent's actual per-line handling of a scroll-window slice
// (lipgloss.NewStyle().Width(w).Render(line), matching help_view.go's
// contentLine styling) across a slice that cuts through the middle of a
// fenced code block, and confirms the re-wrap doesn't introduce new
// unterminated styles beyond whatever (if anything) the source line already had.
func TestHelpContentPerLineRewrapDoesNotAlterANSIBalance(t *testing.T) {
	dracula, err := themes.GetTheme("dracula")
	if err != nil {
		t.Fatalf("GetTheme(dracula): %v", err)
	}

	contentWidth := 60
	lines := renderHelpMarkdown(contentWidth+2, dracula)

	// Find a fenced-code-block-ish window: search for a run of lines
	// bracketed by the code block's margin blank lines is fragile, so
	// instead just take a window from partway into the document (past the
	// TOC/intro, into "Hosting Your Own Server") -- this document's own
	// content is stable enough that a fixed-ish mid-document slice reliably
	// lands inside real body content including code fences.
	start := len(lines) / 2
	end := start + 10
	if end > len(lines) {
		end = len(lines)
	}
	window := lines[start:end]

	for i, contentLine := range window {
		rewrapped := lipgloss.NewStyle().Width(contentWidth).Render(contentLine)
		if lineEndsMidStyle(rewrapped) {
			t.Errorf("window line %d: re-wrapped line ends mid-style (open SGR state) -- would bleed into the scrollbar column appended right after it.\nsource: %q\nrewrapped: %q",
				start+i, contentLine, rewrapped)
		}
	}
}

// TestHelpMarkdownNoLineExceedsContentWidth is a regression test for
// item 3a(b): helpMarkdown used to contain GFM pipe tables that glamour
// doesn't reflow to WithWordWrap's configured width, so they'd hard-clip
// at the Settings panel's actual narrow interior width. Tables were
// replaced with definition-list-style bullet lines, which DO wrap
// normally -- this confirms no rendered line (measured by visible width,
// ignoring ANSI escapes) exceeds the requested content width at a narrow
// panel size.
func TestHelpMarkdownNoLineExceedsContentWidth(t *testing.T) {
	dracula, err := themes.GetTheme("dracula")
	if err != nil {
		t.Fatalf("GetTheme(dracula): %v", err)
	}

	const contentWidth = 50
	lines := renderHelpMarkdown(contentWidth, dracula)

	// renderHelpMarkdown itself wraps to width-2 (see its wrapWidth calc);
	// give a small allowance for that plus glamour's own block indentation.
	const allowance = 4
	for i, line := range lines {
		if w := lipgloss.Width(line); w > contentWidth+allowance {
			t.Errorf("line %d exceeds content width: got %d, want <= %d\nline: %q", i, w, contentWidth+allowance, line)
		}
	}
}

// TestBuildThemedGlamourStyleProducesValidRenderer is a smoke test that
// glamour.NewTermRenderer accepts buildThemedGlamourStyle's output for
// every theme Concord ships, not just dracula -- a malformed StyleConfig
// (e.g. a color string glamour/termenv can't parse) would surface here as
// a renderer construction or Render() error.
func TestBuildThemedGlamourStyleProducesValidRenderer(t *testing.T) {
	for _, name := range themes.ListAvailableThemes() {
		theme, err := themes.GetTheme(name)
		if err != nil {
			t.Fatalf("GetTheme(%s): %v", name, err)
		}
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(buildThemedGlamourStyle(theme)),
			glamour.WithWordWrap(60),
		)
		if err != nil {
			t.Fatalf("theme %s: NewTermRenderer error: %v", name, err)
		}
		if _, err := r.Render("# Heading\n\n**bold** _italic_ `code` [link](http://example.com)\n\n```go\nfmt.Println(\"hi\")\n```\n"); err != nil {
			t.Errorf("theme %s: Render error: %v", name, err)
		}
	}
}
