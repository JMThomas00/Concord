package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func mentionItem(ch uuid.UUID, seq int64, at time.Time, kind, text string, unread bool, thread *uuid.UUID) protocol.MentionItem {
	return protocol.MentionItem{
		Message: &protocol.MessageDisplay{
			Message: &models.Message{ID: uuid.New(), ChannelID: ch, Content: text, CreatedAt: at, Seq: seq, ThreadID: thread},
			Author:  &models.User{ID: uuid.New(), Username: "juniper"},
		},
		Kind: kind, Unread: unread,
	}
}

// Two servers' mentions merge newest first; filters, read marks from
// CHANNEL_READ, and paging behave.
func TestMentionsInboxList(t *testing.T) {
	a, general := newTestAppForRendering(80, nil)
	a.width, a.height = 120, 40
	s1, s2 := uuid.New(), uuid.New()
	a.mentions = &mentionsInbox{servers: map[uuid.UUID]*inboxServer{s1: {name: "Vineyard"}, s2: {name: "Official"}}, order: []uuid.UUID{s1, s2}}
	now := time.Now()
	a.handleMentions(s1, a.activeConn, protocol.MentionsPayload{ServerID: s1, HasMore: true, Items: []protocol.MentionItem{
		mentionItem(general, 9, now.Add(-1*time.Minute), protocol.MentionDirect, "@gh0st you in?", true, nil),
		mentionItem(general, 5, now.Add(-30*time.Minute), protocol.MentionReply, "agreed", true, nil),
	}})
	a.handleMentions(s2, a.activeConn, protocol.MentionsPayload{ServerID: s2, Items: []protocol.MentionItem{
		mentionItem(uuid.New(), 40, now.Add(-10*time.Minute), protocol.MentionEveryone, "@everyone lunch", false, nil),
	}})
	list := a.inboxList()
	if len(list) != 3 || list[0].msg.Content != "@gh0st you in?" || list[1].serverID != s2 || list[2].kind != protocol.MentionReply {
		t.Fatalf("merged %+v", list)
	}
	if a.mentions.servers[s1].before != 5 {
		t.Errorf("next page from %d", a.mentions.servers[s1].before)
	}
	for f, want := range map[int]int{1: 1, 2: 1, 3: 1} {
		a.mentions.filter = f
		if n := len(a.inboxList()); n != want {
			t.Errorf("filter %s: %d", mentionFilters[f], n)
		}
	}
	a.mentions.filter = 0

	out := ansi.Strip(a.renderMentions())
	for _, want := range []string{"@ Mentions", "Vineyard › #", "@you", "↩ reply", "@all", "you in?"} {
		if !strings.Contains(out, want) {
			t.Errorf("the inbox doesn't show %q:\n%s", want, out)
		}
	}

	// Reading the channel up to seq 9 clears both of its marks.
	a.inboxChannelRead(s1, general, 9)
	for _, it := range a.mentions.servers[s1].items {
		if it.unread {
			t.Errorf("still unread: %q", it.msg.Content)
		}
	}
}

// Enter on a mention already on screen selects it, as Alt+M would; one
// elsewhere asks for history around it.
func TestJumpToMention(t *testing.T) {
	msgs := []*MessageDisplay{readMsg(1, false, "old"), readMsg(2, false, "@gh0st look"), readMsg(3, false, "newest")}
	a, general := newTestAppForRendering(60, msgs)
	sid := uuid.New()
	a.activeConn.ServerID = sid
	it := inboxItem{serverID: sid, channelID: general, msg: msgs[1], kind: protocol.MentionDirect}
	a.jumpToMention(it)
	if !a.messageNavMode || a.messageNavIndex != 1 || a.pendingJump != nil {
		t.Errorf("nav %v at %d, pending %+v", a.messageNavMode, a.messageNavIndex, a.pendingJump)
	}

	// Elsewhere: the history request is around the message (or its thread).
	root := uuid.New()
	a.pendingJump = &mentionJump{serverID: sid, channelID: general, messageID: uuid.New(), threadID: &root}
	if got := a.jumpAround(sid, general); got == nil || *got != root {
		t.Errorf("around %v, want the thread's first message", got)
	}
	if a.jumpAround(sid, uuid.New()) != nil {
		t.Error("another channel's history was asked around it")
	}
	// History for something else doesn't land it.
	if a.jumpHistoryLoaded(sid, protocol.MessageHistoryPayload{ChannelID: general}) != nil || a.pendingJump == nil {
		t.Error("landed on history that wasn't around it")
	}
}

// A mention inside a thread opens the thread and selects the reply, once
// the replies are in.
func TestJumpToThreadMention(t *testing.T) {
	root := readMsg(1, false, "thread start")
	a, general := newTestAppForRendering(60, []*MessageDisplay{root, readMsg(5, false, "after")})
	sid := uuid.New()
	a.activeConn.ServerID = sid
	reply := readMsg(3, false, "@gh0st in here")
	reply.ThreadID = &root.ID
	a.jumpToMention(inboxItem{serverID: sid, channelID: general, msg: reply})
	if a.pendingJump == nil || !a.pendingJump.waitingThread || !a.expandedThreads[root.ID] {
		t.Fatalf("waiting for the thread: %+v", a.pendingJump)
	}
	ts := a.activeConn.thread(root.ID)
	ts.replies, ts.loaded = []*MessageDisplay{readMsg(2, true, "mine"), reply}, true
	a.jumpThreadLoaded(sid, root.ID)
	if sel := a.selectedMessage(); !a.messageNavMode || sel == nil || sel.ID != reply.ID {
		t.Errorf("selected %+v", sel)
	}
}
