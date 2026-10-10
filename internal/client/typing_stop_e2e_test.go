package client

import (
	"encoding/json"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// invokeCmd runs cmd and, if the resulting message is itself a
// tea.BatchMsg, recursively invokes every command inside it -- real
// keystrokes route through app.go's Update(), which frequently returns
// tea.Batch(...) rather than firing its side effects (like the typing
// signal sends here) synchronously. Mirrors the same "call the returned
// cmd" pattern reconnect_test.go already established, generalized to
// nested batches.
func invokeCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			invokeCmd(c)
		}
	}
}

// TestTypingStopFiresOnBackspaceToEmptyEndToEnd is a real, live regression
// test for a bug found live 2026-09-08: backspacing a draft to empty never
// sent OpTypingStop at all, leaving peers seeing "X is typing..." for the
// full 5s timeout regardless of the fix landed earlier that session
// (HandleTypingStop/SendTypingStop themselves were correct and already
// covered by server-side tests -- the actual bug was in app.go's Update()
// control flow).
//
// Root cause: the check lived inside the `case tea.KeyMsg:` block, which
// runs BEFORE the *separate*, later `switch a.view { case ViewMain: ...
// a.input.Update(msg) }` block that's what actually applies a keystroke to
// the textarea. wasComposing (computed pre-keystroke) and "is the input
// empty now" were therefore both reading the *same*, not-yet-updated
// a.input.Value() snapshot within one Update() call -- structurally unable
// to both be true at once, since wasComposing requires the value to be
// non-empty and the "now empty" check requires the same un-changed value
// to be empty. No amount of server-side testing could have caught this;
// it only shows up by driving the real bubbletea Update() loop with real
// key events, which is exactly what this test does -- two real clients
// against a real server, one driven through a.Update() the same way the
// shipped binary is.
func TestTypingStopFiresOnBackspaceToEmptyEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live client+server integration test in short mode")
	}

	httpAddr := startFileTransferTestServer(t)

	sender := newTestFileClient(t, httpAddr, "typist", t.TempDir())
	if len(sender.readyPayload.Servers) == 0 {
		t.Fatal("expected the sender to be auto-joined to the default server")
	}
	serverID := sender.readyPayload.Servers[0].ID
	channelID := createTestChannel(t, sender, serverID)

	// Second client, auto-joined to the channel just created (same
	// ordering requirement as the file-transfer e2e test: a client only
	// receives broadcasts for channels it was joined to at identify time).
	observer := newTestFileClient(t, httpAddr, "observer", t.TempDir())

	// Build a real App around the sender's already-connected, already-
	// identified Connection -- driving a.Update() with real key events,
	// the same code path the shipped TUI runs, not a hand-rolled
	// simulation of it.
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = textarea.New()
	a.input.Focus()

	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	sc.Connection = sender.conn
	sc.SetState(StateReady)
	a.activeConn = sc
	a.currentClientServer = &ClientServerInfo{ID: serverID}
	a.currentChannel = &models.Channel{ID: channelID}

	// Type "hi" -- two real KeyRunes events, exactly as a terminal delivers
	// typed characters.
	for _, ch := range "hi" {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		invokeCmd(cmd)
	}

	startMsg := observer.waitForDispatch(5*time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.EventTypingStart
	})
	var startPayload protocol.TypingStartEventPayload
	if err := json.Unmarshal(startMsg.Data, &startPayload); err != nil {
		t.Fatalf("failed to decode typing-start payload: %v", err)
	}
	if startPayload.UserID != sender.userID {
		t.Fatalf("expected the typing-start event to be from the sender, got user %s", startPayload.UserID)
	}

	// Now backspace back to empty -- two real KeyBackspace events -- and
	// confirm the observer sees EventTypingStop promptly, not after the
	// full 5s server-side timeout.
	for range "hi" {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		invokeCmd(cmd)
	}
	if got := a.input.Value(); got != "" {
		t.Fatalf("test setup bug: expected the input to actually be empty after backspacing, got %q", got)
	}

	stopMsg := observer.waitForDispatch(2*time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.EventTypingStop
	})
	var stopPayload protocol.TypingStopEventPayload
	if err := json.Unmarshal(stopMsg.Data, &stopPayload); err != nil {
		t.Fatalf("failed to decode typing-stop payload: %v", err)
	}
	if stopPayload.UserID != sender.userID {
		t.Fatalf("expected the typing-stop event to be from the sender, got user %s", stopPayload.UserID)
	}
}
