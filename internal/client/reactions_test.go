package client

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	// (Selected, so the chip names who rather than counting.)
	if !strings.Contains(ansi.Strip(a.View()), "🍇 you, someone") {
		t.Fatalf("no 🍇 chip naming who: reactions %+v", target.Reactions)
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

// screenRows draws the app's frame on the model terminal and returns each
// row as text.
func screenRows(a *App) []string {
	var out []string
	for _, row := range screenOf(a.View(), a.width, ansi.StringWidth) {
		out = append(out, strings.Join(row, ""))
	}
	return out
}

// rowWith finds the row holding text, and where text starts and ends on it.
func rowWith(t *testing.T, rows []string, text string) (y, start, end int) {
	t.Helper()
	for y, r := range rows {
		if i := strings.Index(r, text); i >= 0 {
			return y, ansi.StringWidth(r[:i]), ansi.StringWidth(r[:i]) + ansi.StringWidth(text)
		}
	}
	t.Fatalf("%q isn't on screen", text)
	return
}

// The chips were written straight after the message's text, which has no
// newline of its own: glued onto its last line, they wrapped to the far
// left of the chat (or, in a thread's box, were cut off). They sit on the
// line under the text, on its side: ending where your own message's text
// ends, starting where someone else's starts.
func TestReactionsSitUnderTheirMessage(t *testing.T) {
	a, _, me := reactionTestApp(t)
	msgs := a.activeConn.Messages[a.currentChannel.ID]
	mine, theirs := msgs[2], msgs[1]
	mine.IsOwn, mine.AuthorID = true, me.ID
	mine.Reactions = []models.Reaction{{Emoji: "👋", Count: 1, UserIDs: []uuid.UUID{me.ID}}, {Emoji: "😁", Count: 2, UserIDs: []uuid.UUID{uuid.New(), uuid.New()}}}
	theirs.Content = "a message from someone else"
	theirs.Reactions = []models.Reaction{{Emoji: "🍇", Count: 3, UserIDs: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}}}
	a.updateChatContent()
	rows := screenRows(a)

	ty, _, tEnd := rowWith(t, rows, "plain text for comparison")
	cy, _, cEnd := rowWith(t, rows, "😁 2 ╯")
	if cy != ty+1 || cEnd != tEnd {
		t.Fatalf("your message's chips: row %d ending at %d; its text: row %d ending at %d", cy, cEnd, ty, tEnd)
	}
	ty, tStart, _ := rowWith(t, rows, "a message from someone else")
	cy, cStart, _ := rowWith(t, rows, "╰ 🍇 3")
	if cy != ty+1 || cStart != tStart {
		t.Fatalf("their message's chips: row %d starting at %d; its text: row %d starting at %d", cy, cStart, ty, tStart)
	}
}

// A thread reply's reactions show inside the thread's box.
func TestReactionsOnAThreadReply(t *testing.T) {
	a, _, _ := reactionTestApp(t)
	root := a.activeConn.Messages[a.currentChannel.ID][2]
	reply := &MessageDisplay{
		Message:    &models.Message{ID: uuid.New(), ChannelID: root.ChannelID, AuthorID: uuid.New(), Content: "a reply in the thread", ThreadID: &root.ID, CreatedAt: root.CreatedAt},
		AuthorName: "grape", ShowHeader: true,
	}
	reply.Reactions = []models.Reaction{{Emoji: "🐻", Count: 1, UserIDs: []uuid.UUID{uuid.New()}}}
	ts := a.activeConn.thread(root.ID)
	ts.replies, ts.loaded = []*MessageDisplay{reply}, true
	ts.summary.ReplyCount = 1
	a.expandedThreads = map[uuid.UUID]bool{root.ID: true}
	a.updateChatContent()
	rows := screenRows(a)
	ry, _, _ := rowWith(t, rows, "a reply in the thread")
	cy, _, _ := rowWith(t, rows, "╰ 🐻 1")
	if cy != ry+1 {
		t.Fatalf("the reply's chips are on row %d, its text on %d", cy, ry)
	}
}

