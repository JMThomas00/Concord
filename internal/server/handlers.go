package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Handlers contains methods for handling client messages
type Handlers struct {
	db            *database.DB
	hub           *Hub
	typingManager *TypingManager
}

// NewHandlers creates a new Handlers instance
func NewHandlers(db *database.DB, hub *Hub) *Handlers {
	h := &Handlers{
		db:  db,
		hub: hub,
	}
	h.typingManager = NewTypingManager(hub)
	return h
}

// Authenticate validates a token and returns the associated user
func (h *Handlers) Authenticate(token string) (*models.User, []uuid.UUID, error) {
	// Hash the token to look up the session
	tokenHash := hashToken(token)

	userID, err := h.db.GetSessionByToken(tokenHash)
	if err != nil {
		return nil, nil, errors.New("invalid or expired token")
	}

	user, err := h.db.GetUserByID(userID)
	if err != nil {
		return nil, nil, errors.New("user not found")
	}

	// Get user's server memberships
	servers, err := h.db.GetUserServers(userID)
	if err != nil {
		return nil, nil, err
	}

	serverIDs := make([]uuid.UUID, len(servers))
	for i, s := range servers {
		serverIDs[i] = s.ID
	}

	return user, serverIDs, nil
}

// UpdateUserStatus updates a user's status in the database
func (h *Handlers) UpdateUserStatus(user *models.User) error {
	return h.db.UpdateUserStatus(user.ID, user.Status, user.StatusText)
}

// GetUserServers returns all servers a user is a member of
func (h *Handlers) GetUserServers(userID uuid.UUID) ([]*models.Server, error) {
	return h.db.GetUserServers(userID)
}

// HandleSendMessage processes a message send request
func (h *Handlers) HandleSendMessage(c *Client, msg *protocol.Message) {
	var payload protocol.SendMessagePayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid message payload")
		return
	}

	// Validate content
	if len(payload.Content) == 0 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Message content cannot be empty")
		return
	}

	if len(payload.Content) > 2000 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Message content too long (max 2000 characters)")
		return
	}

	// Create the message
	newMsg := models.NewMessage(payload.ChannelID, c.UserID, payload.Content)
	if payload.ReplyToID != nil {
		newMsg.ReplyToID = payload.ReplyToID
	}

	// Save to database
	if err := h.db.CreateMessage(newMsg); err != nil {
		log.Printf("Failed to save message: %v", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to save message")
		return
	}

	// Stop typing indicator for this user
	h.typingManager.StopTyping(c.UserID, payload.ChannelID)

	// Get channel to determine server ID
	// For now, we'll broadcast to channel subscribers
	// TODO: Look up channel and get server ID for proper broadcasting

	// Create the response payload
	responsePayload := &protocol.MessageCreatePayload{
		Message: newMsg,
		Author:  c.User,
		Nonce:   payload.Nonce,
	}

	// Broadcast to channel
	h.hub.BroadcastToChannel(payload.ChannelID, protocol.EventMessageCreate, responsePayload, nil)

	log.Printf("Message sent: channel=%s, author=%s", payload.ChannelID, c.User.Username)
}

// HandleTypingStart processes a typing indicator
func (h *Handlers) HandleTypingStart(c *Client, msg *protocol.Message) {
	var payload protocol.TypingStartPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid typing payload")
		return
	}

	// TODO: Look up channel to get server ID
	// For now, use a nil UUID
	h.typingManager.StartTyping(c.UserID, payload.ChannelID, uuid.Nil)
}

// HandlePresenceUpdate processes a presence update
func (h *Handlers) HandlePresenceUpdate(c *Client, msg *protocol.Message) {
	var payload protocol.PresenceUpdatePayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid presence payload")
		return
	}

	// Update user status
	c.User.Status = payload.Status
	c.User.StatusText = payload.StatusText
	c.User.UpdatedAt = time.Now()

	// Save to database
	if err := h.UpdateUserStatus(c.User); err != nil {
		log.Printf("Failed to update user status: %v", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to update status")
		return
	}

	// Broadcast to all servers
	h.hub.BroadcastPresenceUpdate(c.User, c.ServerIDs)
}

