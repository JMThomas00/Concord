package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

// PostMessage posts a chat message and returns its ID once Concord has
// saved it (the ID is what EditMessage needs). It blocks until then, so
// call it off the Handler goroutine, like RequestMembers.
func (c *Conn) PostMessage(ctx context.Context, channelID uuid.UUID, content string, replyTo *uuid.UUID) (uuid.UUID, error) {
	return c.postAndWait(ctx, wire.SendMessagePayload{ChannelID: channelID, Content: content, ReplyToID: replyTo})
}

// PostThreadMessage is PostMessage into a thread (see SendThreadMessage).
func (c *Conn) PostThreadMessage(ctx context.Context, channelID, threadID uuid.UUID, content string) (uuid.UUID, error) {
	return c.postAndWait(ctx, wire.SendMessagePayload{ChannelID: channelID, Content: content, ThreadID: &threadID})
}

// EditMessage replaces the text of one of the plugin's own messages.
func (c *Conn) EditMessage(channelID, messageID uuid.UUID, content string) error {
	return c.Send(wire.OpEditMessage, wire.EditMessagePayload{MessageID: messageID, ChannelID: channelID, Content: content})
}

func (c *Conn) postAndWait(ctx context.Context, p wire.SendMessagePayload) (uuid.UUID, error) {
	p.Nonce = fmt.Sprintf("p%d", c.reqID.Add(1))
	ch := make(chan uuid.UUID, 1)
	c.pendingMu.Lock()
	if c.posts == nil {
		c.posts = map[string]chan uuid.UUID{}
	}
	c.posts[p.Nonce] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.posts, p.Nonce)
		c.pendingMu.Unlock()
	}()
	if err := c.Send(wire.OpSendMessage, p); err != nil {
		return uuid.Nil, err
	}
	select {
	case id := <-ch:
		return id, nil
	case <-ctx.Done():
		return uuid.Nil, ctx.Err()
	}
}

// answerPost hands the echo of the plugin's own message to the PostMessage
// call waiting for it, reporting whether one was; it runs on the read loop.
func (c *Conn) answerPost(p wire.MessageCreatePayload) bool {
	if p.Nonce == "" || p.ChatMessage == nil {
		return false
	}
	c.pendingMu.Lock()
	ch := c.posts[p.Nonce]
	c.pendingMu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- p.ID:
	default:
	}
	return true
}

// Stream writes a reply into a channel as it's produced -- an AI model's
// tokens, a long job's progress -- the way chat apps show a reply being
// typed out. Viewers see it grow, with a cursor, and markdown renders
// properly throughout (an unfinished code block shows as a code block).
//
//	s := c.Stream(ctx, channelID, &question.ID)
//	for token := range tokens {
//		s.Write(token)
//	}
//	s.Close()
//
// Writes are batched into an edit every Interval, so the text can arrive
// in any size of piece. A reply longer than one message continues in a
// new one, split between paragraphs where possible (a code block cut in
// two is closed and reopened). Like PostMessage it blocks on the network,
// so use it off the Handler goroutine. It isn't safe for use by several
// goroutines at once.
type Stream struct {
	// Interval is the least time between updates (default 400ms).
	Interval time.Duration
	// ThreadID, when set before the first Write, posts the reply into that
	// thread: set it to the message you're answering's ThreadID, so a bot
	// mentioned inside a thread answers there.
	ThreadID *uuid.UUID

	c         *Conn
	ctx       context.Context
	channelID uuid.UUID
	replyTo   *uuid.UUID

	mu     sync.Mutex
	text   string    // the current message's text so far
	sent   string    // what Concord has of it
	msgID  uuid.UUID // the current message, once posted
	last   time.Time // last update
	closed bool
	err    error
}

// Stream starts a reply in channelID (replying to replyTo, if set). Nothing
// is posted until the first Write.
func (c *Conn) Stream(ctx context.Context, channelID uuid.UUID, replyTo *uuid.UUID) *Stream {
	return &Stream{c: c, ctx: ctx, channelID: channelID, replyTo: replyTo, Interval: 400 * time.Millisecond}
}

