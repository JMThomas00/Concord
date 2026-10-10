package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// threadTestServer is a server with a channel and three members: alice starts
// threads, bob replies, carol only gets @mentioned.
func threadTestServer(t *testing.T) (*Server, string, *models.Channel, map[string]*models.User, map[string]string) {
	t.Helper()
	srv, wsURL := startPlainTestServer(t)
	users, tokens := map[string]*models.User{}, map[string]string{}
	for _, name := range []string{"alice", "bob", "carol"} {
		users[name], tokens[name] = createTestUserAndToken(t, srv, "thread-"+name)
	}
	s := models.NewServer("Thread Test", users["alice"].ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	everyone := models.NewEveryoneRole(s.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if err := srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID)); err != nil {
			t.Fatal(err)
		}
		if err := srv.db.AddMemberRole(u.ID, s.ID, everyone.ID); err != nil {
			t.Fatal(err)
		}
	}
	ch := models.NewTextChannel(s.ID, "threads")
	if err := srv.db.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	return srv, wsURL, ch, users, tokens
}

func post(t *testing.T, c *testWSClient, ch uuid.UUID, content string, thread *uuid.UUID) *models.Message {
	t.Helper()
	nonce := uuid.NewString()
	c.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: ch, Content: content, ThreadID: thread, Nonce: nonce})
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		if m.Op != protocol.OpDispatch || m.Type != protocol.EventMessageCreate {
			return false
		}
		var p protocol.MessageCreatePayload
		return json.Unmarshal(m.Data, &p) == nil && p.Nonce == nonce
	})
	if got == nil {
		t.Fatalf("no MESSAGE_CREATE for %q", content)
	}
	var p protocol.MessageCreatePayload
	json.Unmarshal(got.Data, &p)
	return p.Message
}

func threadUpdate(t *testing.T, c *testWSClient, thread uuid.UUID) *protocol.ThreadSummary {
	t.Helper()
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		if m.Op != protocol.OpDispatch || m.Type != protocol.EventThreadUpdate {
			return false
		}
		var s protocol.ThreadSummary
		return json.Unmarshal(m.Data, &s) == nil && s.ThreadID == thread
	})
	if got == nil {
		t.Fatal("no THREAD_UPDATE")
	}
	var s protocol.ThreadSummary
	json.Unmarshal(got.Data, &s)
	return &s
}

func history(t *testing.T, c *testWSClient, ch uuid.UUID) []*protocol.MessageDisplay {
	t.Helper()
	c.send(protocol.OpRequestMessages, protocol.MessageHistoryRequest{ChannelID: ch, Limit: 50})
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventMessagesHistory
	})
	if got == nil {
		t.Fatal("no MESSAGES_HISTORY")
	}
	var p protocol.MessageHistoryPayload
	json.Unmarshal(got.Data, &p)
	return p.Messages
}

