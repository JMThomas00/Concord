package protocol

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
)

// OpCode represents the type of WebSocket message
type OpCode int

const (
	// Client -> Server operations
	OpIdentify       OpCode = 0  // Initial authentication
	OpHeartbeat      OpCode = 1  // Keep-alive ping
	OpRequestGuild   OpCode = 2  // Request server data
	OpSendMessage    OpCode = 3  // Send a chat message
	OpTypingStart    OpCode = 4  // User started typing
	OpPresenceUpdate OpCode = 5  // Update user status
	OpVoiceStateUpdate OpCode = 6 // Voice channel join/leave (v2)
	OpChannelCreate    OpCode = 7 // Create channel
	OpChannelUpdate    OpCode = 8 // Update channel
	OpChannelDelete    OpCode = 9 // Delete channel
	OpRequestMessages  OpCode = 16 // Request message history
	OpRoleAssign       OpCode = 17 // Assign a role to a member
	OpRoleRemove       OpCode = 18 // Remove a role from a member
	OpKickMember       OpCode = 19 // Kick a member from the server
	OpBanMember        OpCode = 20 // Ban a member from the server
	OpMuteMember       OpCode = 21 // Server-mute a member
	OpWhisper          OpCode = 22 // Send an ephemeral DM to another connected user
	OpPinMessage       OpCode = 23 // Pin a message in a channel
	OpUnpinMessage     OpCode = 24 // Unpin a message from a channel
	OpTimeoutMember    OpCode = 25 // Temporarily ban a member for X minutes
	OpUnbanMember      OpCode = 26 // Unban a member from the server
	OpCreateRole       OpCode = 27 // Create a new role
	OpUpdateRole       OpCode = 28 // Update an existing role
	OpDeleteRole       OpCode = 29 // Delete a role
	OpUnmuteMember     OpCode = 32 // Unmute a server-muted member
	OpEditMessage      OpCode = 30 // Edit message content
	OpDeleteMessage    OpCode = 31 // Delete message (soft-delete)
	OpGetRetentionPolicy    OpCode = 40 // Get retention policy for server/channel
	OpSetRetentionPolicy    OpCode = 41 // Update retention policy
	OpDeleteRetentionPolicy OpCode = 42 // Remove channel override
	OpPruneMessages         OpCode = 43 // Manual prune trigger
	OpAssignTitle           OpCode = 44 // Assign custom title to member

	// Voice operations (v2)
	OpVoiceSignal     OpCode = 45 // WebRTC SDP/ICE relay between clients (C↔S↔C)
	OpVoiceSpeaking   OpCode = 46 // Client reports speaking state (C→S)
	OpVoiceServerMute OpCode = 47 // Admin: server-mute/deafen a user in voice (C→S)
	OpMoveVoice       OpCode = 48 // Admin: force-move a user to another voice channel (C→S)

	// Server -> Client operations
	OpDispatch       OpCode = 10 // Event dispatch (most messages)
	OpHeartbeatAck   OpCode = 11 // Heartbeat acknowledgment
	OpHello          OpCode = 12 // Initial connection info
	OpReady          OpCode = 13 // Successful authentication
	OpInvalidSession OpCode = 14 // Authentication failed
	OpReconnect      OpCode = 15 // Server requests reconnection
)

// EventType represents the type of dispatched event
type EventType string

