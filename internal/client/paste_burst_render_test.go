package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestViewReplaysCachedFrameDuringPasteBurst is a regression test for a
// real, live-reported bug (2026-09-13): pasting longer text visibly
// "streamed in" one character at a time. Root cause -- see inPasteBurst's
// doc comment in app.go -- is that bubbletea's event loop runs a full
// Update()+View() cycle for every single machine-replayed keystroke a paste
// produces, and View()'s ViewMain path unconditionally re-ran the expensive
// renderMainView()+zone.Scan() pass every single time even though nothing
// but the input box's text was actually changing.
//
// This proves the fix directly: once a full frame has been cached, flagging
// a burst in progress (without going through the keystroke path at all --
// isolating View()'s own fast-path logic from how inPasteBurst gets set)
// must make View() replay the cached frame unchanged, even though app state
// that would otherwise show up in a fresh render (statusMessage, part of the
// status bar renderStatusBar draws) has since changed. Against the pre-fix
// code -- which always recomputed -- this would have failed by returning a
// view containing "MUTATED-DURING-BURST" instead of the stale cached text.
func TestViewReplaysCachedFrameDuringPasteBurst(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput

	baseline := a.View()
	if a.cachedMainView == "" {
		t.Fatal("test setup bug: View() should have populated cachedMainView on a normal render")
	}
	if baseline != a.cachedMainView {
		t.Fatal("test setup bug: the returned view should equal what got cached")
	}

	a.inPasteBurst = true
	a.statusMessage = "MUTATED-DURING-BURST"

	got := a.View()
	if got != baseline {
		t.Errorf("expected View() to replay the cached frame unchanged during a detected paste burst, but it recomputed (mutated state leaked through)")
	}
	if strings.Contains(got, "MUTATED-DURING-BURST") {
		t.Errorf("expected the mutated statusMessage to NOT appear while inPasteBurst is true, but it did -- View() did not take the cached fast path")
	}
}

// TestViewRefreshesOncePasteBurstSettles is the regression guard alongside
// the test above: once the burst flag clears, the very next View() call
// must produce a fresh, up-to-date frame reflecting whatever changed during
// the burst, not stay stuck on the stale cached one.
func TestViewRefreshesOncePasteBurstSettles(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput

	baseline := a.View()

	a.inPasteBurst = true
	a.statusMessage = "MUTATED-DURING-BURST"
	if got := a.View(); got != baseline {
		t.Fatalf("test setup bug: expected the cached fast path to apply while inPasteBurst is true")
	}

	a.inPasteBurst = false
	got := a.View()
	if got == baseline {
		t.Error("expected View() to recompute a fresh frame once the paste burst settled, but it still matched the stale cached one")
	}
	if !strings.Contains(got, "MUTATED-DURING-BURST") {
		t.Errorf("expected the settled render to reflect the statusMessage change made during the burst, but it didn't")
	}
}

// TestRapidKeystrokesSetInPasteBurstAndSettleClearsIt drives the real
// a.Update() path (not manual field manipulation) to confirm the wiring
// end-to-end: a rapid-fire keystroke (no sleep, matching the same
// machine-replayed-paste timing the Enter/Tab burst fixes rely on -- see
// pasteBurstKeyThreshold) sets inPasteBurst, and delivering the
// pasteBurstSettledMsg that Update() scheduled for it (matching generation)
// clears it again -- mirroring helpResizeSettledMsg's own established
// stale-tick-is-a-safe-no-op test shape.
func TestRapidKeystrokesSetInPasteBurstAndSettleClearsIt(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = newTestMessageInput()
	a.input.Focus()

	for _, ch := range "hi" {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	if !a.inPasteBurst {
		t.Fatal("expected a rapid-fire second keystroke to be flagged as part of a paste burst")
	}
	genAtBurst := a.pasteBurstGen
	if genAtBurst == 0 {
		t.Fatal("expected Update() to have bumped pasteBurstGen when scheduling the settle check")
	}

	// A stale settle tick from an earlier generation must be a no-op.
	a.Update(pasteBurstSettledMsg{gen: genAtBurst - 1})
	if !a.inPasteBurst {
		t.Error("expected a stale (superseded) settle tick to leave inPasteBurst untouched")
	}

	// The matching-generation tick actually clears it.
	a.Update(pasteBurstSettledMsg{gen: genAtBurst})
	if a.inPasteBurst {
		t.Error("expected the matching-generation settle tick to clear inPasteBurst")
	}
}
