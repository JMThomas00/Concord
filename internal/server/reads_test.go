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

// postReply sends a reply to another message and returns it as broadcast.
func postReply(t *testing.T, c *testWSClient, ch uuid.UUID, content string, to uuid.UUID) *models.Message {
	t.Helper()
	nonce := uuid.NewString()
	c.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: ch, Content: content, ReplyToID: &to, Nonce: nonce})
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		var p protocol.MessageCreatePayload
		return m.Type == protocol.EventMessageCreate && json.Unmarshal(m.Data, &p) == nil && p.Nonce == nonce
	})
	if got == nil {
		t.Fatalf("no MESSAGE_CREATE for %q", content)
	}
	var p protocol.MessageCreatePayload
	mustUnmarshal(t, got.Data, &p)
	return p.Message
}

func requestMentions(t *testing.T, c *testWSClient, serverID uuid.UUID, before int64) protocol.MentionsPayload {
	t.Helper()
	c.send(protocol.OpRequestMentions, protocol.MentionsRequest{ServerID: serverID, Before: before})
	m := c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMentions || (m.Op == protocol.OpDispatch && m.Type == "") })
	if m == nil || m.Type != protocol.EventMentions {
		t.Fatalf("no MENTIONS: %+v", m)
	}
	var p protocol.MentionsPayload
	mustUnmarshal(t, m.Data, &p)
	return p
}

// The inbox over the wire: a reply reaches the person replied to as a
// mention, MENTIONS lists @name and the reply with their kinds, and a jump
// loads history around a message.
func TestMentionsOverTheWire(t *testing.T) {
	srv, wsURL, users, tokens, ch, _ := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions)
	sid := ch.ServerID
	bob := newTestWSClient(t, wsURL)
	bob.identify(tokens[2])
	alice := newTestWSClient(t, wsURL)
	alice.identify(tokens[1])

	mine := post(t, bob, ch.ID, "bob's question", nil)
	reply := postReply(t, alice, ch.ID, "an answer", mine.ID)
	if len(reply.Mentions) != 1 || reply.Mentions[0] != users[2].ID {
		t.Errorf("the reply's mentions: %v", reply.Mentions)
	}
	self := postReply(t, bob, ch.ID, "answering myself", mine.ID)
	if len(self.Mentions) != 0 {
		t.Errorf("replying to yourself mentioned you: %v", self.Mentions)
	}
	named := post(t, alice, ch.ID, "and @"+users[2].Username, nil)

	p := requestMentions(t, bob, sid, 0)
	if len(p.Items) != 2 || p.Items[0].Message.ID != named.ID || p.Items[0].Kind != protocol.MentionDirect ||
		p.Items[1].Message.ID != reply.ID || p.Items[1].Kind != protocol.MentionReply || !p.Items[1].Unread {
		t.Fatalf("mentions: %+v", p.Items)
	}
	if p.Items[0].Message.Author == nil || p.Items[0].Message.Author.ID != users[1].ID {
		t.Error("no author on a mention")
	}
	if got, _ := unreadsOnSignIn(t, wsURL, tokens[2], sid); got[ch.ID].Mentions != 2 {
		t.Errorf("badge %+v, want 2 mentions", got[ch.ID])
	}

	// History around the reply starts before it and runs to the newest.
	bob.send(protocol.OpRequestMessages, protocol.MessageHistoryRequest{ChannelID: ch.ID, Around: &reply.ID})
	h := bob.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMessagesHistory })
	if h == nil {
		t.Fatal("no history")
	}
	var hist protocol.MessageHistoryPayload
	mustUnmarshal(t, h.Data, &hist)
	if hist.Around == nil || *hist.Around != reply.ID || len(hist.Messages) == 0 || hist.Messages[len(hist.Messages)-1].ID != named.ID {
		t.Errorf("around: %d messages, around %v", len(hist.Messages), hist.Around)
	}

	// Someone outside the server gets neither mentions nor history.
	_, tok := createTestUserAndToken(t, srv, "outsider-"+uuid.NewString()[:6])
	o := newTestWSClient(t, wsURL)
	o.identify(tok)
	o.send(protocol.OpRequestMentions, protocol.MentionsRequest{ServerID: sid})
	if e := o.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == protocol.EventMentions) }); e == nil || e.Type != "" {
		t.Errorf("an outsider's mentions: %+v", e)
	}
	o.send(protocol.OpRequestMessages, protocol.MessageHistoryRequest{ChannelID: ch.ID})
	if e := o.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == protocol.EventMessagesHistory) }); e == nil || e.Type != "" {
		t.Errorf("an outsider read the history: %+v", e)
	}
}
