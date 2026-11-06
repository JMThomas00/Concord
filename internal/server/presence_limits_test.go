package server

import (
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Finding 12 (security audit 2026-11-05): status text had no length limit
// (up to the 512 KB message cap, broadcast to everyone sharing a server).
func TestStatusTextIsLimited(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	s, _, everyone := setupFileTransferTestServer(t, srv)
	_, tok := addMemberWithEveryone(t, srv, s, everyone, "talker")
	c := newTestWSClient(t, wsURL)
	c.identify(tok)

	c.send(protocol.OpPresenceUpdate, protocol.PresenceUpdatePayload{Status: models.StatusOnline, StatusText: strings.Repeat("x", 5000)})
	m := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return (m.Type == protocol.EventPresenceUpdate && strings.Contains(string(m.Data), "status_text")) || (m.Op == protocol.OpDispatch && m.Type == "")
	})
	if m.Type != "" {
		t.Fatalf("5000 characters of status were accepted: %.200s", m.Data)
	}
	c.send(protocol.OpPresenceUpdate, protocol.PresenceUpdatePayload{Status: models.StatusOnline, StatusText: "out\nto   lunch"})
	m = c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return (m.Type == protocol.EventPresenceUpdate && strings.Contains(string(m.Data), "status_text")) || (m.Op == protocol.OpDispatch && m.Type == "")
	})
	if m.Type == "" || !strings.Contains(string(m.Data), "out to lunch") {
		t.Fatalf("a short status: %s %s", m.Type, m.Data)
	}
}