// HandleRequestGuild handles a request for server data
func (h *Handlers) HandleRequestGuild(c *Client, msg *protocol.Message) {
	var payload struct {
		ServerID uuid.UUID `json:"server_id"`
	}
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request payload")
		return
	}

	// Check if user is member of this server
	isMember := false
	for _, sid := range c.ServerIDs {
		if sid == payload.ServerID {
			isMember = true
			break
		}
	}

	if !isMember {
		c.sendError(protocol.ErrorCodeForbidden, "Not a member of this server")
		return
	}

	// Get server data
	server, err := h.db.GetServerByID(payload.ServerID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Server not found")
		return
	}

	// Get channels
	channels, err := h.db.GetServerChannels(payload.ServerID)
	if err != nil {
		log.Printf("Failed to get channels: %v", err)
	}

	// Get roles
	roles, err := h.db.GetServerRoles(payload.ServerID)
	if err != nil {
		log.Printf("Failed to get roles: %v", err)
	}

	// Get members
	members, err := h.db.GetServerMembers(payload.ServerID)
	if err != nil {
		log.Printf("Failed to get members: %v", err)
	}

	// Send server create event with full data
	guildData := map[string]interface{}{
		"server":   server,
		"channels": channels,
		"roles":    roles,
		"members":  members,
	}

	c.SendDispatch(protocol.EventServerCreate, guildData)
}

// HandleJoinServer handles a user joining a server via invite
func (h *Handlers) HandleJoinServer(c *Client, inviteCode string) error {
	// TODO: Implement invite validation and server join logic
	return nil
}

// HandleCreateChannel handles channel creation
func (h *Handlers) HandleCreateChannel(c *Client, serverID uuid.UUID, name string, channelType models.ChannelType) (*models.Channel, error) {
	// Check permissions
	// TODO: Implement permission checking

	// Create channel
	var channel *models.Channel
	switch channelType {
	case models.ChannelTypeText:
		channel = models.NewTextChannel(serverID, name)
	case models.ChannelTypeVoice:
		channel = models.NewVoiceChannel(serverID, name)
	case models.ChannelTypeCategory:
		channel = models.NewCategory(serverID, name)
	default:
		return nil, errors.New("invalid channel type")
	}

	// Save to database
	if err := h.db.CreateChannel(channel); err != nil {
		return nil, err
	}

	// Broadcast to server
	h.hub.BroadcastToServer(serverID, protocol.EventChannelCreate, &protocol.ChannelCreatePayload{
		Channel: channel,
	}, nil)

	return channel, nil
}

// HandleDeleteMessage handles message deletion
func (h *Handlers) HandleDeleteMessage(c *Client, messageID, channelID uuid.UUID) error {
	// TODO: Implement permission checking and message deletion
	// For now, just broadcast the delete event

	payload := &protocol.MessageDeletePayload{
		ID:        messageID,
		ChannelID: channelID,
	}

	h.hub.BroadcastToChannel(channelID, protocol.EventMessageDelete, payload, nil)
	return nil
}

// HandleReaction handles adding/removing reactions
func (h *Handlers) HandleReaction(c *Client, messageID, channelID uuid.UUID, emoji string, add bool) error {
	payload := &protocol.ReactionPayload{
		UserID:    c.UserID,
		ChannelID: channelID,
		MessageID: messageID,
		Emoji:     emoji,
	}

	eventType := protocol.EventMessageReactionAdd
	if !add {
		eventType = protocol.EventMessageReactionRemove
	}

	h.hub.BroadcastToChannel(channelID, eventType, payload, nil)
	return nil
}

// hashToken creates a SHA-256 hash of a token
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// CreateAuthToken generates a new authentication token for a user
func (h *Handlers) CreateAuthToken(userID uuid.UUID, ipAddress, userAgent string) (string, error) {
	// Generate a random token
	token := uuid.New().String() + uuid.New().String()
	tokenHash := hashToken(token)

	// Token expires in 30 days
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	_, err := h.db.CreateSession(userID, tokenHash, ipAddress, userAgent, expiresAt)
	if err != nil {
		return "", err
	}

	return token, nil
}

// RevokeAuthToken revokes an authentication token
func (h *Handlers) RevokeAuthToken(token string) error {
	tokenHash := hashToken(token)
	return h.db.DeleteSession(tokenHash)
}

// RevokeAllUserTokens revokes all tokens for a user
func (h *Handlers) RevokeAllUserTokens(userID uuid.UUID) error {
	return h.db.DeleteUserSessions(userID)
}
