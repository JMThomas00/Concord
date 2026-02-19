package server

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512 * 1024 // 512KB

	// Size of client send buffer
	sendBufferSize = 256

	// Heartbeat interval sent to client
	heartbeatInterval = 45000 // 45 seconds in milliseconds
)

// Client represents a connected WebSocket client
type Client struct {
	// The WebSocket connection
	conn *websocket.Conn

	// The hub this client is connected to
	hub *Hub

	// Buffered channel of outbound messages
	send chan *protocol.Message

	// User information (set after authentication)
	UserID    uuid.UUID
	User      *models.User
	SessionID string

	// Server memberships
	ServerIDs []uuid.UUID

	// Last received sequence number
	lastSeq int64
	seqMu   sync.Mutex

	// Connection state
	authenticated bool
	authMu        sync.RWMutex

	// Handlers for processing messages
	handlers *Handlers
}

// NewClient creates a new client instance
func NewClient(conn *websocket.Conn, hub *Hub, handlers *Handlers) *Client {
	return &Client{
		conn:     conn,
		hub:      hub,
		send:     make(chan *protocol.Message, sendBufferSize),
		handlers: handlers,
	}
}

// ReadPump pumps messages from the WebSocket connection to the hub
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid message format")
			continue
		}

		c.handleMessage(&msg)
	}
}

// WritePump pumps messages from the hub to the WebSocket connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			data, err := json.Marshal(msg)
			if err != nil {
				log.Printf("Failed to marshal message: %v", err)
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("Failed to write message: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendHello sends the initial HELLO message
func (c *Client) SendHello() {
	payload := &protocol.HelloPayload{
		HeartbeatInterval: heartbeatInterval,
	}

	msg, err := protocol.NewMessage(protocol.OpHello, payload)
	if err != nil {
		log.Printf("Failed to create hello message: %v", err)
		return
	}

	c.send <- msg
}

// handleMessage processes an incoming message based on its opcode
func (c *Client) handleMessage(msg *protocol.Message) {
	switch msg.Op {
	case protocol.OpIdentify:
		c.handleIdentify(msg)

	case protocol.OpHeartbeat:
		c.handleHeartbeat(msg)

	case protocol.OpSendMessage:
		c.requireAuth(func() {
			c.handlers.HandleSendMessage(c, msg)
		})

	case protocol.OpTypingStart:
		c.requireAuth(func() {
			c.handlers.HandleTypingStart(c, msg)
		})

	case protocol.OpPresenceUpdate:
		c.requireAuth(func() {
			c.handlers.HandlePresenceUpdate(c, msg)
		})

	case protocol.OpRequestGuild:
		c.requireAuth(func() {
			c.handlers.HandleRequestGuild(c, msg)
		})

	case protocol.OpChannelCreate:
		c.requireAuth(func() {
			c.handlers.HandleCreateChannel(c, msg)
		})

	case protocol.OpChannelUpdate:
		c.requireAuth(func() {
			c.handlers.HandleUpdateChannel(c, msg)
		})

	case protocol.OpChannelDelete:
		c.requireAuth(func() {
			c.handlers.HandleDeleteChannel(c, msg)
		})

	case protocol.OpRequestMessages:
		c.requireAuth(func() {
			c.handlers.HandleRequestMessages(c, msg)
		})

	case protocol.OpRoleAssign:
		c.requireAuth(func() {
			c.handlers.HandleRoleAssign(c, msg)
		})

	case protocol.OpRoleRemove:
		c.requireAuth(func() {
			c.handlers.HandleRoleRemove(c, msg)
		})

	case protocol.OpKickMember:
		c.requireAuth(func() {
			c.handlers.HandleKickMember(c, msg)
		})

	case protocol.OpBanMember:
		c.requireAuth(func() {
			c.handlers.HandleBanMember(c, msg)
		})

	case protocol.OpMuteMember:
		c.requireAuth(func() {
			c.handlers.HandleMuteMember(c, msg)
		})

	case protocol.OpWhisper:
		c.requireAuth(func() {
			c.handlers.HandleWhisper(c, msg)
		})

	default:
		log.Printf("Unknown opcode: %d", msg.Op)
		c.sendError(protocol.ErrorCodeUnknown, "Unknown operation")
	}
}

// handleIdentify processes the IDENTIFY message for authentication
func (c *Client) handleIdentify(msg *protocol.Message) {
	c.authMu.Lock()
	defer c.authMu.Unlock()

	if c.authenticated {
		c.sendError(protocol.ErrorCodeAlreadyAuthenticated, "Already authenticated")
		return
	}

	var payload protocol.IdentifyPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid identify payload")
		return
	}

	// Authenticate the user
	user, serverIDs, err := c.handlers.Authenticate(payload.Token)
	if err != nil {
		log.Printf("Authentication failed: %v", err)
		c.sendInvalidSession("Authentication failed")
		return
	}

	// Set client state
	c.UserID = user.ID
	c.User = user
	c.ServerIDs = serverIDs
	c.SessionID = uuid.New().String()
	c.authenticated = true

	// Register with hub
	c.hub.register <- c

	// Update user status to online
	user.SetOnline()
	c.handlers.UpdateUserStatus(user)

	// Send READY response
	servers, _ := c.handlers.GetUserServers(user.ID)
	readyPayload := &protocol.ReadyPayload{
		SessionID: c.SessionID,
		User:      user,
		Servers:   servers,
	}

	readyMsg, err := protocol.NewMessage(protocol.OpReady, readyPayload)
	if err != nil {
		log.Printf("Failed to create ready message: %v", err)
		return
	}

	c.send <- readyMsg

	// Send SERVER_CREATE for each server with full data (channels, members, roles, users)
	for i, server := range servers {
		channels, _ := c.handlers.db.GetServerChannels(server.ID)
		members, _ := c.handlers.db.GetServerMembers(server.ID)
		roles, _ := c.handlers.db.GetServerRoles(server.ID)

		// Collect user IDs for all members so the client can display names/avatars
		userIDs := make([]uuid.UUID, 0, len(members))
		for _, m := range members {
			userIDs = append(userIDs, m.UserID)
		}
		users, _ := c.handlers.db.GetUsersByIDs(userIDs)

		// CRITICAL FIX: Auto-join all channels in this server
		// This ensures the client receives MESSAGE_CREATE broadcasts
		for _, channel := range channels {
			c.hub.JoinChannel(c.UserID, channel.ID)
		}

		serverCreatePayload := &protocol.ServerCreatePayload{
			Server:   server,
			Channels: channels,
			Members:  members,
			Roles:    roles,
			Users:    users,
		}

		// Use sequence number starting from 1
		seq := int64(i + 1)
		serverCreateMsg, err := protocol.NewDispatch(protocol.EventServerCreate, seq, serverCreatePayload)
		if err != nil {
			log.Printf("Failed to create SERVER_CREATE message: %v", err)
			continue
		}

		c.send <- serverCreateMsg
		log.Printf("Sent SERVER_CREATE for server %s with %d channels, %d members, %d roles, %d users (auto-joined %d channels)",
			server.Name, len(channels), len(members), len(roles), len(users), len(channels))

		// Broadcast SERVER_MEMBER_ADD to all OTHER clients already in this server so
		// their members panels update in real time without needing to reconnect.
		// (The newly connecting user is excluded since they receive the full list via SERVER_CREATE above.)
		var newUserMember *models.ServerMember
		for _, m := range members {
			if m.UserID == user.ID {
				newUserMember = m
				break
			}
		}
		if newUserMember != nil {
			memberAddPayload := &protocol.ServerMemberAddPayload{
				ServerID: server.ID,
				Member:   newUserMember,
				User:     user,
			}
			if err := c.hub.BroadcastToServer(server.ID, protocol.EventServerMemberAdd, memberAddPayload, &user.ID); err != nil {
				log.Printf("Failed to broadcast SERVER_MEMBER_ADD for server %s: %v", server.Name, err)
			}
		}
	}

	// Broadcast presence update to all servers
	c.hub.BroadcastPresenceUpdate(user, serverIDs)

	log.Printf("User authenticated: %s (%s)", user.Username, user.ID)
}

// handleHeartbeat processes heartbeat messages
func (c *Client) handleHeartbeat(msg *protocol.Message) {
	var payload protocol.HeartbeatPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		// Heartbeat can have no payload
		payload.LastSequence = nil
	}

	// Update last sequence if provided
	if payload.LastSequence != nil {
		c.seqMu.Lock()
		c.lastSeq = *payload.LastSequence
		c.seqMu.Unlock()
	}

	// Send heartbeat ACK
	ackMsg, _ := protocol.NewMessage(protocol.OpHeartbeatAck, nil)
	c.send <- ackMsg
}

