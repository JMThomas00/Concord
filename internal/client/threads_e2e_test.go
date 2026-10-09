package client

import (
	"encoding/json"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// TestThreadReplyEndToEnd drives a real App against a real server: Alice
// selects Bob's message (Alt+M, arrows), presses t, types and sends. Bob
// must see the reply arrive with its thread_id and a THREAD_UPDATE, and the
// thread's replies (asked for as a client opening it would) must hold it.
func TestThreadReplyEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live client+server integration test in short mode")
	}
	httpAddr := startFileTransferTestServer(t)
	alice := newTestFileClient(t, httpAddr, "alice-threads", t.TempDir())
	serverID := alice.readyPayload.Servers[0].ID
	channelID := createTestChannel(t, alice, serverID)
	bob := newTestFileClient(t, httpAddr, "bob-threads", t.TempDir())

	// Bob asks something.
	if err := bob.conn.SendMessage(channelID, "Has anyone tested the docker build yet?", nil); err != nil {
		t.Fatal(err)
	}
	created := alice.waitForDispatch(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMessageCreate })
	var rootPayload protocol.MessageCreatePayload
	json.Unmarshal(created.Data, &rootPayload)
	root := rootPayload.Message

	// Alice's client, showing that message.
	a := newLayoutTestApp(t, 120, 40)
	a.styles = a.theme.BuildStyles()
	a.view = ViewMain
	a.input = textarea.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatal(err)
	}
	sc.Connection = alice.conn
	sc.User = &models.User{ID: alice.userID}
	sc.SetState(StateReady)
	sc.Messages[channelID] = []*MessageDisplay{{Message: root, AuthorName: "bob-threads", ShowHeader: true}}
	a.activeConn = sc
	a.currentClientServer = &ClientServerInfo{ID: serverID}
	a.currentChannel = &models.Channel{ID: channelID}

	// Alt+M onto the message, t, type, Enter.
	a.messageNavMode, a.messageNavIndex, a.focus = true, 0, FocusChat
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if a.threadTarget == nil || a.threadTarget.ID != root.ID {
		t.Fatalf("t didn't target the thread: %+v", a.threadTarget)
	}
	a.Update(cmd()) // the exit from message navigation, as the program delivers it
	for _, ch := range "Works on my VM" {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		invokeCmd(cmd)
	}
	if got := a.input.Value(); got != "Works on my VM" {
		t.Fatalf("the message box holds %q (the t of message navigation leaked in?)", got)
	}
	_, cmd = a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	invokeCmd(cmd)

	reply := bob.waitForDispatch(5*time.Second, func(m *protocol.Message) bool {
		if m.Type != protocol.EventMessageCreate {
			return false
		}
		var p protocol.MessageCreatePayload
		return json.Unmarshal(m.Data, &p) == nil && p.Message.Content == "Works on my VM"
	})
	var replyPayload protocol.MessageCreatePayload
	json.Unmarshal(reply.Data, &replyPayload)
	if replyPayload.Message.ThreadID == nil || *replyPayload.Message.ThreadID != root.ID {
		t.Fatalf("the reply arrived with thread_id %v, want %s", replyPayload.Message.ThreadID, root.ID)
	}
	upd := bob.waitForDispatch(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventThreadUpdate })
	var sum protocol.ThreadSummary
	json.Unmarshal(upd.Data, &sum)
	if sum.ThreadID != root.ID || sum.ReplyCount != 1 {
		t.Fatalf("THREAD_UPDATE: %+v", sum)
	}
	if a.threadTarget == nil {
		t.Error("sending left the thread; it should stay until Esc")
	}

	// Bob opens the thread.
	msg, _ := protocol.NewMessage(protocol.OpRequestThread, protocol.ThreadRequest{ChannelID: channelID, ThreadID: root.ID})
	if err := bob.conn.Send(msg); err != nil {
		t.Fatal(err)
	}
	got := bob.waitForDispatch(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventThreadMessages })
	var tm protocol.ThreadMessagesPayload
	json.Unmarshal(got.Data, &tm)
	if len(tm.Messages) != 1 || tm.Messages[0].Content != "Works on my VM" {
		t.Fatalf("the thread holds %d replies", len(tm.Messages))
	}
	if tm.Summary == nil || !tm.Summary.Following || !tm.Summary.Unread {
		t.Errorf("Bob started the thread and Alice replied: following and unread for him, got %+v", tm.Summary)
	}
	_ = uuid.Nil
}
