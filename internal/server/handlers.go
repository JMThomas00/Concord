package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/plugins"
	"github.com/concord-chat/concord/internal/protocol"
)

// Handlers contains methods for handling client messages
type Handlers struct {
	db            *database.DB
	hub           *Hub
	typingManager *TypingManager
	stats         *StatsTracker
	plugins       *plugins.Manager
}

// NewHandlers creates a new Handlers instance
func NewHandlers(db *database.DB, hub *Hub, stats *StatsTracker, pluginManager *plugins.Manager) *Handlers {
	h := &Handlers{
		db:      db,
		hub:     hub,
		stats:   stats,
		plugins: pluginManager,
	}
	h.typingManager = NewTypingManager(hub)
	return h
}

// PluginChannelKindInfos converts every discovered plugin's declared channel
// kinds into the client-facing shape sent at READY, so the client can render
// and offer plugin channel types generically without any compiled-in
// knowledge of which plugins are installed.
func (h *Handlers) PluginChannelKindInfos() []protocol.PluginChannelKindInfo {
	var infos []protocol.PluginChannelKindInfo
	for _, manifest := range h.plugins.Registry().All() {
		for _, ck := range manifest.ChannelKinds {
			info := protocol.PluginChannelKindInfo{
				PluginID:    manifest.Plugin.ID,
				Kind:        ck.Kind,
				DisplayName: ck.DisplayName,
				Icon:        ck.Icon,
				RemotePane:  ck.RemotePane,
			}
			for _, f := range ck.CreateFields {
				info.CreateFields = append(info.CreateFields, protocol.PluginField{
					Key: f.Key, Label: f.Label, Type: f.Type, Options: f.Options,
					Default: f.Default, Required: f.Required,
				})
			}
			infos = append(infos, info)
		}
	}
	return infos
}

// AuthenticatePlugin validates a plugin process's token (issued by
// plugins.Manager at spawn time) and returns its service-account user.
// Unlike Authenticate, there's no session/ban/timeout check — a plugin's
// identity is Concord-issued and process-scoped, not a login.
func (h *Handlers) AuthenticatePlugin(token string) (*models.User, string, error) {
	installed, err := h.plugins.AuthenticateToken(token)
	if err != nil {
		return nil, "", err
	}
	user, err := h.db.GetUserByID(installed.ServiceUserID)
	if err != nil {
		return nil, "", fmt.Errorf("plugin service account not found: %w", err)
	}
	return user, installed.ID, nil
}

// Authenticate validates a token and returns the associated user
func (h *Handlers) Authenticate(token string) (*models.User, []uuid.UUID, error) {
	// Hash the token to look up the session
	tokenHash := hashToken(token)
	AuthLog.Debug("Authenticating token", "token_prefix", token[:min(8, len(token))], "hash_prefix", tokenHash[:16])

	userID, err := h.db.GetSessionByToken(tokenHash)
	if err != nil {
		AuthLog.Warn("Session not found", "hash_prefix", tokenHash[:16], "error", err)
		return nil, nil, errors.New("invalid or expired token")
	}
	AuthLog.Debug("Session found", "user_id", userID)

	user, err := h.db.GetUserByID(userID)
	if err != nil {
		AuthLog.Error("User not found", "user_id", userID, "error", err)
		return nil, nil, errors.New("user not found")
	}
	AuthLog.Info("User authenticated", "user_id", user.ID, "username", user.Username, "discriminator", user.Discriminator)

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

// hasChannelPermission computes whether userID currently has perm in a
// specific channel — server-owner bypass, then base role permissions, then
// the channel's own permission overwrites applied in member > role >
// everyone priority (models.PermissionCalculator.ComputeOverwrites). Unlike
// checkPermission (server-wide roles only), this is what channel-scoped
// checks like Send Messages need, since a channel overwrite can grant or
// revoke a permission a member's roles alone wouldn't decide.
func (h *Handlers) hasChannelPermission(userID uuid.UUID, channel *models.Channel, perm models.Permission) error {
	server, err := h.db.GetServerByID(channel.ServerID)
	if err != nil {
		return errors.New("server not found")
	}
	if server.OwnerID == userID {
		return nil
	}

	member, err := h.db.GetServerMember(channel.ServerID, userID)
	if err != nil {
		return errors.New("not a member of this server")
	}

	allRoles, err := h.db.GetServerRoles(channel.ServerID)
	if err != nil {
		return errors.New("failed to load server roles")
	}

	var everyoneRole *models.Role
	memberRoles := make([]*models.Role, 0, len(member.RoleIDs))
	for _, role := range allRoles {
		if role.IsDefault {
			everyoneRole = role
		}
		for _, rid := range member.RoleIDs {
			if role.ID == rid {
				memberRoles = append(memberRoles, role)
				break
			}
		}
	}
	if everyoneRole == nil {
		return errors.New("server has no @everyone role")
	}

	calc := models.NewPermissionCalculator(server.OwnerID, everyoneRole)
	base := calc.ComputeBasePermissions(member, memberRoles)
	effective := calc.ComputeOverwrites(base, member, channel)

	if effective&perm == 0 {
		return errors.New("insufficient permissions")
	}
	return nil
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

	// Fetch the channel once and reuse it for every check below (lock,
	// permission-overwrite, @everyone) — this used to be fetched twice.
	channel, err := h.db.GetChannelByID(payload.ChannelID)

	// Check if channel is locked (requires ManageMessages permission to post)
	if err == nil && channel.IsLocked && channel.ServerID != uuid.Nil {
		// Check if user has ManageMessages permission to bypass lock
		if err := h.checkPermission(c.UserID, channel.ServerID, models.PermissionManageMessages); err != nil {
			c.sendError(protocol.ErrorCodeForbidden, "This channel is locked. Only users with Manage Messages permission can post.")
			return
		}
	}

	// Channel permission-overwrite check (member > role > everyone priority,
	// see PermissionCalculator.ComputeOverwrites). Skipped for plugin
	// service-account senders: they're never added via db.AddServerMember
	// (see internal/plugins/manager.go), so hasChannelPermission's
	// GetServerMember lookup would always fail for them — a check meant to
	// gate human members must not block a plugin posting its own replies.
	if !c.IsPlugin && err == nil && channel.ServerID != uuid.Nil {
		if permErr := h.hasChannelPermission(c.UserID, channel, models.PermissionSendMessages); permErr != nil {
			c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to send messages in this channel")
			return
		}
	}

	if len(payload.Content) > 2000 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Message content too long (max 2000 characters)")
		return
	}

	// Peer-to-peer attachments: v1 supports at most one per message. The
	// server only ever stores this small manifest -- file bytes transfer
	// directly between clients later, over OpFileTransferSignal.
	if len(payload.Attachments) > 1 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Only one attachment per message is supported")
		return
	}
	var attachments []models.Attachment
	if len(payload.Attachments) == 1 {
		att := payload.Attachments[0]
		if att.Filename == "" || att.Size <= 0 || att.ContentHash == "" {
			c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid attachment manifest")
			return
		}
		if !c.IsPlugin && err == nil && channel.ServerID != uuid.Nil {
			if permErr := h.hasChannelPermission(c.UserID, channel, models.PermissionAttachFiles); permErr != nil {
				c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to attach files in this channel")
				return
			}
		}
		if att.ID == uuid.Nil {
			att.ID = uuid.New()
		}
		att.SenderID = c.UserID // never trust the client to claim another sender
		attachments = []models.Attachment{att}
	}

	// Create the message
	newMsg := models.NewMessage(payload.ChannelID, c.UserID, payload.Content)
	if payload.ReplyToID != nil {
		newMsg.ReplyToID = payload.ReplyToID
	}
	newMsg.Attachments = attachments

	// Check @everyone permission
	if newMsg.MentionEveryone && err == nil && channel.ServerID != uuid.Nil {
		// Check if user has permission to mention everyone
		if err := h.checkPermission(c.UserID, channel.ServerID, models.PermissionMentionEveryone); err != nil {
			c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to mention @everyone")
			return
		}
	}

	// Save to database
	if err := h.db.CreateMessage(newMsg); err != nil {
		MsgLog.Error("Failed to save message", "message_id", newMsg.ID, "channel_id", payload.ChannelID, "error", err)
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

	if channel != nil {
		h.relayMessageToPlugins(channel, newMsg, c)
	}

	// Record message stats
	if h.stats != nil {
		h.stats.RecordMessage()
	}

	MsgLog.Info("Message sent", "channel_id", payload.ChannelID, "author", c.User.Username, "message_id", newMsg.ID)
}

// relayMessageToPlugins delivers a just-created message to any plugin
// service-account connection that needs to see it but was never a
// BroadcastToChannel recipient — plugin connections skip Hub.JoinChannel
// entirely (see identifyAsPlugin), so BroadcastToChannel above never reaches
// them. Two independent, additive conditions, both using the same targeted
// h.hub.SendToUser pattern HandleCreateChannel already relies on for the
// identical EventChannelCreate gap:
//
//  1. Owned-channel mode: channel.Type == ChannelTypePlugin — deliver to the
//     one plugin that owns this channel, so an "AI Passthrough" dedicated
//     channel sees every message posted into it.
//  2. Mention mode: any OTHER running plugin that has opted into mention
//     relay via its server_config_field (mention_enabled/mention_trigger)
//     and whose trigger word appears in the message — lets a plugin respond
//     to e.g. "@burt" typed in an ordinary text channel it doesn't own.
//
// Messages authored by a plugin's own service account never trigger mention
// mode (skipped entirely when c.IsPlugin) — a deliberate v1 choice that
// makes bot-to-bot relay loops structurally impossible rather than merely
// rate-limited away.
func (h *Handlers) relayMessageToPlugins(channel *models.Channel, message *models.Message, c *Client) {
	responsePayload := &protocol.MessageCreatePayload{Message: message, Author: c.User}

	delivered := make(map[string]bool)

	if channel.Type == models.ChannelTypePlugin && channel.PluginID != "" {
		if serviceUserID, err := h.plugins.ServiceUserIDFor(channel.PluginID); err == nil {
			if err := h.hub.SendToUser(serviceUserID, protocol.EventMessageCreate, responsePayload); err != nil {
				MsgLog.Warn("Failed to relay message to owning plugin", "plugin_id", channel.PluginID, "channel_id", channel.ID, "error", err)
			}
			delivered[channel.PluginID] = true
		}
	}

	if c.IsPlugin {
		return
	}
	for _, manifest := range h.plugins.Registry().All() {
		pluginID := manifest.Plugin.ID
		if delivered[pluginID] {
			continue // already got it above, e.g. posted in its own dedicated channel
		}
		cfg, err := h.db.GetPluginServerConfig(pluginID)
		if err != nil || cfg["mention_enabled"] != "true" {
			continue
		}
		trigger := cfg["mention_trigger"]
		if trigger == "" || !mentionsTrigger(message.Content, trigger) {
			continue
		}
		serviceUserID, err := h.plugins.ServiceUserIDFor(pluginID)
		if err != nil {
			continue // plugin not currently running
		}
		if err := h.hub.SendToUser(serviceUserID, protocol.EventMessageCreate, responsePayload); err != nil {
			MsgLog.Warn("Failed to relay mention to plugin", "plugin_id", pluginID, "channel_id", channel.ID, "error", err)
		}
	}
}

