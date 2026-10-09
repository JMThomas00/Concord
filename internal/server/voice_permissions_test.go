package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// Connect and Speak are enforced (2026-10-09): a member with both joins
// voice normally; without Speak they join server-muted, and a later
// self-mute toggle keeps that; without Connect they can't join at all.
func TestVoicePermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	owner, _ := createTestUserAndToken(t, srv, "voice-owner")
	member, token := createTestUserAndToken(t, srv, "voice-member")
	s := models.NewServer("Voice Perms", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	everyone := models.NewEveryoneRole(s.ID)
	if everyone.Permissions&models.PermissionsVoice != models.PermissionsVoice {
		t.Fatal("a new server's @everyone lacks the voice permissions")
	}
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*models.User{owner, member} {
		srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID))
		srv.db.AddMemberRole(u.ID, s.ID, everyone.ID)
	}
	lounge := models.NewVoiceChannel(s.ID, "Lounge")
	if err := srv.db.CreateChannel(lounge); err != nil {
		t.Fatal(err)
	}

	c := newTestWSClient(t, wsURL)
	c.identify(token)
	join := func(selfMuted bool) *protocol.VoiceStateEventPayload {
		t.Helper()
		c.send(protocol.OpVoiceStateUpdate, protocol.VoiceStateUpdatePayload{ServerID: s.ID, ChannelID: &lounge.ID, IsSelfMuted: selfMuted})
		got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
			if m.Op != protocol.OpDispatch {
				return false
			}
			if m.Type == "" { // an error
				return true
			}
			var p protocol.VoiceStateEventPayload
			return m.Type == protocol.EventVoiceStateUpdate && json.Unmarshal(m.Data, &p) == nil && p.UserID == member.ID
		})
		if got == nil || got.Type == "" {
			return nil
		}
		var p protocol.VoiceStateEventPayload
		json.Unmarshal(got.Data, &p)
		return &p
	}
	leave := func() {
		c.send(protocol.OpVoiceStateUpdate, protocol.VoiceStateUpdatePayload{ServerID: s.ID})
		c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventVoiceStateUpdate })
	}
	setEveryone := func(p models.Permission) {
		everyone.Permissions = p
		if err := srv.db.UpdateRole(everyone); err != nil {
			t.Fatal(err)
		}
	}

	if p := join(false); p == nil || p.IsServerMuted {
		t.Fatalf("with Connect and Speak: %+v", p)
	}
	leave()

	setEveryone(everyone.Permissions &^ models.PermissionSpeak)
	if p := join(false); p == nil || !p.IsServerMuted {
		t.Fatalf("without Speak, not joined server-muted: %+v", p)
	}
	if p := join(true); p == nil || !p.IsServerMuted {
		t.Fatalf("a self-mute toggle reported the server mute as lifted: %+v", p)
	}
	leave()

	setEveryone(everyone.Permissions &^ models.PermissionConnect)
	if p := join(false); p != nil {
		t.Fatalf("without Connect, joined anyway: %+v", p)
	}
	_ = uuid.Nil
}