const (
	// Connection events
	EventReady            EventType = "READY"
	EventResumed          EventType = "RESUMED"
	
	// Server events
	EventServerCreate     EventType = "SERVER_CREATE"
	EventServerUpdate     EventType = "SERVER_UPDATE"
	EventServerDelete     EventType = "SERVER_DELETE"
	EventServerMemberAdd  EventType = "SERVER_MEMBER_ADD"
	EventServerMemberRemove EventType = "SERVER_MEMBER_REMOVE"
	EventServerMemberUpdate EventType = "SERVER_MEMBER_UPDATE"
	
	// Channel events
	EventChannelCreate    EventType = "CHANNEL_CREATE"
	EventChannelUpdate    EventType = "CHANNEL_UPDATE"
	EventChannelDelete    EventType = "CHANNEL_DELETE"
	
	// Message events
	EventMessageCreate    EventType = "MESSAGE_CREATE"
	EventMessageUpdate    EventType = "MESSAGE_UPDATE"
	EventMessageDelete    EventType = "MESSAGE_DELETE"
	EventMessageReactionAdd EventType = "MESSAGE_REACTION_ADD"
	EventMessageReactionRemove EventType = "MESSAGE_REACTION_REMOVE"
	EventMessagesHistory  EventType = "MESSAGES_HISTORY"
	EventMessagePin       EventType = "MESSAGE_PIN"
	EventMessageUnpin     EventType = "MESSAGE_UNPIN"

	// User events
	EventPresenceUpdate   EventType = "PRESENCE_UPDATE"
	EventTypingStart      EventType = "TYPING_START"
	EventTypingStop       EventType = "TYPING_STOP"
	EventUserUpdate       EventType = "USER_UPDATE"
	
	// Whisper events
	EventWhisperCreate    EventType = "WHISPER_CREATE"

	// System message events
	EventSystemMessage    EventType = "SYSTEM_MESSAGE"

	// Role events
	EventRoleCreate       EventType = "ROLE_CREATE"
	EventRoleUpdate       EventType = "ROLE_UPDATE"
	EventRoleDelete       EventType = "ROLE_DELETE"
	EventTitleUpdate      EventType = "TITLE_UPDATE"

	// Moderation events
	EventMemberKicked     EventType = "MEMBER_KICKED"
	EventMemberBanned     EventType = "MEMBER_BANNED"
	EventMemberUnbanned   EventType = "MEMBER_UNBANNED"
	EventMemberMuted      EventType = "MEMBER_MUTED"
	EventMemberUnmuted    EventType = "MEMBER_UNMUTED"

	// Retention policy events
	EventRetentionPolicyUpdate EventType = "RETENTION_POLICY_UPDATE"
	EventMessagesPruned        EventType = "MESSAGES_PRUNED"

	// Voice events (v2)
	EventVoiceStateUpdate  EventType = "VOICE_STATE_UPDATE"
	EventVoiceServerUpdate EventType = "VOICE_SERVER_UPDATE"
	EventVoiceSpeaking     EventType = "VOICE_SPEAKING"
	EventVoiceSignal       EventType = "VOICE_SIGNAL" // S→C relay of a WebRTC SDP/ICE signal
)

// Message represents a WebSocket message envelope
type Message struct {
	Op   OpCode          `json:"op"`
	Data json.RawMessage `json:"d,omitempty"`
	Seq  *int64          `json:"s,omitempty"`  // Sequence number for dispatches
	Type EventType       `json:"t,omitempty"`  // Event type for dispatches
}

// NewMessage creates a new protocol message
func NewMessage(op OpCode, data interface{}) (*Message, error) {
	var rawData json.RawMessage
	if data != nil {
		var err error
		rawData, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}
	return &Message{
		Op:   op,
		Data: rawData,
	}, nil
}

// NewDispatch creates a new dispatch message
func NewDispatch(eventType EventType, seq int64, data interface{}) (*Message, error) {
	msg, err := NewMessage(OpDispatch, data)
	if err != nil {
		return nil, err
	}
	msg.Seq = &seq
	msg.Type = eventType
	return msg, nil
}

// --- Client -> Server Payloads ---

// IdentifyPayload is sent by the client to authenticate
type IdentifyPayload struct {
	Token      string            `json:"token"`
	Properties ConnectionProperties `json:"properties,omitempty"`
}

