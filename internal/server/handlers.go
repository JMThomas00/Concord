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
	log.Printf("Authenticating token (first 8 chars): %s..., hash: %s", token[:min(8, len(token))], tokenHash[:16])

	userID, err := h.db.GetSessionByToken(tokenHash)
	if err != nil {
		log.Printf("Session not found for token hash: %s, error: %v", tokenHash[:16], err)
		return nil, nil, errors.New("invalid or expired token")
	}
	log.Printf("Session found for user ID: %s", userID)

	user, err := h.db.GetUserByID(userID)
	if err != nil {
		log.Printf("User not found for ID %s: %v", userID, err)
		return nil, nil, errors.New("user not found")
	}
	log.Printf("User authenticated: ID=%s, Username=%s#%s", user.ID, user.Username, user.Discriminator)

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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// UpdateUserStatus updates a user's status in the database
func (h *Handlers) UpdateUserStatus(user *models.User) error {
	return h.db.UpdateUserStatus(user.ID, user.Status, user.StatusText)
}

// GetUserServers returns all servers a user is a member of
func (h *Handlers) GetUserServers(userID uuid.UUID) ([]*models.Server, error) {
	return h.db.GetUserServers(userID)
}

// checkPermission verifies if a user has a specific permission on a server
func (h *Handlers) checkPermission(userID, serverID uuid.UUID, perm models.Permission) error {
	// Check if user is server owner
	server, err := h.db.GetServerByID(serverID)
	if err != nil {
		return errors.New("server not found")
	}

	if server.OwnerID == userID {
		return nil // Owners have all permissions
	}

	// Check role-based permissions
	member, err := h.db.GetServerMember(serverID, userID)
	if err != nil {
		return errors.New("not a member of this server")
	}

	// Check permissions for each role
	for _, roleID := range member.RoleIDs {
		role, err := h.db.GetRoleByID(roleID)
		if err != nil {
			continue // Skip if role not found
		}

		if role.HasPermission(perm) {
			return nil // User has permission through this role
		}
	}

	return errors.New("insufficient permissions")
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

	// Reject messages from server-muted members
	if len(c.ServerIDs) > 0 {
		member, err := h.db.GetServerMember(c.ServerIDs[0], c.UserID)
		if err == nil && member.IsMuted {
			c.sendError(protocol.ErrorCodeForbidden, "You are muted on this server")
			return
		}
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

// HandleCreateChannel handles channel creation requests
func (h *Handlers) HandleCreateChannel(c *Client, msg *protocol.Message) {
	var req protocol.ChannelCreateRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}

	// Validate input
	if req.Name == "" {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Channel name cannot be empty")
		return
	}
	if len(req.Name) > 100 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Channel name too long (max 100 characters)")
		return
	}

	// Check permissions
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageChannels); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Create channel
	var channel *models.Channel
	switch req.Type {
	case models.ChannelTypeText:
		channel = models.NewTextChannel(req.ServerID, req.Name)
	case models.ChannelTypeVoice:
		channel = models.NewVoiceChannel(req.ServerID, req.Name)
	case models.ChannelTypeCategory:
		channel = models.NewCategory(req.ServerID, req.Name)
	default:
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid channel type")
		return
	}

	// Set optional fields
	if req.CategoryID != nil {
		channel.CategoryID = *req.CategoryID
	}
	channel.Position = req.Position

	// Save to database
	if err := h.db.CreateChannel(channel); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to create channel")
		return
	}

	// Auto-join all connected users on this server to the new channel
	onlineUsers := h.hub.GetOnlineUsers(req.ServerID)
	for _, userID := range onlineUsers {
		h.hub.JoinChannel(userID, channel.ID)
	}
	log.Printf("Auto-joined %d users to new channel %s", len(onlineUsers), channel.Name)

	// Broadcast to all server members
	payload := protocol.ChannelCreatePayload{Channel: channel}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventChannelCreate, payload, nil)
}

