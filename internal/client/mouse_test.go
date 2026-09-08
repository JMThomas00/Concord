package client

import (
	"testing"
	"time"

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

// TestResolveClickedMemberRowNoActiveConnDoesNothing confirms the resolver
// fails safe when there's no active connection (buildFlatMemberList returns
// nil in that case) rather than panicking on a nil member list.
func TestResolveClickedMemberRowNoActiveConnDoesNothing(t *testing.T) {
	a := &App{}
	if idx, ok := a.resolveClickedMemberRow(tea.MouseMsg{}); ok {
		t.Errorf("expected ok=false with no active connection, got idx=%d ok=%v", idx, ok)
	}
}

// newTestMemberContextMenuApp builds a minimal App with an open member
// context menu in action-list mode (not volume-slider mode), for testing
// handleMemberContextMenuMouse's guard clauses in isolation.
func newTestMemberContextMenuApp(actions []MemberAction, animFrame int) *App {
	a := &App{}
	a.memberContextMenu = &MemberContextMenu{
		Actions:       actions,
		SelectedIndex: 0,
		AnimFrame:     animFrame,
	}
	return a
}

// TestHandleMemberContextMenuMouseAnimationGate confirms clicks are ignored
// while the popup's pop-in animation is still running -- this animation
// re-renders at a narrower width each frame (see renderMemberContextMenuOverlay),
// which can corrupt that frame's zone bounds, so clicks must wait for it to
// settle rather than risk resolving against a corrupted frame.
func TestHandleMemberContextMenuMouseAnimationGate(t *testing.T) {
	a := newTestMemberContextMenuApp([]MemberAction{{Label: "Kick", Key: "K"}}, 0)
	cmd := a.handleMemberContextMenuMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd != nil {
		t.Error("expected clicks to be ignored while AnimFrame < contextMenuMaxFrames")
	}
	if a.memberContextMenu.SelectedIndex != 0 {
		t.Error("expected no state mutation during the animation gate")
	}
}

// TestHandleMemberContextMenuMouseIgnoresNonPressNonLeftEvents mirrors the
// equivalent Link Browser test -- only a genuine left-button press should
// ever execute an action.
func TestHandleMemberContextMenuMouseIgnoresNonPressNonLeftEvents(t *testing.T) {
	a := newTestMemberContextMenuApp([]MemberAction{{Label: "Kick", Key: "K"}}, contextMenuMaxFrames)
	cmd := a.handleMemberContextMenuMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if cmd != nil {
		t.Error("expected a release event to be ignored")
	}
	cmd = a.handleMemberContextMenuMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	if cmd != nil {
		t.Error("expected a right-click press to be ignored")
	}
}

// TestHandleMemberContextMenuMouseVolumeSliderModeIgnored confirms clicks
// are a no-op while the menu is in per-user volume-slider mode -- there are
// no discrete rows to click there, and dragging is explicitly out of scope.
func TestHandleMemberContextMenuMouseVolumeSliderModeIgnored(t *testing.T) {
	a := newTestMemberContextMenuApp([]MemberAction{{Label: "Kick", Key: "K"}}, contextMenuMaxFrames)
	a.memberContextMenu.VolumeSlider = &VolumeSliderState{Volume: 1.0}
	cmd := a.handleMemberContextMenuMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd != nil {
		t.Error("expected volume-slider mode clicks to be ignored")
	}
}

// TestHandleMemberContextMenuMouseNoZoneMatchDoesNothing mirrors the
// equivalent Link Browser test -- an unmatched click (no real render/scan
// pass happened in this unit-test context) must not guess an action.
func TestHandleMemberContextMenuMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := newTestMemberContextMenuApp([]MemberAction{{Label: "Kick", Key: "K"}, {Label: "Ban", Key: "B"}}, contextMenuMaxFrames)
	cmd := a.handleMemberContextMenuMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0})
	if cmd != nil {
		t.Error("expected no command when the click doesn't land in any known action row")
	}
	if a.memberContextMenu == nil {
		t.Fatal("context menu should still be open -- an unmatched click must not close it")
	}
	if a.memberContextMenu.SelectedIndex != 0 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestToggleCategoryCollapsedFlipsAndPersists confirms a category click
// toggles its collapsed state (both directions) and rebuilds the flat list
// to reflect it -- the real behavior a category-row click should trigger,
// as opposed to selecting it like an ordinary channel.
func TestToggleCategoryCollapsedFlipsAndPersists(t *testing.T) {
	a := newLayoutTestApp(t, 160, 40)
	catID := a.channelTree.FlatList[0].Channel.ID // the GENERAL category from the fixture
	if !a.channelTree.FlatList[0].IsCategory {
		t.Fatal("test fixture assumption broken: FlatList[0] is not a category")
	}
	initialLen := len(a.channelTree.FlatList)

	a.toggleCategoryCollapsed(catID)
	if !a.collapsedCategories[catID] {
		t.Error("expected the category to be collapsed after the first toggle")
	}
	if len(a.channelTree.FlatList) >= initialLen {
		t.Errorf("expected FlatList to shrink once children are hidden, got %d (was %d)", len(a.channelTree.FlatList), initialLen)
	}

	a.toggleCategoryCollapsed(catID)
	if a.collapsedCategories[catID] {
		t.Error("expected the category to be expanded again after the second toggle")
	}
	if len(a.channelTree.FlatList) != initialLen {
		t.Errorf("expected FlatList to restore to %d entries once re-expanded, got %d", initialLen, len(a.channelTree.FlatList))
	}
}