// ConnectionProperties contains client information
type ConnectionProperties struct {
	OS      string `json:"os"`
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

// HeartbeatPayload is sent to keep the connection alive
type HeartbeatPayload struct {
	LastSequence *int64 `json:"last_sequence"`
}

// SendMessagePayload is sent when a user sends a message
type SendMessagePayload struct {
	ChannelID uuid.UUID  `json:"channel_id"`
	Content   string     `json:"content"`
	ReplyToID *uuid.UUID `json:"reply_to_id,omitempty"`
	Nonce     string     `json:"nonce,omitempty"` // Client-generated ID for deduplication
}

// EditMessagePayload is sent to edit a message
type EditMessagePayload struct {
	MessageID uuid.UUID `json:"message_id"`
	ChannelID uuid.UUID `json:"channel_id"`
	Content   string    `json:"content"`
}

// DeleteMessagePayload is sent to delete a message
type DeleteMessagePayload struct {
	MessageID uuid.UUID `json:"message_id"`
	ChannelID uuid.UUID `json:"channel_id"`
}

// TypingStartPayload is sent when a user starts typing
type TypingStartPayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
}

// PresenceUpdatePayload is sent to update user presence
type PresenceUpdatePayload struct {
	Status     models.UserStatus `json:"status"`
	StatusText string            `json:"status_text,omitempty"`
}

// ChannelCreateRequest is sent by clients to create a channel
type ChannelCreateRequest struct {
	ServerID   uuid.UUID           `json:"server_id"`
	Name       string              `json:"name"`
	Type       models.ChannelType  `json:"type"`
	CategoryID *uuid.UUID          `json:"category_id,omitempty"`
	Position   int                 `json:"position,omitempty"`
	MaxUsers   int                 `json:"max_users,omitempty"` // Voice channel capacity (0 = unlimited)
}

// ChannelUpdateRequest is sent by clients to update a channel
type ChannelUpdateRequest struct {
	ServerID   uuid.UUID         `json:"server_id"`
	ChannelID  uuid.UUID         `json:"channel_id"`
	Name       *string           `json:"name,omitempty"`
	Type       *models.ChannelType `json:"type,omitempty"` // nil = keep existing type
	CategoryID *uuid.UUID        `json:"category_id,omitempty"`
	Position   *int              `json:"position,omitempty"`   // Deprecated - kept for compatibility
	SortOrder  *int              `json:"sort_order,omitempty"` // NEW: Use for all ordering operations
	IsLocked   *bool             `json:"is_locked,omitempty"`
	MaxUsers   *int              `json:"max_users,omitempty"` // Voice channel capacity (0 = unlimited)
}

// ChannelDeleteRequest is sent by clients to delete a channel
type ChannelDeleteRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"`
}

// MessageHistoryRequest requests historical messages for a channel
type MessageHistoryRequest struct {
	ChannelID uuid.UUID  `json:"channel_id"`
	Limit     int        `json:"limit,omitempty"`     // Default: 200
	Before    *uuid.UUID `json:"before,omitempty"`    // Pagination
}

// RoleAssignRequest assigns a role to a member
type RoleAssignRequest struct {
	ServerID uuid.UUID `json:"server_id"`
	UserID   uuid.UUID `json:"user_id"`
	RoleName string    `json:"role_name"`
}

// RoleRemoveRequest removes a role from a member
type RoleRemoveRequest struct {
	ServerID uuid.UUID `json:"server_id"`
	UserID   uuid.UUID `json:"user_id"`
	RoleName string    `json:"role_name"`
	Force    bool      `json:"force,omitempty"` // Override last-admin protection
}

// KickMemberRequest kicks a member from a server
type KickMemberRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"` // Channel where command was issued
	UserID    uuid.UUID `json:"user_id"`
	Reason    string    `json:"reason,omitempty"`
}

// BanMemberRequest bans a member from a server
type BanMemberRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"` // Channel where command was issued
	UserID    uuid.UUID `json:"user_id"`
	Reason    string    `json:"reason,omitempty"`
}

// MuteMemberRequest server-mutes (or unmutes) a member
type MuteMemberRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"` // Channel where command was issued
	UserID    uuid.UUID `json:"user_id"`
	Mute      bool      `json:"mute"` // true=mute, false=unmute
	Duration  int       `json:"duration,omitempty"` // Duration in minutes (0 = permanent)
}

// TimeoutMemberRequest temporarily bans a member for X minutes
type TimeoutMemberRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"` // Channel where command was issued
	UserID    uuid.UUID `json:"user_id"`
	Duration  int       `json:"duration"` // Duration in minutes
	Reason    string    `json:"reason,omitempty"`
}