// streamLimit leaves room under wire.MaxMessageLength for closing a code
// block when a long reply is split.
const streamLimit = wire.MaxMessageLength - 64

// Write appends text to the reply, updating it if Interval has passed.
func (s *Stream) Write(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("stream is closed")
	}
	if s.err != nil {
		return s.err
	}
	s.text += text
	for len(s.text) > streamLimit {
		if err := s.splitLocked(); err != nil {
			return err
		}
	}
	if time.Since(s.last) >= s.Interval {
		return s.flushLocked(wire.StreamWriting)
	}
	return nil
}

// Close finishes the reply: the last text arrives and the cursor goes.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.err
	}
	s.closed = true
	if s.err != nil {
		return s.err
	}
	return s.flushLocked(wire.StreamDone)
}

// Text is the reply so far, in the message being written now (earlier
// messages of a long reply are finished).
func (s *Stream) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text
}

// flushLocked brings Concord's copy of the current message up to date.
// state is StreamWriting mid-reply or StreamDone to finish the message.
func (s *Stream) flushLocked(state string) error {
	if strings.TrimSpace(s.text) == "" {
		return nil // nothing to show yet (or ever: Close on an empty reply)
	}
	s.last = time.Now()
	if s.msgID == uuid.Nil {
		p := wire.SendMessagePayload{ChannelID: s.channelID, Content: s.text, ReplyToID: s.replyTo, ThreadID: s.ThreadID}
		if state == wire.StreamWriting {
			p.Stream = wire.StreamWriting
		} // a reply finished before its first update is just a message
		id, err := s.c.postAndWait(s.ctx, p)
		if err != nil {
			s.err = err
			return err
		}
		s.msgID, s.sent = id, s.text
		return nil
	}
	if s.text == s.sent && state == wire.StreamWriting {
		return nil
	}
	err := s.c.Send(wire.OpEditMessage, wire.EditMessagePayload{MessageID: s.msgID, ChannelID: s.channelID, Content: s.text, Stream: state})
	if err != nil {
		s.err = err
		return err
	}
	s.sent = s.text
	return nil
}

// splitLocked finishes the current message at a good break before
// streamLimit and starts the next one with the rest.
func (s *Stream) splitLocked() error {
	head, rest := splitReply(s.text, streamLimit)
	s.text = head
	if err := s.flushLocked(wire.StreamDone); err != nil {
		return err
	}
	s.text, s.sent, s.msgID = rest, "", uuid.Nil
	s.replyTo = nil // the continuation follows on; only the first message replies
	return nil
}

// splitReply cuts text into a message of at most limit bytes and the rest,
// preferring a paragraph break, then a line break, then a space. A code
// block open at the cut is closed in the first part and reopened (with its
// language) in the second.
func splitReply(text string, limit int) (head, rest string) {
	cut := strings.LastIndex(text[:limit], "\n\n")
	if cut < limit/2 {
		cut = strings.LastIndex(text[:limit], "\n")
	}
	if cut < limit/2 {
		cut = strings.LastIndex(text[:limit], " ")
	}
	if cut < limit/2 {
		cut = limit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
	}
	head, rest = strings.TrimRight(text[:cut], " \n"), strings.TrimLeft(text[cut:], " \n")
	if fence, opener := openFence(head); fence != "" {
		head += "\n" + fence
		rest = opener + "\n" + rest
	}
	return head, rest
}

// openFence reports the fenced code block text leaves open, if any: its
// closing marker (``` or ~~~, as long as the opener) and its opening line.
func openFence(text string) (fence, opener string) {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) > 3 {
			continue
		}
		marker := fenceRun(trimmed)
		switch {
		case marker == "":
		case fence == "":
			fence, opener = marker, trimmed
		case marker[0] == fence[0] && len(marker) >= len(fence) && strings.TrimSpace(trimmed[len(marker):]) == "":
			fence, opener = "", ""
		}
	}
	return fence, opener
}

func fenceRun(line string) string {
	for _, ch := range []byte{'`', '~'} {
		n := 0
		for n < len(line) && line[n] == ch {
			n++
		}
		if n >= 3 {
			return line[:n]
		}
	}
	return ""
}
