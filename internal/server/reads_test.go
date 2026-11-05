package server

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func TestMentionNames(t *testing.T) {
	cases := map[string][]string{
		"hi @Bob and @bob.":                {"bob"},
		"@alice, @carol-2 @everyone @here": {"alice", "carol-2"},
		"mail me at me@example.com":        nil,
		"@ alone, and @":                   nil,
		"(@dave)":                          {"dave"},
	}
	for in, want := range cases {
		if got := mentionNames(in); !reflect.DeepEqual(got, want) {
			t.Errorf("mentionNames(%q) = %q, want %q", in, got, want)
		}
	}
}

// unreadsOnSignIn signs in and returns the SERVER_CREATE unreads for one server.
func unreadsOnSignIn(t *testing.T, wsURL, token string, serverID uuid.UUID) (map[uuid.UUID]protocol.ChannelUnread, *testWSClient) {
	t.Helper()
	c := newTestWSClient(t, wsURL)
	c.send(protocol.OpIdentify, protocol.IdentifyPayload{Token: token})
	m := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		if m.Type != protocol.EventServerCreate {
			return false
		}
		var p protocol.ServerCreatePayload
		return json.Unmarshal(m.Data, &p) == nil && p.Server != nil && p.Server.ID == serverID
	})
	if m == nil {
		t.Fatal("no SERVER_CREATE")
	}
	var p protocol.ServerCreatePayload
	mustUnmarshal(t, m.Data, &p)
	out := map[uuid.UUID]protocol.ChannelUnread{}
	for _, u := range p.Unreads {
		out[u.ChannelID] = u
	}
	return out, c
}

// Unreads survive signing out, count mentions by @name, and a read on one
// device reaches the others.
func TestUnreadsOverTheWire(t *testing.T) {
	srv, wsURL, users, tokens, ch, _ := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions)
	sid := ch.ServerID

	// The channel was made after users[2] joined, so its first message is new to them.
	got0, first := unreadsOnSignIn(t, wsURL, tokens[2], sid)
	if got0[ch.ID].Unread != 1 {
		t.Fatalf("before: %+v", got0[ch.ID])
	}
	first.send(protocol.OpChannelRead, protocol.ChannelReadRequest{ChannelID: ch.ID})
	if first.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventChannelRead }) == nil {
		t.Fatal("no CHANNEL_READ")
	}

	author := newTestWSClient(t, wsURL)
	author.identify(tokens[1])
	post(t, author, ch.ID, "morning all", nil)
	m := post(t, author, ch.ID, "hey @"+users[2].Username+", look", nil)
	if len(m.Mentions) != 1 || m.Mentions[0] != users[2].ID || m.Seq == 0 {
		t.Errorf("mentions %v, seq %d", m.Mentions, m.Seq)
	}

	got, phone := unreadsOnSignIn(t, wsURL, tokens[2], sid)
	if u := got[ch.ID]; u.Unread != 2 || u.Mentions != 1 {
		t.Fatalf("after two messages: %+v", u)
	}
	_, laptop := unreadsOnSignIn(t, wsURL, tokens[2], sid)

	laptop.send(protocol.OpChannelRead, protocol.ChannelReadRequest{ChannelID: ch.ID})
	ev := phone.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventChannelRead })
	if ev == nil {
		t.Fatal("the other device wasn't told")
	}
	var read protocol.ChannelReadPayload
	mustUnmarshal(t, ev.Data, &read)
	if read.ChannelID != ch.ID || read.ReadSeq != m.Seq {
		t.Errorf("CHANNEL_READ %+v, want seq %d", read, m.Seq)
	}
	if got, _ := unreadsOnSignIn(t, wsURL, tokens[2], sid); got[ch.ID].Unread != 0 || got[ch.ID].ReadSeq != m.Seq {
		t.Errorf("after reading: %+v", got[ch.ID])
	}

	// Someone who isn't a member can't move anyone's position.
	outsider, tok := createTestUserAndToken(t, srv, "outsider-"+uuid.NewString()[:6])
	_ = outsider
	o := newTestWSClient(t, wsURL)
	o.identify(tok)
	o.send(protocol.OpChannelRead, protocol.ChannelReadRequest{ChannelID: ch.ID})
	if e := o.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == protocol.EventChannelRead) }); e == nil || e.Type != "" {
		t.Errorf("an outsider got %+v, want an error", e)
	}
}