// mentionsTrigger reports whether content contains an @-mention of trigger
// as a whole word — e.g. trigger "burt" matches "@burt" but not "@burton",
// so a plain strings.Contains isn't enough.
func mentionsTrigger(content, trigger string) bool {
	pattern := `(?i)@` + regexp.QuoteMeta(trigger) + `\b`
	matched, err := regexp.MatchString(pattern, content)
	return err == nil && matched
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
		DBLog.Error("Failed to update user status", "user_id", c.User.ID, "status", payload.Status, "error", err)
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
		DBLog.Error("Failed to get channels", "server_id", payload.ServerID, "error", err)
	}

	// Get roles
	roles, err := h.db.GetServerRoles(payload.ServerID)
	if err != nil {
		DBLog.Error("Failed to get roles", "server_id", payload.ServerID, "error", err)
	}

	// Get members
	members, err := h.db.GetServerMembers(payload.ServerID)
	if err != nil {
		DBLog.Error("Failed to get members", "server_id", payload.ServerID, "error", err)
	}

	// Get voice states
	voiceStates, err := h.db.GetVoiceStatesForServer(payload.ServerID)
	if err != nil {
		DBLog.Error("Failed to get voice states", "server_id", payload.ServerID, "error", err)
	}

	// Collect user IDs for member display
	userIDs := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		userIDs = append(userIDs, m.UserID)
	}
	users, _ := h.db.GetUsersByIDs(userIDs)

	guildPayload := &protocol.ServerCreatePayload{
		Server:      server,
		Channels:    channels,
		Members:     members,
		Roles:       roles,
		Users:       users,
		VoiceStates: voiceStates,
	}

	c.SendDispatch(protocol.EventServerCreate, guildPayload)
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
	case models.ChannelTypePlugin:
		kindDef, ok := h.plugins.Registry().Lookup(req.PluginID, req.PluginChannelKind)
		if !ok {
			c.sendError(protocol.ErrorCodeInvalidPayload, "Unknown plugin channel kind")
			return
		}
		for _, f := range kindDef.CreateFields {
			if f.Required && req.PluginConfig[f.Key] == "" {
				c.sendError(protocol.ErrorCodeInvalidPayload, fmt.Sprintf("Missing required field %q", f.Label))
				return
			}
		}
		channel = models.NewPluginChannel(req.ServerID, req.Name, req.PluginID, req.PluginChannelKind)
	default:
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid channel type")
		return
	}

	// Set optional fields
	if req.CategoryID != nil {
		// Categories cannot have a parent category - reject the request
		if channel.Type == models.ChannelTypeCategory && *req.CategoryID != uuid.Nil {
			c.sendError(protocol.ErrorCodeInvalidPayload, "Channel groups cannot be nested inside other channel groups")
			return
		}
		channel.CategoryID = *req.CategoryID
	}
	channel.Position = req.Position
	if req.MaxUsers > 0 {
		channel.MaxUsers = req.MaxUsers
	}

	// Save to database
	if err := h.db.CreateChannel(channel); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to create channel")
		return
	}

	if channel.Type == models.ChannelTypePlugin && len(req.PluginConfig) > 0 {
		if err := h.db.SetPluginChannelConfig(channel.ID, req.PluginConfig); err != nil {
			MsgLog.Error("Failed to persist plugin channel config", "channel_id", channel.ID, "error", err)
		}
	}

	// Auto-join all connected users on this server to the new channel
	onlineUsers := h.hub.GetOnlineUsers(req.ServerID)
	for _, userID := range onlineUsers {
		h.hub.JoinChannel(userID, channel.ID)
	}
	HubLog.Info("Auto-joined users to new channel", "user_count", len(onlineUsers), "channel_name", channel.Name, "channel_id", channel.ID)

	// Broadcast to all server members.
	payload := protocol.ChannelCreatePayload{Channel: channel, PluginConfig: req.PluginConfig}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventChannelCreate, payload, nil)

	// Plugin connections are deliberately never registered in the
	// per-server broadcast list (identifyAsPlugin skips populating
	// ServerIDs, so a plugin isn't a recipient of every server-wide
	// broadcast — role changes, bans, member updates, etc., which a
	// narrowly-scoped service account has no business seeing) — so the
	// BroadcastToServer call above never actually reaches a plugin's own
	// service-account connection, contrary to what this function used to
	// assume. For a newly created plugin channel, directly notify the
	// specific plugin that owns it instead, using the same
	// ServiceUserIDFor+SendToUser pattern every pane-lifecycle handler
	// below already relies on.
	if channel.Type == models.ChannelTypePlugin {
		serviceUserID, err := h.plugins.ServiceUserIDFor(channel.PluginID)
		if err != nil {
			MsgLog.Warn("Plugin not running, could not notify of new channel", "plugin_id", channel.PluginID, "channel_id", channel.ID, "error", err)
		} else if err := h.hub.SendToUser(serviceUserID, protocol.EventChannelCreate, payload); err != nil {
			MsgLog.Error("Failed to notify plugin of its new channel", "plugin_id", channel.PluginID, "channel_id", channel.ID, "error", err)
		}
	}
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
	if req.Type != nil {
		newType := *req.Type
		if newType != models.ChannelTypeText && newType != models.ChannelTypeVoice && newType != models.ChannelTypeCategory {
			c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid channel type")
			return
		}
		// Promoting to a category clears any parent it belongs to
		if newType == models.ChannelTypeCategory && channel.Type != models.ChannelTypeCategory {
			channel.CategoryID = uuid.Nil
		}
		channel.Type = newType
	}
	if req.CategoryID != nil {
		// Safety check: categories cannot have a parent category
		if channel.Type == models.ChannelTypeCategory && *req.CategoryID != uuid.Nil {
			c.sendError(protocol.ErrorCodeInvalidPayload, "Channel groups cannot be nested")
			return
		}
		channel.CategoryID = *req.CategoryID
	}
	if req.Position != nil {
		channel.Position = *req.Position
	}
	if req.SortOrder != nil {
		channel.SortOrder = *req.SortOrder
		channel.Position = *req.SortOrder // Keep in sync
	}
	if req.IsLocked != nil {
		channel.IsLocked = *req.IsLocked
	}
	if req.MaxUsers != nil {
		channel.MaxUsers = *req.MaxUsers
	}

	if channel.Type == models.ChannelTypePlugin && len(req.PluginConfig) > 0 {
		if kindDef, ok := h.plugins.Registry().Lookup(channel.PluginID, channel.PluginChannelKind); ok {
			for _, f := range kindDef.CreateFields {
				if f.Required && req.PluginConfig[f.Key] == "" {
					c.sendError(protocol.ErrorCodeInvalidPayload, fmt.Sprintf("Missing required field %q", f.Label))
					return
				}
			}
		}
		if err := h.db.SetPluginChannelConfig(channel.ID, req.PluginConfig); err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Failed to save plugin channel config")
			return
		}
		channel.PluginConfig = req.PluginConfig
	}

	if err := h.db.UpdateChannel(channel); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to update channel")
		return
	}

	// Broadcast to all server members
	payload := protocol.ChannelUpdatePayload{Channel: channel}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventChannelUpdate, payload, nil)

	// Plugin connections aren't recipients of the per-server broadcast above
	// (see the identical note in HandleCreateChannel) — if this update
	// touched the plugin's own config, notify its service-account connection
	// directly so a running plugin picks up the new values live instead of
	// only on its next restart.
	if channel.Type == models.ChannelTypePlugin && len(req.PluginConfig) > 0 {
		serviceUserID, err := h.plugins.ServiceUserIDFor(channel.PluginID)
		if err != nil {
			MsgLog.Warn("Plugin not running, could not notify of channel config update", "plugin_id", channel.PluginID, "channel_id", channel.ID, "error", err)
		} else if err := h.hub.SendToUser(serviceUserID, protocol.EventChannelUpdate, payload); err != nil {
			MsgLog.Error("Failed to notify plugin of channel config update", "plugin_id", channel.PluginID, "channel_id", channel.ID, "error", err)
		}
	}
}

