package client

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// TestHandleServerManagementMouseNilStateDoesNothing mirrors
// TestHandleSettingsMouseNilStateDoesNothing -- a mouse event arriving on
// some stale/unexpected frame must not panic when the state is nil.
func TestHandleServerManagementMouseNilStateDoesNothing(t *testing.T) {
	a := &App{}
	if cmd := a.handleServerManagementMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); cmd != nil {
		t.Error("expected nil with a nil serverManagementState")
	}
}

// TestHandleServerManagementMouseIgnoresNonPressNonLeftEvents mirrors the
// equivalent Settings/Link Browser tests -- only a genuine left-button
// press should ever act.
func TestHandleServerManagementMouseIgnoresNonPressNonLeftEvents(t *testing.T) {
	a := &App{serverManagementState: &ServerManagementState{Categories: []string{"Channels", "Roles"}}}
	if cmd := a.handleServerManagementMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}); cmd != nil {
		t.Error("expected a release event to be ignored")
	}
	if cmd := a.handleServerManagementMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight}); cmd != nil {
		t.Error("expected a right-click press to be ignored")
	}
}

// TestHandleServerManagementMouseNoZoneMatchDoesNothing confirms an
// unmatched click (no real render/scan pass happened in this unit-test
// context) returns nil and mutates no state, mirroring the equivalent
// Settings test.
func TestHandleServerManagementMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{Categories: []string{"Channels", "Roles", "Members", "Messages", "Plugins"}, SelectedCategory: 0}
	a := &App{serverManagementState: s}
	if cmd := a.handleServerManagementMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land in any known zone")
	}
	if s.SelectedCategory != 0 || s.FocusOnForm {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandleChannelsCategoryMouseNoZoneMatchDoesNothing and its siblings
// below cover the remaining four category list resolvers' guard-clause
// path -- each mirrors resolveClickedChannelRow's own "no match" test.
func TestHandleChannelsCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := &App{serverManagementState: &ServerManagementState{SelectedChannel: 2}}
	if cmd := a.handleChannelsCategoryMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if a.serverManagementState.SelectedChannel != 2 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleRolesCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := &App{serverManagementState: &ServerManagementState{SelectedRole: 1}}
	if cmd := a.handleRolesCategoryMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if a.serverManagementState.SelectedRole != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleMembersCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := &App{serverManagementState: &ServerManagementState{SelectedMember: 3}}
	if cmd := a.handleMembersCategoryMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if a.serverManagementState.SelectedMember != 3 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleMessagesCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := &App{serverManagementState: &ServerManagementState{SelectedOverride: 1}}
	if cmd := a.handleMessagesCategoryMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if a.serverManagementState.SelectedOverride != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandlePluginsCategoryMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := &App{serverManagementState: &ServerManagementState{SelectedPlugin: 1}}
	if cmd := a.handlePluginsCategoryMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if a.serverManagementState.SelectedPlugin != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandlePermissionsEditorMouseNoZoneMatchDoesNothing covers the
// Permissions Editor's guard-clause path -- a real click (see
// TestWriteZoneMarkedLinesSpansAllLines for the round-trip pattern used
// elsewhere) would synthesize Space via handlePermissionsEditorKey, but an
// unmatched one must touch nothing.
func TestHandlePermissionsEditorMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{PermSelectedIndex: 2}
	a := &App{serverManagementState: s}
	if cmd := a.handlePermissionsEditorMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known permission row")
	}
	if s.PermSelectedIndex != 2 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleOverwriteTargetPickerMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{OverwriteTargetIndex: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleOverwriteTargetPickerMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known target row")
	}
	if s.OverwriteTargetIndex != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleOverwriteEditorMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{OverwriteSelectedIndex: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleOverwriteEditorMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known permission row")
	}
	if s.OverwriteSelectedIndex != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleRoleAssignMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{RoleAssignFocus: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleRoleAssignMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known role row")
	}
	if s.RoleAssignFocus != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleFilterPanelMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{FilterPanelFocus: 2}
	a := &App{serverManagementState: s}
	if cmd := a.handleFilterPanelMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known filter option")
	}
	if s.FilterPanelFocus != 2 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandleKickConfirmMouseNoZoneMatchDoesNothing and its Ban/Unmute/
