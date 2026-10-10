package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TestHelpResizeDebounceSkipsRecomputeMidResize is a regression test for a
// real, live-reported bug (2026-09-11): renderHelpContent recomputed the
// full Help & Guide markdown document (a ~40ms goldmark+chroma pass) on
// every single width mismatch, and a live window-drag resize fires many
// tea.WindowSizeMsg ticks in quick succession -- reported as both very slow
// redraws while widening and garbled/torn frames while narrowing. While
// a.helpResizing is true (mid-drag, before the debounce settle timer
// fires), renderHelpContent must keep showing its last-computed cache
// rather than recomputing on every width change.
func TestHelpResizeDebounceSkipsRecomputeMidResize(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.settingsState = &SettingsState{}

	a.width = 120
	a.renderHelpContent(100, 40)
	firstWidth := a.settingsState.HelpRenderWidth
	if firstWidth == 0 {
		t.Fatal("test setup bug: expected an initial cached render width")
	}

	// Simulate being mid-resize: a real WindowSizeMsg would set this, but
	// drive the flag directly here to isolate renderHelpContent's own gating
	// logic from Update()'s resize-tick handling (covered separately below).
	a.helpResizing = true
	a.renderHelpContent(60, 40) // a much narrower width, as if actively dragging narrower

	if got := a.settingsState.HelpRenderWidth; got != firstWidth {
		t.Errorf("expected cache to stay at the pre-resize width (%d) while mid-resize, got %d -- recompute wasn't skipped", firstWidth, got)
	}

	// Once the debounce settles, the next render must catch up to the
	// current (narrower) width.
	a.helpResizing = false
	a.renderHelpContent(60, 40)
	if got := a.settingsState.HelpRenderWidth; got == firstWidth {
		t.Error("expected the cache to recompute to the new width once helpResizing cleared, but it didn't")
	}
}

// TestHelpResizeContentNeverOverflowsWidthDuringDebounce confirms the
// belt-and-suspenders fix alongside the debounce: even while showing a
// stale, wider-than-current cache (the deliberate tradeoff during the
// debounce window), no rendered row exceeds the current, narrower
// contentWidth -- MaxWidth truncates it rather than letting it spill past
// the panel border into the terminal's own native line-wrapping (the
// mechanism behind the reported "hard clipping" visual garbling).
func TestHelpResizeContentNeverOverflowsWidthDuringDebounce(t *testing.T) {
	const width, height = 50, 40

	// Baseline: a fresh app rendering directly at the target width, with no
	// stale wider cache involved -- establishes what a correct row count
	// looks like for this width/height, however renderHelpContent's own
	// layout math actually works out (not assumed to be exactly `height`).
	baseline := newLayoutTestApp(t, 120, height)
	baseline.settingsState = &SettingsState{}
	baselineOut := baseline.renderHelpContent(width, height)
	baselineLineCount := len(strings.Split(baselineOut, "\n"))

	// Now the real scenario: render wide first so the cache holds long
	// lines, then simulate mid-resize to a much narrower width -- the cache
	// is intentionally NOT recomputed (helpResizing gate), so
	// renderHelpContent has to render the wide, stale lines into the same
	// narrow box the baseline above used.
	a := newLayoutTestApp(t, 120, height)
	a.settingsState = &SettingsState{}
	a.renderHelpContent(150, height)
	if len(a.settingsState.HelpRenderedLines) == 0 {
		t.Fatal("test setup bug: expected non-empty rendered lines")
	}
	a.helpResizing = true
	out := a.renderHelpContent(width, height)

	// renderHelpContent always wraps its return value in its own
	// Width(width).Border(RoundedBorder()) box, which legitimately adds 2
	// columns (1 per side) on top of the requested width -- that's not the
	// overflow this test is guarding against, so the real per-row budget is
	// width+2.
	const wantMaxWidth = width + 2
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if w := lipgloss.Width(line); w > wantMaxWidth {
			t.Errorf("line %d is %d visible columns wide, wider than the panel's own box width (%d = requested width %d + 2 for its border)", i, w, wantMaxWidth, width)
		}
	}

	// The real failure mode this guards against isn't a too-WIDE row (a
	// naive lipgloss.Style.Width(N) actually word-WRAPS content wider than N
	// into several rows, each individually within budget -- see
	// ansitruncate's doc comment in renderHelpContent) -- it's one logical
	// row exploding into several physical ones, breaking the fixed-height
	// contract every other Settings/Server Management page relies on (see
	// TestRenderMainViewMatchesReportedHeight's own history of this exact
	// bug class). A stale, too-wide cached line must still collapse back to
	// exactly one physical row, so the total line count must match the
	// clean baseline exactly.
	if got := len(lines); got != baselineLineCount {
		t.Errorf("renderHelpContent(width=%d, height=%d) with a stale wide cache produced %d lines, want %d (the clean-render baseline) -- a stale wide cached line likely wrapped into multiple physical rows instead of being truncated to one", width, height, got, baselineLineCount)
	}
}

// TestWindowResizeDebounceIgnoresStaleSettleTick drives the real Update()
// path: two resizes in quick succession must leave the FIRST settle tick's
// message stale (its gen no longer matches a.helpResizeGen) so it's a
// no-op, while the SECOND (latest) tick actually clears a.helpResizing --
// exactly the behavior a live drag-resize needs, where only the final size
// should ever trigger the real recompute.
func TestWindowResizeDebounceIgnoresStaleSettleTick(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain

	_, cmd1 := a.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if !a.helpResizing {
		t.Fatal("expected helpResizing to be true immediately after a resize")
	}
	gen1 := a.helpResizeGen

	_, cmd2 := a.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	gen2 := a.helpResizeGen
	if gen2 == gen1 {
		t.Fatal("expected a second resize to bump helpResizeGen again")
	}

	msg1 := cmd1()
	a.Update(msg1)
	if !a.helpResizing {
		t.Error("expected the stale first settle tick to be a no-op, but helpResizing was cleared early")
	}

	msg2 := cmd2()
	a.Update(msg2)
	if a.helpResizing {
		t.Error("expected the latest settle tick to clear helpResizing")
	}
}