// HandleUpdateChannelOverwrite sets or clears one role/member permission
// overwrite on a channel. Gated on PermissionManageChannels, same as
// HandleUpdateChannel — editing a channel's per-role/per-member permissions
// is a channel-management action, not a server-wide role-management one.
func (h *Handlers) HandleUpdateChannelOverwrite(c *Client, msg *protocol.Message) {
	var req protocol.UpdateChannelOverwriteRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}

	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageChannels); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	if req.TargetType != "role" && req.TargetType != "member" {
		c.sendError(protocol.ErrorCodeInvalidPayload, "target_type must be \"role\" or \"member\"")
		return
	}

	channel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Channel not found")
		return
	}
	if channel.ServerID != req.ServerID {
		c.sendError(protocol.ErrorCodeForbidden, "Channel belongs to different server")
		return
	}

	if req.Delete {
		if err := h.db.DeleteChannelPermissionOverwrite(req.ChannelID, req.TargetID); err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Failed to delete overwrite")
			return
		}
	} else {
		ow := models.PermissionOverwrite{ID: req.TargetID, Type: req.TargetType, Allow: req.Allow, Deny: req.Deny}
		if err := h.db.SetChannelPermissionOverwrite(req.ChannelID, ow); err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Failed to save overwrite")
			return
		}
	}

	// Re-fetch so the broadcast carries the channel's full, current overwrite
	// list (GetChannelByID populates PermissionOverwrites) rather than a
	// stale copy from before this edit.
	channel, err = h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to reload channel")
		return
	}

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

	// If deleting a category, get children first (before DB delete removes them)
	var childIDs []uuid.UUID
	if channel.Type == models.ChannelTypeCategory {
		childIDs, err = h.db.GetChildChannelIDs(req.ChannelID)
		if err != nil {
			DBLog.Warn("Could not fetch child channels for category", "category_id", req.ChannelID, "error", err)
			// Don't fail - the DB cascade will still handle deletion
		}
	}

	if err := h.db.DeleteChannel(req.ChannelID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to delete channel")
		return
	}

	// Broadcast delete events for child channels first
	for _, childID := range childIDs {
		childPayload := protocol.ChannelDeletePayload{
			ChannelID: childID,
			ServerID:  req.ServerID,
			Type:      models.ChannelTypeText, // Children are never categories (after migration)
		}
		h.hub.BroadcastToServer(req.ServerID, protocol.EventChannelDelete, childPayload, nil)
	}

	// Broadcast delete event for the category itself
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

	MsgLog.Debug("Requesting messages", "user_id", c.UserID, "channel_id", req.ChannelID, "limit", req.Limit)

	// Get messages from database (filtered by user for whispers)
	messages, err := h.db.GetChannelMessages(req.ChannelID, req.Limit, nil, c.UserID)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to retrieve messages")
		MsgLog.Error("Failed to get channel messages", "channel_id", req.ChannelID, "error", err)
		return
	}

	MsgLog.Debug("Messages retrieved", "count", len(messages), "channel_id", req.ChannelID)

	// Build MessageDisplay array with author info
	var displayMessages []*protocol.MessageDisplay
	for _, dbMsg := range messages {
		var author *models.User
		var recipient *models.User

		// System messages don't have authors
		if dbMsg.Type == models.MessageTypeSystem {
			author = nil // System message
		} else {
			// Get author from database for regular messages
			var err error
			author, err = h.db.GetUserByID(dbMsg.AuthorID)
			if err != nil {
				MsgLog.Error("Failed to get author for message", "message_id", dbMsg.ID, "author_id", dbMsg.AuthorID, "error", err)
				continue
			}
		}

		// For whispers, get recipient info
		if dbMsg.IsWhisper && dbMsg.RecipientID != nil {
			var err error
			recipient, err = h.db.GetUserByID(*dbMsg.RecipientID)
			if err != nil {
				MsgLog.Warn("Failed to get recipient for whisper", "message_id", dbMsg.ID, "recipient_id", *dbMsg.RecipientID, "error", err)
				// Continue anyway, whisper will just not show recipient name
			}
		}

		displayMessages = append(displayMessages, &protocol.MessageDisplay{
			Message:   dbMsg,
			Author:    author,
			Recipient: recipient,
		})
	}

	// Get all pinned messages for this channel (separate query to ensure we get them all)
	pinnedMessages, err := h.db.GetPinnedMessages(req.ChannelID)
	if err != nil {
		MsgLog.Warn("Failed to get pinned messages", "channel_id", req.ChannelID, "error", err)
		pinnedMessages = nil // Continue without pinned messages
	}

	// Send history via OpDispatch
	payload := &protocol.MessageHistoryPayload{
		ChannelID:      req.ChannelID,
		Messages:       displayMessages,
		HasMore:        len(messages) == req.Limit, // Simple pagination check
		PinnedMessages: pinnedMessages,
	}

	h.hub.SendToUser(c.UserID, protocol.EventMessagesHistory, payload)
}

// HandleDeleteMessage handles message deletion
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

	// Check if removing admin role from last admin (unless --force is used)
	if role.Permissions&models.PermissionAdministrator != 0 && !req.Force {
		// Count how many members have admin permissions
		members, err := h.db.GetServerMembers(req.ServerID)
		if err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Failed to check admin count")
			return
		}
		adminCount := 0
		for _, member := range members {
			memberRoles, err := h.db.GetMemberRoles(member.UserID, req.ServerID)
			if err != nil {
				continue
			}
			for _, r := range memberRoles {
				if r.Permissions&models.PermissionAdministrator != 0 {
					adminCount++
					break
				}
			}
		}
		// If this is the last admin, block the removal
		if adminCount <= 1 {
			c.sendError(protocol.ErrorCodeForbidden, "⚠️ Cannot remove the last admin. Use '/role remove @user Admin --force' to override.")
			return
		}
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

	DBLog.Debug("HandleKickMember", "target", target.Username, "channelID", req.ChannelID, "channelID==Nil", req.ChannelID == uuid.Nil)

	// Send warning message to the channel where the command was issued (if specified)
	if req.ChannelID != uuid.Nil {
		warningMsg := fmt.Sprintf("⚠️ %s will be kicked in 5 seconds...", target.Username)
		h.sendSystemMessage(req.ChannelID, warningMsg)
	} else {
		DBLog.Debug("HandleKickMember: skipping warning message, channelID is nil")
	}

	// Wait 5 seconds
	time.Sleep(5 * time.Second)

	// Increment kick count (member stays in server_members table for tracking)
	if err := h.db.IncrementMemberKickCount(req.UserID, req.ServerID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to update kick count")
		return
	}

	// Get updated member and broadcast update (don't remove from server_members)
	member, err := h.db.GetServerMember(req.ServerID, req.UserID)
	if err == nil {
		roles, _ := h.db.GetMemberRoles(req.ServerID, req.UserID)
		DBLog.Debug("HandleKickMember: broadcasting update", "IsBanned", member.IsBanned, "KickCount", member.KickCount)
		updatePayload := &protocol.ServerMemberUpdatePayload{
			ServerID: req.ServerID,
			Member:   member,
			User:     target,
			Roles:    roles,
		}
		h.hub.BroadcastToServer(req.ServerID, protocol.EventServerMemberUpdate, updatePayload, nil)
	} else {
		DBLog.Debug("HandleKickMember: error getting updated member", "error", err)
	}

	// Send funny system message to the channel where the command was issued (if specified)
	if req.ChannelID != uuid.Nil {
		kickMsg := getRandomMessage(kickMessages, target.Username)
		h.sendSystemMessage(req.ChannelID, kickMsg)
	}

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

	// Send warning message to the channel where the command was issued
	warningMsg := fmt.Sprintf("⚠️ %s will be banned in 5 seconds...", target.Username)
	h.sendSystemMessage(req.ChannelID, warningMsg)

	// Wait 5 seconds
	time.Sleep(5 * time.Second)

	if err := h.db.AddBan(req.ServerID, req.UserID, c.UserID, req.Reason); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to ban member")
		return
	}

	// Mark member as banned (don't remove from server_members)
	if err := h.db.SetMemberBanned(req.UserID, req.ServerID, true); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to update ban status")
		return
	}

	// Get updated member and broadcast update (not removal)
	member, err := h.db.GetServerMember(req.ServerID, req.UserID)
	if err == nil {
		// Populate full member data for client display
		roles, _ := h.db.GetMemberRoles(req.ServerID, req.UserID)
		updatePayload := &protocol.ServerMemberUpdatePayload{
			ServerID: req.ServerID,
			Member:   member,
			User:     target,
			Roles:    roles,
		}
		h.hub.BroadcastToServer(req.ServerID, protocol.EventServerMemberUpdate, updatePayload, nil)
	}

	// Send funny ban message to the channel where the command was issued
	if req.ChannelID != uuid.Nil {
		banMsg := getRandomMessage(banMessages, target.Username)
		h.sendSystemMessage(req.ChannelID, banMsg)
	}

	// Disconnect the banned user
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

	// Send funny mute/unmute message to the channel where the command was issued
	target, _ := h.db.GetUserByID(req.UserID)
	if target != nil {
		var muteMsg string
		if req.Mute {
			muteMsg = getRandomMessage(muteMessages, target.Username)
			// Append duration info if specified
			if req.Duration > 0 {
				muteMsg += fmt.Sprintf(" (%d minutes)", req.Duration)
			}
		} else {
			muteMsg = getRandomMessage(unmuteMessages, target.Username)
		}
		h.sendSystemMessage(req.ChannelID, muteMsg)
	}

	// If duration is specified, add timed mute
	if req.Mute && req.Duration > 0 {
		DBLog.Info("Adding timed mute", "user_id", req.UserID, "duration", req.Duration, "channel_id", req.ChannelID)
		if err := h.db.AddMute(req.ServerID, req.UserID, req.ChannelID, c.UserID, req.Duration); err != nil {
			DBLog.Error("Failed to add timed mute", "server_id", req.ServerID, "user_id", req.UserID, "duration", req.Duration, "error", err)
		}
	} else if !req.Mute {
		// Remove any active timed mute when unmuting
		_ = h.db.RemoveMute(req.ServerID, req.UserID)
	}

	h.broadcastMemberUpdate(req.ServerID, req.UserID)
}