// Delete siblings mirror the "no zone match" guard-clause pattern for the
// four Tab-cycled-button confirm dialogs.
func TestHandleKickConfirmMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{KickConfirmFocusedBtn: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleKickConfirmMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on Yes or Cancel")
	}
	if s.KickConfirmFocusedBtn != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleBanConfirmMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{BanConfirmFocusedBtn: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleBanConfirmMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on Yes or Cancel")
	}
	if s.BanConfirmFocusedBtn != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleUnmuteConfirmMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{UnmuteConfirmFocusedBtn: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleUnmuteConfirmMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on Yes or Cancel")
	}
	if s.UnmuteConfirmFocusedBtn != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleSrvMgmtDeleteConfirmMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{DeleteConfirmFocusedButton: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleSrvMgmtDeleteConfirmMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on Yes or Cancel")
	}
	if s.DeleteConfirmFocusedButton != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandleMuteDurationMouseNoZoneMatchDoesNothing confirms the mute-
// duration page's click-to-focus-only resolver (no auto-submit, since Enter
// there is a single global "apply the mute now" action with no dedicated
// confirm button in the render) mutates nothing on an unmatched click.
func TestHandleMuteDurationMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{MuteDurationFocus: 2}
	a := &App{serverManagementState: s}
	if cmd := a.handleMuteDurationMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known field")
	}
	if s.MuteDurationFocus != 2 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleRemoveExemptMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{RemoveExemptSelected: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleRemoveExemptMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if s.RemoveExemptSelected != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleChannelPickerMouseNoZoneMatchDoesNothing(t *testing.T) {
	s := &ServerManagementState{OverrideChannelSelected: 1}
	a := &App{serverManagementState: s}
	if cmd := a.handleChannelPickerMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if s.OverrideChannelSelected != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleRetentionFormMouseNoZoneMatchDoesNothing(t *testing.T) {
	form := &RetentionFormState{FocusField: 1}
	s := &ServerManagementState{RetentionFormState: form}
	a := &App{serverManagementState: s}
	if cmd := a.handleRetentionFormMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known field or button")
	}
	if form.FocusField != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandlePruneConfirmMouseNoZoneMatchDoesNothing(t *testing.T) {
	a := &App{serverManagementState: &ServerManagementState{}}
	if cmd := a.handlePruneConfirmMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on Yes or Cancel")
	}
}

func TestHandleMoveDialogMouseNoZoneMatchDoesNothing(t *testing.T) {
	state := &MoveDialogState{SelectedIndex: 1}
	s := &ServerManagementState{MoveDialogState: state}
	a := &App{serverManagementState: s}
	if cmd := a.handleMoveDialogMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known row")
	}
	if state.SelectedIndex != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestHandleChannelFormMouseNoZoneMatchDoesNothing and
// TestHandleRoleFormMouseNoZoneMatchDoesNothing cover the two most complex
// resolvers' guard-clause path -- neither should touch FocusField (or any
// other form field) on a click that lands nowhere.
func TestHandleChannelFormMouseNoZoneMatchDoesNothing(t *testing.T) {
	state := &ChannelFormState{FocusField: 1, TypeIndex: 0}
	s := &ServerManagementState{ChannelFormState: state}
	a := &App{serverManagementState: s}
	if cmd := a.handleChannelFormMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known field")
	}
	if state.FocusField != 1 || state.TypeIndex != 0 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

func TestHandleRoleFormMouseNoZoneMatchDoesNothing(t *testing.T) {
	state := &RoleFormState{FocusField: 2, ColorIndex: 1}
	s := &ServerManagementState{RoleFormState: state}
	a := &App{serverManagementState: s}
	if cmd := a.handleRoleFormMouse(tea.MouseMsg{X: 0, Y: 0}); cmd != nil {
		t.Error("expected no command when the click doesn't land on any known field")
	}
	if state.FocusField != 2 || state.ColorIndex != 1 {
		t.Error("expected no state mutation on an unmatched click")
	}
}

// TestSetChannelFormFocusBlursOthers confirms the click-to-focus-field
// helper applies the same blur-all-then-focus-one pattern
// handleChannelFormKey's local focusField closure uses for Tab/Shift+Tab,
// mirroring TestSetAddServerFocusBlursOthers's shape for the Settings
// Add/Edit Server form.
func TestSetChannelFormFocusBlursOthers(t *testing.T) {
	state := &ChannelFormState{}
	state.NameTextInput = textinput.New()
	state.MaxUsersInput = textinput.New()
	state.NameTextInput.Focus()
	state.TypeIndex = 1 // voice, so maxUsersField is meaningful

	a := &App{}
	layout := computeChannelFormLayout(state)
	a.setChannelFormFocus(state, layout, layout.maxUsersField)

	if state.NameTextInput.Focused() {
		t.Error("expected the previously-focused Name field to be blurred")
	}
	if !state.MaxUsersInput.Focused() {
		t.Error("expected the Max Users field to be focused")
	}
	if state.FocusField != layout.maxUsersField {
		t.Errorf("expected FocusField=%d, got %d", layout.maxUsersField, state.FocusField)
	}
}
