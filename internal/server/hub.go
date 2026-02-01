package server

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	// Registered clients by user ID
	clients map[uuid.UUID]*Client

	// Clients by server ID for efficient broadcasting
	serverClients map[uuid.UUID]map[uuid.UUID]*Client

	// Clients by channel ID for typing indicators and DMs
	channelClients map[uuid.UUID]map[uuid.UUID]*Client

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Inbound messages from clients
	broadcast chan *BroadcastMessage

	// Mutex for thread-safe operations
	mu sync.RWMutex

	// Sequence number for dispatch messages
	sequence int64
	seqMu    sync.Mutex
}

// BroadcastMessage represents a message to be sent to multiple clients
type BroadcastMessage struct {
	// Target specification (one of these should be set)
	UserID    *uuid.UUID // Send to specific user
	ServerID  *uuid.UUID // Send to all users in server
	ChannelID *uuid.UUID // Send to all users in channel
	
	// Exclude this user from broadcast (usually the sender)
	ExcludeUserID *uuid.UUID
	
	// The message to send
	Message *protocol.Message
}

// NewHub creates a new Hub instance
func NewHub() *Hub {
	return &Hub{
		clients:        make(map[uuid.UUID]*Client),
		serverClients:  make(map[uuid.UUID]map[uuid.UUID]*Client),
		channelClients: make(map[uuid.UUID]map[uuid.UUID]*Client),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		broadcast:      make(chan *BroadcastMessage, 256),
		sequence:       0,
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.registerClient(client)

		case client := <-h.unregister:
			h.unregisterClient(client)

		case msg := <-h.broadcast:
			h.broadcastMessage(msg)
		}
	}
}

// registerClient adds a client to the hub
func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Add to main client map
	h.clients[client.UserID] = client

	// Add to server client maps
	for _, serverID := range client.ServerIDs {
		if h.serverClients[serverID] == nil {
			h.serverClients[serverID] = make(map[uuid.UUID]*Client)
		}
		h.serverClients[serverID][client.UserID] = client
	}

	log.Printf("Client registered: user=%s, servers=%d", client.UserID, len(client.ServerIDs))
}

// unregisterClient removes a client from the hub
func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client.UserID]; !ok {
		return
	}

	// Remove from main client map
	delete(h.clients, client.UserID)

	// Remove from server client maps
	for _, serverID := range client.ServerIDs {
		if h.serverClients[serverID] != nil {
			delete(h.serverClients[serverID], client.UserID)
			if len(h.serverClients[serverID]) == 0 {
				delete(h.serverClients, serverID)
			}
		}
	}

	// Remove from channel client maps
	for channelID, clients := range h.channelClients {
		delete(clients, client.UserID)
		if len(clients) == 0 {
			delete(h.channelClients, channelID)
		}
	}

	// Close the client's send channel
	close(client.send)

	log.Printf("Client unregistered: user=%s", client.UserID)
}

// broadcastMessage sends a message to the appropriate clients
func (h *Hub) broadcastMessage(msg *BroadcastMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var targets []*Client

	switch {
	case msg.UserID != nil:
		// Send to specific user
		if client, ok := h.clients[*msg.UserID]; ok {
			targets = []*Client{client}
		}

	case msg.ServerID != nil:
		// Send to all users in server
		if clients, ok := h.serverClients[*msg.ServerID]; ok {
			for _, client := range clients {
				targets = append(targets, client)
			}
		}

	case msg.ChannelID != nil:
		// Send to all users in channel
		if clients, ok := h.channelClients[*msg.ChannelID]; ok {
			for _, client := range clients {
				targets = append(targets, client)
			}
		}
	}

	// Send to all targets, excluding the sender if specified
	for _, client := range targets {
		if msg.ExcludeUserID != nil && client.UserID == *msg.ExcludeUserID {
			continue
		}

		select {
		case client.send <- msg.Message:
		default:
			// Client's buffer is full, skip
			log.Printf("Client buffer full, dropping message: user=%s", client.UserID)
		}
	}
}

// NextSequence returns the next sequence number for dispatch messages
func (h *Hub) NextSequence() int64 {
	h.seqMu.Lock()
	defer h.seqMu.Unlock()
	h.sequence++
	return h.sequence
}

// GetClient returns a client by user ID
func (h *Hub) GetClient(userID uuid.UUID) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[userID]
}

// GetOnlineUsers returns a list of online user IDs for a server
func (h *Hub) GetOnlineUsers(serverID uuid.UUID) []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var users []uuid.UUID
	if clients, ok := h.serverClients[serverID]; ok {
		for userID := range clients {
			users = append(users, userID)
		}
	}
	return users
}

// JoinChannel adds a client to a channel's client list
func (h *Hub) JoinChannel(userID, channelID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.channelClients[channelID] == nil {
		h.channelClients[channelID] = make(map[uuid.UUID]*Client)
	}

	if client, ok := h.clients[userID]; ok {
		h.channelClients[channelID][userID] = client
	}
}

// LeaveChannel removes a client from a channel's client list
func (h *Hub) LeaveChannel(userID, channelID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.channelClients[channelID] != nil {
		delete(h.channelClients[channelID], userID)
		if len(h.channelClients[channelID]) == 0 {
			delete(h.channelClients, channelID)
		}
	}
}

