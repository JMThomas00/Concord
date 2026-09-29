package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// voiceUserEntry tracks a user's current voice channel for disconnect cleanup.
// conn is the connection that joined (the one carrying the audio), so only
// that connection closing ends the user's voice session; nil when unknown.
type voiceUserEntry struct {
	serverID  uuid.UUID
	channelID uuid.UUID
	conn      *Client
}

// Hub maintains the set of active clients and broadcasts messages.
//
// A user can be connected more than once (two computers, or a reconnect
// racing the old socket's cleanup). Every live connection is kept, and
// anything addressed to a user, server or channel reaches all of them. A
// user goes offline only when their last connection closes.
type Hub struct {
	// Live connections by user ID, oldest first.
	clients map[uuid.UUID][]*Client

	// Online users by server ID, for server broadcasts.
	serverClients map[uuid.UUID]map[uuid.UUID]struct{}

	// Users by channel ID, for channel broadcasts (typing, voice speaking).
	channelClients map[uuid.UUID]map[uuid.UUID]struct{}

	// voiceChannelUsers maps channelID → set of userIDs currently in that voice channel.
	voiceChannelUsers map[uuid.UUID]map[uuid.UUID]struct{}

	// voiceUserChannel maps userID → their current voice channel entry (for fast lookup on disconnect).
	voiceUserChannel map[uuid.UUID]voiceUserEntry

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

	// onVoiceLeave is an optional callback invoked (in a goroutine) when a user
	// disconnects while in a voice channel. Handlers.go sets this to handle DB cleanup.
	onVoiceLeave func(userID, serverID, channelID uuid.UUID)

	// onUserDisconnect is an optional callback invoked (in a goroutine) when a
	// user disconnects, with their Status already set to StatusOffline.
	// Handlers.go sets this to persist the offline status to the database --
	// see unregisterClient's own doc comment for the real bug this closes.
	onUserDisconnect func(user *models.User)

	// onClientGone is invoked (in a goroutine) for every connection that
	// unregisters, including plugin connections, users in no server, and a
	// user's connection whose other connections are still open.
	onClientGone func(c *Client)
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
		clients:           make(map[uuid.UUID][]*Client),
		serverClients:     make(map[uuid.UUID]map[uuid.UUID]struct{}),
		channelClients:    make(map[uuid.UUID]map[uuid.UUID]struct{}),
		voiceChannelUsers: make(map[uuid.UUID]map[uuid.UUID]struct{}),
		voiceUserChannel:  make(map[uuid.UUID]voiceUserEntry),
		register:          make(chan *Client),
		unregister:        make(chan *Client),
		broadcast:         make(chan *BroadcastMessage, 256),
		sequence:          0,
	}
}

// SetVoiceLeaveCallback registers the callback invoked when a disconnecting user
// was in a voice channel. The callback is run in a new goroutine to avoid blocking
// the hub's event loop.
func (h *Hub) SetVoiceLeaveCallback(fn func(userID, serverID, channelID uuid.UUID)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onVoiceLeave = fn
}

// SetUserDisconnectCallback registers the callback invoked when a client
// disconnects, so the caller can persist the resulting offline status to the
// database. The callback is run in a new goroutine to avoid blocking the
// hub's event loop.
func (h *Hub) SetUserDisconnectCallback(fn func(user *models.User)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onUserDisconnect = fn
}

// SetClientGoneCallback registers the callback invoked when a user's
// current connection unregisters; see Hub.onClientGone.
func (h *Hub) SetClientGoneCallback(fn func(c *Client)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onClientGone = fn
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

	conns := h.clients[client.UserID]
	for _, c := range conns {
		if c == client {
			return // identified twice on one socket
		}
	}
	h.clients[client.UserID] = append(conns, client)

	// Add to server client maps
	for _, serverID := range client.ServerIDs {
		if h.serverClients[serverID] == nil {
			h.serverClients[serverID] = make(map[uuid.UUID]struct{})
		}
		h.serverClients[serverID][client.UserID] = struct{}{}
	}

	HubLog.Info("Client registered", "user_id", client.UserID, "server_count", len(client.ServerIDs), "connections", len(conns)+1)
}