// HandleTimeoutMember temporarily bans a member for X minutes (requires PermissionKickMembers).
func (h *Handlers) HandleTimeoutMember(c *Client, msg *protocol.Message) {
	var req protocol.TimeoutMemberRequest
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

	// Send warning message to the channel where the command was issued
	warningMsg := fmt.Sprintf("⚠️ %s will be timed out in 5 seconds...", target.Username)
	h.sendSystemMessage(req.ChannelID, warningMsg)

	// Wait 5 seconds
	time.Sleep(5 * time.Second)

	// Add timeout to database
	if err := h.db.AddTimeout(req.ServerID, req.UserID, req.ChannelID, c.UserID, req.Duration, req.Reason); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to add timeout")
		return
	}

	// Remove from server
	if err := h.db.RemoveServerMember(req.UserID, req.ServerID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to remove member")
		return
	}

	// Broadcast member removal
	removePayload := &protocol.ServerMemberRemovePayload{ServerID: req.ServerID, User: target}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventServerMemberRemove, removePayload, nil)

	// Send timeout message to the channel where the command was issued
	timeoutMsg := getRandomMessage(timeoutMessages, target.Username) + fmt.Sprintf(" (%d minutes)", req.Duration)
	h.sendSystemMessage(req.ChannelID, timeoutMsg)

	// Force-close the connection
	h.hub.mu.RLock()
	timedOut, ok := h.hub.clients[req.UserID]
	h.hub.mu.RUnlock()
	if ok {
		timedOut.conn.Close()
	}
}

// HandleUnbanMember unbans a member from the server (requires PermissionBanMembers).
func (h *Handlers) HandleUnbanMember(c *Client, msg *protocol.Message) {
	var req protocol.UnbanMemberRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionBanMembers); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Find the banned user by username
	target, err := h.db.GetBannedUserByUsername(req.ServerID, req.Username)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, fmt.Sprintf("No banned user found with username %s", req.Username))
		return
	}

	// Remove the ban
	if err := h.db.RemoveBan(req.ServerID, target.ID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to remove ban")
		return
	}

	// Mark member as unbanned
	if err := h.db.SetMemberBanned(target.ID, req.ServerID, false); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to update ban status")
		return
	}

	// Get updated member and broadcast update
	member, err := h.db.GetServerMember(req.ServerID, target.ID)
	if err == nil {
		// Populate full member data for client display
		roles, _ := h.db.GetMemberRoles(req.ServerID, target.ID)
		updatePayload := &protocol.ServerMemberUpdatePayload{
			ServerID: req.ServerID,
			Member:   member,
			User:     target,
			Roles:    roles,
		}
		h.hub.BroadcastToServer(req.ServerID, protocol.EventServerMemberUpdate, updatePayload, nil)
	}

	// Send system message to the channel where the command was issued
	if req.ChannelID != uuid.Nil {
		unbanMsg := fmt.Sprintf("✅ %s has been unbanned. Welcome back!", target.Username)
		h.sendSystemMessage(req.ChannelID, unbanMsg)
	}
}

// HandleCreateRole handles role creation requests
func (h *Handlers) HandleCreateRole(c *Client, msg *protocol.Message) {
	var req protocol.CreateRoleRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}

	// Check permission: must have ManageRoles or be server owner
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageRoles); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Validate role name
	if len(req.Name) < 1 || len(req.Name) > 32 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Role name must be 1-32 characters")
		return
	}

	// Check if role name already exists (case-insensitive)
	existingRoles, err := h.db.GetServerRoles(req.ServerID)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to check existing roles")
		return
	}
	for _, role := range existingRoles {
		if role.Name == req.Name {
			c.sendError(protocol.ErrorCodeInvalidPayload, fmt.Sprintf("Role %q already exists", req.Name))
			return
		}
	}

	// Calculate position (highest non-@everyone position + 10)
	maxPosition := 0
	for _, role := range existingRoles {
		if !role.IsDefault && role.Position > maxPosition {
			maxPosition = role.Position
		}
	}
	position := maxPosition + 10

	// Create role in database
	role := &models.Role{
		ID:            uuid.New(),
		ServerID:      req.ServerID,
		Name:          req.Name,
		Color:         req.Color,
		Permissions:   models.Permission(req.Permissions),
		Position:      position,
		DisplayOrder:  0, // Default
		IsHoisted:     req.IsHoisted,
		IsMentionable: req.IsMentionable,
		IsDefault:     false,
	}
	if req.DisplayOrder != nil {
		role.DisplayOrder = *req.DisplayOrder
	}

	if err := h.db.CreateRole(role); err != nil {
		DBLog.Error("Failed to create role", "role_name", req.Name, "server_id", req.ServerID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to create role")
		return
	}

	// Broadcast EventRoleCreate to all server members
	h.hub.BroadcastToServer(req.ServerID, protocol.EventRoleCreate, role, nil)

	// Send system message to the channel where the command was issued
	createMsg := fmt.Sprintf("✅ Role %q created", role.Name)
	h.sendSystemMessage(req.ChannelID, createMsg)
}

// HandleUpdateRole updates an existing role
func (h *Handlers) HandleUpdateRole(c *Client, msg *protocol.Message) {
	var req protocol.UpdateRoleRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}

	// Check permission: must have ManageRoles or be server owner
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageRoles); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Get role from DB
	role, err := h.db.GetRoleByID(req.RoleID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Role not found")
		return
	}

	// Verify role belongs to correct server
	if role.ServerID != req.ServerID {
		c.sendError(protocol.ErrorCodeNotFound, "Role not found")
		return
	}

	// Prevent editing @everyone
	if role.IsDefault {
		c.sendError(protocol.ErrorCodeForbidden, "Cannot edit @everyone role")
		return
	}

	// Validate name
	if len(req.Name) < 1 || len(req.Name) > 32 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Role name must be 1-32 characters")
		return
	}

	// Check for duplicate name (excluding current role)
	existingRoles, err := h.db.GetServerRoles(req.ServerID)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to check existing roles")
		return
	}
	for _, existingRole := range existingRoles {
		if existingRole.Name == req.Name && existingRole.ID != req.RoleID {
			c.sendError(protocol.ErrorCodeInvalidPayload, fmt.Sprintf("Role %q already exists", req.Name))
			return
		}
	}

	// Check if permission change would leave zero admins
	newPerms := models.Permission(req.Permissions)
	if err := h.db.CanModifyRolePermissions(req.RoleID, req.ServerID, newPerms); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Update role fields
	role.Name = req.Name
	role.Permissions = newPerms
	role.Color = req.Color
	role.IsHoisted = req.IsHoisted
	role.IsMentionable = req.IsMentionable
	if req.DisplayOrder != nil {
		role.DisplayOrder = *req.DisplayOrder
	}

	if err := h.db.UpdateRole(role); err != nil {
		DBLog.Error("Failed to update role", "role_id", req.RoleID, "role_name", role.Name, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to update role")
		return
	}

	// Broadcast update
	h.hub.BroadcastToServer(req.ServerID, protocol.EventRoleUpdate, role, nil)

	// Send system message
	updateMsg := fmt.Sprintf("✅ Role %q updated", role.Name)
	h.sendSystemMessage(req.ChannelID, updateMsg)
}

// HandleDeleteRole deletes a role
func (h *Handlers) HandleDeleteRole(c *Client, msg *protocol.Message) {
	var req protocol.DeleteRoleRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid payload")
		return
	}

	// Check permission: must have ManageRoles or be server owner
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageRoles); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Get role for validation and system message
	role, err := h.db.GetRoleByID(req.RoleID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Role not found")
		return
	}

	// Verify role belongs to correct server
	if role.ServerID != req.ServerID {
		c.sendError(protocol.ErrorCodeNotFound, "Role not found")
		return
	}

	// Check if deletion would leave zero admins
	if err := h.db.CanDeleteRole(req.RoleID, req.ServerID); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// DeleteRole checks IsDefault internally
	if err := h.db.DeleteRole(req.RoleID, req.ServerID); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	// Broadcast deletion
	deleteData := map[string]interface{}{
		"server_id": req.ServerID,
		"role_id":   req.RoleID,
	}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventRoleDelete, deleteData, nil)

	// Send system message
	deleteMsg := fmt.Sprintf("✅ Role %q deleted", role.Name)
	h.sendSystemMessage(req.ChannelID, deleteMsg)
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

	// Check if recipient is online
	if !h.hub.IsUserOnline(payload.TargetUserID) {
		c.sendError(protocol.ErrorCodeNotFound, "User is not online")
		return
	}

	// Create and save whisper message to database
	whisperMsg := &models.Message{
		ID:          uuid.New(),
		ChannelID:   payload.ChannelID,
		AuthorID:    c.UserID,
		Content:     payload.Content,
		Type:        models.MessageTypeDefault,
		CreatedAt:   time.Now(),
		IsWhisper:   true,
		RecipientID: &payload.TargetUserID,
	}

	if err := h.db.CreateMessage(whisperMsg); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to save whisper")
		MsgLog.Error("Failed to save whisper", "message_id", whisperMsg.ID, "sender_id", c.UserID, "recipient_id", payload.TargetUserID, "error", err)
		return
	}

	// Get recipient user info
	recipientUser, err := h.db.GetUserByID(payload.TargetUserID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Recipient user not found")
		DBLog.Error("Failed to get recipient user", "recipient_id", payload.TargetUserID, "error", err)
		return
	}

	dispatch := &protocol.WhisperCreatePayload{
		FromUser:  c.User,
		ToUser:    recipientUser,
		ChannelID: payload.ChannelID,
		Content:   payload.Content,
		Timestamp: whisperMsg.CreatedAt,
	}

	// Deliver to recipient
	_ = h.hub.SendToUser(payload.TargetUserID, protocol.EventWhisperCreate, dispatch)
	// Echo to sender as well
	_ = h.hub.SendToUser(c.UserID, protocol.EventWhisperCreate, dispatch)
}

// HandlePinMessage pins a message in a channel
func (h *Handlers) HandlePinMessage(c *Client, msg *protocol.Message) {
	var req protocol.PinMessageRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid pin request")
		return
	}

	// Get the message to pin
	message, err := h.db.GetMessage(req.MessageID)
	if err != nil || message == nil {
		c.sendError(protocol.ErrorCodeNotFound, "Message not found")
		return
	}

	// Verify channel matches
	if message.ChannelID != req.ChannelID {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Channel ID mismatch")
		return
	}

	// TODO: Add permission check for MANAGE_MESSAGES
	// For now, allow anyone to pin

	// Update database to set message as pinned
	if err := h.db.SetMessagePinned(req.MessageID, true); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to pin message")
		MsgLog.Error("Failed to pin message", "message_id", req.MessageID, "channel_id", req.ChannelID, "error", err)
		return
	}

	// Update the message object to reflect pinned status
	message.IsPinned = true

	// Broadcast pin event to all users in the channel
	payload := &protocol.MessagePinPayload{
		ChannelID: req.ChannelID,
		Message:   message,
		PinnedBy:  c.User,
		Timestamp: time.Now(),
	}

	h.hub.BroadcastToChannel(req.ChannelID, protocol.EventMessagePin, payload, nil)
}