// HandleUpdateChannel handles channel update requests
func (h *Handlers) HandleUpdateChannel(c *Client, msg *protocol.Message) {
	var req protocol.ChannelUpdateRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}

	// Check permissions
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageChannels); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Get existing channel
	channel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Channel not found")
		return
	}

	// Verify channel belongs to this server
	if channel.ServerID != req.ServerID {
		c.sendError(protocol.ErrorCodeForbidden, "Channel belongs to different server")
		return
	}

	// Apply updates
	if req.Name != nil {
		if *req.Name == "" {
			c.sendError(protocol.ErrorCodeInvalidPayload, "Channel name cannot be empty")
			return
		}
		channel.Name = *req.Name
	}
	if req.CategoryID != nil {
		channel.CategoryID = *req.CategoryID
	}
	if req.Position != nil {
		channel.Position = *req.Position
	}

	if err := h.db.UpdateChannel(channel); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to update channel")
		return
	}

	// Broadcast to all server members
	payload := protocol.ChannelUpdatePayload{Channel: channel}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventChannelUpdate, payload, nil)
}

// HandleDeleteChannel handles channel deletion requests
func (h *Handlers) HandleDeleteChannel(c *Client, msg *protocol.Message) {
	var req protocol.ChannelDeleteRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}

	// Check permissions
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageChannels); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Get channel to verify it exists and belongs to this server
	channel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Channel not found")
		return
	}

	if channel.ServerID != req.ServerID {
		c.sendError(protocol.ErrorCodeForbidden, "Channel belongs to different server")
		return
	}

	if err := h.db.DeleteChannel(req.ChannelID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to delete channel")
		return
	}

	// Broadcast to all server members
	payload := protocol.ChannelDeletePayload{
		ChannelID: req.ChannelID,
		ServerID:  req.ServerID,
		Type:      channel.Type,
	}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventChannelDelete, payload, nil)
}

// HandleRequestMessages handles message history requests
func (h *Handlers) HandleRequestMessages(c *Client, msg *protocol.Message) {
	var req protocol.MessageHistoryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}

	// Default limit (200 messages for better history visibility)
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 200
	}

	log.Printf("HandleRequestMessages: user=%s, channel=%s, limit=%d", c.UserID, req.ChannelID, req.Limit)

	// Get messages from database
	messages, err := h.db.GetChannelMessages(req.ChannelID, req.Limit, nil)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to retrieve messages")
		log.Printf("Failed to get channel messages: %v", err)
		return
	}

	log.Printf("HandleRequestMessages: found %d messages for channel %s", len(messages), req.ChannelID)

	// Build MessageDisplay array with author info
	var displayMessages []*protocol.MessageDisplay
	for _, dbMsg := range messages {
		// Get author from database
		author, err := h.db.GetUserByID(dbMsg.AuthorID)
		if err != nil {
			log.Printf("Failed to get author for message %s: %v", dbMsg.ID, err)
			continue
		}

		displayMessages = append(displayMessages, &protocol.MessageDisplay{
			Message: dbMsg,
			Author:  author,
		})
	}

	// Send history via OpDispatch
	payload := &protocol.MessageHistoryPayload{
		ChannelID: req.ChannelID,
		Messages:  displayMessages,
		HasMore:   len(messages) == req.Limit, // Simple pagination check
	}

	h.hub.SendToUser(c.UserID, protocol.EventMessagesHistory, payload)
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

// HandleRoleAssign assigns a named role to a member (requires PermissionManageRoles).
func (h *Handlers) HandleRoleAssign(c *Client, msg *protocol.Message) {
	var req protocol.RoleAssignRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageRoles); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	role, err := h.db.GetRoleByName(req.ServerID, req.RoleName)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Role not found")
		return
	}
	if err := h.db.AddMemberRole(req.UserID, req.ServerID, role.ID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to assign role")
		return
	}
	h.broadcastMemberUpdate(req.ServerID, req.UserID)
}

// HandleRoleRemove removes a named role from a member (requires PermissionManageRoles).
func (h *Handlers) HandleRoleRemove(c *Client, msg *protocol.Message) {
	var req protocol.RoleRemoveRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageRoles); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	role, err := h.db.GetRoleByName(req.ServerID, req.RoleName)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Role not found")
		return
	}
	if err := h.db.RemoveMemberRole(req.UserID, req.ServerID, role.ID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to remove role")
		return
	}
	h.broadcastMemberUpdate(req.ServerID, req.UserID)
}

