package server

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/protocol"
)

// TestHandleTypingStopClearsIndicatorImmediately is a regression test for a
// real gap: before OpTypingStop/HandleTypingStop existed, the only way to
// clear a typing indicator was the message-send path (which already calls
// StopTyping directly) or the 5s timeout -- there was no way to signal
// "stopped without sending" at all. This proves the new handler actually
// removes the indicator, without waiting on TypingManager's cleanup
// goroutine/timeout.
func TestHandleTypingStopClearsIndicatorImmediately(t *testing.T) {
	hub := NewHub()
	tm := NewTypingManager(hub)
	h := &Handlers{typingManager: tm}

	userID := uuid.New()
	channelID := uuid.New()

	tm.StartTyping(userID, channelID, uuid.Nil, "testuser", false)

	tm.mu.RLock()
	_, stillTyping := tm.indicators[channelID][userID]
	tm.mu.RUnlock()
	if !stillTyping {
		t.Fatal("expected StartTyping to record the indicator")
	}

	payload := protocol.TypingStartPayload{ChannelID: channelID}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	msg := &protocol.Message{Op: protocol.OpTypingStop, Data: data}

	c := &Client{UserID: userID, send: make(chan *protocol.Message, 1)}
	h.HandleTypingStop(c, msg)

	tm.mu.RLock()
	_, stillThere := tm.indicators[channelID][userID]
	tm.mu.RUnlock()
	if stillThere {
		t.Error("expected the typing indicator to be cleared immediately by HandleTypingStop")
	}
}

// TestHandleTypingStopWithNoActiveIndicatorDoesNothing confirms it's a safe
// no-op (no panic) when there was nothing to clear -- e.g. a stray stop
// signal after the 5s timeout already fired naturally.
func TestHandleTypingStopWithNoActiveIndicatorDoesNothing(t *testing.T) {
	hub := NewHub()
	tm := NewTypingManager(hub)
	h := &Handlers{typingManager: tm}

	payload := protocol.TypingStartPayload{ChannelID: uuid.New()}
	data, _ := json.Marshal(payload)
	msg := &protocol.Message{Op: protocol.OpTypingStop, Data: data}

	c := &Client{UserID: uuid.New(), send: make(chan *protocol.Message, 1)}
	h.HandleTypingStop(c, msg) // must not panic
}
