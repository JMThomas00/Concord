package client

import (
	"testing"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
)

func intPtr(v int) *int { return &v }

// TestHandleEditAction_ChannelModeWhenRowFocused is a regression test for
// the real gap this phase closes: E always built Mode: "server" regardless
// of which row (if any) was highlighted in the Exempt Channels / Channel
// Overrides list, so there was no way to set a custom per-channel retention
// value -- only the binary N-key "fully exempt" toggle. With a row focused,
// E should now edit that specific channel's override, pre-filled from its
// current values.
func TestHandleEditAction_ChannelModeWhenRowFocused(t *testing.T) {
	ch0 := uuid.New()
	ch1 := uuid.New()
	s := &ServerManagementState{
		Categories:       []string{"Channels", "Roles", "Members", "Messages", "Plugins"},
		SelectedCategory: 3,
		FocusOnForm:      true,
		ChannelOverrides: []*models.MessageRetentionPolicy{
			{ChannelID: &ch0}, // full exemption -- all nil
			{ChannelID: &ch1, TimeRetentionDays: intPtr(7), MaxMessageCount: intPtr(500)},
		},
		SelectedOverride: 1,
	}
	a := &App{serverManagementState: s}

	a.handleEditAction()

	form := s.RetentionFormState
	if form == nil {
		t.Fatal("expected RetentionFormState to be set")
	}
	if form.Mode != "channel" {
		t.Errorf("Mode = %q, want %q", form.Mode, "channel")
	}
	if form.ChannelID == nil || *form.ChannelID != ch1 {
		t.Errorf("ChannelID = %v, want %v", form.ChannelID, ch1)
	}
	if form.TimeRetentionDays != "7" {
		t.Errorf("TimeRetentionDays = %q, want %q", form.TimeRetentionDays, "7")
	}
	if form.MaxMessageCount != "500" {
		t.Errorf("MaxMessageCount = %q, want %q", form.MaxMessageCount, "500")
	}
	if form.SystemTimeRetentionDays != "" {
		t.Errorf("SystemTimeRetentionDays = %q, want empty (was nil on the override)", form.SystemTimeRetentionDays)
	}
}

// TestHandleEditAction_ServerModeWhenNoRowFocused is the regression guard
// for the existing path: with no channel overrides at all, E must still
// edit the server default exactly as before.
func TestHandleEditAction_ServerModeWhenNoRowFocused(t *testing.T) {
	s := &ServerManagementState{
		Categories:       []string{"Channels", "Roles", "Members", "Messages", "Plugins"},
		SelectedCategory: 3,
		FocusOnForm:      true,
		RetentionPolicy: &models.MessageRetentionPolicy{
			TimeRetentionDays: intPtr(30),
		},
	}
	a := &App{serverManagementState: s}

	a.handleEditAction()

	form := s.RetentionFormState
	if form == nil {
		t.Fatal("expected RetentionFormState to be set")
	}
	if form.Mode != "server" {
		t.Errorf("Mode = %q, want %q", form.Mode, "server")
	}
	if form.ChannelID != nil {
		t.Errorf("ChannelID = %v, want nil", form.ChannelID)
	}
	if form.TimeRetentionDays != "30" {
		t.Errorf("TimeRetentionDays = %q, want %q", form.TimeRetentionDays, "30")
	}
}

// TestHandleEditAction_ServerModeWhenSelectedOverrideOutOfBounds guards the
// bounds check itself -- SelectedOverride can be left stale (e.g. pointing
// past the end after the last override in the list was just removed)
// without a guaranteed re-clamp before E is pressed again.
func TestHandleEditAction_ServerModeWhenSelectedOverrideOutOfBounds(t *testing.T) {
	ch0 := uuid.New()
	s := &ServerManagementState{
		Categories:       []string{"Channels", "Roles", "Members", "Messages", "Plugins"},
		SelectedCategory: 3,
		FocusOnForm:      true,
		ChannelOverrides: []*models.MessageRetentionPolicy{{ChannelID: &ch0}},
		SelectedOverride: 5, // stale/out of bounds
	}
	a := &App{serverManagementState: s}

	a.handleEditAction()

	form := s.RetentionFormState
	if form == nil {
		t.Fatal("expected RetentionFormState to be set")
	}
	if form.Mode != "server" {
		t.Errorf("Mode = %q, want %q (out-of-bounds SelectedOverride should fall back to server default)", form.Mode, "server")
	}
}

// TestRetentionOverrideSuffix covers the label helper distinguishing a full
// exemption from a custom per-channel limit -- before this phase, every row
// in the list rendered as a plain "# name" with no indication of which kind
// of override it actually was.
func TestRetentionOverrideSuffix(t *testing.T) {
	tests := []struct {
		name     string
		override *models.MessageRetentionPolicy
		want     string
	}{
		{"fully exempt", &models.MessageRetentionPolicy{}, "(exempt)"},
		{"time only", &models.MessageRetentionPolicy{TimeRetentionDays: intPtr(7)}, "(7d)"},
		{"count only", &models.MessageRetentionPolicy{MaxMessageCount: intPtr(500)}, "(500 msgs)"},
		{"time and count", &models.MessageRetentionPolicy{TimeRetentionDays: intPtr(7), MaxMessageCount: intPtr(500)}, "(7d, 500 msgs)"},
		{"all three", &models.MessageRetentionPolicy{TimeRetentionDays: intPtr(7), SystemTimeRetentionDays: intPtr(3), MaxMessageCount: intPtr(500)}, "(7d, sys 3d, 500 msgs)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retentionOverrideSuffix(tt.override); got != tt.want {
				t.Errorf("retentionOverrideSuffix() = %q, want %q", got, tt.want)
			}
		})
	}
}