// HandleUnpinMessage unpins a message from a channel
func (h *Handlers) HandleUnpinMessage(c *Client, msg *protocol.Message) {
	var req protocol.UnpinMessageRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid unpin request")
		return
	}

	// Get the message to unpin
	message, err := h.db.GetMessage(req.MessageID)
	if err != nil || message == nil {
		c.sendError(protocol.ErrorCodeNotFound, "Message not found")
		return
	}

	// Verify channel matches
	if message.ChannelID != req.ChannelID {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Channel ID mismatch")
		return
	}

	// TODO: Add permission check for MANAGE_MESSAGES
	// For now, allow anyone to unpin

	// Update database to set message as unpinned
	if err := h.db.SetMessagePinned(req.MessageID, false); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to unpin message")
		MsgLog.Error("Failed to unpin message", "message_id", req.MessageID, "channel_id", req.ChannelID, "error", err)
		return
	}

	// Update the message object to reflect unpinned status
	message.IsPinned = false

	// Broadcast unpin event to all users in the channel
	payload := &protocol.MessagePinPayload{
		ChannelID: req.ChannelID,
		Message:   message,
		PinnedBy:  c.User,
		Timestamp: time.Now(),
	}

	h.hub.BroadcastToChannel(req.ChannelID, protocol.EventMessageUnpin, payload, nil)
}

// HandleEditMessage handles editing an existing message
func (h *Handlers) HandleEditMessage(c *Client, msg *protocol.Message) {
	var payload protocol.EditMessagePayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid edit message payload")
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

	// Get original message
	origMsg, err := h.db.GetMessage(payload.MessageID)
	if err != nil || origMsg == nil {
		c.sendError(protocol.ErrorCodeNotFound, "Message not found")
		return
	}

	// Permission check: user can only edit their own messages, or admin/mod with PermissionManageMessages
	if origMsg.AuthorID != c.UserID {
		// Check if user has PermissionManageMessages
		channel, err := h.db.GetChannelByID(payload.ChannelID)
		if err != nil || channel.ServerID == uuid.Nil {
			c.sendError(protocol.ErrorCodeForbidden, "You can only edit your own messages")
			return
		}

		if err := h.checkPermission(c.UserID, channel.ServerID, models.PermissionManageMessages); err != nil {
			c.sendError(protocol.ErrorCodeForbidden, "You can only edit your own messages")
			return
		}
	}

	// Update message
	origMsg.Content = payload.Content
	origMsg.EditedAt = new(time.Time)
	*origMsg.EditedAt = time.Now()

	if err := h.db.UpdateMessage(origMsg); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to update message")
		MsgLog.Error("Failed to update message", "message_id", payload.MessageID, "error", err)
		return
	}

	// Broadcast update event
	updatePayload := &protocol.MessageUpdatePayload{
		ID:        payload.MessageID,
		ChannelID: payload.ChannelID,
		Content:   payload.Content,
		EditedAt:  origMsg.EditedAt,
	}

	h.hub.BroadcastToChannel(payload.ChannelID, protocol.EventMessageUpdate, updatePayload, nil)
}

// HandleDeleteMessage handles deleting a message (soft-delete with permission check)
func (h *Handlers) HandleDeleteMessage(c *Client, msg *protocol.Message) {
	var payload protocol.DeleteMessagePayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid delete message payload")
		return
	}

	// Get original message
	origMsg, err := h.db.GetMessage(payload.MessageID)
	if err != nil || origMsg == nil {
		c.sendError(protocol.ErrorCodeNotFound, "Message not found")
		return
	}

	// Permission check
	isOwn := origMsg.AuthorID == c.UserID
	withinWindow := time.Since(origMsg.CreatedAt) < 24*time.Hour

	if isOwn && !withinWindow {
		// Check if user has PermissionManageMessages
		channel, err := h.db.GetChannelByID(payload.ChannelID)
		if err != nil || channel.ServerID == uuid.Nil {
			c.sendError(protocol.ErrorCodeForbidden, "You can only delete your own messages within 24 hours")
			return
		}

		if err := h.checkPermission(c.UserID, channel.ServerID, models.PermissionManageMessages); err != nil {
			c.sendError(protocol.ErrorCodeForbidden, "You can only delete your own messages within 24 hours")
			return
		}
	} else if !isOwn {
		// Check if user has PermissionManageMessages
		channel, err := h.db.GetChannelByID(payload.ChannelID)
		if err != nil || channel.ServerID == uuid.Nil {
			c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to delete this message")
			return
		}

		if err := h.checkPermission(c.UserID, channel.ServerID, models.PermissionManageMessages); err != nil {
			c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to delete this message")
			return
		}
	}

	// Soft-delete message
	if err := h.db.SoftDeleteMessage(payload.MessageID, c.UserID); err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to delete message")
		MsgLog.Error("Failed to delete message", "message_id", payload.MessageID, "error", err)
		return
	}

	// Broadcast delete event
	deletePayload := &protocol.MessageDeletePayload{
		ID:        payload.MessageID,
		ChannelID: payload.ChannelID,
	}

	h.hub.BroadcastToChannel(payload.ChannelID, protocol.EventMessageDelete, deletePayload, nil)
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

// Funny system message templates for moderation actions
var (
	muteMessages = []string{
		"🔇 %s has been muted. Enjoy the silence!",
		"🤐 %s's voice has been temporarily confiscated.",
		"🙊 %s is now on a mandatory vacation from talking.",
		"⛔ %s has been sentenced to quiet time.",
		"🚫 %s discovered the 'speak less' achievement!",
		"🔕 %s's keyboard privileges have been revoked.",
		"😶 %s is taking an involuntary vow of silence.",
		"🤫 %s has entered stealth mode (not by choice).",
		"📴 %s's megaphone has been disabled.",
		"🙈 %s is practicing the art of saying nothing.",
		"⏸️ %s has been put on pause.",
		"🎭 %s is now a mime (temporarily).",
	}

	unmuteMessages = []string{
		"🔊 %s can speak again! Brace yourselves.",
		"📢 %s's voice has been returned. Everyone hide.",
		"🗣️ %s is back! The silence was nice while it lasted.",
		"🎤 %s's ban on fun has been lifted.",
		"✅ %s has rejoined the conversation. May the gods have mercy.",
		"🔓 %s is unleashed. Ear plugs recommended.",
		"🎉 %s can talk again! (Unfortunately)",
		"🌟 %s's mute sentence has been served.",
		"🔔 %s's voice is back from vacation.",
		"📣 %s is free to resume their TED talk.",
		"🎊 %s has been unmuted. Prepare for chaos.",
		"💬 %s's typing privileges restored!",
	}

	kickMessages = []string{
		"👢 %s has been kicked. They'll be back... probably.",
		"🚪 %s was shown the exit. Don't let the door hit you!",
		"✈️ %s has been yeeted from the server.",
		"🌪️ %s was swept away by the ban hammer (lite edition).",
		"🎪 %s left the chat (not voluntarily).",
		"🏃 %s is speedrunning a server exit.",
		"💨 %s vanished. Poof!",
		"🎯 %s hit the eject button (with help).",
		"🌊 %s was washed away.",
		"⚡ %s has been disconnected from reality.",
		"🎢 %s took the express lane out.",
		"🚀 %s has left orbit.",
		"🍃 %s was blown away by moderator wind.",
	}

	banMessages = []string{
		"🔨 %s has been banned. Farewell, sweet chaos.",
		"⛔ %s has been permanently archived.",
		"🚫 %s's membership has expired (forever).",
		"💀 %s has been sent to the shadow realm.",
		"🗿 %s is now a legend (banned legends count, right?).",
		"🌑 %s entered the void and won't be returning.",
		"📛 %s has been blacklisted from existence here.",
		"❌ %s discovered what 'consequences' means.",
		"🏴‍☠️ %s walked the plank.",
		"⚰️ %s's server adventure has concluded.",
		"🔐 %s's access has been permanently revoked.",
		"🎭 %s's final curtain call.",
		"🌪️ %s was tornadoed into the ban dimension.",
	}

	timeoutMessages = []string{
		"⏰ %s has been timed out. Time for some reflection.",
		"⏸️ %s is taking a mandatory break.",
		"🚨 %s has been sent to timeout. Think about what you did!",
		"⌛ %s is in timeout. The clock is ticking...",
		"🕐 %s has been temporarily evicted.",
		"⏲️ %s is on a forced vacation.",
		"🔄 %s needs a cooldown period.",
		"⛔ %s's server privileges have been paused.",
		"📵 %s is disconnected (temporarily).",
		"🎮 %s has been put in the penalty box.",
		"⚠️ %s is serving time.",
		"🏖️ %s is on involuntary leave.",
	}

	timeoutExpiryMessages = []string{
		"⏰ %s's timeout has expired. They're back!",
		"🔓 %s has been released from timeout. Behave this time!",
		"✅ %s's sentence is served. Welcome back!",
		"⌛ %s's time is up. Second chances activated!",
		"🎉 %s is no longer timed out. Play nice!",
		"🔔 %s's timeout expired. Let's see how long this lasts...",
		"🎊 %s is back from the penalty box!",
		"🌟 %s's mandatory reflection period is over.",
		"🔄 %s has returned from their involuntary vacation.",
		"✨ %s's timeout ended. Everyone be nice... or else!",
		"🕐 %s is back! The timeout gods have spoken.",
		"🎭 %s's intermission is over. Back to the show!",
		"🚪 %s has been let back in. Don't make us regret it!",
	}
)