// withReactions gives the newest message (someone else's) two reactions,
// one of them yours, and returns it.
func withReactions(t *testing.T, a *App, me *models.User) *MessageDisplay {
	t.Helper()
	msgs := a.activeConn.Messages[a.currentChannel.ID]
	m := msgs[len(msgs)-1]
	alice, bob := uuid.New(), uuid.New()
	a.activeConn.Members = []*MemberDisplay{
		{User: &models.User{ID: alice, Username: "alice"}}, {User: &models.User{ID: bob, Username: "bob"}},
	}
	m.Reactions = []models.Reaction{
		{Emoji: "🍇", Count: 2, UserIDs: []uuid.UUID{alice, bob}},
		{Emoji: "👍", Count: 2, UserIDs: []uuid.UUID{me.ID, alice}},
	}
	a.updateChatContent()
	return m
}

// A selected message (Alt+M, or clicked) shows who reacted in place of the
// counts, on the same row (an extra row of names was too much), and its
// highlight covers them.
func TestASelectedMessageSaysWhoReacted(t *testing.T) {
	a, _, me := reactionTestApp(t)
	withReactions(t, a, me)
	if strings.Contains(ansi.Strip(a.View()), "alice, bob") {
		t.Fatal("names show without the message selected")
	}
	before := screenRows(a)
	ty, _, _ := rowWith(t, before, "plain text for comparison")
	a.Update(altM())
	a.updateChatContent()
	rows := screenRows(a)
	cy, _, _ := rowWith(t, rows, "╰ 🍇 alice, bob  👍 you, alice")
	if cy != ty+1 {
		t.Fatalf("the names are on row %d, not the one under the text (%d)", cy, ty+1)
	}
	if strings.Contains(rows[cy+1], "you") || strings.Contains(rows[cy+1], "🍇") {
		t.Fatalf("an extra row: %q", rows[cy+1])
	}
	sel := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color(a.theme.Colors.Selection)))
	for _, l := range strings.Split(a.View(), "\n") {
		if strings.Contains(ansi.Strip(l), "alice, bob") && sel != "" && !strings.Contains(l, sel) {
			t.Fatal("the reactions line isn't highlighted with its selected message")
		}
	}
}

// Clicking a chip reacts with its emoji, or takes yours off.
func TestClickingAReactionChip(t *testing.T) {
	a, sent, me := reactionTestApp(t)
	withReactions(t, a, me)
	a.View()
	click := func(text string) {
		y, x, _ := rowWith(t, screenRows(a), text)
		a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	click("🍇 2")
	if r := nextReaction(t, sent); r.Emoji != "🍇" || r.Remove {
		t.Fatalf("clicking 🍇 (not yours): %+v", r)
	}
	click("👍 2")
	if r := nextReaction(t, sent); r.Emoji != "👍" || !r.Remove {
		t.Fatalf("clicking 👍 (yours): %+v", r)
	}
}

// Hovering over a chip shows who reacted with it; moving off hides it.
func TestHoveringAReactionChip(t *testing.T) {
	a, _, me := reactionTestApp(t)
	withReactions(t, a, me)
	a.View()
	filter := InputFilter()
	move := func(x, y int) {
		if m := filter(a, tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}); m != nil {
			a.Update(m)
		}
	}
	y, x, _ := rowWith(t, screenRows(a), "🍇 2")
	move(x+1, y)
	if !strings.Contains(ansi.Strip(a.View()), "│ 🍇 alice, bob │") { // in its box
		t.Fatalf("hovering 🍇 doesn't say who:\n%s", ansi.Strip(a.View()))
	}
	move(1, 1)
	if strings.Contains(ansi.Strip(a.View()), "alice, bob") {
		t.Fatal("the names stayed after the mouse moved off")
	}
}
