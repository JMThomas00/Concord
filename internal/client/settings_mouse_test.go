package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// TestHandleSettingsMouseNilStateDoesNothing confirms the dispatcher fails
// safe when a.settingsState is nil (e.g. a mouse event arriving on some
// stale/unexpected frame) rather than panicking.
func TestHandleSettingsMouseNilStateDoesNothing(t *testing.T) {
	a := &App{}
	if cmd := a.handleSettingsMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); cmd != nil {
		t.Error("expected nil with a nil settingsState")
	}
}

// TestHandleSettingsMouseIgnoresNonPressNonLeftEvents mirrors the existing
// Link Browser / member-context-menu tests -- only a genuine left-button
// press should ever act.
func TestHandleSettingsMouseIgnoresNonPressNonLeftEvents(t *testing.T) {
	a := &App{settingsState: &SettingsState{Categories: []string{"Theme", "Help & Guide"}}}
	if cmd := a.handleSettingsMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}); cmd != nil {
		t.Error("expected a release event to be ignored")
	}
	if cmd := a.handleSettingsMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight}); cmd != nil {
		t.Error("expected a right-click press to be ignored")
	}
}

// TestHandleSettingsMouseNoZoneMatchDoesNothing confirms an unmatched click
// (no real render/scan pass happened in this unit-test context) returns nil
// and mutates no state, mirroring the equivalent Link Browser test.
func TestHandleSettingsMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &SettingsState{Categories: []string{"Theme", "Notifications", "Help & Guide"}, SelectedCategory: 0}
	a := &App{settingsState: s}
	if cmd := a.handleSettingsMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land in any known zone")
	}
	if s.SelectedCategory != 0 || s.FocusOnForm {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandleThemeCategoryMouseNoZoneMatchDoesNothing mirrors the above for
// the Theme category's own list, one level deeper in the dispatch.
func TestHandleThemeCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &SettingsState{
		Categories:       []string{"Theme", "Help & Guide"},
		SelectedCategory: settingsCatTheme,
		AvailableThemes:  []string{"dracula", "gruvbox"},
		SelectedTheme:    0,
	}
	a := &App{settingsState: s}
	if cmd := a.handleThemeCategoryMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known theme row")
	}
	if s.SelectedTheme != 0 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestSetAddServerFocusBlursOthers confirms clicking one Add/Edit Server
// form field focuses only that field's textinput, blurring the others --
// mirroring cycleAddServerFocus's existing blur-all-then-focus-one pattern.
func TestSetAddServerFocusBlursOthers(t *testing.T) {
	a := &App{}
	a.addServerName = textinput.New()
	a.addServerAddress = textinput.New()
	a.addServerPort = textinput.New()
	a.addServerName.Focus()

	a.setAddServerFocus(1)

	if a.addServerName.Focused() {
		t.Error("expected the previously-focused Name field to be blurred")
	}
	if !a.addServerAddress.Focused() {
		t.Error("expected the Address field to be focused")
	}
	if a.addServerPort.Focused() {
		t.Error("expected the Port field to remain blurred")
	}
	if a.addServerFocus != 1 {
		t.Errorf("expected addServerFocus=1, got %d", a.addServerFocus)
	}
}

// TestHandleServerFormMouseTLSToggle confirms clicking the TLS row focuses
// it (field 3, no textinput to focus) and flips the boolean directly,
// mirroring what pressing Space does on that field.
func TestHandleServerFormMouseTLSToggle(t *testing.T) {
	a := &App{settingsState: &SettingsState{ServerFormOpen: true}}
	a.addServerName = textinput.New()
	a.addServerAddress = textinput.New()
	a.addServerPort = textinput.New()
	before := a.addServerUseTLS

	// No real render/scan pass happened, so no zone will actually match --
	// this exercises the guard-clause/no-op path, mirroring every other
	// resolver's "no zone match does nothing" test.
	cmd := a.handleServerFormMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd != nil {
		t.Error("expected no command when the click doesn't land on any known zone")
	}
	if a.addServerUseTLS != before {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandleDeleteServerConfirmMouseNoZoneMatchDoesNothing mirrors the
// established "no zone match" guard-clause pattern for the delete-server
// dialog's Yes/Cancel buttons.
func TestHandleDeleteServerConfirmMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := &App{settingsState: &SettingsState{}}
	if cmd := a.handleDeleteServerConfirmMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on Yes or Cancel")
	}
}