// sendSystemMessage broadcasts a system message to a channel
func (h *Handlers) sendSystemMessage(channelID uuid.UUID, content string) {
	// Create and save system message to database for persistence
	msg := models.NewSystemMessage(channelID, content, models.MessageTypeSystem)
	if err := h.db.CreateMessage(msg); err != nil {
		MsgLog.Error("Failed to save system message", "channel_id", channelID, "error", err)
		// Continue anyway to broadcast the message even if DB save fails
	}

	// Broadcast to all users in the channel
	payload := &protocol.SystemMessagePayload{
		ChannelID: channelID,
		Content:   content,
		Timestamp: msg.CreatedAt,
	}
	h.hub.BroadcastToChannel(channelID, protocol.EventSystemMessage, payload, nil)
}

// getRandomMessage returns a random message from the slice formatted with username
func getRandomMessage(messages []string, username string) string {
	if len(messages) == 0 {
		return fmt.Sprintf("Moderation action performed on %s", username)
	}
	msg := messages[rand.Intn(len(messages))]
	return fmt.Sprintf(msg, username)
}

// --- Message Retention Policy Handlers ---

// HandleGetRetentionPolicy retrieves the effective retention policy for a channel or server
func (h *Handlers) HandleGetRetentionPolicy(c *Client, msg *protocol.Message) {
	var req protocol.GetRetentionPolicyRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request payload")
		return
	}

	// Permission check: ManageMessages
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageMessages); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Insufficient permissions")
		return
	}

	// Get effective policy
	var policy *models.MessageRetentionPolicy
	var err error
	if req.ChannelID != nil {
		policy, err = h.db.GetRetentionPolicy(req.ServerID, *req.ChannelID)
	} else {
		policy, err = h.db.GetRetentionPolicyDirect(req.ServerID, nil)
	}

	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to retrieve retention policy")
		return
	}

	// Send response (policy can be nil if not configured)
	// When requesting server default, also include all channel overrides
	payload := &protocol.RetentionPolicyUpdatePayload{Policy: policy}
	if req.ChannelID == nil {
		overrides, _ := h.db.ListChannelOverrides(req.ServerID)
		payload.ChannelOverrides = overrides
	}
	h.hub.SendToUser(c.UserID, protocol.EventRetentionPolicyUpdate, payload)
}

// HandleSetRetentionPolicy creates or updates a retention policy
func (h *Handlers) HandleSetRetentionPolicy(c *Client, msg *protocol.Message) {
	var req protocol.SetRetentionPolicyRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request payload")
		return
	}

	// Permission check: ManageMessages
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageMessages); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Insufficient permissions")
		return
	}

	// Build/update policy
	policy := models.NewRetentionPolicy(req.ServerID, c.UserID)
	policy.ChannelID = req.ChannelID
	policy.TimeRetentionDays = req.TimeRetentionDays
	policy.SystemTimeRetentionDays = req.SystemTimeRetentionDays
	policy.MaxMessageCount = req.MaxMessageCount
	policy.UpdatedAt = time.Now()

	if err := h.db.UpsertRetentionPolicy(policy); err != nil {
		DBLog.Error("Failed to upsert retention policy", "server_id", req.ServerID, "channel_id", req.ChannelID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to save retention policy")
		return
	}

	// Broadcast update to all users on this server (include all overrides for full picture)
	overrides, _ := h.db.ListChannelOverrides(req.ServerID)
	payload := &protocol.RetentionPolicyUpdatePayload{Policy: policy, ChannelOverrides: overrides}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventRetentionPolicyUpdate, payload, nil)
}

// HandleDeleteRetentionPolicy removes a channel-specific override
func (h *Handlers) HandleDeleteRetentionPolicy(c *Client, msg *protocol.Message) {
	var req protocol.DeleteRetentionPolicyRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request payload")
		return
	}

	// Permission check: ManageMessages
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageMessages); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Insufficient permissions")
		return
	}

	if err := h.db.DeleteRetentionPolicy(req.ServerID, req.ChannelID); err != nil {
		DBLog.Error("Failed to delete retention policy", "server_id", req.ServerID, "channel_id", req.ChannelID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to delete retention policy")
		return
	}

	// Broadcast updated state (server default + remaining overrides)
	serverDefault, _ := h.db.GetRetentionPolicyDirect(req.ServerID, nil)
	remainingOverrides, _ := h.db.ListChannelOverrides(req.ServerID)
	payload := &protocol.RetentionPolicyUpdatePayload{Policy: serverDefault, ChannelOverrides: remainingOverrides}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventRetentionPolicyUpdate, payload, nil)
}

// HandlePruneMessages executes manual message pruning
func (h *Handlers) HandlePruneMessages(c *Client, msg *protocol.Message) {
	var req protocol.PruneMessagesRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request payload")
		return
	}

	// Permission check: Administrator required for manual pruning
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionAdministrator); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Administrator permission required")
		return
	}

	// Execute pruning
	var results map[uuid.UUID]*models.PruneStats
	var err error
	if req.ChannelID != nil {
		stats, pruneErr := h.db.PruneChannelMessages(req.ServerID, *req.ChannelID)
		if pruneErr != nil {
			err = pruneErr
		} else {
			results = map[uuid.UUID]*models.PruneStats{*req.ChannelID: stats}
		}
	} else {
		results, err = h.db.PruneServerMessages(req.ServerID)
	}

	if err != nil {
		DBLog.Error("Failed to prune messages", "server_id", req.ServerID, "channel_id", req.ChannelID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to prune messages")
		return
	}

	// Record history for each channel
	totalDeleted := 0
	for channelID, stats := range results {
		history := &models.MessagePruneHistory{
			ID:              uuid.New(),
			ServerID:        req.ServerID,
			ChannelID:       &channelID,
			MessagesDeleted: stats.TotalDeleted,
			TimeBasedCount:  stats.TimeBasedDeleted,
			CountBasedCount: stats.CountBasedDeleted,
			TriggerType:     "manual",
			TriggeredBy:     &c.UserID,
			ExecutedAt:      time.Now(),
			DurationMs:      stats.DurationMs,
		}
		if err := h.db.RecordPruneHistory(history); err != nil {
			DBLog.Error("Failed to record prune history", "server_id", req.ServerID, "channel_id", channelID, "error", err)
		}
		totalDeleted += stats.TotalDeleted
	}

	// Broadcast results
	payload := &protocol.MessagesPrunedPayload{
		ServerID:     req.ServerID,
		ChannelStats: results,
		TriggerType:  "manual",
		TriggeredBy:  &c.UserID,
		ExecutedAt:   time.Now(),
		TotalDeleted: totalDeleted,
	}
	h.hub.BroadcastToServer(req.ServerID, protocol.EventMessagesPruned, payload, nil)
}

// HandleAssignTitle assigns or clears a custom title for a server member
func (h *Handlers) HandleAssignTitle(c *Client, msg *protocol.Message) {
	var req protocol.AssignTitleRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		Logger.Error("Failed to parse assign title request", "error", err)
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}

	// Permission check: ManageNicknames
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageNicknames); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to manage titles")
		return
	}

	// Validate title length
	if len(req.Title) > 50 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Title too long (max 50 characters)")
		return
	}

	// Update server member title
	if err := h.db.UpdateServerMemberTitle(req.ServerID, req.UserID, req.Title); err != nil {
		DBLog.Error("Failed to update member title", "server_id", req.ServerID, "user_id", req.UserID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to update title")
		return
	}

	// Broadcast update to all clients in the server
	payload := &protocol.AssignTitlePayload{
		ServerID: req.ServerID,
		UserID:   req.UserID,
		Title:    req.Title,
	}
	if err := h.hub.BroadcastToServer(req.ServerID, protocol.EventTitleUpdate, payload, nil); err != nil {
		HubLog.Error("Failed to broadcast title update", "server_id", req.ServerID, "user_id", req.UserID, "error", err)
	}

	Logger.Info("Title updated", "user_id", req.UserID, "server_id", req.ServerID, "title", req.Title)
}

// HandleSetNickname sets a member's nickname (models.ServerMember.Nickname —
// distinct from CustomTitle, which HandleAssignTitle above manages).
// Renaming yourself requires only PermissionChangeNickname (granted to
// @everyone by default, see models.NewEveryoneRole, but revocable per-role
// like any other permission); renaming someone else requires the stronger
// PermissionManageNicknames, same gate HandleAssignTitle uses.
func (h *Handlers) HandleSetNickname(c *Client, msg *protocol.Message) {
	var req protocol.SetNicknameRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}

	targetID := req.UserID
	if targetID == uuid.Nil {
		targetID = c.UserID
	}

	requiredPerm := models.PermissionChangeNickname
	permErrMsg := "You don't have permission to change your nickname"
	if targetID != c.UserID {
		requiredPerm = models.PermissionManageNicknames
		permErrMsg = "You don't have permission to manage nicknames"
	}
	if err := h.checkPermission(c.UserID, req.ServerID, requiredPerm); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, permErrMsg)
		return
	}

	if len(req.Nickname) > 32 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Nickname too long (max 32 characters)")
		return
	}

	if err := h.db.UpdateServerMemberNickname(req.ServerID, targetID, req.Nickname); err != nil {
		DBLog.Error("Failed to update member nickname", "server_id", req.ServerID, "user_id", targetID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to update nickname")
		return
	}

	payload := &protocol.NicknamePayload{ServerID: req.ServerID, UserID: targetID, Nickname: req.Nickname}
	if err := h.hub.BroadcastToServer(req.ServerID, protocol.EventNicknameUpdate, payload, nil); err != nil {
		HubLog.Error("Failed to broadcast nickname update", "server_id", req.ServerID, "user_id", targetID, "error", err)
	}

	Logger.Info("Nickname updated", "user_id", targetID, "server_id", req.ServerID, "nickname", req.Nickname)
}

// ── Voice Handlers ─────────────────────────────────────────────────────────────