// UnbanMemberRequest unbans a member from a server
type UnbanMemberRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"` // Channel where command was issued
	Username  string    `json:"username"`    // Username to unban (can't use UserID since they're not a member)
}

// UnmuteMemberRequest unmutes a server-muted member
type UnmuteMemberRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"` // Channel where command was issued
	UserID    uuid.UUID `json:"user_id"`
}

// CreateRoleRequest creates a new role on a server
type CreateRoleRequest struct {
	ServerID      uuid.UUID `json:"server_id"`
	ChannelID     uuid.UUID `json:"channel_id"` // Channel where command was issued (for system message)
	Name          string    `json:"name"`
	Permissions   uint64    `json:"permissions"`    // Permission bitfield
	Color         int       `json:"color"`
	DisplayOrder  *int      `json:"display_order,omitempty"` // Member panel sort order (lower = top)
	IsHoisted     bool      `json:"is_hoisted"`
	IsMentionable bool      `json:"is_mentionable"`
}

// UpdateRoleRequest updates an existing role
type UpdateRoleRequest struct {
	ServerID      uuid.UUID `json:"server_id"`
	ChannelID     uuid.UUID `json:"channel_id"` // Channel where command was issued (for system message)
	RoleID        uuid.UUID `json:"role_id"`
	Name          string    `json:"name"`
	Permissions   uint64    `json:"permissions"`    // Permission bitfield
	Color         int       `json:"color"`
	DisplayOrder  *int      `json:"display_order,omitempty"` // Member panel sort order (lower = top)
	IsHoisted     bool      `json:"is_hoisted"`
	IsMentionable bool      `json:"is_mentionable"`
}

// DeleteRoleRequest deletes a role from a server
type DeleteRoleRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"` // Channel where command was issued (for system message)
	RoleID    uuid.UUID `json:"role_id"`
}

// WhisperPayload is sent by a client to whisper to another user
type WhisperPayload struct {
	TargetUserID uuid.UUID `json:"target_user_id"`
	ChannelID    uuid.UUID `json:"channel_id"`
	Content      string    `json:"content"`
}

// WhisperCreatePayload is dispatched to both sender and recipient
type WhisperCreatePayload struct {
	FromUser  *models.User `json:"from_user"`
	ToUser    *models.User `json:"to_user"`
	ChannelID uuid.UUID    `json:"channel_id"`
	Content   string       `json:"content"`
	Timestamp time.Time    `json:"timestamp"`
}

// PinMessageRequest pins a message in a channel
type PinMessageRequest struct {
	ChannelID uuid.UUID `json:"channel_id"`
	MessageID uuid.UUID `json:"message_id"`
}

// UnpinMessageRequest unpins a message from a channel
type UnpinMessageRequest struct {
	ChannelID uuid.UUID `json:"channel_id"`
	MessageID uuid.UUID `json:"message_id"`
}

// MessagePinPayload is dispatched when a message is pinned or unpinned
type MessagePinPayload struct {
	ChannelID uuid.UUID      `json:"channel_id"`
	Message   *models.Message `json:"message"`
	PinnedBy  *models.User   `json:"pinned_by"`
	Timestamp time.Time      `json:"timestamp"`
}

// SystemMessagePayload represents a system-generated message (moderation actions, etc.)
type SystemMessagePayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// --- Server -> Client Payloads ---

// HelloPayload is sent on initial connection
type HelloPayload struct {
	HeartbeatInterval int `json:"heartbeat_interval"` // Milliseconds
}

// ReadyPayload is sent after successful authentication
type ReadyPayload struct {
	SessionID   string           `json:"session_id"`
	User        *models.User     `json:"user"`
	Servers     []*models.Server `json:"servers"`
	PrivateChannels []*models.Channel `json:"private_channels,omitempty"`
	ResumeURL   string           `json:"resume_url,omitempty"`
}

// ServerCreatePayload is sent for each server the user is a member of (after READY)
type ServerCreatePayload struct {
	*models.Server
	Channels    []*models.Channel      `json:"channels"`
	Members     []*models.ServerMember `json:"members"`
	Roles       []*models.Role         `json:"roles"`
	Users       []*models.User         `json:"users"`
	VoiceStates []*models.VoiceState   `json:"voice_states"`
}