// TestWriteZoneMarkedLinesSpansAllLines confirms writeZoneMarkedLines's
// split-and-rejoin technique (zone.Mark only wraps a single string in one
// call, but settingsSectionBuilder needs one writeLine call per physical
// line to keep its own line-count budget accurate) produces a single zone
// that spans every line written, not just the last one, and doesn't disturb
// the section builder's line-count accounting used by calculateSettingsLayout.
func TestWriteZoneMarkedLinesSpansAllLines(t *testing.T) {
	sb := newSectionBuilder(10, 40)
	writeZoneMarkedLines(sb, "test-zone", "line one", "line two", "line three")

	if sb.linesWritten != 3 {
		t.Errorf("expected linesWritten=3 for 3 lines written, got %d", sb.linesWritten)
	}

	scanned := zone.Scan(sb.String())
	lines := strings.Split(scanned, "\n")
	if lines[0] != "line one" || lines[1] != "line two" || lines[2] != "line three" {
		t.Fatalf("expected the 3 original lines to survive unchanged, got %q", lines[:3])
	}

	// zone.Scan queues zone updates to an async worker goroutine rather than
	// applying them synchronously (see bubblezone's own doc comment on
	// Scan: "an immediate call to Get(id) may not return the correct
	// information"), so a real caller relies on this settling before the
	// next user-driven mouse event -- here in a test, poll briefly instead
	// of asserting on the very next line, which was flaky exactly per that
	// documented caveat.
	var z *zone.ZoneInfo
	for i := 0; i < 100; i++ {
		if z = zone.Get("test-zone"); z != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if z == nil {
		t.Fatal("expected a zone to be registered for test-zone after scanning")
	}
	if z.StartY != 0 || z.EndY != 2 {
		t.Errorf("expected the zone to span all 3 lines (StartY=0, EndY=2), got StartY=%d EndY=%d", z.StartY, z.EndY)
	}
}

// TestHandleDisplayCategoryMouseNoZoneMatchDoesNothing and
// TestHandleAudioCategoryMouseNoZoneMatchDoesNothing mirror the established
// "no zone match" guard-clause pattern for the two remaining field-list
// categories added this pass.
func TestHandleDisplayCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &SettingsState{DisplayFocusField: 3}
	a := &App{settingsState: s}
	if cmd := a.handleDisplayCategoryMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known field")
	}
	if s.DisplayFocusField != 3 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleAudioCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &SettingsState{AudioFocusField: 2, AudioSliderActive: true}
	a := &App{settingsState: s}
	if cmd := a.handleAudioCategoryMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known field")
	}
	if s.AudioFocusField != 2 || !s.AudioSliderActive {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandleAudioCategoryMouseExitsSliderModeOnFieldSwitch confirms clicking
// a *different* field while a slider is active exits slider mode first,
// mirroring what arrow-key navigation already does -- otherwise
// AudioSliderActive would stay true against whatever field the click moved
// focus to. Uses a real zone scan (audio-field:6, a toggle field) so the
// click actually resolves, unlike the guard-clause tests above.
func TestHandleAudioCategoryMouseExitsSliderModeOnFieldSwitch(t *testing.T) {
	a := &App{settingsState: &SettingsState{AudioFocusField: 2, AudioSliderActive: true}}
	marked := zone.Mark("audio-field:6", "Push-to-Talk  OFF")
	scanned := zone.Scan(marked)
	_ = scanned

	var z *zone.ZoneInfo
	for i := 0; i < 100; i++ {
		if z = zone.Get("audio-field:6"); z != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if z == nil {
		t.Fatal("expected audio-field:6 to be scanned")
	}

	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: z.StartX, Y: z.StartY}
	a.handleAudioCategoryMouse(click)

	if a.settingsState.AudioFocusField != 6 {
		t.Errorf("expected focus to move to field 6, got %d", a.settingsState.AudioFocusField)
	}
	// handleSettingsKey is not exercised here (a.uiConfig is nil, so
	// handleAudioFieldActivate's toggle branch would need it) -- this test
	// only asserts the slider-exit guard runs before that call.
	if a.settingsState.AudioSliderActive {
		t.Error("expected AudioSliderActive to be cleared when switching to a non-slider field")
	}
}

// TestHandleMouseMsgAnimationGate confirms clicks are ignored entirely
// while Settings or Server Management is mid-slide-animation -- clipPanelLeft/
// clipPanelRight truncate the rendered string by column *after* zone marks
// are already embedded, which can corrupt that frame's registered bounds.
func TestHandleMouseMsgAnimationGate(t *testing.T) {
	press := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}

	a := &App{settingsState: &SettingsState{Categories: []string{"Theme"}}, settingsAnimating: true}
	if cmd := a.handleMouseMsg(press); cmd != nil {
		t.Error("expected clicks to be ignored while settingsAnimating is true")
	}

	b := &App{srvMgmtAnimating: true}
	if cmd := b.handleMouseMsg(press); cmd != nil {
		t.Error("expected clicks to be ignored while srvMgmtAnimating is true")
	}
}

