package server

import (
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func init() {
	// Initialize loggers for tests (silent mode)
	InitLogger(log.FatalLevel)
}

func TestNewClient(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.hub != hub {
		t.Error("client hub not set correctly")
	}

	if client.handlers != handlers {
		t.Error("client handlers not set correctly")
	}

	if client.send == nil {
		t.Error("send channel not initialized")
	}

	if cap(client.send) != sendBufferSize {
		t.Errorf("expected send buffer size %d, got %d", sendBufferSize, cap(client.send))
	}

	if client.authenticated {
		t.Error("client should not be authenticated initially")
	}
}

func TestClientAuthentication(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)
	userID := uuid.New()

	// Initially not authenticated
	if client.IsAuthenticated() {
		t.Error("client should not be authenticated initially")
	}

	// Set user info (simulating successful authentication)
	client.UserID = userID
	client.User = &models.User{
		ID:            userID,
		Username:      "testuser",
		Discriminator: "0001",
	}
	client.SessionID = "test_session"

	client.authMu.Lock()
	client.authenticated = true
	client.authMu.Unlock()

	// Now should be authenticated
	if !client.IsAuthenticated() {
		t.Error("client should be authenticated after setting auth flag")
	}
}

func TestClientSendMessage(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	// Send a message to the client's send channel
	testMsg := &protocol.Message{
		Op:   protocol.OpDispatch,
		Type: "TEST_EVENT",
	}

	// Send message
	select {
	case client.send <- testMsg:
		// Message sent successfully
	case <-time.After(100 * time.Millisecond):
		t.Fatal("failed to send message to client channel")
	}

	// Receive message
	select {
	case receivedMsg := <-client.send:
		if receivedMsg.Op != protocol.OpDispatch {
			t.Errorf("expected OpDispatch, got %d", receivedMsg.Op)
		}
		if receivedMsg.Type != "TEST_EVENT" {
			t.Errorf("expected TEST_EVENT, got %s", receivedMsg.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("failed to receive message from client channel")
	}
}

func TestClientSendBufferCapacity(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	// Fill the send buffer
	for i := 0; i < sendBufferSize; i++ {
		msg := &protocol.Message{
			Op: protocol.OpDispatch,
		}

		select {
		case client.send <- msg:
			// Message sent successfully
		case <-time.After(10 * time.Millisecond):
			t.Fatalf("failed to send message %d (buffer should not be full yet)", i)
		}
	}

	// Buffer should now be full
	// Try to send one more message with timeout
	extraMsg := &protocol.Message{
		Op: protocol.OpDispatch,
	}

	select {
	case client.send <- extraMsg:
		t.Error("buffer should be full, send should block")
	case <-time.After(50 * time.Millisecond):
		// Expected - buffer is full
	}

	// Drain one message
	<-client.send

	// Now we should be able to send again
	select {
	case client.send <- extraMsg:
		// Successfully sent after draining
	case <-time.After(10 * time.Millisecond):
		t.Error("should be able to send after draining buffer")
	}
}

func TestClientSequenceTracking(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	// Initial sequence should be 0
	if client.lastSeq != 0 {
		t.Errorf("expected initial sequence 0, got %d", client.lastSeq)
	}

	// Update sequence
	newSeq := int64(42)
	client.seqMu.Lock()
	client.lastSeq = newSeq
	client.seqMu.Unlock()

	// Verify sequence was updated
	client.seqMu.Lock()
	if client.lastSeq != newSeq {
		t.Errorf("expected sequence %d, got %d", newSeq, client.lastSeq)
	}
	client.seqMu.Unlock()
}

func TestClientServerMemberships(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	// Initially no server memberships
	if len(client.ServerIDs) != 0 {
		t.Error("client should have no server memberships initially")
	}

	// Add server memberships
	serverIDs := []uuid.UUID{
		uuid.New(),
		uuid.New(),
		uuid.New(),
	}
	client.ServerIDs = serverIDs

	if len(client.ServerIDs) != 3 {
		t.Errorf("expected 3 server memberships, got %d", len(client.ServerIDs))
	}

	// Verify server IDs match
	for i, expected := range serverIDs {
		if client.ServerIDs[i] != expected {
			t.Errorf("server ID %d mismatch: expected %v, got %v", i, expected, client.ServerIDs[i])
		}
	}
}

func TestClientConstants(t *testing.T) {
	// Verify constant values are reasonable
	tests := []struct {
		name      string
		value     interface{}
		condition bool
		message   string
	}{
		{"writeWait positive", writeWait, writeWait > 0, "writeWait should be positive"},
		{"pongWait positive", pongWait, pongWait > 0, "pongWait should be positive"},
		{"pingPeriod positive", pingPeriod, pingPeriod > 0, "pingPeriod should be positive"},
		{"pingPeriod < pongWait", pingPeriod, pingPeriod < pongWait, "pingPeriod should be less than pongWait"},
		{"maxMessageSize positive", maxMessageSize, maxMessageSize > 0, "maxMessageSize should be positive"},
		{"sendBufferSize positive", sendBufferSize, sendBufferSize > 0, "sendBufferSize should be positive"},
		{"heartbeatInterval positive", heartbeatInterval, heartbeatInterval > 0, "heartbeatInterval should be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.condition {
				t.Error(tt.message)
			}
		})
	}

	// Verify timing relationships
	expectedPingPeriod := (pongWait * 9) / 10
	if pingPeriod != expectedPingPeriod {
		t.Errorf("pingPeriod should be 90%% of pongWait: expected %v, got %v", expectedPingPeriod, pingPeriod)
	}
}

func TestClientIsAuthenticatedConcurrency(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	// Test concurrent reads/writes to authenticated flag
	done := make(chan bool, 2)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			client.authMu.Lock()
			client.authenticated = true
			client.authMu.Unlock()

			client.authMu.Lock()
			client.authenticated = false
			client.authMu.Unlock()
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = client.IsAuthenticated()
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// If we got here without race condition, test passed
}

func TestClientSequenceConcurrency(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	// Test concurrent sequence updates
	done := make(chan bool, 3)

	// Writer goroutines
	for w := 0; w < 3; w++ {
		go func() {
			for i := 0; i < 100; i++ {
				client.seqMu.Lock()
				client.lastSeq++
				client.seqMu.Unlock()
			}
			done <- true
		}()
	}

	// Wait for all writers
	for w := 0; w < 3; w++ {
		<-done
	}

	// Final sequence should be 300
	client.seqMu.Lock()
	finalSeq := client.lastSeq
	client.seqMu.Unlock()

	if finalSeq != 300 {
		t.Errorf("expected final sequence 300, got %d", finalSeq)
	}
}

func TestClientMessageChannelNonBlocking(t *testing.T) {
	hub := NewHub()
	handlers := &Handlers{
		db:  nil,
		hub: hub,
	}

	client := NewClient(nil, hub, handlers)

	// Send messages until buffer is full
	messagesSent := 0
	for i := 0; i < sendBufferSize+10; i++ {
		msg := &protocol.Message{Op: protocol.OpDispatch}
		select {
		case client.send <- msg:
			messagesSent++
		default:
			// Buffer full, stop sending
			break
		}
	}

	if messagesSent != sendBufferSize {
		t.Errorf("expected to send %d messages, sent %d", sendBufferSize, messagesSent)
	}
}
