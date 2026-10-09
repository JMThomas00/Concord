package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// threadFixture is a channel with a thread (alice's question, two replies
// from bob) between two ordinary messages.
func threadFixture(t *testing.T) (*App, uuid.UUID, *MessageDisplay, []*MessageDisplay) {
	t.Helper()
	alice, bob := uuid.New(), uuid.New()
	now := time.Now()
	msg := func(author uuid.UUID, name, text string, ago time.Duration, thread *uuid.UUID) *MessageDisplay {
		return &MessageDisplay{
			Message:    &models.Message{ID: uuid.New(), AuthorID: author, Content: text, CreatedAt: now.Add(-ago), ThreadID: thread},
			AuthorName: name, ShowHeader: true,
		}
	}
	before := msg(bob, "bob", "Morning all", 30*time.Minute, nil)
	root := msg(alice, "alice", "Has anyone tested the docker build yet?", 20*time.Minute, nil)
	after := msg(bob, "bob", "Unrelated chatter", 1*time.Minute, nil)
	a, ch := newTestAppForRendering(100, []*MessageDisplay{before, root, after})
	for _, m := range []*MessageDisplay{before, root, after} {
		m.ChannelID = ch
	}
	replies := []*MessageDisplay{
		msg(bob, "bob", "Works on my VM", 10*time.Minute, &root.ID),
		msg(bob, "bob", "Also fine on Linux", 5*time.Minute, &root.ID),
	}
	for _, r := range replies {
		r.ChannelID = ch
	}
	a.activeConn.User = &models.User{ID: alice}
	a.view = ViewMain
	a.input = textarea.New()
	a.focus = FocusChat
	last := replies[1]
	a.applyThreadSummary(a.activeConn, protocol.ThreadSummary{
		ThreadID: root.ID, ChannelID: ch, ReplyCount: 2,
		LastReply: &protocol.MessageDisplay{Message: last.Message, Author: &models.User{ID: bob, Username: "bob"}},
		Following: true, Unread: true,
	})
	return a, ch, root, replies
}


// ansiContent is everything updateChatContent rendered, not just what the
// 20-row viewport shows.
func ansiContent(a *App) string {
	a.chatViewport.Height = 400
	return a.chatViewport.View()
}

// Minimised: the first message in a box whose top edge has + and the reply
// count (marked new while unread), then the latest reply; replies aren't
// navigable, and the messages around it aren't boxed.
func TestAMinimisedThreadShowsItsLatestReply(t *testing.T) {
	a, _, root, replies := threadFixture(t)
	out := ansi.Strip(ansiContent(updated(a)))
	for _, want := range []string{"╭─ + ", "🧵 2 replies · new", "Has anyone tested", "↳ bob: Also fine on Linux · 5m ago", "╰──"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
	if strings.Contains(out, replies[0].Content) {
		t.Error("a minimised thread shows a reply other than the latest")
	}
	if n := len(a.visibleMessages()); n != 3 {
		t.Errorf("%d messages to navigate, want 3 (the replies stay in the minimised thread)", n)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Morning all") || strings.Contains(l, "Unrelated chatter") {
			if strings.Contains(l, "│") {
				t.Errorf("a message outside the thread is boxed: %q", l)
			}
		}
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("a line is %d wide, wider than the chat: %q", w, l)
		}
	}
	_ = root
}

func updated(a *App) *App { a.updateChatContent(); return a }

// Expanded (Enter on the first message in message navigation): every reply
// inside the box, − in its top edge, the replies navigable, and the thread
// read.
func TestEnterExpandsAThread(t *testing.T) {
	a, _, root, replies := threadFixture(t)
	ts := a.activeConn.thread(root.ID)
	ts.replies, ts.loaded = replies, true
	a.messageNavMode, a.messageNavIndex = true, 1 // alice's question
	a.handleKeyPress(keyOf("enter"))
	if !a.expandedThreads[root.ID] {
		t.Fatal("Enter on a thread's first message didn't expand it")
	}
	out := ansi.Strip(ansiContent(updated(a)))
	for _, want := range []string{"╭─ − ", "Works on my VM", "Also fine on Linux"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
	if strings.Contains(out, "· new") {
		t.Error("an expanded thread still says new")
	}
	if n := len(a.visibleMessages()); n != 5 {
		t.Errorf("%d messages to navigate, want 5 (the replies join in)", n)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Works on my VM") && !strings.HasPrefix(strings.TrimLeft(l, " "), "│") {
			t.Errorf("a reply is outside the box: %q", l)
		}
	}
	a.handleKeyPress(keyOf("enter"))
	if a.expandedThreads[root.ID] {
		t.Error("Enter again didn't minimise it")
	}
}

// t on a reply replies in its thread (the first message is the target, so a
// reply to a reply stays in the same thread); Esc leaves the thread.
func TestTRepliesInTheThread(t *testing.T) {
	a, _, root, replies := threadFixture(t)
	ts := a.activeConn.thread(root.ID)
	ts.replies, ts.loaded = replies, true
	a.expandedThreads = map[uuid.UUID]bool{root.ID: true}
	a.messageNavMode, a.messageNavIndex = true, 3 // the second reply
	a.handleKeyPress(keyOf("t"))
	if a.threadTarget == nil || a.threadTarget.ID != root.ID {
		t.Fatalf("t on a reply targets %v, want the thread's first message", a.threadTarget)
	}
	if !strings.Contains(a.threadQuote, "Has anyone tested") {
		t.Errorf("the message box says %q", a.threadQuote)
	}
	a.messageNavMode = false
	a.handleKeyPress(keyOf("esc"))
	if a.threadTarget != nil {
		t.Error("Esc didn't leave the thread")
	}
}

