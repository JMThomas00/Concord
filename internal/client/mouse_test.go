package client

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// zone.Get (and everything in mouse.go built on it) panics if the global
// manager was never initialized. In the shipped binary that's guaranteed by
// cmd/client/main.go running before any Update()/View() call; test code
// constructs *App directly with no main(), so it needs the same call once
// per test binary.
func init() {
	zone.NewGlobal()
}

// TestResolveMessageAtLine covers the click-to-select-message math: a click
// row within the chat viewport's *currently rendered* content, combined
// with the scroll offset, must map back to the message whose recorded
// starting line (a.messageLineOffsets) is the closest one at or before that
// absolute line -- mirroring TestResolveLinkBrowserRowIndex's shape for the
// link browser's own row resolver.
func TestResolveMessageAtLine(t *testing.T) {
	// Three messages starting at content lines 0, 3, and 7 (e.g. the second
	// message wrapped to 4 lines).
	lineOffsets := []int{0, 3, 7}

	cases := []struct {
		name            string
		viewportYOffset int
		clickLine       int
		wantIdx         int
		wantOK          bool
	}{
		{"no scroll, click on first message's first line", 0, 0, 0, true},
		{"no scroll, click on first message's second line", 0, 2, 0, true},
		{"no scroll, click exactly on second message's start", 0, 3, 1, true},
		{"no scroll, click in the middle of the second message", 0, 5, 1, true},
		{"no scroll, click on third message", 0, 7, 2, true},
		{"no scroll, click past the end still resolves to the last message", 0, 50, 2, true},
		{"scrolled down 5, click 2 rows into the viewport lands on the third message's start", 5, 2, 2, true},
		{"scrolled down 5, click at the very top of the viewport is still within the second message", 5, 0, 1, true},
		{"empty offsets fail cleanly", 0, 0, 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offsets := lineOffsets
			if tc.name == "empty offsets fail cleanly" {
				offsets = nil
			}
			idx, ok := resolveMessageAtLine(offsets, tc.viewportYOffset, tc.clickLine)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && idx != tc.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tc.wantIdx)
			}
		})
	}
}

// TestResolveMessageAtLineBeforeFirstMessage confirms a click above the
// first message's recorded line (e.g. a leading blank/date-separator row)
// is correctly rejected rather than silently resolving to message 0 --
// this is the exact failure mode a naive "find nearest" implementation
// (rather than "find nearest at-or-before") could get wrong.
func TestResolveMessageAtLineBeforeFirstMessage(t *testing.T) {
	lineOffsets := []int{2, 5}
	if _, ok := resolveMessageAtLine(lineOffsets, 0, 1); ok {
		t.Fatal("expected a click before the first message's line to fail, not resolve to message 0")
	}
	if idx, ok := resolveMessageAtLine(lineOffsets, 0, 2); !ok || idx != 0 {
		t.Fatalf("expected click exactly at the first message's line to resolve to 0, got idx=%d ok=%v", idx, ok)
	}
}

// newTestLinkBrowserMouseApp mirrors newTestLinkBrowserApp in
// link_browser_test.go but is local to this file to keep the two mouse vs.
// keyboard test files independent.
func newTestLinkBrowserMouseApp(links []string) *App {
	a := &App{}
	a.linkBrowserState = &LinkBrowserState{
		AllLinks: links,
		Links:    links,
	}
	return a
}

// TestHandleLinkBrowserMouseIgnoresNonPressNonLeftEvents confirms the mouse
// handler doesn't react to wheel/release/motion events -- only a genuine
// left-button press should ever open a link, since those other event types
// share the same tea.MouseMsg type and could otherwise misfire.
func TestHandleLinkBrowserMouseIgnoresNonPressNonLeftEvents(t *testing.T) {
	a := newTestLinkBrowserMouseApp([]string{"https://one.example"})

	cmd := a.handleLinkBrowserMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if cmd != nil {
		t.Error("expected a release event to be ignored")
	}
	cmd = a.handleLinkBrowserMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	if cmd != nil {
		t.Error("expected a right-click press to be ignored")
	}
	if a.linkBrowserState == nil {
		t.Fatal("link browser should still be open -- neither event should have closed it")
	}
}

// TestHandleLinkBrowserMouseNoZoneMatchDoesNothing confirms clicking
// somewhere that isn't any known link row (no zones registered in this
// unit-test context, since nothing has gone through a real render/scan
// pass) safely returns nil rather than panicking or guessing a link.
func TestHandleLinkBrowserMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := newTestLinkBrowserMouseApp([]string{"https://one.example", "https://two.example"})

	cmd := a.handleLinkBrowserMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0})
	if cmd != nil {
		t.Error("expected no command when the click doesn't land in any known zone")
	}
	if a.linkBrowserState == nil {
		t.Fatal("link browser should still be open -- an unmatched click must not close it")
	}
}

// TestSetFocusAppliesInputBlurFocusSideEffect confirms setFocus mirrors
// cycleFocus's own blur/focus handling of the textarea exactly, since every
// mouse click-to-focus path in mouse.go routes through this helper instead
// of assigning a.focus directly.
func TestSetFocusAppliesInputBlurFocusSideEffect(t *testing.T) {
	a := &App{}
	a.input = textarea.New()

	a.setFocus(FocusInput)
	if !a.input.Focused() {
		t.Error("expected entering FocusInput to focus the textarea")
	}

	a.setFocus(FocusChat)
	if a.input.Focused() {
		t.Error("expected leaving FocusInput to blur the textarea")
	}
	if a.focus != FocusChat {
		t.Errorf("expected focus to be FocusChat, got %v", a.focus)
	}

	// A no-op call (already-current focus) must not toggle anything.
	a.setFocus(FocusChat)
	if a.focus != FocusChat {
		t.Error("expected re-setting the same focus to be a no-op")
	}
}