// unregisterClient removes a client from the hub. The user stays online
// while any of their other connections is open; only the last one closing
// takes them out of every server and channel and announces them offline.
func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()

	conns := h.clients[client.UserID]
	idx := -1
	for i, c := range conns {
		if c == client {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return // never registered (didn't identify)
	}
	remaining := append(conns[:idx:idx], conns[idx+1:]...)

	// The user's voice session ends if this is the connection that joined
	// (it carried the audio), or if no connection is left at all.
	voiceEntry, wasInVoice := h.voiceUserChannel[client.UserID]
	wasInVoice = wasInVoice && (voiceEntry.conn == client || len(remaining) == 0)

	if len(remaining) > 0 {
		h.clients[client.UserID] = remaining
		if wasInVoice {
			h.removeVoiceLocked(client.UserID, voiceEntry.channelID)
		}
		client.closeSend()
		cb := h.onVoiceLeave
		goneCb := h.onClientGone
		h.mu.Unlock()

		if goneCb != nil {
			go goneCb(client)
		}
		if wasInVoice && cb != nil {
			go cb(client.UserID, voiceEntry.serverID, voiceEntry.channelID)
		}
		HubLog.Info("Client unregistered; user still connected", "user_id", client.UserID, "connections", len(remaining))
		return
	}

	// Save for presence broadcast (before removing from maps)
	user := client.User
	serverIDs := make([]uuid.UUID, len(client.ServerIDs))
	copy(serverIDs, client.ServerIDs)

	if wasInVoice {
		h.removeVoiceLocked(client.UserID, voiceEntry.channelID)
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
	client.closeSend()

	cb := h.onVoiceLeave
	disconnectCb := h.onUserDisconnect
	goneCb := h.onClientGone

	h.mu.Unlock()

	if goneCb != nil {
		go goneCb(client)
	}

	HubLog.Info("Client unregistered", "user_id", client.UserID)

	// Broadcast offline presence to all servers this user was in.
	//
	// Real bug fixed 2026-09-13: this broadcast is the ONLY thing that ever
	// told anyone this user went offline -- the offline status was never
	// persisted to the database, only announced live to whichever clients
	// happened to be connected at that exact moment. A user who disconnected
	// while no other client was around to receive the broadcast (or who
	// simply lost power/network without a clean close) stayed "online"
	// forever in storage, since SetOnline()+UpdateUserStatus() (client.go,
	// on identify) is the only other place status is ever written. The next
	// person to connect would then read that stale "online" status straight
	// out of the database via SERVER_CREATE's Users[] list, showing a member
	// as online whose machine had been off for weeks. onUserDisconnect below
	// closes that gap by actually persisting the offline status, the same
	// way HandlePresenceUpdate already does for an explicit /status change.
	if user != nil && len(serverIDs) > 0 {
		offlineUser := *user
		offlineUser.Status = models.StatusOffline
		h.BroadcastPresenceUpdate(&offlineUser, serverIDs)
		if disconnectCb != nil {
			go disconnectCb(&offlineUser)
		}
	}

	// If the user was in a voice channel, run DB cleanup + broadcast leave event
	if wasInVoice && cb != nil {
		go cb(client.UserID, voiceEntry.serverID, voiceEntry.channelID)
	}
}

// broadcastMessage sends a message to the appropriate clients
func (h *Hub) broadcastMessage(msg *BroadcastMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var targets []*Client

	switch {
	case msg.UserID != nil:
		// Send to every connection of one user
		targets = h.clients[*msg.UserID]

	case msg.ServerID != nil:
		// Send to all users in server
		for userID := range h.serverClients[*msg.ServerID] {
			targets = append(targets, h.clients[userID]...)
		}

	case msg.ChannelID != nil:
		// Send to all users in channel
		if users, ok := h.channelClients[*msg.ChannelID]; ok {
			for userID := range users {
				targets = append(targets, h.clients[userID]...)
			}
			HubLog.Debug("Broadcasting to channel", "channel_id", msg.ChannelID, "client_count", len(targets))
		} else {
			HubLog.Debug("No clients found in channel", "channel_id", msg.ChannelID)
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
			HubLog.Warn("Client buffer full, dropping message", "user_id", client.UserID)
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

// GetClient returns userID's most recent connection, or nil. For plugin
// service accounts (one process each) that's the plugin's connection.
func (h *Hub) GetClient(userID uuid.UUID) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if conns := h.clients[userID]; len(conns) > 0 {
		return conns[len(conns)-1]
	}
	return nil
}

// ConnectionCount returns the number of live connections (a user signed in
// twice counts twice); ConnectedClientCount counts users.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, conns := range h.clients {
		n += len(conns)
	}
	return n
}

// Connections returns every live connection.
func (h *Hub) Connections() []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []*Client
	for _, conns := range h.clients {
		out = append(out, conns...)
	}
	return out
}

