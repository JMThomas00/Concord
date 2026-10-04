package server

import (
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// A category only groups channels; a message sent to one is refused
// rather than stored where no one can see it.
func TestMessagesToACategoryAreRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wire-level integration test in short mode")
	}
	srv, wsURL := startPlainTestServer(t)
	owner, token := createTestUserAndToken(t, srv, "cat-owner")
	s := models.NewServer("Category Test", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(owner.ID, s.ID)); err != nil {
		t.Fatal(err)
	}
	cat := models.NewTextChannel(s.ID, "GROUP")
	cat.Type = models.ChannelTypeCategory
	if err := srv.db.CreateChannel(cat); err != nil {
		t.Fatal(err)
	}

	c := newTestWSClient(t, wsURL)
	c.identify(token)
	c.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: cat.ID, Content: "hello?"})
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && (m.Type == protocol.EventMessageCreate || m.Type == "")
	})
	if got == nil || got.Type != "" || !strings.Contains(string(got.Data), "Categories") {
		t.Fatalf("expected a refusal, got %+v", got)
	}
	if msgs, _ := srv.db.GetChannelMessages(cat.ID, 10, nil, owner.ID); len(msgs) != 0 {
		t.Fatalf("%d messages stored in the category", len(msgs))
	}
}