// HandleKickMember removes a member from the server (requires PermissionKickMembers).
func (h *Handlers) HandleKickMember(c *Client, msg *protocol.Message) {
	var req protocol.KickMemberRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionKickMembers); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	target, err := h.db.GetUserByID(req.UserID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "User not found")
		return
	}
	if err := h.db.RemoveServerMember(req.UserID, req.ServerID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to kick member")
		return
	}
	// Notify server of removal
	removePayload := &protocol.ServerMemberRemovePayload{ServerID: req.ServerID, User: target}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventServerMemberRemove, removePayload, nil)
	// Force-close the kicked user's connection
	h.hub.mu.RLock()
	kicked, ok := h.hub.clients[req.UserID]
	h.hub.mu.RUnlock()
	if ok {
		kicked.conn.Close()
	}
}

// HandleBanMember bans a member from the server (requires PermissionBanMembers).
func (h *Handlers) HandleBanMember(c *Client, msg *protocol.Message) {
	var req protocol.BanMemberRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionBanMembers); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	target, err := h.db.GetUserByID(req.UserID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "User not found")
		return
	}
	if err := h.db.AddBan(req.ServerID, req.UserID, c.UserID, req.Reason); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to ban member")
		return
	}
	_ = h.db.RemoveServerMember(req.UserID, req.ServerID)
	removePayload := &protocol.ServerMemberRemovePayload{ServerID: req.ServerID, User: target}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventServerMemberRemove, removePayload, nil)
	h.hub.mu.RLock()
	banned, ok := h.hub.clients[req.UserID]
	h.hub.mu.RUnlock()
	if ok {
		banned.conn.Close()
	}
}

// HandleMuteMember server-mutes or unmutes a member (requires PermissionMuteMembers).
func (h *Handlers) HandleMuteMember(c *Client, msg *protocol.Message) {
	var req protocol.MuteMemberRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionMuteMembers); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	if err := h.db.SetMemberMuted(req.ServerID, req.UserID, req.Mute); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to update mute state")
		return
	}
	h.broadcastMemberUpdate(req.ServerID, req.UserID)
}

// HandleWhisper routes an ephemeral DM to a specific connected user.
func (h *Handlers) HandleWhisper(c *Client, msg *protocol.Message) {
	var payload protocol.WhisperPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid whisper payload")
		return
	}
	if len(payload.Content) == 0 || len(payload.Content) > 2000 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Whisper content must be 1-2000 characters")
		return
	}

	dispatch := &protocol.WhisperCreatePayload{
		FromUser:  c.User,
		Content:   payload.Content,
		Timestamp: time.Now(),
	}

	// Deliver to recipient; if offline, return error
	if !h.hub.IsUserOnline(payload.TargetUserID) {
		c.sendError(protocol.ErrorCodeNotFound, "User is not online")
		return
	}
	_ = h.hub.SendToUser(payload.TargetUserID, protocol.EventWhisperCreate, dispatch)
	// Echo to sender as well
	_ = h.hub.SendToUser(c.UserID, protocol.EventWhisperCreate, dispatch)
}

// broadcastMemberUpdate fetches updated member data and broadcasts EventServerMemberUpdate.
func (h *Handlers) broadcastMemberUpdate(serverID, userID uuid.UUID) {
	member, err := h.db.GetServerMember(serverID, userID)
	if err != nil {
		return
	}
	user, err := h.db.GetUserByID(userID)
	if err != nil {
		return
	}
	roles, err := h.db.GetMemberRoles(serverID, userID)
	if err != nil {
		roles = nil
	}
	payload := &protocol.ServerMemberUpdatePayload{
		ServerID: serverID,
		Member:   member,
		User:     user,
		Roles:    roles,
	}
	h.hub.BroadcastToServer(serverID, protocol.EventServerMemberUpdate, payload, nil)
}
