package client

import (
	"testing"

	"github.com/concord-chat/concord/internal/models"
)

// TestMemberDisplayGetDisplayName covers the exact gap found in manual
// testing 2026-09-05: a set nickname was never surfaced anywhere in the
// client (member list or message author line) because every render site
// called m.User.GetDisplayName(), which only knows about models.User's own
// (global) DisplayName field — never models.ServerMember.Nickname, a
// separate per-server field. MemberDisplay.GetDisplayName() is the fix:
// nickname first, then the user's own display name/username.
func TestMemberDisplayGetDisplayName(t *testing.T) {
	user := &models.User{Username: "gh0st"}

	t.Run("nickname set takes priority", func(t *testing.T) {
		m := &MemberDisplay{User: user, Member: &models.ServerMember{Nickname: "Testy McTestface"}}
		if got := m.GetDisplayName(); got != "Testy McTestface" {
			t.Errorf("expected nickname to win, got %q", got)
		}
	})

	t.Run("no nickname falls back to user display name", func(t *testing.T) {
		m := &MemberDisplay{User: user, Member: &models.ServerMember{}}
		if got := m.GetDisplayName(); got != "gh0st" {
			t.Errorf("expected fallback to username, got %q", got)
		}
	})

	t.Run("nil Member falls back cleanly", func(t *testing.T) {
		m := &MemberDisplay{User: user}
		if got := m.GetDisplayName(); got != "gh0st" {
			t.Errorf("expected fallback to username with nil Member, got %q", got)
		}
	})

	t.Run("nil User returns empty, doesn't panic", func(t *testing.T) {
		m := &MemberDisplay{}
		if got := m.GetDisplayName(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

// TestMemberOrUserName covers the message-payload-side resolution used for
// both a live MESSAGE_CREATE and a MESSAGES_HISTORY entry, where the server
// now carries the author's ServerMember alongside their User (see
// protocol.MessageCreatePayload.Member / protocol.MessageDisplay.Member).
func TestMemberOrUserName(t *testing.T) {
	cases := []struct {
		name     string
		member   *models.ServerMember
		username string
		want     string
	}{
		{"nickname set", &models.ServerMember{Nickname: "Nicky"}, "gh0st", "Nicky"},
		{"nickname empty falls back", &models.ServerMember{Nickname: ""}, "gh0st", "gh0st"},
		{"nil member falls back", nil, "gh0st", "gh0st"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memberOrUserName(tc.member, tc.username); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