// A reply that arrives goes into its thread, not the channel; a summary
// sent to the whole channel makes the thread ours (we're a follower) and
// unread (someone else wrote the latest reply).
func TestRepliesArriveInTheirThread(t *testing.T) {
	a, ch, root, replies := threadFixture(t)
	ts := a.activeConn.thread(root.ID)
	ts.replies, ts.loaded = replies, true
	ts.summary.Following, ts.summary.Unread = false, false

	reply := &MessageDisplay{Message: &models.Message{ID: uuid.New(), ChannelID: ch, ThreadID: &root.ID, Content: "One more", CreatedAt: time.Now()}, AuthorName: "bob"}
	a.addThreadReply(a.activeConn, reply)
	if len(a.activeConn.GetMessages(ch)) != 3 {
		t.Error("a thread reply landed in the channel")
	}
	if len(ts.replies) != 3 {
		t.Errorf("the thread has %d replies, want 3", len(ts.replies))
	}

	bob := replies[0].AuthorID
	a.applyThreadSummary(a.activeConn, protocol.ThreadSummary{
		ThreadID: root.ID, ChannelID: ch, ReplyCount: 3,
		LastReply: &protocol.MessageDisplay{Message: &models.Message{AuthorID: bob, Content: "One more"}},
		Followers: []uuid.UUID{bob, a.activeConn.User.ID},
	})
	if s := a.activeConn.thread(root.ID).summary; !s.Following || !s.Unread {
		t.Errorf("after a reply from bob, following %v unread %v; want both", s.Following, s.Unread)
	}
}

// The message after an expanded thread keeps its author line, even when the
// thread's last reply is by the same person a minute earlier.
func TestTheMessageAfterAThreadKeepsItsAuthor(t *testing.T) {
	a, _, root, replies := threadFixture(t)
	ts := a.activeConn.thread(root.ID)
	ts.replies, ts.loaded = replies, true
	a.toggleThread(root.ID)
	out := ansi.Strip(ansiContent(updated(a)))
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.Contains(l, "Unrelated chatter") && (i == 0 || !strings.Contains(lines[i-1], "bob")) {
			t.Fatalf("the message after the thread lost its author line:\n%s", out)
		}
	}
}

// The chat box's bottom edge counts the channel's active and unread threads
// (Jordan's sketch), and Alt+T lists them; Enter opens one expanded, with
// the message box replying in it.
func TestBottomEdgeAndAltTFindAThread(t *testing.T) {
	a, _, root, replies := threadFixture(t)
	ts := a.activeConn.thread(root.ID)
	ts.replies, ts.loaded = replies, true
	full, short := a.threadIndicator(time.Now())
	for _, want := range []string{"1 active thread", "1 unread", "Alt+T"} {
		if !strings.Contains(ansi.Strip(full), want) {
			t.Errorf("%q missing from the indicator %q", want, ansi.Strip(full))
		}
	}
	if !strings.Contains(ansi.Strip(short), "Alt+T") {
		t.Errorf("short indicator %q", ansi.Strip(short))
	}
	box := strings.Repeat("│\n", 3) + "╰" + strings.Repeat("─", 78) + "╯"
	edge := ansi.Strip(strings.Split(a.embedChatBottom(box, "bob is typing...", a.styles.UsernameOther), "\n")[3])
	if !strings.Contains(edge, "bob is typing") || !strings.HasSuffix(edge, "Alt+T ─╯") || ansi.StringWidth(edge) != 80 {
		t.Errorf("bottom edge %q", edge)
	}

	a.handleKeyPress(keyOf("alt+t"))
	if a.threadList == nil || len(a.threadList.items) != 1 {
		t.Fatalf("Alt+T didn't list the thread: %+v", a.threadList)
	}
	if out := ansi.Strip(a.renderThreadList()); !strings.Contains(out, "Threads in #") || !strings.Contains(out, "2 replies") {
		t.Errorf("the list:\n%s", out)
	}
	a.handleKeyPress(keyOf("enter"))
	if a.threadList != nil || !a.expandedThreads[root.ID] || a.threadTarget == nil || a.threadTarget.ID != root.ID {
		t.Fatalf("Enter in the list: list %v expanded %v target %v", a.threadList, a.expandedThreads[root.ID], a.threadTarget)
	}
}

// The optional in-channel line: off by default; "all" shows the latest
// reply per thread, in time order among the messages.
func TestThreadRepliesInTheChannel(t *testing.T) {
	a, ch, root, _ := threadFixture(t)
	reply := &MessageDisplay{Message: &models.Message{ID: uuid.New(), ChannelID: ch, ThreadID: &root.ID, Content: "Works", CreatedAt: time.Now().Add(-3 * time.Minute)}, AuthorName: "bob"}
	a.noteThreadReply(a.activeConn, reply, false)
	if strings.Contains(ansi.Strip(ansiContent(updated(a))), "replied in a thread") {
		t.Fatal("the in-channel line shows while the setting is off")
	}
	a.notifConfig.ThreadLines = ThreadLinesFollowed
	if strings.Contains(ansi.Strip(ansiContent(updated(a))), "replied in a thread") {
		t.Fatal("followed-only shows a thread we don't follow")
	}
	a.notifConfig.ThreadLines = ThreadLinesAll
	out := ansi.Strip(ansiContent(updated(a)))
	i, j := strings.Index(out, "bob replied in a thread"), strings.Index(out, "Unrelated chatter")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("the line should come before the message after it in time:\n%s", out)
	}
	if len(a.threadNoticeLines) != 1 {
		t.Errorf("%d clickable notice lines, want 1", len(a.threadNoticeLines))
	}
}