// Threads end to end (Threads Plan, phase 1): replies go into a thread and
// stay out of channel history, which shows a summary instead; the replies
// load by thread; following, unread and read work; a reply to a reply stays
// in the same thread; a thread from another channel is refused; a deleted
// first message still heads its thread.
func TestThreadsEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL, ch, users, tokens := threadTestServer(t)
	alice, bob := newTestWSClient(t, wsURL), newTestWSClient(t, wsURL)
	alice.identify(tokens["alice"])
	bob.identify(tokens["bob"])

	root := post(t, alice, ch.ID, "Has anyone tested the docker build yet?", nil)
	post(t, alice, ch.ID, "Unrelated channel chatter", nil)

	// Bob replies in the thread and mentions Carol.
	r1 := post(t, bob, ch.ID, "Works on my VM <@"+users["carol"].ID.String()+">", &root.ID)
	if r1.ThreadID == nil || *r1.ThreadID != root.ID {
		t.Fatalf("reply's thread_id = %v, want %s", r1.ThreadID, root.ID)
	}
	sum := threadUpdate(t, alice, root.ID)
	if sum.ReplyCount != 1 || sum.LastReply == nil || sum.LastReply.Content != r1.Content {
		t.Fatalf("THREAD_UPDATE after the first reply: %+v", sum)
	}
	followers := map[uuid.UUID]bool{}
	for _, id := range sum.Followers {
		followers[id] = true
	}
	for _, name := range []string{"alice", "bob", "carol"} {
		if !followers[users[name].ID] {
			t.Errorf("%s doesn't follow the thread (starter, replier, mentioned): %v", name, sum.Followers)
		}
	}

	// A reply to a reply goes into the same thread.
	r2 := post(t, alice, ch.ID, "Thanks!", &r1.ID)
	if r2.ThreadID == nil || *r2.ThreadID != root.ID {
		t.Fatalf("a reply to a reply landed in %v, want the thread %s", r2.ThreadID, root.ID)
	}
	post(t, bob, ch.ID, "Also fine on Linux", &root.ID)

	// History: the first message and the chatter, no replies; a summary.
	msgs := history(t, alice, ch.ID)
	if len(msgs) != 2 {
		t.Fatalf("history has %d messages, want 2 (replies stay in the thread)", len(msgs))
	}
	if msgs[0].ID != root.ID || msgs[0].Thread == nil {
		t.Fatalf("the first message has no thread summary: %+v", msgs[0])
	}
	if s := msgs[0].Thread; s.ReplyCount != 3 || !s.Following || !s.Unread || len(s.Participants) != 2 {
		t.Errorf("alice's summary: replies %d following %v unread %v participants %v; want 3, true, true (bob replied last), 2",
			s.ReplyCount, s.Following, s.Unread, s.Participants)
	}
	if msgs[1].Thread != nil {
		t.Error("a message without replies has a thread summary")
	}

	// The thread's replies, oldest first.
	alice.send(protocol.OpRequestThread, protocol.ThreadRequest{ChannelID: ch.ID, ThreadID: root.ID})
	got := alice.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventThreadMessages
	})
	if got == nil {
		t.Fatal("no THREAD_MESSAGES")
	}
	var tm protocol.ThreadMessagesPayload
	json.Unmarshal(got.Data, &tm)
	if len(tm.Messages) != 3 || tm.Messages[0].ID != r1.ID || tm.Messages[1].ID != r2.ID {
		t.Fatalf("thread replies out of order or missing: %d", len(tm.Messages))
	}
	if tm.Messages[0].Author == nil || tm.Messages[0].Author.ID != users["bob"].ID {
		t.Error("a reply came without its author")
	}

	// Reading it clears unread, for alice only.
	alice.send(protocol.OpThreadRead, protocol.ThreadReadPayload{ChannelID: ch.ID, ThreadID: root.ID})
	if s := threadUpdate(t, alice, root.ID); s.Unread || !s.Following {
		t.Fatalf("after reading: %+v", s)
	}
	if s := history(t, alice, ch.ID)[0].Thread; s.Unread {
		t.Error("history still says unread after reading")
	}

	// Posting into a thread from another channel is refused.
	other := models.NewTextChannel(ch.ServerID, "elsewhere")
	if err := srv.db.CreateChannel(other); err != nil {
		t.Fatal(err)
	}
	bob.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: other.ID, Content: "sneaky", ThreadID: &root.ID})
	refused := bob.readUntil(5*time.Second, func(m *protocol.Message) bool {
		var e protocol.ErrorPayload
		return m.Op == protocol.OpDispatch && m.Type == "" && json.Unmarshal(m.Data, &e) == nil && e.Message != ""
	})
	if refused == nil {
		t.Error("a reply into another channel's thread wasn't refused")
	}

	// Deleting the first message keeps its thread, headed by an empty
	// "deleted" placeholder.
	alice.send(protocol.OpDeleteMessage, protocol.DeleteMessagePayload{MessageID: root.ID, ChannelID: ch.ID})
	alice.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventMessageDelete
	})
	msgs = history(t, alice, ch.ID)
	if len(msgs) != 2 || msgs[0].ID != root.ID || !msgs[0].Deleted || msgs[0].Content != "" || msgs[0].Thread == nil {
		t.Fatalf("after deleting the first message: %+v", msgs[0].Message)
	}
}