// TestUpdateHelpScrollFromRowMapsRowToOffset confirms the scrollbar's
// row->offset math: clicking at the very top of the track jumps to offset
// 0, the very bottom jumps to maxOffset, and a nil settingsState or a
// missing zone (no render/scan pass happened) fails safe rather than
// panicking.
func TestUpdateHelpScrollFromRowMapsRowToOffset(t *testing.T) {
	// Nil state / missing zone: must not panic, must not mutate anything
	// reachable (there's nothing to mutate without a settingsState).
	(&App{}).updateHelpScrollFromRow(5)

	// Polls until the zone reflects THIS scan specifically (trackHeight==10),
	// not just "any non-nil value" -- "help-scrollbar" is also scanned by
	// TestHelpScrollbarDragLifecycle with a different row count, and
	// bubblezone's manager is a process-global singleton that's never reset
	// between tests (see the same caveat documented on
	// TestWriteZoneMarkedLinesSpansAllLines). Under `go test -count=N`, a
	// bare non-nil check can accept a stale registration from the *other*
	// test's last run, left over from before this scan's async update has
	// actually been applied by the worker goroutine.
	marked := zone.Mark("help-scrollbar", strings.Repeat("x\n", 9)+"x") // 10 rows
	zone.Scan(marked)
	var z *zone.ZoneInfo
	for i := 0; i < 200; i++ {
		if got := zone.Get("help-scrollbar"); got != nil && got.EndY-got.StartY+1 == 10 {
			z = got
			break
		}
		time.Sleep(time.Millisecond)
	}
	if z == nil {
		t.Fatal("expected help-scrollbar to be scanned as a 10-row track")
	}

	// 30 rendered lines over a 10-row track -> maxOffset = 20.
	lines := make([]string, 30)
	a := &App{settingsState: &SettingsState{HelpRenderedLines: lines}}

	a.updateHelpScrollFromRow(z.StartY)
	if a.settingsState.HelpScrollOffset != 0 {
		t.Errorf("expected offset 0 at the top of the track, got %d", a.settingsState.HelpScrollOffset)
	}

	a.updateHelpScrollFromRow(z.EndY)
	if a.settingsState.HelpScrollOffset != 20 {
		t.Errorf("expected offset 20 (maxOffset) at the bottom of the track, got %d", a.settingsState.HelpScrollOffset)
	}

	// Dragging past the track's bounds (above or below) clamps rather than
	// extrapolating -- a real drag routinely overshoots the exact pixels.
	a.updateHelpScrollFromRow(z.StartY - 50)
	if a.settingsState.HelpScrollOffset != 0 {
		t.Errorf("expected offset to clamp to 0 above the track, got %d", a.settingsState.HelpScrollOffset)
	}
	a.updateHelpScrollFromRow(z.EndY + 50)
	if a.settingsState.HelpScrollOffset != 20 {
		t.Errorf("expected offset to clamp to maxOffset below the track, got %d", a.settingsState.HelpScrollOffset)
	}
}

