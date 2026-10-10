package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"
)

func memberFixture(name string, status models.UserStatus, role *models.Role) *MemberDisplay {
	return &MemberDisplay{
		User:        &models.User{ID: uuid.New(), Username: name, Status: status},
		HighestRole: role,
		AvatarColor: "#bd93f9",
	}
}

// newMembersTestApp: admin/mod/regular online, a mod and a no-role member
// offline, and one member in a voice channel.
func newMembersTestApp(t *testing.T) (*App, map[string]*MemberDisplay) {
	t.Helper()
	a := newLayoutTestApp(t, 170, 40)
	admin := &models.Role{ID: uuid.New(), Name: "Admin", DisplayOrder: 0, Color: 0xffb86c}
	mod := &models.Role{ID: uuid.New(), Name: "Mod", DisplayOrder: 1}
	voiceCh := &models.Channel{ID: uuid.New(), Name: "lounge", Type: models.ChannelTypeVoice}

	m := map[string]*MemberDisplay{
		"ash":    memberFixture("ash", models.StatusOnline, admin),
		"juni":   memberFixture("juni", models.StatusIdle, mod),
		"pixel":  memberFixture("pixel", models.StatusOnline, nil),
		"rook":   memberFixture("rook", models.StatusOffline, mod),
		"wren":   memberFixture("wren", models.StatusOffline, nil),
		"talker": memberFixture("talker", models.StatusOnline, nil),
	}
	var list []*MemberDisplay
	for _, n := range []string{"wren", "pixel", "rook", "talker", "juni", "ash"} { // deliberately unsorted
		list = append(list, m[n])
	}
	a.activeConn = &ServerConnection{
		User:        m["ash"].User,
		Members:     list,
		Channels:    map[uuid.UUID][]*models.Channel{uuid.New(): {voiceCh}},
		VoiceStates: map[uuid.UUID]*models.VoiceState{m["talker"].User.ID: {UserID: m["talker"].User.ID, ChannelID: voiceCh.ID}},
	}
	return a, m
}

func names(list []*MemberDisplay) string {
	var out []string
	for _, m := range list {
		out = append(out, m.User.Username)
	}
	return strings.Join(out, ",")
}

func TestMemberSectionsGroupByPresenceThenRole(t *testing.T) {
	a, _ := newMembersTestApp(t)
	voice, online, offline := a.memberSections()

	if len(voice) != 1 || voice[0].name != "lounge" || names(voice[0].members) != "talker" {
		t.Errorf("voice groups = %+v, want lounge: talker", voice)
	}
	if got := names(online); got != "ash,juni,pixel" {
		t.Errorf("online = %s, want ash,juni,pixel (Admin, then Mod, then no role)", got)
	}
	if got := names(offline); got != "rook,wren" {
		t.Errorf("offline = %s, want rook,wren (Mod, then no role)", got)
	}
	if got := names(a.buildFlatMemberList()); got != "talker,ash,juni,pixel,rook,wren" {
		t.Errorf("keyboard order = %s, want voice, then online, then offline", got)
	}
}

func TestMembersPanelShowsOnlineAndOfflineGroups(t *testing.T) {
	a, _ := newMembersTestApp(t)
	view := ansi.Strip(zone.Scan(a.renderUserList(30, 40)))

	onlineAt := strings.Index(view, "── ONLINE (3) ──")
	offlineAt := strings.Index(view, "── OFFLINE (2) ──")
	if onlineAt < 0 || offlineAt < 0 || offlineAt < onlineAt {
		t.Fatalf("expected ONLINE (3) then OFFLINE (2) headers:\n%s", view)
	}
	for _, old := range []string{"── ADMIN", "── MOD", "── MEMBERS"} {
		if strings.Contains(view, old) {
			t.Errorf("old role-group header %q still present", old)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, " ash") && !strings.Contains(line, "Admin") {
			t.Errorf("expected ash's row to carry the Admin role label: %q", line)
		}
	}
}

// The "> " selection marker must land on exactly the member keyboard
// navigation has selected, for every index -- the panel and
// buildFlatMemberList share memberSections, so they can't drift apart.
func TestMemberSelectionMarkerMatchesKeyboardOrder(t *testing.T) {
	a, _ := newMembersTestApp(t)
	a.focus = FocusUserList
	flat := a.buildFlatMemberList()
	for i, want := range flat {
		a.selectedMemberIndex = i
		view := ansi.Strip(zone.Scan(a.renderUserList(30, 40)))
		var marked []string
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "> (") {
				marked = append(marked, line)
			}
		}
		if len(marked) != 1 || !strings.Contains(marked[0], want.User.Username) {
			t.Errorf("index %d: want %s marked, got %q", i, want.User.Username, marked)
		}
	}
}
