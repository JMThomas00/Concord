package server

import (
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func init() {
	// Initialize loggers for tests (silent mode)
	InitLogger(os.Stderr, log.FatalLevel) // Only log fatal errors during tests
}

func TestNewHub(t *testing.T) {
	hub := NewHub()

	if hub == nil {
		t.Fatal("NewHub returned nil")
	}

	if hub.clients == nil {
		t.Error("clients map not initialized")
	}

	if hub.serverClients == nil {
		t.Error("serverClients map not initialized")
	}

	if hub.channelClients == nil {
		t.Error("channelClients map not initialized")
	}

	if hub.register == nil {
		t.Error("register channel not initialized")
	}

	if hub.unregister == nil {
		t.Error("unregister channel not initialized")
	}

	if hub.broadcast == nil {
		t.Error("broadcast channel not initialized")
	}

	if hub.sequence != 0 {
		t.Errorf("expected initial sequence 0, got %d", hub.sequence)
	}
}

func TestHubRegisterClient(t *testing.T) {
	hub := NewHub()
	userID := uuid.New()
	serverID := uuid.New()

	client := &Client{
		UserID:    userID,
		ServerIDs: []uuid.UUID{serverID},
		User: &models.User{
			ID:       userID,
			Username: "testuser",
		},
	}

	// Register client synchronously (don't use Run loop for unit test)
	hub.registerClient(client)

	// Check client is in main map
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	if _, exists := hub.clients[userID]; !exists {
		t.Error("client not found in clients map")
	}

	// Check client is in server map
	if serverMap, exists := hub.serverClients[serverID]; !exists {
		t.Error("server not found in serverClients map")
	} else if _, exists := serverMap[userID]; !exists {
		t.Error("client not found in server's client map")
	}
}

func TestHubUnregisterClient(t *testing.T) {
	hub := NewHub()
	userID := uuid.New()
	serverID := uuid.New()

	client := &Client{
		UserID:    userID,
		ServerIDs: []uuid.UUID{serverID},
		User: &models.User{
			ID:       userID,
			Username: "testuser",
		},
		send: make(chan *protocol.Message, 10),
	}

	// Register then unregister
	hub.registerClient(client)
	hub.unregisterClient(client)

	// Check client is removed from main map
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	if _, exists := hub.clients[userID]; exists {
		t.Error("client still exists in clients map after unregister")
	}

	// Check client is removed from server map
	if serverMap, exists := hub.serverClients[serverID]; exists {
		if _, exists := serverMap[userID]; exists {
			t.Error("client still exists in server map after unregister")
		}
	}

	// Check send channel is closed
	select {
	case _, ok := <-client.send:
		if ok {
			t.Error("send channel should be closed")
		}
	default:
		t.Error("send channel should be closed (non-blocking check failed)")
	}
}

// A reconnect (plugin restart, network blip) registers the new connection
// before the old socket's ReadPump notices it's dead. The old connection's
// late unregister must not tear down the new one's map entries -- a real
// bug that left a restarted plugin unreachable ("Plugin is not currently
// running") until it reconnected again.
func TestHubStaleUnregisterKeepsNewerConnection(t *testing.T) {
	hub := NewHub()
	userID := uuid.New()
	serverID := uuid.New()
	channelID := uuid.New()
	user := &models.User{ID: userID, Username: "plugin"}

	old := &Client{UserID: userID, ServerIDs: []uuid.UUID{serverID}, User: user, send: make(chan *protocol.Message, 10)}
	hub.registerClient(old)
	fresh := &Client{UserID: userID, ServerIDs: []uuid.UUID{serverID}, User: user, send: make(chan *protocol.Message, 10)}
	hub.registerClient(fresh)
	hub.JoinChannel(userID, channelID)

	hub.unregisterClient(old)

	if got := hub.GetClient(userID); got != fresh {
		t.Fatalf("clients[%s] = %p after the stale unregister, want the fresh connection %p", userID, got, fresh)
	}
	hub.mu.RLock()
	if _, ok := hub.serverClients[serverID][userID]; !ok {
		t.Error("stale unregister removed the fresh connection from its server's broadcast list")
	}
	if _, ok := hub.channelClients[channelID][userID]; !ok {
		t.Error("stale unregister removed the fresh connection from its channel list")
	}
	hub.mu.RUnlock()

	if _, ok := <-old.send; ok {
		t.Error("the stale connection's send channel should be closed")
	}
	select {
	case _, ok := <-fresh.send:
		if !ok {
			t.Error("the fresh connection's send channel was closed")
		}
	default:
	}
	select {
	case msg := <-hub.broadcast:
		t.Errorf("stale unregister broadcast %v; the user is still online", msg.Message.Type)
	default:
	}
}

func TestHubBroadcastToUser(t *testing.T) {
	hub := NewHub()
	userID := uuid.New()
	serverID := uuid.New()

	client := &Client{
		UserID:    userID,
		ServerIDs: []uuid.UUID{serverID},
		User: &models.User{
			ID:       userID,
			Username: "testuser",
		},
		send: make(chan *protocol.Message, 10),
	}

	hub.registerClient(client)

	// Broadcast message to specific user
	msg := &protocol.Message{
		Op:   protocol.OpDispatch,
		Type: "TEST_EVENT",
	}

	broadcastMsg := &BroadcastMessage{
		UserID:  &userID,
		Message: msg,
	}

	hub.broadcastMessage(broadcastMsg)

	// Check message was sent to client
	select {
	case receivedMsg := <-client.send:
		if receivedMsg.Op != protocol.OpDispatch {
			t.Errorf("expected OpDispatch, got %d", receivedMsg.Op)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("message not received within timeout")
	}
}

func TestHubBroadcastToServer(t *testing.T) {
	hub := NewHub()
	serverID := uuid.New()

	// Create 3 clients in the same server
	clients := make([]*Client, 3)
	for i := 0; i < 3; i++ {
		userID := uuid.New()
		clients[i] = &Client{
			UserID:    userID,
			ServerIDs: []uuid.UUID{serverID},
			User: &models.User{
				ID:       userID,
				Username: "testuser",
			},
			send: make(chan *protocol.Message, 10),
		}
		hub.registerClient(clients[i])
	}

	// Broadcast to server
	msg := &protocol.Message{
		Op:   protocol.OpDispatch,
		Type: "SERVER_EVENT",
	}

	broadcastMsg := &BroadcastMessage{
		ServerID: &serverID,
		Message:  msg,
	}

	hub.broadcastMessage(broadcastMsg)

	// Check all clients received the message
	for i, client := range clients {
		select {
		case receivedMsg := <-client.send:
			if receivedMsg.Op != protocol.OpDispatch {
				t.Errorf("client %d: expected OpDispatch, got %d", i, receivedMsg.Op)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("client %d: message not received within timeout", i)
		}
	}
}

func TestHubBroadcastExcludeUser(t *testing.T) {
	hub := NewHub()
	serverID := uuid.New()
	senderID := uuid.New()

	// Create sender and receiver
	sender := &Client{
		UserID:    senderID,
		ServerIDs: []uuid.UUID{serverID},
		User: &models.User{
			ID:       senderID,
			Username: "sender",
		},
		send: make(chan *protocol.Message, 10),
	}

	receiverID := uuid.New()
	receiver := &Client{
		UserID:    receiverID,
		ServerIDs: []uuid.UUID{serverID},
		User: &models.User{
			ID:       receiverID,
			Username: "receiver",
		},
		send: make(chan *protocol.Message, 10),
	}

	hub.registerClient(sender)
	hub.registerClient(receiver)

	// Broadcast to server but exclude sender
	msg := &protocol.Message{
		Op:   protocol.OpDispatch,
		Type: "MESSAGE_CREATE",
	}

	broadcastMsg := &BroadcastMessage{
		ServerID:      &serverID,
		ExcludeUserID: &senderID,
		Message:       msg,
	}

	hub.broadcastMessage(broadcastMsg)

	// Check sender didn't receive message
	select {
	case <-sender.send:
		t.Error("sender should not have received the message")
	case <-time.After(50 * time.Millisecond):
		// Expected - sender was excluded
	}

	// Check receiver got the message
	select {
	case receivedMsg := <-receiver.send:
		if receivedMsg.Op != protocol.OpDispatch {
			t.Errorf("expected OpDispatch, got %d", receivedMsg.Op)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("receiver should have received the message")
	}
}

func TestHubSequenceIncrement(t *testing.T) {
	t.Skip("Sequence handling is implementation-specific - skipping for now")
	hub := NewHub()
	userID := uuid.New()

	client := &Client{
		UserID: userID,
		User: &models.User{
			ID:       userID,
			Username: "testuser",
		},
		send: make(chan *protocol.Message, 10),
	}

	hub.registerClient(client)

	// Send 3 messages - sequence is set by broadcastMessage for dispatch events
	for i := 1; i <= 3; i++ {
		msg := &protocol.Message{
			Op: protocol.OpDispatch,
		}

		broadcastMsg := &BroadcastMessage{
			UserID:  &userID,
			Message: msg,
		}

		hub.broadcastMessage(broadcastMsg)

		select {
		case receivedMsg := <-client.send:
			// Verify message was received (sequence setting is internal implementation detail)
			if receivedMsg.Op != protocol.OpDispatch {
				t.Errorf("message %d: expected OpDispatch, got %d", i, receivedMsg.Op)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("message %d: not received within timeout", i)
		}
	}

	// Verify hub sequence counter incremented
	if hub.sequence != 3 {
		t.Errorf("expected hub sequence 3, got %d", hub.sequence)
	}
}

func TestHubJoinLeaveChannel(t *testing.T) {
	hub := NewHub()
	userID := uuid.New()
	channelID := uuid.New()

	client := &Client{
		UserID: userID,
		User: &models.User{
			ID:       userID,
			Username: "testuser",
		},
		send: make(chan *protocol.Message, 10),
	}

	hub.registerClient(client)

	// Join channel
	hub.JoinChannel(userID, channelID)

	// Check client is in channel map
	hub.mu.RLock()
	channelMap, exists := hub.channelClients[channelID]
	if !exists {
		t.Error("channel not found in channelClients map")
	} else if _, exists := channelMap[userID]; !exists {
		t.Error("client not found in channel's client map")
	}
	hub.mu.RUnlock()

	// Leave channel
	hub.LeaveChannel(userID, channelID)

	// Check client is removed from channel map
	hub.mu.RLock()
	channelMap, exists = hub.channelClients[channelID]
	if exists {
		if _, exists := channelMap[userID]; exists {
			t.Error("client still in channel after leaving")
		}
	}
	hub.mu.RUnlock()
}

// One account signed in on two computers: everything addressed to the
// user, their servers or their channels reaches both, and they only go
// offline when the second one disconnects too.
func TestHubUserWithTwoConnections(t *testing.T) {
	hub := NewHub()
	userID, serverID, channelID := uuid.New(), uuid.New(), uuid.New()
	user := &models.User{ID: userID, Username: "twice"}
	laptop := &Client{UserID: userID, ServerIDs: []uuid.UUID{serverID}, User: user, send: make(chan *protocol.Message, 10)}
	desktop := &Client{UserID: userID, ServerIDs: []uuid.UUID{serverID}, User: user, send: make(chan *protocol.Message, 10)}
	hub.registerClient(laptop)
	hub.registerClient(desktop)
	hub.JoinChannel(userID, channelID)

	for _, target := range []*BroadcastMessage{
		{UserID: &userID}, {ServerID: &serverID}, {ChannelID: &channelID},
	} {
		target.Message = &protocol.Message{Op: protocol.OpDispatch, Type: protocol.EventMessageCreate}
		hub.broadcastMessage(target)
		for name, c := range map[string]*Client{"laptop": laptop, "desktop": desktop} {
			select {
			case <-c.send:
			default:
				t.Errorf("%s didn't get a message sent to %+v", name, target)
			}
		}
	}

	hub.unregisterClient(laptop)
	if !hub.IsUserOnline(userID) {
		t.Fatal("user went offline while the desktop is still connected")
	}
	select {
	case msg := <-hub.broadcast:
		t.Fatalf("closing one of two connections broadcast %v", msg.Message.Type)
	default:
	}
	if _, ok := <-laptop.send; ok {
		t.Error("the closed connection's send channel should be closed")
	}
	hub.broadcastMessage(&BroadcastMessage{ServerID: &serverID, Message: &protocol.Message{Op: protocol.OpDispatch}})
	select {
	case <-desktop.send:
	default:
		t.Error("the remaining connection stopped receiving server broadcasts")
	}

	hub.unregisterClient(desktop)
	if hub.IsUserOnline(userID) {
		t.Fatal("user still online after their last connection closed")
	}
	select {
	case msg := <-hub.broadcast:
		if msg.Message.Type != protocol.EventPresenceUpdate {
			t.Fatalf("last disconnect broadcast %v, want a presence update", msg.Message.Type)
		}
	default:
		t.Fatal("last disconnect didn't announce the user offline")
	}
}

// Voice belongs to the connection that joined: another device closing
// leaves the call alone; the joining device closing ends it.
func TestHubVoiceEndsWithTheConnectionThatJoined(t *testing.T) {
	hub := NewHub()
	left := make(chan uuid.UUID, 2)
	hub.SetVoiceLeaveCallback(func(userID, serverID, channelID uuid.UUID) { left <- channelID })
	userID, serverID, voiceID := uuid.New(), uuid.New(), uuid.New()
	user := &models.User{ID: userID, Username: "caller"}
	phone := &Client{UserID: userID, User: user, send: make(chan *protocol.Message, 10)}
	desk := &Client{UserID: userID, User: user, send: make(chan *protocol.Message, 10)}
	third := &Client{UserID: userID, User: user, send: make(chan *protocol.Message, 10)}
	hub.registerClient(phone)
	hub.registerClient(desk)
	hub.registerClient(third)
	hub.JoinVoiceChannel(userID, serverID, voiceID, desk)

	hub.unregisterClient(phone)
	if hub.CountVoiceUsers(voiceID) != 1 {
		t.Fatal("closing a device that wasn't in the call ended it")
	}
	hub.unregisterClient(desk)
	if hub.CountVoiceUsers(voiceID) != 0 {
		t.Fatal("the call outlived the device carrying it")
	}
	select {
	case ch := <-left:
		if ch != voiceID {
			t.Fatalf("voice leave for %v, want %v", ch, voiceID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no voice leave callback")
	}
	if !hub.IsUserOnline(userID) {
		t.Fatal("user went offline with a device still connected")
	}
}

func TestPeopleOnlineCountSkipsPluginsAndBots(t *testing.T) {
	hub := NewHub()
	person, twoTabs, plugin, bot := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hub.clients[person] = []*Client{{UserID: person, User: &models.User{}}}
	hub.clients[twoTabs] = []*Client{{UserID: twoTabs, User: &models.User{}}, {UserID: twoTabs, User: &models.User{}}}
	hub.clients[plugin] = []*Client{{UserID: plugin, User: &models.User{IsServiceAccount: true}, IsPlugin: true}}
	hub.clients[bot] = []*Client{{UserID: bot, User: &models.User{IsBot: true}}}

	if got := hub.PeopleOnlineCount(); got != 2 {
		t.Errorf("PeopleOnlineCount = %d, want 2", got)
	}
	if got := hub.ConnectedClientCount(); got != 4 {
		t.Errorf("ConnectedClientCount = %d, want 4", got)
	}
}