// --- Event Payloads ---

// MessageCreatePayload is dispatched when a message is created
type MessageCreatePayload struct {
	*models.Message
	Author *models.User   `json:"author"`
	Member *models.ServerMember `json:"member,omitempty"`
	Nonce  string         `json:"nonce,omitempty"`
}

// MessageHistoryPayload contains historical messages for a channel
type MessageHistoryPayload struct {
	ChannelID      uuid.UUID         `json:"channel_id"`
	Messages       []*MessageDisplay `json:"messages"`
	HasMore        bool              `json:"has_more"`
	PinnedMessages []*models.Message `json:"pinned_messages,omitempty"`
}

// MessageDisplay is the client-side message representation
type MessageDisplay struct {
	*models.Message
	Author    *models.User `json:"author"`
	Recipient *models.User `json:"recipient,omitempty"` // For whispers only
}

// MessageUpdatePayload is dispatched when a message is edited
type MessageUpdatePayload struct {
	ID        uuid.UUID  `json:"id"`
	ChannelID uuid.UUID  `json:"channel_id"`
	Content   string     `json:"content,omitempty"`
	EditedAt  *time.Time `json:"edited_at,omitempty"`
}

// MessageDeletePayload is dispatched when a message is deleted
type MessageDeletePayload struct {
	ID        uuid.UUID `json:"id"`
	ChannelID uuid.UUID `json:"channel_id"`
	ServerID  uuid.UUID `json:"server_id,omitempty"`
}

// TypingStartEventPayload is dispatched when a user starts typing
type TypingStartEventPayload struct {
	ChannelID uuid.UUID    `json:"channel_id"`
	ServerID  uuid.UUID    `json:"server_id,omitempty"`
	UserID    uuid.UUID    `json:"user_id"`
	Timestamp time.Time    `json:"timestamp"`
	Member    *models.ServerMember `json:"member,omitempty"`
}

// TypingStopEventPayload is dispatched when a user stops typing (message sent)
type TypingStopEventPayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	UserID    uuid.UUID `json:"user_id"`
}

// PresenceUpdateEventPayload is dispatched when a user's presence changes
type PresenceUpdateEventPayload struct {
	User       *models.User      `json:"user"`
	ServerID   uuid.UUID         `json:"server_id,omitempty"`
	Status     models.UserStatus `json:"status"`
	StatusText string            `json:"status_text,omitempty"`
}

// ServerMemberAddPayload is dispatched when a member joins a server
type ServerMemberAddPayload struct {
	ServerID uuid.UUID           `json:"server_id"`
	Member   *models.ServerMember `json:"member"`
	User     *models.User        `json:"user"`
}

// ServerMemberRemovePayload is dispatched when a member leaves a server
type ServerMemberRemovePayload struct {
	ServerID uuid.UUID    `json:"server_id"`
	User     *models.User `json:"user"`
}

// ServerMemberUpdatePayload is dispatched when a member's roles or state changes
type ServerMemberUpdatePayload struct {
	ServerID uuid.UUID            `json:"server_id"`
	Member   *models.ServerMember `json:"member"`
	User     *models.User         `json:"user"`
	Roles    []*models.Role       `json:"roles"`
}

// ChannelCreatePayload is dispatched when a channel is created
type ChannelCreatePayload struct {
	*models.Channel
}

// ChannelUpdatePayload is dispatched when a channel is updated
type ChannelUpdatePayload struct {
	*models.Channel
}

// ChannelDeletePayload is dispatched when a channel is deleted
type ChannelDeletePayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	ServerID  uuid.UUID `json:"server_id,omitempty"`
	Type      models.ChannelType `json:"type,omitempty"`
}

// ReactionPayload is dispatched for reaction add/remove events
type ReactionPayload struct {
	UserID    uuid.UUID `json:"user_id"`
	ChannelID uuid.UUID `json:"channel_id"`
	MessageID uuid.UUID `json:"message_id"`
	ServerID  uuid.UUID `json:"server_id,omitempty"`
	Emoji     string    `json:"emoji"`
}