// HandleVoiceStateUpdate handles a client joining, leaving, or updating voice state.
// OpVoiceStateUpdate (6): C→S
func (h *Handlers) HandleVoiceStateUpdate(c *Client, msg *protocol.Message) {
	var req protocol.VoiceStateUpdatePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid voice state payload")
		return
	}

	// Leaving voice
	if req.ChannelID == nil {
		if err := h.db.ClearVoiceState(c.UserID, req.ServerID); err != nil {
			DBLog.Error("Failed to clear voice state", "user_id", c.UserID, "error", err)
		}
		h.hub.LeaveVoiceChannel(c.UserID)

		// Broadcast departure to server
		leavePayload := &protocol.VoiceStateEventPayload{
			UserID:   c.UserID,
			ServerID: req.ServerID,
			User:     c.User,
		}
		_ = h.hub.BroadcastToServer(req.ServerID, protocol.EventVoiceStateUpdate, leavePayload, nil)
		Logger.Info("User left voice", "user_id", c.UserID, "server_id", req.ServerID)
		return
	}

	// Joining or updating — check capacity
	channel, err := h.db.GetChannelByID(*req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Voice channel not found")
		return
	}
	if channel.MaxUsers > 0 {
		current, _ := h.db.GetVoiceStatesForChannel(*req.ChannelID)
		// Allow if already in this channel (update, not new join)
		alreadyIn := false
		for _, vs := range current {
			if vs.UserID == c.UserID {
				alreadyIn = true
				break
			}
		}
		if !alreadyIn && len(current) >= channel.MaxUsers {
			c.sendError(protocol.ErrorCodeForbidden, "Voice channel is full")
			return
		}
	}

	// Persist voice state
	if err := h.db.SetVoiceState(c.UserID, req.ServerID, *req.ChannelID, req.IsSelfMuted, req.IsSelfDeafened); err != nil {
		DBLog.Error("Failed to set voice state", "user_id", c.UserID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to join voice channel")
		return
	}
	h.hub.JoinVoiceChannel(c.UserID, req.ServerID, *req.ChannelID)

	// Broadcast new state to all server members
	joinPayload := &protocol.VoiceStateEventPayload{
		UserID:         c.UserID,
		ServerID:       req.ServerID,
		ChannelID:      req.ChannelID,
		IsSelfMuted:    req.IsSelfMuted,
		IsSelfDeafened: req.IsSelfDeafened,
		User:           c.User,
	}
	_ = h.hub.BroadcastToServer(req.ServerID, protocol.EventVoiceStateUpdate, joinPayload, nil)

	// Send the joining client their WebRTC connection info
	voiceServerPayload := &protocol.VoiceServerUpdatePayload{
		ServerID:  req.ServerID,
		ChannelID: *req.ChannelID,
		Token:     uuid.New().String(), // ephemeral session token
		Endpoint:  "", // Phase 4: WebRTC endpoint from server config
		STUNUrls:  []string{"stun:stun.l.google.com:19302"},
	}
	h.hub.SendToUser(c.UserID, protocol.EventVoiceServerUpdate, voiceServerPayload)

	Logger.Info("User joined voice", "user_id", c.UserID, "channel_id", *req.ChannelID)
}

// HandleVoiceSignal relays a WebRTC SDP/ICE candidate between two clients.
// The server acts as a dumb relay; it never inspects the SDP content.
// OpVoiceSignal (45): C→S→C
func (h *Handlers) HandleVoiceSignal(c *Client, msg *protocol.Message) {
	var req protocol.VoiceSignalPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid voice signal payload")
		return
	}

	// Build relay payload that includes the source user ID so the recipient
	// knows whose offer/answer/candidate this is.
	relay := &protocol.VoiceSignalRelayPayload{
		SourceUserID: c.UserID,
		ChannelID:    req.ChannelID,
		Type:         req.Type,
		SDP:          req.SDP,
		Candidate:    req.Candidate,
	}
	h.hub.SendToUser(req.TargetUserID, protocol.EventVoiceSignal, relay)
}

// HandleFileTransferSignal relays a peer-to-peer file transfer signal
// (download request, then WebRTC SDP/ICE) between two clients. Mirrors
// HandleVoiceSignal's dumb-relay shape exactly -- the server never inspects
// or stores file content, only these small signaling payloads.
// OpFileTransferSignal (59): C→S→C
func (h *Handlers) HandleFileTransferSignal(c *Client, msg *protocol.Message) {
	var req protocol.FileTransferSignalPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid file transfer signal payload")
		return
	}

	// A "request" starts a new transfer and needs the target (the file's
	// original sender) to actually be connected -- unlike voice, where both
	// peers already share a channel-join lifecycle, a download can be
	// attempted long after the sharer sent the message and went offline.
	if req.Type == "request" && !h.hub.IsUserOnline(req.TargetUserID) {
		relay := &protocol.FileTransferSignalRelayPayload{
			SourceUserID: req.TargetUserID,
			AttachmentID: req.AttachmentID,
			Type:         "offline",
		}
		h.hub.SendToUser(c.UserID, protocol.EventFileTransferSignal, relay)
		return
	}

	relay := &protocol.FileTransferSignalRelayPayload{
		SourceUserID: c.UserID,
		AttachmentID: req.AttachmentID,
		Type:         req.Type,
		SDP:          req.SDP,
		Candidate:    req.Candidate,
	}
	h.hub.SendToUser(req.TargetUserID, protocol.EventFileTransferSignal, relay)
}

// HandleVoiceSpeaking handles speaking state notification and broadcasts it to
// other members of the same voice channel.
// OpVoiceSpeaking (46): C→S
func (h *Handlers) HandleVoiceSpeaking(c *Client, msg *protocol.Message) {
	var req protocol.VoiceSpeakingPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid speaking payload")
		return
	}

	event := &protocol.VoiceSpeakingEventPayload{
		UserID:     c.UserID,
		ChannelID:  req.ChannelID,
		IsSpeaking: req.IsSpeaking,
	}
	_ = h.hub.BroadcastToChannel(req.ChannelID, protocol.EventVoiceSpeaking, event, &c.UserID)
}

// HandleVoiceServerMute handles an admin server-muting or deafening a user in voice.
// OpVoiceServerMute (47): C→S
func (h *Handlers) HandleVoiceServerMute(c *Client, msg *protocol.Message) {
	var req protocol.VoiceServerMutePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid voice server mute payload")
		return
	}

	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionMuteMembers); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to mute members")
		return
	}

	if err := h.db.SetServerVoiceMute(req.UserID, req.ServerID, req.Muted, req.Deafened); err != nil {
		DBLog.Error("Failed to set server voice mute", "user_id", req.UserID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to update voice mute")
		return
	}

	// Broadcast updated state to server so clients suppress/restore audio
	event := &protocol.VoiceStateEventPayload{
		UserID:           req.UserID,
		ServerID:         req.ServerID,
		IsServerMuted:    req.Muted,
		IsServerDeafened: req.Deafened,
	}
	_ = h.hub.BroadcastToServer(req.ServerID, protocol.EventVoiceStateUpdate, event, nil)

	Logger.Info("Server voice mute applied", "target_user_id", req.UserID, "muted", req.Muted, "deafened", req.Deafened)
}

// HandleMoveVoice handles an admin force-moving a user to a different voice channel.
// OpMoveVoice (48): C→S
func (h *Handlers) HandleMoveVoice(c *Client, msg *protocol.Message) {
	var req protocol.MoveVoicePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid move-voice payload")
		return
	}

	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionMuteMembers); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "You don't have permission to move members")
		return
	}

	// Destination must be a voice channel.
	destChannel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Destination channel not found")
		return
	}
	if destChannel.Type != models.ChannelTypeVoice {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Destination must be a voice channel")
		return
	}

	// Enforce capacity on destination.
	if destChannel.MaxUsers > 0 {
		current, _ := h.db.GetVoiceStatesForChannel(req.ChannelID)
		if len(current) >= destChannel.MaxUsers {
			c.sendError(protocol.ErrorCodeForbidden, "Destination voice channel is full")
			return
		}
	}

	// Broadcast leave from current channel (ChannelID nil = left voice).
	leavePayload := &protocol.VoiceStateEventPayload{
		UserID:   req.UserID,
		ServerID: req.ServerID,
	}
	_ = h.hub.BroadcastToServer(req.ServerID, protocol.EventVoiceStateUpdate, leavePayload, nil)

	// Update DB and hub to new channel.
	if err := h.db.SetVoiceState(req.UserID, req.ServerID, req.ChannelID, false, false); err != nil {
		DBLog.Error("Failed to set voice state for move", "user_id", req.UserID, "error", err)
		c.sendError(protocol.ErrorCodeServerError, "Failed to move user")
		return
	}
	h.hub.JoinVoiceChannel(req.UserID, req.ServerID, req.ChannelID)

	// Broadcast join in new channel.
	joinPayload := &protocol.VoiceStateEventPayload{
		UserID:    req.UserID,
		ServerID:  req.ServerID,
		ChannelID: &req.ChannelID,
	}
	_ = h.hub.BroadcastToServer(req.ServerID, protocol.EventVoiceStateUpdate, joinPayload, nil)

	// Send the moved user a new VoiceServerUpdate so their client reconnects.
	voiceServerPayload := &protocol.VoiceServerUpdatePayload{
		ServerID:  req.ServerID,
		ChannelID: req.ChannelID,
		Token:     uuid.New().String(),
		Endpoint:  "",
		STUNUrls:  []string{"stun:stun.l.google.com:19302"},
	}
	h.hub.SendToUser(req.UserID, protocol.EventVoiceServerUpdate, voiceServerPayload)

	Logger.Info("User moved in voice", "admin_id", c.UserID, "target_id", req.UserID, "channel_id", req.ChannelID)
}

// handleVoiceLeave is invoked by the hub's voice-leave callback when a client
// disconnects while still in a voice channel. It cleans up DB state and broadcasts
// the departure event to the server.
func (h *Handlers) handleVoiceLeave(userID, serverID, channelID uuid.UUID) {
	if err := h.db.ClearVoiceState(userID, serverID); err != nil {
		DBLog.Error("Failed to clear voice state on disconnect", "user_id", userID, "error", err)
	}

	leavePayload := &protocol.VoiceStateEventPayload{
		UserID:   userID,
		ServerID: serverID,
		// ChannelID nil → client left voice
	}
	_ = h.hub.BroadcastToServer(serverID, protocol.EventVoiceStateUpdate, leavePayload, nil)
	Logger.Info("Voice state cleared on disconnect", "user_id", userID, "channel_id", channelID)
}

