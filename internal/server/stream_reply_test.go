package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// A plugin streams a reply: it learns its message's ID from the echo of
// its own post, grows it with stream edits that don't mark it edited, and
// finishes it. Members watching see every step; nobody else may stream
// into it.
func TestPluginStreamsAReply(t *testing.T) {
	r := newPaneTestRig(t)
	r.plugin.User = &models.User{ID: r.plugin.UserID, Username: "hello", IsServiceAccount: true}
	_, watcher := r.member(t, "watcher")
	_, other := r.member(t, "other")

	asPlugin := func(op protocol.OpCode, payload interface{}, handle func(*Client, *protocol.Message)) {
		msg, err := protocol.NewMessage(op, payload)
		if err != nil {
			t.Fatal(err)
		}
		handle(r.plugin, msg)
	}
	asPlugin(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: r.channel.ID, Content: "Thinking", Nonce: "p1", Stream: protocol.StreamWriting}, r.srv.handlers.HandleSendMessage)

	var echo protocol.MessageCreatePayload
	r.pluginReceives(t, protocol.EventMessageCreate, &echo)
	if echo.Nonce != "p1" || echo.Message == nil || echo.Stream != protocol.StreamWriting {
		t.Fatalf("plugin's echo = nonce %q stream %q", echo.Nonce, echo.Stream)
	}
	id := echo.Message.ID

	var created protocol.MessageCreatePayload
	m := watcher.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMessageCreate })
	_ = json.Unmarshal(m.Data, &created)
	if created.Message.ID != id || created.Stream != protocol.StreamWriting {
		t.Fatalf("watcher saw the new message as %+v stream %q", created.Message, created.Stream)
	}

	for _, step := range []struct{ content, stream string }{
		{"Thinking about it", protocol.StreamWriting},
		{"Thinking about it: **yes**.", protocol.StreamDone},
	} {
		asPlugin(protocol.OpEditMessage, protocol.EditMessagePayload{MessageID: id, ChannelID: r.channel.ID, Content: step.content, Stream: step.stream}, r.srv.handlers.HandleEditMessage)
		var upd protocol.MessageUpdatePayload
		m := watcher.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMessageUpdate })
		_ = json.Unmarshal(m.Data, &upd)
		if upd.Content != step.content || upd.Stream != step.stream || upd.EditedAt != nil {
			t.Fatalf("watcher saw update %+v, want %q (%s) and not edited", upd, step.content, step.stream)
		}
	}
	saved, err := r.srv.db.GetMessage(id)
	if err != nil || saved.Content != "Thinking about it: **yes**." || saved.EditedAt != nil {
		t.Fatalf("saved message %+v, %v", saved, err)
	}

	// Someone else can't stream into it (or edit it at all).
	other.send(protocol.OpEditMessage, protocol.EditMessagePayload{MessageID: id, ChannelID: r.channel.ID, Content: "hijacked", Stream: protocol.StreamWriting})
	other.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Op == protocol.OpDispatch && m.Type == "" }) // the error
	if saved, _ := r.srv.db.GetMessage(id); saved.Content != "Thinking about it: **yes**." {
		t.Fatalf("another member changed the reply to %q", saved.Content)
	}
}
