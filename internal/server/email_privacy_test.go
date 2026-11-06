package server

import (
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
)

// Found while fixing the 2026-11-05 audit (finding 19): every user object
// sent over the wire carried the account's email, so each member's
// SERVER_CREATE listed every other member's address, and messages and
// whispers carried their author's.
func TestOtherMembersNeverSeeAnEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	s, channel, everyone := setupFileTransferTestServer(t, srv)
	amy, amyTok := addMemberWithEveryone(t, srv, s, everyone, "amy")
	bob, bobTok := addMemberWithEveryone(t, srv, s, everyone, "bob")
	if amy.Email == "" {
		t.Fatal("the test user has no email")
	}
	a, b := newTestWSClient(t, wsURL), newTestWSClient(t, wsURL)
	a.identify(amyTok)
	b.send(protocol.OpIdentify, protocol.IdentifyPayload{Token: bobTok})

	seen := func(m *protocol.Message, what string) {
		t.Helper()
		if strings.Contains(string(m.Data), amy.Email) {
			t.Errorf("amy's email reached bob in %s", what)
		}
	}
	seen(b.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventServerCreate }), "SERVER_CREATE")
	a.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: channel.ID, Content: "hi"})
	seen(b.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMessageCreate }), "MESSAGE_CREATE")
	a.send(protocol.OpWhisper, protocol.WhisperPayload{TargetUserID: bob.ID, ChannelID: channel.ID, Content: "psst"})
	seen(b.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventWhisperCreate }), "WHISPER_CREATE")
}