// ── Plugin Platform Handlers ────────────────────────────────────────────────
//
// The Hub relays every plugin pane/event message without parsing it: a human
// client's Enter/Input/Resize/Leave gets routed to the owning plugin's
// service-account connection, and a plugin's Frame push gets routed to the
// one viewer it named. Concord never understands what's inside a frame.

// pluginServiceClient resolves the connected Client for the plugin that owns
// a channel, or sends an error and returns nil if the channel isn't a plugin
// channel or that plugin isn't currently connected.
func (h *Handlers) pluginServiceClient(c *Client, channelID uuid.UUID) *Client {
	channel, err := h.db.GetChannelByID(channelID)
	if err != nil || channel.Type != models.ChannelTypePlugin {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Not a plugin channel")
		return nil
	}
	serviceUserID, err := h.plugins.ServiceUserIDFor(channel.PluginID)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Plugin not installed")
		return nil
	}
	target := h.hub.GetClient(serviceUserID)
	if target == nil {
		c.sendError(protocol.ErrorCodeServerError, "Plugin is not currently running")
		return nil
	}
	return target
}

// HandlePluginPaneEnter relays a viewer opening a plugin channel to that
// plugin's process, stamping ViewerID server-side (never trust the client).
func (h *Handlers) HandlePluginPaneEnter(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneEnterPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	req.ViewerID = c.UserID
	target := h.pluginServiceClient(c, req.ChannelID)
	if target == nil {
		return
	}
	if err := h.hub.SendToUser(target.UserID, protocol.EventPluginPaneEnter, req); err != nil {
		MsgLog.Error("Failed to relay plugin pane enter", "channel_id", req.ChannelID, "error", err)
	}
}

// HandlePluginPaneResize relays a viewport size change.
func (h *Handlers) HandlePluginPaneResize(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneResizePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	req.ViewerID = c.UserID
	target := h.pluginServiceClient(c, req.ChannelID)
	if target == nil {
		return
	}
	if err := h.hub.SendToUser(target.UserID, protocol.EventPluginPaneResize, req); err != nil {
		MsgLog.Error("Failed to relay plugin pane resize", "channel_id", req.ChannelID, "error", err)
	}
}

// HandlePluginPaneInput relays one forwarded keypress to the owning plugin.
func (h *Handlers) HandlePluginPaneInput(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneInputPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	req.ViewerID = c.UserID
	target := h.pluginServiceClient(c, req.ChannelID)
	if target == nil {
		return
	}
	if err := h.hub.SendToUser(target.UserID, protocol.EventPluginPaneInput, req); err != nil {
		MsgLog.Error("Failed to relay plugin pane input", "channel_id", req.ChannelID, "error", err)
	}
}

// HandlePluginPaneLeave relays a viewer navigating away from a plugin channel.
func (h *Handlers) HandlePluginPaneLeave(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneLeavePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	req.ViewerID = c.UserID
	target := h.pluginServiceClient(c, req.ChannelID)
	if target == nil {
		return
	}
	if err := h.hub.SendToUser(target.UserID, protocol.EventPluginPaneLeave, req); err != nil {
		MsgLog.Error("Failed to relay plugin pane leave", "channel_id", req.ChannelID, "error", err)
	}
}

// HandlePluginPaneFrame relays a plugin's rendered frame to the one viewer it
// named. Only a connection that identified as that plugin may push frames.
func (h *Handlers) HandlePluginPaneFrame(c *Client, msg *protocol.Message) {
	if !c.IsPlugin {
		c.sendError(protocol.ErrorCodeForbidden, "Only plugin connections may push frames")
		return
	}
	var req protocol.PluginPaneFramePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if err := h.hub.SendToUser(req.ViewerID, protocol.EventPluginPaneFrame, req); err != nil {
		MsgLog.Error("Failed to relay plugin pane frame", "channel_id", req.ChannelID, "viewer_id", req.ViewerID, "error", err)
	}
}

// HandlePluginEvent handles the generic {plugin_id, kind, payload} envelope.
// Kind == "notify" posts a system message into the plugin's server-admin-
// configured activity channel, reusing sendSystemMessage verbatim — the
// exact function moderation actions already use. Any Kind sent with
// ViewerID set is relayed unchanged to that specific viewer's own client
// via EventPluginEvent — e.g. a plugin telling one viewer's pane to close
// (Kind "leave_pane") — letting a plugin add new viewer-directed signals
// without a protocol change. Anything else is logged and dropped.
func (h *Handlers) HandlePluginEvent(c *Client, msg *protocol.Message) {
	if !c.IsPlugin {
		c.sendError(protocol.ErrorCodeForbidden, "Only plugin connections may send plugin events")
		return
	}
	var req protocol.PluginEventPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	req.PluginID = c.PluginID // never trust a client-claimed plugin id

	if req.Kind != "notify" {
		if req.ViewerID != uuid.Nil {
			if err := h.hub.SendToUser(req.ViewerID, protocol.EventPluginEvent, req); err != nil {
				MsgLog.Error("Failed to relay plugin event to viewer", "plugin_id", req.PluginID, "viewer_id", req.ViewerID, "kind", req.Kind, "error", err)
			}
			return
		}
		MsgLog.Warn("Unhandled plugin event kind", "plugin_id", req.PluginID, "kind", req.Kind)
		return
	}

	var notify protocol.PluginNotifyEventPayload
	if err := json.Unmarshal(req.Payload, &notify); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid notify payload")
		return
	}

	config, err := h.db.GetPluginServerConfig(req.PluginID)
	if err != nil {
		MsgLog.Error("Failed to load plugin server config", "plugin_id", req.PluginID, "error", err)
		return
	}
	channelIDStr := config["activity_notify_channel"]
	if channelIDStr == "" {
		return // Admin hasn't configured a notification channel — silently drop
	}
	channelID, err := uuid.Parse(channelIDStr)
	if err != nil {
		return
	}
	h.sendSystemMessage(channelID, notify.Content)
}

// HandleGetPluginConfig answers the Settings > Plugins list/config request.
func (h *Handlers) HandleGetPluginConfig(c *Client, msg *protocol.Message) {
	var req protocol.PluginConfigGetRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageServer); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	installedList, err := h.db.ListInstalledPlugins()
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to load plugins")
		return
	}

	infos := make([]protocol.PluginInfo, 0, len(installedList))
	for _, installed := range installedList {
		infos = append(infos, h.buildPluginInfo(installed))
	}

	reply, err := protocol.NewMessage(protocol.OpDispatch, protocol.PluginConfigListPayload{Plugins: infos})
	if err == nil {
		reply.Type = protocol.EventPluginConfigUpdate
		c.send <- reply
	}
}

// buildPluginInfo assembles one plugin's Settings > Plugins row: its
// manifest-declared server_config_field definitions plus its currently
// stored values. Factored out of HandleGetPluginConfig so
// HandleSetPluginConfig's self-notify push (below) can build the same shape
// for a single plugin without re-querying every installed plugin.
func (h *Handlers) buildPluginInfo(installed *models.InstalledPlugin) protocol.PluginInfo {
	manifest, _ := h.plugins.Registry().Manifest(installed.ID)
	info := protocol.PluginInfo{
		ID:        installed.ID,
		Name:      installed.Name,
		Version:   installed.Version,
		Enabled:   installed.Enabled,
		Status:    installed.Status,
		LastError: installed.LastError,
	}
	if manifest != nil {
		info.Product = manifest.Plugin.Product
		for _, f := range manifest.ServerConfigFields {
			info.ConfigFields = append(info.ConfigFields, protocol.PluginField{
				Key: f.Key, Label: f.Label, Type: f.Type, Options: f.Options,
				Default: f.Default, Required: f.Required,
			})
		}
	}
	values, err := h.db.GetPluginServerConfig(installed.ID)
	if err == nil {
		info.ConfigValues = values
	}
	return info
}

// HandleSetPluginConfig toggles a plugin's enabled flag and/or updates its
// server-wide config field values from the Settings > Plugins page.
func (h *Handlers) HandleSetPluginConfig(c *Client, msg *protocol.Message) {
	var req protocol.PluginConfigSetRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageServer); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}

	if req.Enabled != nil {
		if err := h.plugins.SetEnabled(req.PluginID, *req.Enabled); err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Failed to update plugin: "+err.Error())
			return
		}
	}
	for key, value := range req.Config {
		if err := h.db.SetPluginServerConfig(req.PluginID, key, value); err != nil {
			MsgLog.Error("Failed to persist plugin server config", "plugin_id", req.PluginID, "key", key, "error", err)
		}
	}

	h.HandleGetPluginConfig(c, msg) // re-send the updated list to the admin who made the change

	// A plugin has no other way to learn its own server_config_field values
	// changed (OpPluginConfigGet is gated behind PermissionManageServer for
	// a human client — there's no analogous self-query a plugin's own
	// connection can make). Push it the same targeted way HandleCreateChannel
	// already notifies a plugin about its own new channel, so e.g. an AI
	// Passthrough install can react live when an admin flips mention_enabled
	// on, rather than only picking it up on its next reconnect.
	if installed, err := h.db.GetInstalledPlugin(req.PluginID); err == nil && installed != nil {
		if serviceUserID, err := h.plugins.ServiceUserIDFor(req.PluginID); err == nil {
			info := h.buildPluginInfo(installed)
			if err := h.hub.SendToUser(serviceUserID, protocol.EventPluginConfigUpdate, protocol.PluginConfigListPayload{Plugins: []protocol.PluginInfo{info}}); err != nil {
				MsgLog.Warn("Failed to push updated config to plugin", "plugin_id", req.PluginID, "error", err)
			}
		}
	}
}