// TestResolveClickedChannelRowNoZoneMatchDoesNothing confirms the resolver
// (now returning the full *ChannelTreeNode, not just a uuid, so callers can
// branch on IsCategory -- see handleMainViewMouse) fails safe on an
// unmatched click, mirroring the equivalent tests for the other resolvers.
func TestResolveClickedChannelRowNoZoneMatchDoesNothing(t *testing.T) {
	a := newLayoutTestApp(t, 160, 40)
	if node, ok := a.resolveClickedChannelRow(tea.MouseMsg{X: 0, Y: 0}); ok {
		t.Errorf("expected ok=false when no channel-row zone has been scanned, got node=%v", node)
	}
}

// TestResolveClickedServerRowNilConfigMgr confirms the resolver fails safe
// (rather than panicking on a nil a.configMgr) -- a real state during early
// App construction before NewConfigManager has run.
func TestResolveClickedServerRowNilConfigMgr(t *testing.T) {
	a := &App{}
	if idx, ok := a.resolveClickedServerRow(tea.MouseMsg{}); ok {
		t.Errorf("expected ok=false with a nil configMgr, got idx=%d ok=%v", idx, ok)
	}
}

// TestResolveClickedServerRowNoZoneMatchDoesNothing mirrors
// TestHandleLinkBrowserMouseNoZoneMatchDoesNothing: with a real (but
// zone-less, since no render/scan pass has happened) App, a click matches no
// server row and returns ok=false rather than guessing index 0.
func TestResolveClickedServerRowNoZoneMatchDoesNothing(t *testing.T) {
	a := newLayoutTestApp(t, 160, 40)
	if idx, ok := a.resolveClickedServerRow(tea.MouseMsg{X: 0, Y: 0}); ok {
		t.Errorf("expected ok=false when no server-row zone has been scanned, got idx=%d ok=%v", idx, ok)
	}
}

// TestResolveClickedStatusBarHintNoZoneMatchDoesNothing confirms an
// unmatched click returns ok=false rather than guessing at a segment --
// mirrors every other resolver's "no zone match" test in this file.
func TestResolveClickedStatusBarHintNoZoneMatchDoesNothing(t *testing.T) {
	// X/Y chosen far outside anything this file's other status-bar tests
	// scan -- (0, 0) isn't safe here since bubblezone's manager is a
	// process-global singleton that's never reset between tests, and
	// another test in this file legitimately registers a zone starting at
	// (0, 0) for its own isolated single-segment scan (a real
	// TOCTOU-shaped hazard under `go test -count=N`, not merely a style
	// nit -- it silently passes once and fails on every repeat).
	a := &App{}
	if cmd, ok := a.resolveClickedStatusBarHint(tea.MouseMsg{X: 9999, Y: 9999}); ok || cmd != nil {
		t.Errorf("expected ok=false and cmd=nil when no status-bar zone has been scanned, got cmd=%v ok=%v", cmd, ok)
	}
}

// TestResolveClickedStatusBarHintQuit confirms clicking the "Ctrl+Q: Quit"
// segment returns tea.Quit, using a real zone.Mark/Scan/Get round trip
// (rather than a bare zoneInBounds check) since the resolver's return value
// itself -- not just whether it fired -- is what this test cares about.
func TestResolveClickedStatusBarHintQuit(t *testing.T) {
	marked := zone.Mark("statusbar-quit", "Ctrl+Q: Quit")
	zone.Scan(marked)

	var z *zone.ZoneInfo
	for i := 0; i < 100; i++ {
		if z = zone.Get("statusbar-quit"); z != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if z == nil {
		t.Fatal("expected statusbar-quit to be scanned")
	}

	a := &App{}
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: z.StartX, Y: z.StartY}
	cmd, ok := a.resolveClickedStatusBarHint(click)
	if !ok {
		t.Fatal("expected the click to resolve to the quit segment")
	}
	if cmd == nil {
		t.Fatal("expected a non-nil tea.Quit command")
	}
}

// TestResolveClickedStatusBarHintServerSettingsGatedByAdmin confirms a
// click on the "Ctrl+B: Server Settings" segment is a no-op (ok=true,
// cmd=nil) for a non-admin -- defense in depth alongside the fact that
// renderStatusBar only marks this zone at all when the viewer is an admin.
func TestResolveClickedStatusBarHintServerSettingsGatedByAdmin(t *testing.T) {
	marked := zone.Mark("statusbar-server-settings", "Ctrl+B: Server Settings")
	zone.Scan(marked)

	var z *zone.ZoneInfo
	for i := 0; i < 100; i++ {
		if z = zone.Get("statusbar-server-settings"); z != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if z == nil {
		t.Fatal("expected statusbar-server-settings to be scanned")
	}

	a := &App{} // no activeConn -> currentUserRoleLevel() returns roleLevelMember
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: z.StartX, Y: z.StartY}
	cmd, ok := a.resolveClickedStatusBarHint(click)
	if !ok {
		t.Fatal("expected the click to resolve to the server-settings segment")
	}
	if cmd != nil {
		t.Error("expected no command for a non-admin clicking Server Settings")
	}
}
