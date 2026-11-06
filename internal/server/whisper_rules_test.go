package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Finding 10 (security audit 2026-11-05): whispers checked only that the
// recipient was online, so a muted member, or anyone, could whisper anyone
// and name any channel.
func TestWhispersFollowTheChannelsRules(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	s, channel, everyone := setupFileTransferTestServer(t, srv)
	amy, amyTok := addMemberWithEveryone(t, srv, s, everyone, "amy")
	bob, bobTok := addMemberWithEveryone(t, srv, s, everyone, "bob")
	// Someone on another server of the same Concord, not on this one.
	other := models.NewServer("Elsewhere", bob.ID)
	if err := srv.db.CreateServer(other); err != nil {
		t.Fatal(err)
	}
	otherEveryone := models.NewEveryoneRole(other.ID)
	srv.db.CreateRole(otherEveryone)
	otherCh := models.NewTextChannel(other.ID, "lobby")
	srv.db.CreateChannel(otherCh)
	out, outTok := addMemberWithEveryone(t, srv, other, otherEveryone, "outsider")

	a, b, o := newTestWSClient(t, wsURL), newTestWSClient(t, wsURL), newTestWSClient(t, wsURL)
	a.identify(amyTok)
	b.identify(bobTok)
	o.identify(outTok)

	whisper := func(c *testWSClient, to *models.User, ch *models.Channel, text string) {
		c.send(protocol.OpWhisper, protocol.WhisperPayload{TargetUserID: to.ID, ChannelID: ch.ID, Content: text})
	}
	firstWhisper := func(c *testWSClient) string {
		m := c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventWhisperCreate })
		var p protocol.WhisperCreatePayload
		json.Unmarshal(m.Data, &p)
		return p.Content
	}
	refused := func(c *testWSClient, want string) {
		t.Helper()
		m := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
			return m.Type == protocol.EventWhisperCreate || (m.Op == protocol.OpDispatch && m.Type == "")
		})
		if m.Type != "" || !strings.Contains(string(m.Data), want) {
			t.Fatalf("expected a refusal with %q, got %s %s", want, m.Type, m.Data)
		}
	}

	// The outsider whispers amy, from their own server's channel and from hers.
	whisper(o, amy, otherCh, "from elsewhere")
	refused(o, "aren't a member")
	whisper(o, amy, channel, "naming her channel")
	refused(o, "permission")

	// Amy, muted, whispers bob.
	if err := srv.db.SetMemberMuted(s.ID, amy.ID, true); err != nil {
		t.Fatal(err)
	}
	whisper(a, bob, channel, "while muted")
	refused(a, "muted")
	srv.db.SetMemberMuted(s.ID, amy.ID, false)

	// A normal whisper still arrives, and it's the first bob sees.
	whisper(a, bob, channel, "hello bob")
	if got := firstWhisper(b); got != "hello bob" {
		t.Fatalf("bob's first whisper was %q", got)
	}
	_ = out
}
