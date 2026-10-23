package client

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func TestReactionsNeedANewEnoughServer(t *testing.T) {
	for v, want := range map[string]bool{
		"0.1.7": true, "v0.1.7": true, "0.1.10": true, "0.2.0": true, "1.0.0": true, "dev": true,
		"0.1.6": false, "0.1.4": false, "": false, "unknown": false,
	} {
		if got := takesReactions(&ServerConnection{ServerVersion: v}); got != want {
			t.Errorf("server %q: takes reactions %v, want %v", v, got, want)
		}
	}
}

func TestApplyReaction(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	var list []models.Reaction
	list = applyReaction(list, "👍", a, true)
	list = applyReaction(list, "🍇", b, true)
	list = applyReaction(list, "👍", b, true)
	list = applyReaction(list, "👍", b, true) // twice is once
	if len(list) != 2 || list[0].Emoji != "👍" || list[0].Count != 2 || list[1].Count != 1 {
		t.Fatalf("after adding: %+v", list)
	}
	list = applyReaction(list, "👍", a, false)
	list = applyReaction(list, "🍇", a, false) // not theirs: nothing
	if list[0].Count != 1 || list[0].UserIDs[0] != b || list[1].Count != 1 {
		t.Fatalf("after taking one off: %+v", list)
	}
	list = applyReaction(list, "👍", b, false)
	if len(list) != 1 || list[0].Emoji != "🍇" {
		t.Fatalf("an emoji nobody has should go: %+v", list)
	}
}

// reactionTestApp is the emoji test app on a v0.1.7 server, signed in,
// with the emoji messages in the channel; sent holds what it sends.
func reactionTestApp(t *testing.T) (a *App, sent chan *protocol.Message, me *models.User) {
	t.Helper()
	a = newEmojiTestApp(t)
	me = &models.User{ID: uuid.New(), Username: "me"}
	sent = make(chan *protocol.Message, 16)
	a.activeConn = &ServerConnection{ServerVersion: "0.1.7", User: me,
		Connection: &Connection{connected: true, send: sent}}
	withEmojiMessages(t, a)
	return a, sent, me
}

func nextReaction(t *testing.T, sent chan *protocol.Message) protocol.ReactionRequest {
	t.Helper()
	select {
	case m := <-sent:
		if m.Op != protocol.OpReaction {
			t.Fatalf("sent op %d, want a reaction", m.Op)
		}
		var r protocol.ReactionRequest
		if err := json.Unmarshal(m.Data, &r); err != nil {
			t.Fatal(err)
		}
		return r
	default:
		t.Fatal("nothing was sent")
	}
	return protocol.ReactionRequest{}
}

// Alt+M, +, a search, Enter: the reaction goes to the server; the chips
// appear when the server says so, and picking it again takes it off.
func TestReactingToAMessage(t *testing.T) {
	a, sent, me := reactionTestApp(t)
	a.Update(altM()) // selects the newest message
	if !a.messageNavMode {
		t.Fatal("Alt+M didn't select a message")
	}
	target := a.visibleMessages()[a.messageNavIndex]
	pickerKeys(a, "+")
	if a.emojiPicker == nil || a.emojiPicker.ReactTo != target {
		t.Fatalf("+ didn't open the picker on the message: %+v", a.emojiPicker)
	}
	if !strings.Contains(ansi.Strip(a.View()), "React to grape") {
		t.Fatal("the picker doesn't say what it's reacting to")
	}
	for _, k := range []string{"g", "r", "a", "p", "e", "s", "enter"} {
		typeSlowly(a, k) // (fast keys look like a paste, which shows a cached frame)
	}
	if a.emojiPicker != nil {
		t.Fatal("picking didn't close the picker")
	}
	req := nextReaction(t, sent)
	if req.MessageID != target.ID || req.Emoji != "🍇" || req.Remove {
		t.Fatalf("sent %+v", req)
	}
	if a.input.Value() != "" {
		t.Fatalf("reacting put the emoji in the message box: %q", a.input.Value())
	}

	// The server tells everyone; the chip appears, highlighted as yours.
	event := func(typ protocol.EventType, user uuid.UUID, emoji string) {
		data, _ := json.Marshal(protocol.ReactionPayload{UserID: user, ChannelID: target.ChannelID, MessageID: target.ID, Emoji: emoji})
		a.handleReactionEvent(a.activeConn, data, typ == protocol.EventMessageReactionAdd)
	}
	event(protocol.EventMessageReactionAdd, me.ID, "🍇")
	event(protocol.EventMessageReactionAdd, uuid.New(), "🍇")
	if !strings.Contains(ansi.Strip(a.View()), "🍇 2") {
		t.Fatalf("no 🍇 2 chip: reactions %+v, nav %v, edit %v, channel msgs %d", target.Reactions, a.messageNavMode, a.inMessageEditMode, len(a.activeConn.Messages[target.ChannelID]))
	}

	// Picking it again takes it off; the picker lists who reacted.
	pickerKeys(a, "+")
	if !strings.Contains(ansi.Strip(a.View()), "🍇 you, someone") {
		t.Fatalf("the picker doesn't say who reacted:\n%s", ansi.Strip(a.View()))
	}
	for _, k := range []string{"g", "r", "a", "p", "e", "s", "enter"} {
		typeSlowly(a, k) // (fast keys look like a paste, which shows a cached frame)
	}
	if req := nextReaction(t, sent); !req.Remove || req.Emoji != "🍇" {
		t.Fatalf("picking your own reaction again should take it off: %+v", req)
	}
	event(protocol.EventMessageReactionRemove, me.ID, "🍇")
	event(protocol.EventMessageReactionRemove, target.Reactions[0].UserIDs[0], "🍇")
	if strings.Contains(ansi.Strip(a.View()), "🍇 ") && len(target.Reactions) != 0 {
		t.Fatalf("the chip should be gone: %+v", target.Reactions)
	}
}

// An older server doesn't take reactions: + says so instead of opening a
// picker that would do nothing.
func TestReactingOnAnOlderServer(t *testing.T) {
	a, sent, _ := reactionTestApp(t)
	a.activeConn.ServerVersion = "0.1.6"
	a.Update(altM())
	pickerKeys(a, "+")
	if a.emojiPicker != nil {
		t.Fatal("the picker opened for a server that can't take reactions")
	}
	if !strings.Contains(a.statusMessage, "v0.1.7") {
		t.Fatalf("status: %q", a.statusMessage)
	}
	if len(sent) != 0 {
		t.Fatal("something was sent")
	}
}

func altM() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true} }
