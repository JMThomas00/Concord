package plugintest

import (
	"time"
	"unicode/utf8"

	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Posted is a chat message the plugin posted, as it stands now.
type Posted struct {
	ID        uuid.UUID
	ChannelID uuid.UUID
	ReplyToID *uuid.UUID
	Content   string
	Stream    string // wire.StreamWriting while being streamed, wire.StreamDone after; "" for a plain message
	Edits     int
}

// post records a message and echoes it back as Concord does (with its
// nonce), so PostMessage gets the new ID.
func (s *Server) post(ws *websocket.Conn, p wire.SendMessagePayload) {
	s.checkLength(p.Content)
	msg := &Posted{ID: uuid.New(), ChannelID: p.ChannelID, ReplyToID: p.ReplyToID, Content: p.Content, Stream: p.Stream}
	s.mu.Lock()
	s.posted[msg.ID] = msg
	s.order = append(s.order, msg.ID)
	s.mu.Unlock()
	s.chat <- p
	s.writeTo(ws, wire.OpDispatch, wire.EventMessageCreate, wire.MessageCreatePayload{
		ChatMessage: &wire.ChatMessage{ID: msg.ID, ChannelID: p.ChannelID, AuthorID: s.Self.ID, Content: p.Content, ReplyToID: p.ReplyToID},
		Author:      &s.Self,
		Nonce:       p.Nonce,
		Stream:      p.Stream,
	})
}

// edit applies an OpEditMessage, flagging edits Concord would refuse.
func (s *Server) edit(e wire.EditMessagePayload) {
	s.checkLength(e.Content)
	s.mu.Lock()
	msg := s.posted[e.MessageID]
	if msg != nil {
		msg.Content = e.Content
		msg.Edits++
		if e.Stream != "" {
			msg.Stream = e.Stream
		}
	}
	s.mu.Unlock()
	if msg == nil {
		s.t.Errorf("plugintest: the plugin edited message %s, which isn't one of its own", e.MessageID)
		return
	}
	select { // never block the connection: Posted() has the state regardless
	case s.edits <- e:
	default:
	}
}

func (s *Server) checkLength(content string) {
	if len(content) > wire.MaxMessageLength {
		s.t.Errorf("plugintest: a %d-byte message; Concord refuses more than %d", len(content), wire.MaxMessageLength)
	}
	if !utf8.ValidString(content) {
		s.t.Errorf("plugintest: a message that isn't valid UTF-8 (split inside a character?)")
	}
}

// NextEdit waits for the plugin's next message edit.
func (s *Server) NextEdit() wire.EditMessagePayload {
	s.t.Helper()
	select {
	case e := <-s.edits:
		return e
	case <-time.After(Timeout):
		s.t.Fatalf("plugintest: no message edit within %v", Timeout)
		return wire.EditMessagePayload{}
	}
}

// Posted returns every message the plugin has posted, oldest first, with
// its current text.
func (s *Server) Posted() []Posted {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Posted, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, *s.posted[id])
	}
	return out
}

// WaitStreamDone waits until the plugin has finished the streamed message
// id (wire.StreamDone) and returns it.
func (s *Server) WaitStreamDone(id uuid.UUID) Posted {
	s.t.Helper()
	deadline := time.Now().Add(Timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		msg := s.posted[id]
		var cp Posted
		if msg != nil {
			cp = *msg
		}
		s.mu.Unlock()
		if msg != nil && cp.Stream != wire.StreamWriting {
			return cp
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.t.Fatalf("plugintest: message %s still streaming after %v", id, Timeout)
	return Posted{}
}