// --- Error Payloads ---

// ErrorPayload represents an error response
type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Common error codes
const (
	ErrorCodeUnknown           = 0
	ErrorCodeUnauthorized      = 4001
	ErrorCodeInvalidPayload    = 4002
	ErrorCodeNotFound          = 4003
	ErrorCodeForbidden         = 4004
	ErrorCodeRateLimited       = 4005
	ErrorCodeServerError       = 4006
	ErrorCodeSessionInvalid    = 4007
	ErrorCodeSessionTimeout    = 4008
	ErrorCodeAlreadyAuthenticated = 4009
)

// --- Retention Policy Payloads ---

// GetRetentionPolicyRequest requests a retention policy
type GetRetentionPolicyRequest struct {
	ServerID  uuid.UUID  `json:"server_id"`
	ChannelID *uuid.UUID `json:"channel_id,omitempty"` // NULL = server default
}

// SetRetentionPolicyRequest updates a retention policy
type SetRetentionPolicyRequest struct {
	ServerID                uuid.UUID  `json:"server_id"`
	ChannelID               *uuid.UUID `json:"channel_id,omitempty"` // NULL = server default
	TimeRetentionDays       *int       `json:"time_retention_days,omitempty"`
	SystemTimeRetentionDays *int       `json:"system_time_retention_days,omitempty"`
	MaxMessageCount         *int       `json:"max_message_count,omitempty"`
}

// DeleteRetentionPolicyRequest removes a channel override
type DeleteRetentionPolicyRequest struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"`
}

// PruneMessagesRequest triggers manual message pruning
type PruneMessagesRequest struct {
	ServerID  uuid.UUID  `json:"server_id"`
	ChannelID *uuid.UUID `json:"channel_id,omitempty"` // NULL = all channels
}

// RetentionPolicyUpdatePayload is dispatched when a retention policy changes
type RetentionPolicyUpdatePayload struct {
	Policy           *models.MessageRetentionPolicy   `json:"policy"`
	ChannelOverrides []*models.MessageRetentionPolicy `json:"channel_overrides,omitempty"`
}

// MessagesPrunedPayload is dispatched when messages are pruned
type MessagesPrunedPayload struct {
	ServerID     uuid.UUID                   `json:"server_id"`
	ChannelStats map[uuid.UUID]*models.PruneStats `json:"channel_stats"`
	TriggerType  string                      `json:"trigger_type"` // "manual" or "automatic"
	TriggeredBy  *uuid.UUID                  `json:"triggered_by,omitempty"`
	ExecutedAt   time.Time                   `json:"executed_at"`
	TotalDeleted int                         `json:"total_deleted"`
}

// AssignTitleRequest assigns a custom title to a member
type AssignTitleRequest struct {
	ServerID uuid.UUID `json:"server_id"`
	UserID   uuid.UUID `json:"user_id"`
	Title    string    `json:"title"` // Empty string = clear title
}

// AssignTitlePayload is dispatched when a member's title is updated
type AssignTitlePayload struct {
	ServerID uuid.UUID `json:"server_id"`
	UserID   uuid.UUID `json:"user_id"`
	Title    string    `json:"title"`
}

// CloseCode represents WebSocket close codes
type CloseCode int

const (
	CloseNormal            CloseCode = 1000
	CloseGoingAway         CloseCode = 1001
	CloseUnknownError      CloseCode = 4000
	CloseUnknownOpCode     CloseCode = 4001
	CloseDecodeError       CloseCode = 4002
	CloseNotAuthenticated  CloseCode = 4003
	CloseAuthFailed        CloseCode = 4004
	CloseAlreadyAuth       CloseCode = 4005
	CloseInvalidSeq        CloseCode = 4007
	CloseRateLimited       CloseCode = 4008
	CloseSessionTimeout    CloseCode = 4009
	CloseInvalidShard      CloseCode = 4010
	CloseShardingRequired  CloseCode = 4011
	CloseInvalidAPIVersion CloseCode = 4012
	CloseInvalidIntents    CloseCode = 4013
	CloseDisallowedIntents CloseCode = 4014
)