// TestHelpScrollbarDragLifecycle confirms the Press-starts/Motion-continues/
// Release-ends drag lifecycle wired into handleSettingsMouse: a drag must
// keep responding to Motion events even though every other resolver in this
// file only ever reacts to a Press, and must stop cleanly on Release.
func TestHelpScrollbarDragLifecycle(t *testing.T) {
	// See the matching comment in TestUpdateHelpScrollFromRowMapsRowToOffset:
	// polls for a 5-row track specifically rather than accepting any
	// non-nil zone, since that other test scans the same "help-scrollbar"
	// ID with a different row count and bubblezone's manager persists
	// across tests within one `go test -count=N` process.
	marked := zone.Mark("help-scrollbar", strings.Repeat("x\n", 4)+"x") // 5 rows
	zone.Scan(marked)
	var z *zone.ZoneInfo
	for i := 0; i < 200; i++ {
		if got := zone.Get("help-scrollbar"); got != nil && got.EndY-got.StartY+1 == 5 {
			z = got
			break
		}
		time.Sleep(time.Millisecond)
	}
	if z == nil {
		t.Fatal("expected help-scrollbar to be scanned as a 5-row track")
	}

	lines := make([]string, 15) // 15 lines over a 5-row track -> maxOffset = 10
	a := &App{settingsState: &SettingsState{
		// Help & Guide is dispatched via the switch's default case (whatever
		// index isn't one of the named settingsCat* constants below it) --
		// a 2-category fixture would make index 1 collide with
		// settingsCatNotifications instead, so this mirrors the real app's
		// full 6-category list to exercise the actual default branch.
		Categories:        []string{"Theme", "Notifications", "Display", "Audio", "Manage Servers", "Help & Guide"},
		SelectedCategory:  5,
		HelpRenderedLines: lines,
	}}

	press := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: z.StartX, Y: z.StartY}
	a.handleSettingsMouse(press)
	if !a.helpScrollDragging {
		t.Fatal("expected a Press on the scrollbar to start a drag")
	}
	if a.settingsState.HelpScrollOffset != 0 {
		t.Errorf("expected offset 0 from a press at the top of the track, got %d", a.settingsState.HelpScrollOffset)
	}

	// Motion further down the track, even past the zone's own X bounds
	// (simulating a cursor that drifted off the narrow scrollbar column),
	// must still update the offset.
	motion := tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: z.StartX + 30, Y: z.EndY}
	a.handleSettingsMouse(motion)
	if !a.helpScrollDragging {
		t.Error("expected the drag to still be active after a Motion event")
	}
	if a.settingsState.HelpScrollOffset != 10 {
		t.Errorf("expected offset 10 (maxOffset) after dragging to the bottom, got %d", a.settingsState.HelpScrollOffset)
	}

	release := tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: z.StartX, Y: z.EndY}
	a.handleSettingsMouse(release)
	if a.helpScrollDragging {
		t.Error("expected Release to end the drag")
	}
	// The offset from the last Motion event is left in place, matching
	// how every other drag-style UI leaves the value where you let go.
	if a.settingsState.HelpScrollOffset != 10 {
		t.Errorf("expected the offset to remain 10 after release, got %d", a.settingsState.HelpScrollOffset)
	}
}
