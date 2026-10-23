package server

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// reactionServer is a server with an owner and two plain members (whose
// @everyone role is given perms), a channel and a message in it.
func reactionServer(t *testing.T, perms models.Permission) (srv *Server, wsURL string, users []*models.User, tokens []string, ch *models.Channel, msg *models.Message) {
	t.Helper()
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL = startPlainTestServer(t)
	for _, name := range []string{"react-owner", "react-a", "react-b"} {
		u, tok := createTestUserAndToken(t, srv, name+"-"+uuid.NewString()[:6])
		users, tokens = append(users, u), append(tokens, tok)
	}
	s := models.NewServer("Reactions", users[0].ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	everyone := models.NewEveryoneRole(s.ID)
	everyone.Permissions = perms
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID))
		srv.db.AddMemberRole(u.ID, s.ID, everyone.ID)
	}
	ch = models.NewTextChannel(s.ID, "general")
	if err := srv.db.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	msg = models.NewMessage(ch.ID, users[0].ID, "react to me")
	if err := srv.db.CreateMessage(msg); err != nil {
		t.Fatal(err)
	}
	return
}

// react sends one reaction request and returns what came back: the
// reaction event, or an error (Type "").
func react(t *testing.T, wsURL, token string, req protocol.ReactionRequest) *protocol.Message {
	t.Helper()
	c := newTestWSClient(t, wsURL)
	c.identify(token)
	c.send(protocol.OpReaction, req)
	return c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && (m.Type == "" ||
			m.Type == protocol.EventMessageReactionAdd || m.Type == protocol.EventMessageReactionRemove)
	})
}

func reactionsOn(t *testing.T, srv *Server, msg *models.Message) []models.Reaction {
	t.Helper()
	m, err := srv.db.GetMessage(msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	return m.Reactions
}

// Members react and un-react; the channel hears each, and history carries
// the reactions: each emoji in the order first used, with who.
func TestReactionsAddRemoveAndLoad(t *testing.T) {
	srv, wsURL, users, tokens, ch, msg := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions)
	if got := react(t, wsURL, tokens[1], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "👍"}); got.Type != protocol.EventMessageReactionAdd {
		t.Fatalf("a member's reaction: %s %s", got.Type, got.Data)
	}
	react(t, wsURL, tokens[2], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "🍇"})
	react(t, wsURL, tokens[2], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "👍"})
	if added, _ := srv.db.AddReaction(msg.ID, users[1].ID, "👍"); added {
		t.Fatal("the same reaction twice counted twice")
	}

	got := reactionsOn(t, srv, msg)
	if len(got) != 2 || got[0].Emoji != "👍" || got[0].Count != 2 || got[0].UserIDs[0] != users[1].ID || got[1].Emoji != "🍇" {
		t.Fatalf("reactions on the message: %+v", got)
	}
	history, err := srv.db.GetChannelMessages(ch.ID, 50, nil, users[0].ID)
	if err != nil || len(history) != 1 || len(history[0].Reactions) != 2 {
		t.Fatalf("history doesn't carry the reactions: %+v %v", history, err)
	}

	if got := react(t, wsURL, tokens[1], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "👍", Remove: true}); got.Type != protocol.EventMessageReactionRemove {
		t.Fatalf("taking a reaction off: %s %s", got.Type, got.Data)
	}
	if got := reactionsOn(t, srv, msg); got[0].Count != 1 || got[0].UserIDs[0] != users[2].ID {
		t.Fatalf("after taking one off: %+v", got)
	}
}

// Reacting needs Add Reactions; taking your own reaction off doesn't.
// Only emoji, and at most maxReactionEmoji different ones per message.
func TestReactionsAreChecked(t *testing.T) {
	perms := models.NewEveryoneRole(uuid.Nil).Permissions &^ models.PermissionAddReactions
	srv, wsURL, users, tokens, ch, msg := reactionServer(t, perms)
	if got := react(t, wsURL, tokens[1], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "👍"}); got.Type != "" {
		t.Fatalf("reacted without Add Reactions: %s", got.Type)
	}
	srv.db.AddReaction(msg.ID, users[1].ID, "🍇") // from before the permission went
	if got := react(t, wsURL, tokens[1], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "🍇", Remove: true}); got.Type != protocol.EventMessageReactionRemove {
		t.Fatalf("couldn't take their own reaction off: %s %s", got.Type, got.Data)
	}

	if got := react(t, wsURL, tokens[0], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "hello"}); got.Type != "" {
		t.Fatalf("text accepted as a reaction: %s", got.Type)
	}
	if got := react(t, wsURL, tokens[0], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "👍🏽"}); got.Type != protocol.EventMessageReactionAdd {
		t.Fatalf("a skin-toned emoji was refused: %s %s", got.Type, got.Data)
	}

	full := []string{"😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "🙃", "😉", "😊", "😇", "🥰", "😍", "🤩", "😘", "😗", "😚"} // 19, with 👍🏽: 20
	for _, e := range full {
		srv.db.AddReaction(msg.ID, users[2].ID, e)
	}
	if got := react(t, wsURL, tokens[0], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "🍇"}); got.Type != "" {
		t.Fatalf("a 21st different emoji went on: %s", got.Type)
	}
	if got := react(t, wsURL, tokens[0], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: msg.ID, Emoji: "😀"}); got.Type != protocol.EventMessageReactionAdd {
		t.Fatalf("adding to an emoji already there was refused: %s %s", got.Type, got.Data)
	}
}

// A whisper's reactions are for its two people: anyone else is told it
// isn't there.
func TestReactionsOnAWhisper(t *testing.T) {
	srv, wsURL, users, tokens, ch, _ := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions)
	w := models.NewMessage(ch.ID, users[0].ID, "psst")
	w.IsWhisper, w.RecipientID = true, &users[1].ID
	if err := srv.db.CreateMessage(w); err != nil {
		t.Fatal(err)
	}
	if got := react(t, wsURL, tokens[2], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: w.ID, Emoji: "👀"}); got.Type != "" {
		t.Fatalf("a third person reacted to a whisper: %s", got.Type)
	}
	if got := react(t, wsURL, tokens[1], protocol.ReactionRequest{ChannelID: ch.ID, MessageID: w.ID, Emoji: "👀"}); got.Type != protocol.EventMessageReactionAdd {
		t.Fatalf("the whisper's recipient couldn't react: %s %s", got.Type, got.Data)
	}
}
