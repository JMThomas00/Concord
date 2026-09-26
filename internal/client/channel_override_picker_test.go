package client

import (
	"fmt"
	"strings"
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// TestHandleCreateChannelOverrideListsAllTextChannels is a regression test
// for a bug reported live 2026-09-11: "not all channels are present when
// going to Server Settings > Messages > N, pick a text channel." Builds a
// server with more text channels than a typical settings panel's visible
// row count and confirms handleCreateChannelOverride's underlying data
// (s.OverrideChannelList) includes every one of them -- proving the data
// layer itself is complete, so if channels still don't visibly appear the
// bug is in renderChannelPickerPage's lack of scroll-windowing, not here.
func TestHandleCreateChannelOverrideListsAllTextChannels(t *testing.T) {
	a := newLayoutTestApp(t, 120, 30)
	a.view = ViewServerManagement

	serverID := uuid.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	a.activeConn = sc
	a.currentServer = &models.Server{ID: serverID}

	const numChannels = 20
	var channels []*models.Channel
	wantNames := make(map[string]bool, numChannels)
	for i := 0; i < numChannels; i++ {
		name := fmt.Sprintf("channel-%02d", i)
		channels = append(channels, &models.Channel{
			ID:   uuid.New(),
			Name: name,
			Type: models.ChannelTypeText,
		})
		wantNames[name] = true
	}
	sc.SetChannels(serverID, channels)

	a.serverManagementState = &ServerManagementState{
		Categories:       []string{"Channels", "Roles", "Members", "Messages"},
		SelectedCategory: 3,
	}

	a.handleCreateChannelOverride()

	s := a.serverManagementState
	if !s.OverrideChannelPickerOpen {
		t.Fatal("expected the channel picker to open")
	}
	if got := len(s.OverrideChannelList); got != numChannels {
		t.Fatalf("expected all %d text channels in the picker list, got %d", numChannels, got)
	}
	for _, ch := range s.OverrideChannelList {
		delete(wantNames, ch.Name)
	}
	if len(wantNames) != 0 {
		t.Errorf("channels missing from the picker list: %v", wantNames)
	}
}

// TestVisibleListWindowStaysWithinBoundsAndKeepsSelectionVisible table-tests
// the windowing helper behind the fix above.
func TestVisibleListWindowStaysWithinBoundsAndKeepsSelectionVisible(t *testing.T) {
	tests := []struct {
		name              string
		selected, total, visible int
	}{
		{"short list, no windowing needed", 0, 5, 10},
		{"selection at start", 0, 30, 10},
		{"selection in middle", 15, 30, 10},
		{"selection at end", 29, 30, 10},
		{"zero visible rows (degenerate)", 5, 30, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := visibleListWindow(tt.selected, tt.total, tt.visible)
			if start < 0 || end > tt.total || start > end {
				t.Fatalf("visibleListWindow(%d, %d, %d) = (%d, %d), out of [0, %d] bounds", tt.selected, tt.total, tt.visible, start, end, tt.total)
			}
			if tt.visible > 0 && tt.total > tt.visible {
				if end-start > tt.visible {
					t.Errorf("window (%d, %d) is wider than visible=%d", start, end, tt.visible)
				}
				if tt.selected < start || tt.selected >= end {
					t.Errorf("selected index %d not within window (%d, %d)", tt.selected, start, end)
				}
			}
		})
	}
}

// TestRenderChannelPickerPageNeverExceedsRequestedHeight is a regression
// test for the actual reported symptom: renderChannelPickerPage used to
// write every channel in s.OverrideChannelList into the panel regardless
// of how many rows actually fit, so a long channel list rendered taller
// than the bordered box -- and bubbletea's renderer silently drops excess
// lines from the TOP of the whole screen (same failure mode as the
// 2026-09-06 renderMainView height bug), making it look like channels near
// the top of the list had vanished. This renders the picker with far more
// channels than a modest terminal height can show and asserts the output
// is still exactly `height` lines.
func TestRenderChannelPickerPageNeverExceedsRequestedHeight(t *testing.T) {
	a := newLayoutTestApp(t, 120, 24)
	s := &ServerManagementState{}
	for i := 0; i < 40; i++ {
		s.OverrideChannelList = append(s.OverrideChannelList, &models.Channel{
			ID:   uuid.New(),
			Name: fmt.Sprintf("channel-%02d", i),
		})
	}
	s.OverrideChannelSelected = 25 // deep into the list, past what 24 rows could show unwindowed

	for _, height := range []int{20, 24, 40} {
		out := a.renderChannelPickerPage(100, height, s)
		if got := strings.Count(out, "\n") + 1; got != height {
			t.Errorf("height=%d: renderChannelPickerPage produced %d lines, want %d", height, got, height)
		}
	}
}