// requireAuth wraps a handler to require authentication
func (c *Client) requireAuth(handler func()) {
	c.authMu.RLock()
	authenticated := c.authenticated
	c.authMu.RUnlock()

	if !authenticated {
		c.sendError(protocol.ErrorCodeUnauthorized, "Not authenticated")
		return
	}

	handler()
}

// sendError sends an error message to the client
func (c *Client) sendError(code int, message string) {
	payload := &protocol.ErrorPayload{
		Code:    code,
		Message: message,
	}

	// For errors, we use a dispatch with no event type
	data, _ := json.Marshal(payload)
	msg := &protocol.Message{
		Op:   protocol.OpDispatch,
		Data: data,
	}

	select {
	case c.send <- msg:
	default:
		log.Printf("Failed to send error, buffer full")
	}
}

// sendInvalidSession sends an INVALID_SESSION message
func (c *Client) sendInvalidSession(reason string) {
	payload := map[string]string{"reason": reason}
	msg, _ := protocol.NewMessage(protocol.OpInvalidSession, payload)

	select {
	case c.send <- msg:
	default:
	}

	// Close connection after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		c.conn.Close()
	}()
}

// Send sends a message to the client
func (c *Client) Send(msg *protocol.Message) {
	select {
	case c.send <- msg:
	default:
		log.Printf("Client send buffer full, dropping message")
	}
}

// SendDispatch sends a dispatch event to the client
func (c *Client) SendDispatch(eventType protocol.EventType, data interface{}) error {
	seq := c.hub.NextSequence()
	msg, err := protocol.NewDispatch(eventType, seq, data)
	if err != nil {
		return err
	}

	c.Send(msg)
	return nil
}

// IsAuthenticated returns whether the client is authenticated
func (c *Client) IsAuthenticated() bool {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.authenticated
}

// GetLastSequence returns the last received sequence number
func (c *Client) GetLastSequence() int64 {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()
	return c.lastSeq
}

// Close gracefully closes the client connection
func (c *Client) Close() {
	c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.conn.Close()
}