// AddClientToServer adds a client to a server's broadcast list
func (h *Hub) AddClientToServer(userID, serverID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.serverClients[serverID] == nil {
		h.serverClients[serverID] = make(map[uuid.UUID]*Client)
	}

	if client, ok := h.clients[userID]; ok {
		h.serverClients[serverID][userID] = client
		client.ServerIDs = append(client.ServerIDs, serverID)
	}
}

// RemoveClientFromServer removes a client from a server's broadcast list
func (h *Hub) RemoveClientFromServer(userID, serverID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.serverClients[serverID] != nil {
		delete(h.serverClients[serverID], userID)
		if len(h.serverClients[serverID]) == 0 {
			delete(h.serverClients, serverID)
		}
	}

	if client, ok := h.clients[userID]; ok {
		for i, id := range client.ServerIDs {
			if id == serverID {
				client.ServerIDs = append(client.ServerIDs[:i], client.ServerIDs[i+1:]...)
				break
			}
		}
	}
}

// BroadcastToServer sends a message to all users in a server
func (h *Hub) BroadcastToServer(serverID uuid.UUID, eventType protocol.EventType, data interface{}, excludeUser *uuid.UUID) error {
	msg, err := protocol.NewDispatch(eventType, h.NextSequence(), data)
	if err != nil {
		return err
	}

	h.broadcast <- &BroadcastMessage{
		ServerID:      &serverID,
		ExcludeUserID: excludeUser,
		Message:       msg,
	}

	return nil
}

// BroadcastToChannel sends a message to all users in a channel
func (h *Hub) BroadcastToChannel(channelID uuid.UUID, eventType protocol.EventType, data interface{}, excludeUser *uuid.UUID) error {
	msg, err := protocol.NewDispatch(eventType, h.NextSequence(), data)
	if err != nil {
		return err
	}

	h.broadcast <- &BroadcastMessage{
		ChannelID:     &channelID,
		ExcludeUserID: excludeUser,
		Message:       msg,
	}

	return nil
}

// SendToUser sends a message to a specific user
func (h *Hub) SendToUser(userID uuid.UUID, eventType protocol.EventType, data interface{}) error {
	msg, err := protocol.NewDispatch(eventType, h.NextSequence(), data)
	if err != nil {
		return err
	}

	h.broadcast <- &BroadcastMessage{
		UserID:  &userID,
		Message: msg,
	}

	return nil
}

// BroadcastPresenceUpdate sends a presence update to relevant servers
func (h *Hub) BroadcastPresenceUpdate(user *models.User, serverIDs []uuid.UUID) {
	payload := &protocol.PresenceUpdateEventPayload{
		User:       user,
		Status:     user.Status,
		StatusText: user.StatusText,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal presence update: %v", err)
		return
	}

	for _, serverID := range serverIDs {
		msg := &protocol.Message{
			Op:   protocol.OpDispatch,
			Data: data,
			Type: protocol.EventPresenceUpdate,
		}
		seq := h.NextSequence()
		msg.Seq = &seq

		serverIDCopy := serverID
		h.broadcast <- &BroadcastMessage{
			ServerID: &serverIDCopy,
			Message:  msg,
		}
	}
}

// TypingTimeout is how long typing indicators last
const TypingTimeout = 10 * time.Second

// TypingIndicator tracks active typing users
type TypingIndicator struct {
	UserID    uuid.UUID
	ChannelID uuid.UUID
	ExpiresAt time.Time
}

// TypingManager manages typing indicators
type TypingManager struct {
	indicators map[uuid.UUID]map[uuid.UUID]*TypingIndicator // channel -> user -> indicator
	mu         sync.RWMutex
	hub        *Hub
}

// NewTypingManager creates a new typing manager
func NewTypingManager(hub *Hub) *TypingManager {
	tm := &TypingManager{
		indicators: make(map[uuid.UUID]map[uuid.UUID]*TypingIndicator),
		hub:        hub,
	}

	// Start cleanup goroutine
	go tm.cleanup()

	return tm
}

// StartTyping marks a user as typing in a channel
func (tm *TypingManager) StartTyping(userID, channelID, serverID uuid.UUID) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.indicators[channelID] == nil {
		tm.indicators[channelID] = make(map[uuid.UUID]*TypingIndicator)
	}

	tm.indicators[channelID][userID] = &TypingIndicator{
		UserID:    userID,
		ChannelID: channelID,
		ExpiresAt: time.Now().Add(TypingTimeout),
	}

	// Broadcast typing event
	payload := &protocol.TypingStartEventPayload{
		ChannelID: channelID,
		ServerID:  serverID,
		UserID:    userID,
		Timestamp: time.Now(),
	}

	tm.hub.BroadcastToChannel(channelID, protocol.EventTypingStart, payload, &userID)
}

// StopTyping removes typing indicator (called when message is sent)
func (tm *TypingManager) StopTyping(userID, channelID uuid.UUID) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.indicators[channelID] != nil {
		delete(tm.indicators[channelID], userID)
	}
}

// cleanup removes expired typing indicators
func (tm *TypingManager) cleanup() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tm.mu.Lock()
		now := time.Now()
		for channelID, users := range tm.indicators {
			for userID, indicator := range users {
				if now.After(indicator.ExpiresAt) {
					delete(users, userID)
				}
			}
			if len(users) == 0 {
				delete(tm.indicators, channelID)
			}
		}
		tm.mu.Unlock()
	}
}
