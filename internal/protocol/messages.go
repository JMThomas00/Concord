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
	
	// User events
	EventPresenceUpdate   EventType = "PRESENCE_UPDATE"
	EventTypingStart      EventType = "TYPING_START"
	EventUserUpdate       EventType = "USER_UPDATE"
	
	// Role events
	EventRoleCreate       EventType = "ROLE_CREATE"
	EventRoleUpdate       EventType = "ROLE_UPDATE"
	EventRoleDelete       EventType = "ROLE_DELETE"
	
	// Voice events (v2)
	EventVoiceStateUpdate EventType = "VOICE_STATE_UPDATE"
	EventVoiceServerUpdate EventType = "VOICE_SERVER_UPDATE"
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

// TypingStartPayload is sent when a user starts typing
type TypingStartPayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
}

// PresenceUpdatePayload is sent to update user presence
type PresenceUpdatePayload struct {
	Status     models.UserStatus `json:"status"`
	StatusText string            `json:"status_text,omitempty"`
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

// --- Event Payloads ---

// MessageCreatePayload is dispatched when a message is created
type MessageCreatePayload struct {
	*models.Message
	Author *models.User   `json:"author"`
	Member *models.ServerMember `json:"member,omitempty"`
	Nonce  string         `json:"nonce,omitempty"`
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
	ID       uuid.UUID `json:"id"`
	ServerID uuid.UUID `json:"server_id,omitempty"`
	Type     models.ChannelType `json:"type"`
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

// CloseCode represents WebSocket close codes
type CloseCode int

const (
	CloseNormal           CloseCode = 1000
	CloseGoingAway        CloseCode = 1001
	CloseUnknownError     CloseCode = 4000
	CloseUnknownOpCode    CloseCode = 4001
	CloseDecodeError      CloseCode = 4002
	CloseNotAuthenticated CloseCode = 4003
	CloseAuthFailed       CloseCode = 4004
	CloseAlreadyAuth      CloseCode = 4005
	CloseInvalidSeq       CloseCode = 4007
	CloseRateLimited      CloseCode = 4008
	CloseSessionTimeout   CloseCode = 4009
	CloseInvalidShard     CloseCode = 4010
	CloseShardingRequired CloseCode = 4011
	CloseInvalidAPIVersion CloseCode = 4012
	CloseInvalidIntents   CloseCode = 4013
	CloseDisallowedIntents CloseCode = 4014
)