// ── Voice Payloads ─────────────────────────────────────────────────────────────

// VoiceStateUpdatePayload is sent C→S when joining, leaving, or updating voice state.
// A nil ChannelID means the user is leaving all voice channels on this server.
type VoiceStateUpdatePayload struct {
	ServerID       uuid.UUID  `json:"server_id"`
	ChannelID      *uuid.UUID `json:"channel_id"` // nil = leave
	IsSelfMuted    bool       `json:"is_self_muted"`
	IsSelfDeafened bool       `json:"is_self_deafened"`
}

// VoiceStateEventPayload is dispatched S→C whenever a member's voice state changes.
type VoiceStateEventPayload struct {
	UserID           uuid.UUID    `json:"user_id"`
	ServerID         uuid.UUID    `json:"server_id"`
	ChannelID        *uuid.UUID   `json:"channel_id"` // nil = user left voice
	IsSelfMuted      bool         `json:"is_self_muted"`
	IsSelfDeafened   bool         `json:"is_self_deafened"`
	IsServerMuted    bool         `json:"is_server_muted"`
	IsServerDeafened bool         `json:"is_server_deafened"`
	User             *models.User `json:"user,omitempty"`
}

// VoiceServerUpdatePayload is sent S→C to provide WebRTC connection info after
// a client joins a voice channel. STUNUrls is populated from server config.
type VoiceServerUpdatePayload struct {
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id"`
	Token     string    `json:"token"`              // ephemeral session token
	Endpoint  string    `json:"endpoint"`           // server host:port for signaling relay
	STUNUrls  []string  `json:"stun_urls"`
	TURNUrls  []string  `json:"turn_urls,omitempty"`
}

// VoiceSignalPayload relays a WebRTC SDP offer/answer or ICE candidate between
// two clients via the server. The server forwards it without inspecting content.
type VoiceSignalPayload struct {
	TargetUserID uuid.UUID       `json:"target_user_id"`
	ChannelID    uuid.UUID       `json:"channel_id"`
	Type         string          `json:"type"`                // "offer", "answer", "candidate"
	SDP          string          `json:"sdp,omitempty"`
	Candidate    json.RawMessage `json:"candidate,omitempty"` // RTCIceCandidateInit JSON
}

// VoiceSignalRelayPayload is dispatched S→C when the server relays a WebRTC signal.
// SourceUserID identifies who sent the original signal so the recipient can route it.
type VoiceSignalRelayPayload struct {
	SourceUserID uuid.UUID       `json:"source_user_id"`
	ChannelID    uuid.UUID       `json:"channel_id"`
	Type         string          `json:"type"`                // "offer", "answer", "candidate"
	SDP          string          `json:"sdp,omitempty"`
	Candidate    json.RawMessage `json:"candidate,omitempty"` // RTCIceCandidateInit JSON
}

// VoiceSpeakingPayload is sent C→S when the client's speaking state changes.
type VoiceSpeakingPayload struct {
	ChannelID  uuid.UUID `json:"channel_id"`
	IsSpeaking bool      `json:"is_speaking"`
}

// VoiceSpeakingEventPayload is dispatched S→C when a member starts/stops speaking.
type VoiceSpeakingEventPayload struct {
	UserID     uuid.UUID `json:"user_id"`
	ChannelID  uuid.UUID `json:"channel_id"`
	IsSpeaking bool      `json:"is_speaking"`
}

// VoiceServerMutePayload is sent C→S by an admin to server-mute/deafen a user in voice.
type VoiceServerMutePayload struct {
	ServerID uuid.UUID `json:"server_id"`
	UserID   uuid.UUID `json:"user_id"`
	Muted    bool      `json:"muted"`
	Deafened bool      `json:"deafened"`
}

// MoveVoicePayload is sent C→S by an admin to force-move a user to a different voice channel.
type MoveVoicePayload struct {
	ServerID  uuid.UUID `json:"server_id"`
	UserID    uuid.UUID `json:"user_id"`    // user to move
	ChannelID uuid.UUID `json:"channel_id"` // destination voice channel
}
