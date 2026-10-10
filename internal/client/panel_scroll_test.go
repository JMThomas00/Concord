package client

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"
)

func plainLines(s string) []string { return strings.Split(ansi.Strip(zone.Scan(s)), "\n") }

// A panel renders exactly its height whether or not it's scrolled.
func assertHeight(t *testing.T, what string, view string, want int) {
	t.Helper()
	if got := len(plainLines(view)); got != want {
		t.Fatalf("%s is %d lines, want %d", what, got, want)
	}
}

func TestChannelListScrollsToSelectionAndWheelLeavesSelectionAlone(t *testing.T) {
	a := newLayoutTestApp(t, 160, 30)
	a.channelTree = newBenchChannelTree(map[uuid.UUID]bool{}) // 110 rows
	flat := a.channelTree.FlatList
	last := flat[len(flat)-1].Channel
	a.currentChannel = last

	view := a.renderChannelList(26, 30)
	assertHeight(t, "channel list", view, 30)
	joined := strings.Join(plainLines(view), "\n")
	if !strings.Contains(joined, last.Name) {
		t.Fatalf("selected last channel %q not scrolled into view:\n%s", last.Name, joined)
	}
	if !strings.Contains(joined, "┃") {
		t.Error("expected a scrollbar thumb on an overflowing channel list")
	}

	// Wheel up scrolls the list but must not change the open channel.
	for i := 0; i < 5; i++ {
		a.channelScroll.wheel(true)
	}
	joined = strings.Join(plainLines(a.renderChannelList(26, 30)), "\n")
	if a.currentChannel != last {
		t.Fatal("wheel scrolling changed the selected channel")
	}
	if strings.Contains(joined, last.Name) {
		t.Error("wheel up should have scrolled the last channel out of view")
	}

	// Selecting a channel again (arrow keys) brings the view back to it.
	a.currentChannel = flat[len(flat)-2].Channel
	if joined = strings.Join(plainLines(a.renderChannelList(26, 30)), "\n"); !strings.Contains(joined, a.currentChannel.Name) {
		t.Error("a new selection should scroll back into view")
	}
}

func TestChannelListWithoutOverflowHasNoScrollbar(t *testing.T) {
	a := newLayoutTestApp(t, 160, 30) // 4-row tree
	if strings.Contains(strings.Join(plainLines(a.renderChannelList(26, 30)), "\n"), "┃") {
		t.Error("no scrollbar expected when everything fits")
	}
}

func TestWheelOverChannelListRoutesToItsScroll(t *testing.T) {
	a := newLayoutTestApp(t, 160, 30)
	a.view = ViewMain
	a.channelTree = newBenchChannelTree(map[uuid.UUID]bool{})
	zone.Scan(a.renderMainView()) // register panel zones
	// bubblezone records zones on a background goroutine, so give it a moment.
	z := zone.Get("channel-list")
	for deadline := time.Now().Add(time.Second); (z == nil || z.IsZero()) && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		z = zone.Get("channel-list")
	}
	if z == nil || z.IsZero() {
		t.Fatal("channel-list zone not registered")
	}
	a.Update(tea.MouseMsg{X: z.StartX + 2, Y: z.StartY + 3, Type: tea.MouseWheelDown})
	if a.channelScroll.offset != panelWheelStep {
		t.Errorf("wheel down over the channel list: offset %d, want %d", a.channelScroll.offset, panelWheelStep)
	}
}

func TestMembersPanelScrollsToSelectedMember(t *testing.T) {
	a := newLayoutTestApp(t, 170, 20)
	var list []*MemberDisplay
	for i := 0; i < 40; i++ {
		status := models.StatusOnline
		if i%3 == 0 {
			status = models.StatusOffline
		}
		list = append(list, memberFixture(fmt.Sprintf("user%02d", i), status, nil))
	}
	a.activeConn = &ServerConnection{User: list[0].User, Members: list}
	a.focus = FocusUserList
	flat := a.buildFlatMemberList()
	a.selectedMemberIndex = len(flat) - 1

	view := a.renderUserList(30, 20)
	assertHeight(t, "members panel", view, 20)
	joined := strings.Join(plainLines(view), "\n")
	if !strings.Contains(joined, "> ") || !strings.Contains(joined, flat[len(flat)-1].User.Username) {
		t.Fatalf("selected last member not scrolled into view:\n%s", joined)
	}

	a.selectedMemberIndex = 0
	if joined = strings.Join(plainLines(a.renderUserList(30, 20)), "\n"); !strings.Contains(joined, flat[0].User.Username) {
		t.Error("selecting the first member should scroll back to the top")
	}
}

// With both panels overflowing, the whole screen must still be exactly the
// terminal's height (an extra line makes bubbletea drop the top row).
func TestMainViewHeightWithOverflowingPanels(t *testing.T) {
	for _, h := range []int{24, 40} {
		a := newLayoutTestApp(t, 170, h)
		a.channelTree = newBenchChannelTree(map[uuid.UUID]bool{})
		a.currentChannel = a.channelTree.FlatList[len(a.channelTree.FlatList)-1].Channel
		var list []*MemberDisplay
		for i := 0; i < 40; i++ {
			list = append(list, memberFixture(fmt.Sprintf("user%02d", i), models.StatusOnline, nil))
		}
		a.activeConn = &ServerConnection{User: list[0].User, Members: list}
		assertHeight(t, fmt.Sprintf("main view at height %d", h), a.renderMainView(), h)
	}
}

// A blank line closes each group so voice / online / offline read apart.
func TestMemberGroupsAreSeparatedByABlankLine(t *testing.T) {
	a, _ := newMembersTestApp(t)
	lines := plainLines(a.renderUserList(30, 40))
	for _, header := range []string{"── ONLINE", "── OFFLINE"} {
		for i, l := range lines {
			if strings.Contains(l, header) {
				if prev := strings.Trim(lines[i-1], "│ "); prev != "" {
					t.Errorf("expected a blank line above %q, got %q", header, lines[i-1])
				}
			}
		}
	}
}