// DisconnectUser closes every live WebSocket connection of userID. Each
// connection's own ReadPump then fails and unregisters it through the
// normal path. Used when a plugin is stopped or its token rotated (so a
// process that authenticated with the old token can't keep acting), and
// for kicks, bans and timeouts.
func (h *Hub) DisconnectUser(userID uuid.UUID) {
	h.mu.RLock()
	conns := append([]*Client(nil), h.clients[userID]...)
	h.mu.RUnlock()
	for _, client := range conns {
		if client.conn != nil {
			client.conn.Close()
		}
	}
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

// JoinVoiceChannel records that a user is now in a voice channel.
// If the user was already in another voice channel (same server), they are
// moved. conn is the connection that joined (its closing ends the session);
// nil keeps the previous one (an admin moving someone).
func (h *Hub) JoinVoiceChannel(userID, serverID, channelID uuid.UUID, conn *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove from previous voice channel if any
	if prev, ok := h.voiceUserChannel[userID]; ok {
		if conn == nil {
			conn = prev.conn
		}
		h.removeVoiceLocked(userID, prev.channelID)
	}

	// Add to new voice channel
	if h.voiceChannelUsers[channelID] == nil {
		h.voiceChannelUsers[channelID] = make(map[uuid.UUID]struct{})
	}
	h.voiceChannelUsers[channelID][userID] = struct{}{}
	h.voiceUserChannel[userID] = voiceUserEntry{serverID: serverID, channelID: channelID, conn: conn}
}

// LeaveVoiceChannel removes a user from their current voice channel.
func (h *Hub) LeaveVoiceChannel(userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if entry, ok := h.voiceUserChannel[userID]; ok {
		h.removeVoiceLocked(userID, entry.channelID)
	}
}

// removeVoiceLocked forgets userID's voice membership. h.mu must be held.
func (h *Hub) removeVoiceLocked(userID, channelID uuid.UUID) {
	delete(h.voiceUserChannel, userID)
	if ch := h.voiceChannelUsers[channelID]; ch != nil {
		delete(ch, userID)
		if len(ch) == 0 {
			delete(h.voiceChannelUsers, channelID)
		}
	}
}

// CountVoiceUsers returns the number of users currently in a voice channel.
func (h *Hub) CountVoiceUsers(channelID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.voiceChannelUsers[channelID])
}

// GetVoiceUsers returns the user IDs currently in a voice channel.
func (h *Hub) GetVoiceUsers(channelID uuid.UUID) []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	users := make([]uuid.UUID, 0, len(h.voiceChannelUsers[channelID]))
	for uid := range h.voiceChannelUsers[channelID] {
		users = append(users, uid)
	}
	return users
}

// JoinChannel adds an online user to a channel's broadcast list.
func (h *Hub) JoinChannel(userID, channelID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.clients[userID]) == 0 {
		return
	}
	if h.channelClients[channelID] == nil {
		h.channelClients[channelID] = make(map[uuid.UUID]struct{})
	}
	h.channelClients[channelID][userID] = struct{}{}
}

// LeaveChannel removes a user from a channel's broadcast list.
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

// AddClientToServer adds an online user (all their connections) to a
// server's broadcast list.
func (h *Hub) AddClientToServer(userID, serverID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conns := h.clients[userID]
	if len(conns) == 0 {
		return
	}
	if h.serverClients[serverID] == nil {
		h.serverClients[serverID] = make(map[uuid.UUID]struct{})
	}
	h.serverClients[serverID][userID] = struct{}{}
	for _, client := range conns {
		client.ServerIDs = append(client.ServerIDs, serverID)
	}
}

// RemoveClientFromServer removes a user (all their connections) from a
// server's broadcast list.
func (h *Hub) RemoveClientFromServer(userID, serverID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.serverClients[serverID] != nil {
		delete(h.serverClients[serverID], userID)
		if len(h.serverClients[serverID]) == 0 {
			delete(h.serverClients, serverID)
		}
	}

	for _, client := range h.clients[userID] {
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

// ConnectedClientCount returns the number of currently authenticated WebSocket clients.
func (h *Hub) ConnectedClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// IsUserOnline reports whether a user with the given ID has an active connection.
func (h *Hub) IsUserOnline(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
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
		HubLog.Error("Failed to marshal presence update", "user_id", user.ID, "error", err)
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
const TypingTimeout = 5 * time.Second

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

// StartTyping marks a user as typing in a channel. username is carried
// straight through to the broadcast payload rather than requiring clients
// to resolve it themselves — a plugin's service-account user is never a
// ServerMember, so a client-side member-list lookup can't ever find it.
// isBot flags a plugin's own service-account connection so the client can
// render "X is thinking" instead of "X is typing".
func (tm *TypingManager) StartTyping(userID, channelID, serverID uuid.UUID, username string, isBot bool) {
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
		Username:  username,
		IsBot:     isBot,
		Timestamp: time.Now(),
	}

	tm.hub.BroadcastToChannel(channelID, protocol.EventTypingStart, payload, &userID)
}

// StopTyping removes typing indicator and notifies channel peers (called when message is sent)
func (tm *TypingManager) StopTyping(userID, channelID uuid.UUID) {
	tm.mu.Lock()
	exists := tm.indicators[channelID] != nil
	if exists {
		delete(tm.indicators[channelID], userID)
	}
	tm.mu.Unlock()

	if exists {
		payload := &protocol.TypingStopEventPayload{
			ChannelID: channelID,
			UserID:    userID,
		}
		tm.hub.BroadcastToChannel(channelID, protocol.EventTypingStop, payload, &userID)
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
