package dashboard

import (
	"fmt"
	"strings"
	"testing"
)

// TestUpdateInPlaceDoesNotBlankBeforeRedrawing is a regression test for a
// real, live-reported bug: the hybrid --hybrid dashboard's four stat boxes
// visibly flashed once per second (updateHybridDashboardLoop's tick
// interval). The old implementation wrote the update in several separate
// fmt.Print calls: first a loop clearing every dashboard line ("\033[2K"),
// then a second, later call printing the real content -- letting the
// terminal actually paint the fully-blanked intermediate frame in between.
// UpdateInPlace now returns one string for the caller to print in a single
// call, with no separate blanking pass at all -- confirmed here by
// checking the old clear-every-line code ("\033[2K") never appears, since
// the fix replaced it with a per-line end-of-line clear ("\033[K")
// interleaved with that same line's real content instead.
func TestUpdateInPlaceDoesNotBlankBeforeRedrawing(t *testing.T) {
	h := NewHybridRenderer()
	h.SetDashboardStartLine(3)
	h.SetScrollRegionStartLine(12)

	out := h.UpdateInPlace()

	if strings.Contains(out, "\033[2K") {
		t.Error("expected no whole-line-clear codes (\\033[2K) -- that's the old blank-then-redraw pattern that caused the flash")
	}
	if strings.Contains(out, "\033[B") {
		t.Error("expected no cursor-down-only movement codes (\\033[B) -- another artifact of the old line-by-line blanking loop")
	}
}

// TestUpdateInPlaceIsOneMoveToStartRenderMoveToScrollSequence confirms the
// returned string has the expected shape: position to the dashboard's
// start line, the rendered panel content (matching RenderInitial), then
// position to the scroll region's start line -- everything in one string,
// so the caller's single fmt.Print call is genuinely atomic from the
// terminal's point of view.
func TestUpdateInPlaceIsOneMoveToStartRenderMoveToScrollSequence(t *testing.T) {
	h := NewHybridRenderer()
	h.SetDashboardStartLine(5)
	h.SetScrollRegionStartLine(20)

	out := h.UpdateInPlace()

	wantStart := fmt.Sprintf("\033[%d;1H", 5)
	if !strings.HasPrefix(out, wantStart) {
		t.Errorf("expected output to start with %q, got prefix: %q", wantStart, out[:min(len(out), 20)])
	}

	wantEnd := fmt.Sprintf("\033[%d;1H", 20)
	if !strings.HasSuffix(out, wantEnd) {
		t.Errorf("expected output to end with %q, got suffix: %q", wantEnd, out[max(0, len(out)-20):])
	}
}

// TestUpdateInPlaceContentMatchesRenderInitial confirms the panel content
// itself (stripped of the per-line "\033[K" this adds) is exactly what
// RenderInitial produces -- the fix changes how the update is framed and
// written, not what it actually draws.
func TestUpdateInPlaceContentMatchesRenderInitial(t *testing.T) {
	h := NewHybridRenderer()
	h.SetDashboardStartLine(1)
	h.SetScrollRegionStartLine(10)
	h.UpdateSystemStats(0, 0, 0, 3, 100, 0)

	out := h.UpdateInPlace()
	got := strings.ReplaceAll(out, "\033[K", "")
	got = strings.TrimPrefix(got, "\033[1;1H")
	got = strings.TrimSuffix(got, "\033[10;1H")
	got = strings.ReplaceAll(got, "\r\n", "\n")

	want := h.RenderInitial()
	if got != want {
		t.Errorf("expected UpdateInPlace's content to match RenderInitial once framing is stripped\ngot:  %q\nwant: %q", got, want)
	}
}
