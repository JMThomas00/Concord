// Package wire is the Concord plugin protocol: the JSON messages a plugin
// process exchanges with a Concord server over its WebSocket connection.
//
// It's the single source of truth for those shapes. Concord itself uses
// these types (its internal protocol package aliases them), so a plugin
// built against this package speaks exactly what the server sends. A
// plugin written in another language can follow PROTOCOL.md in the SDK's
// root instead.
package wire

import (
	"encoding/json"

	"github.com/google/uuid"
)

// OpCode identifies a message's purpose.
type OpCode int

// The opcodes a plugin sends or receives. Concord defines more (for human
// clients); a plugin can ignore any it doesn't know.
const (
	OpIdentify     OpCode = 0  // plugin → server: authenticate (IdentifyPayload)
	OpHeartbeat    OpCode = 1  // plugin → server: keep-alive
	OpSendMessage  OpCode = 3  // plugin → server: post a chat message (SendMessagePayload)
	OpTypingStart  OpCode = 4  // plugin → server: show "typing…" in a channel
	OpEditMessage  OpCode = 30 // plugin → server: change one of your own messages (EditMessagePayload)
	OpDispatch     OpCode = 10 // server → plugin: an event; see Message.Type
	OpHeartbeatAck OpCode = 11 // server → plugin: reply to OpHeartbeat
	OpHello        OpCode = 12 // server → plugin: first message on connect (HelloPayload)
	OpReady        OpCode = 13 // server → plugin: identify succeeded (ReadyPayload)
	OpInvalid      OpCode = 14 // server → plugin: identify failed

	OpPluginPaneFrame OpCode = 53 // plugin → server: a rendered frame (PluginPaneFramePayload)
	OpPluginEvent     OpCode = 54 // plugin → server: generic event (PluginEventPayload)
	OpTypingStop      OpCode = 60 // plugin → server: clear "typing…"
)

// EventType names an OpDispatch event.
type EventType string

// The dispatch events a plugin receives.
const (
	EventPluginPaneEnter    EventType = "PLUGIN_PANE_ENTER"    // a viewer opened one of your panes (PluginPaneEnterPayload)
	EventPluginPaneInput    EventType = "PLUGIN_PANE_INPUT"    // a viewer pressed a key (PluginPaneInputPayload)
	EventPluginPaneResize   EventType = "PLUGIN_PANE_RESIZE"   // a viewer's pane changed size or theme (PluginPaneResizePayload)
	EventPluginPaneLeave    EventType = "PLUGIN_PANE_LEAVE"    // a viewer left, or disconnected (PluginPaneLeavePayload)
	EventPluginEvent        EventType = "PLUGIN_EVENT"         // e.g. a "members" reply (PluginEventPayload)
	EventPluginConfigUpdate EventType = "PLUGIN_CONFIG_UPDATE" // your server settings (PluginConfigListPayload)
	EventChannelCreate      EventType = "CHANNEL_CREATE"       // one of your channels was created (ChannelCreatePayload)
	EventChannelUpdate      EventType = "CHANNEL_UPDATE"       // one of your channels (sent for each on connect) (ChannelUpdatePayload)
	EventChannelDelete      EventType = "CHANNEL_DELETE"       // one of your channels was deleted (ChannelDeletePayload)
	EventMessageCreate      EventType = "MESSAGE_CREATE"       // a chat message relayed to you (MessageCreatePayload)
)

// Message is the envelope every WebSocket frame carries.
type Message struct {
	Op   OpCode          `json:"op"`
	Data json.RawMessage `json:"d,omitempty"`
	Seq  *int64          `json:"s,omitempty"` // Sequence number for dispatches
	Type EventType       `json:"t,omitempty"` // Event type for dispatches
}

// NewMessage builds an envelope with data marshalled into it.
func NewMessage(op OpCode, data interface{}) (*Message, error) {
	msg := &Message{Op: op}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		msg.Data = raw
	}
	return msg, nil
}

// ClientTypePlugin is IdentifyPayload.ClientType for a plugin process.
const ClientTypePlugin = "plugin"

// IdentifyPayload authenticates a plugin: Token is CONCORD_PLUGIN_TOKEN,
// ClientType is ClientTypePlugin.
type IdentifyPayload struct {
	Token      string `json:"token"`
	ClientType string `json:"client_type,omitempty"`
}

// HelloPayload is OpHello's data.
type HelloPayload struct {
	HeartbeatInterval int `json:"heartbeat_interval"` // milliseconds
}

// ReadyPayload is OpReady's data (the fields a plugin uses).
type ReadyPayload struct {
	SessionID string `json:"session_id"`
	User      *User  `json:"user"` // the plugin's own service account
}

// User is a Concord account (the fields a plugin uses).
type User struct {
	ID            uuid.UUID `json:"id"`
	Username      string    `json:"username"`
	Discriminator string    `json:"discriminator"`
	DisplayName   string    `json:"display_name,omitempty"`
}

// Name is how the user appears: display name, else username.
func (u *User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// ErrorPayload is sent (as an OpDispatch with no Type) when a request fails.
type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ChannelTypePlugin is Channel.Type for plugin-provided channels.
const ChannelTypePlugin = 5

// Channel is a Concord channel (the fields a plugin uses).
type Channel struct {
	ID                uuid.UUID         `json:"id"`
	ServerID          uuid.UUID         `json:"server_id,omitempty"`
	Name              string            `json:"name"`
	Topic             string            `json:"topic,omitempty"`
	Type              int               `json:"type"`
	PluginID          string            `json:"plugin_id,omitempty"`
	PluginChannelKind string            `json:"plugin_channel_kind,omitempty"`
	PluginConfig      map[string]string `json:"plugin_config,omitempty"` // the kind's create_field values
}

// ChannelCreatePayload is EventChannelCreate's data.
type ChannelCreatePayload struct {
	*Channel
	PluginConfig map[string]string `json:"plugin_config,omitempty"`
}

// ChannelUpdatePayload is EventChannelUpdate's data.
type ChannelUpdatePayload struct {
	*Channel
}

// ChannelDeletePayload is EventChannelDelete's data.
type ChannelDeletePayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	ServerID  uuid.UUID `json:"server_id,omitempty"`
	Type      int       `json:"type,omitempty"`
}

// ChatMessage is a chat message (the fields a plugin uses).
type ChatMessage struct {
	ID        uuid.UUID  `json:"id"`
	ChannelID uuid.UUID  `json:"channel_id"`
	AuthorID  uuid.UUID  `json:"author_id"`
	Content   string     `json:"content"`
	ReplyToID *uuid.UUID `json:"reply_to_id,omitempty"`
}

// MessageCreatePayload is EventMessageCreate's data. For a message you
// sent yourself, Nonce is the one you sent it with.
type MessageCreatePayload struct {
	*ChatMessage
	Author *User  `json:"author"`
	Nonce  string `json:"nonce,omitempty"`
	Stream string `json:"stream,omitempty"`
}

// Stream states for a reply written a piece at a time: post it with
// StreamWriting, edit it with StreamWriting as it grows, and finish with an
// edit marked StreamDone. Clients show a cursor while it's being written,
// don't mark it "edited", and render unfinished markdown (an open code
// block) sensibly. plugin.Conn.Stream does all of this.
const (
	StreamWriting = "writing"
	StreamDone    = "done"
)

// MaxMessageLength is the most characters one chat message can hold.
const MaxMessageLength = 2000

// SendMessagePayload is OpSendMessage's data.
type SendMessagePayload struct {
	ChannelID uuid.UUID  `json:"channel_id"`
	Content   string     `json:"content"`
	ReplyToID *uuid.UUID `json:"reply_to_id,omitempty"`
	Nonce     string     `json:"nonce,omitempty"`  // echoed back in your own MESSAGE_CREATE
	Stream    string     `json:"stream,omitempty"` // StreamWriting for a reply that will grow by edits
}

// EditMessagePayload is OpEditMessage's data.
type EditMessagePayload struct {
	MessageID uuid.UUID `json:"message_id"`
	ChannelID uuid.UUID `json:"channel_id"`
	Content   string    `json:"content"`
	Stream    string    `json:"stream,omitempty"` // StreamWriting or StreamDone while streaming a reply
}

// TypingPayload is OpTypingStart/OpTypingStop's data.
type TypingPayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
}

// ── Remote panes ────────────────────────────────────────────────────────────
//
// A plugin channel with remote_pane = true shows whatever the plugin draws.
// Each viewer who opens it sends Enter (with their size, theme and name),
// then Input for each key while it's focused, Resize when their pane
// changes, and Leave when they go. The plugin answers with Frames: full
// screens of text (with SGR color codes), for one viewer or everyone.

// PaneTheme is the viewer's current Concord theme, sent with Enter/Resize so
// a plugin can render in matching colors. Palette keys are the theme's base
// color names (background, foreground, comment, selection, current_line,
// red, orange, yellow, green, cyan, purple, pink); values are "#rrggbb" or
// ANSI palette indexes ("0"-"15"), or "" for the terminal's own default.
// ColorProfile is the viewer terminal's capability: "truecolor", "ansi256",
// "ansi" or "ascii" -- render no richer than this.
type PaneTheme struct {
	Name         string            `json:"name,omitempty"`
	Palette      map[string]string `json:"palette,omitempty"`
	ColorProfile string            `json:"color_profile,omitempty"`
}

// PluginPaneEnterPayload is sent when a viewer opens one of your panes --
// and re-sent for every current viewer when your plugin (re)connects, so a
// restarted plugin repaints everyone.
type PluginPaneEnterPayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	ViewerID  uuid.UUID `json:"viewer_id,omitempty"` // Server-stamped on relay; ignored if client-supplied
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	// Server-stamped: the viewer's username and how they appear in this
	// server (nickname, else display name, else username).
	ViewerName        string     `json:"viewer_name,omitempty"`
	ViewerDisplayName string     `json:"viewer_display_name,omitempty"`
	Theme             *PaneTheme `json:"theme,omitempty"`
}

// PluginPaneResizePayload reports a viewport size (or theme) change for an active pane.
type PluginPaneResizePayload struct {
	ChannelID         uuid.UUID  `json:"channel_id"`
	ViewerID          uuid.UUID  `json:"viewer_id,omitempty"`
	Width             int        `json:"width"`
	Height            int        `json:"height"`
	ViewerName        string     `json:"viewer_name,omitempty"`
	ViewerDisplayName string     `json:"viewer_display_name,omitempty"`
	Theme             *PaneTheme `json:"theme,omitempty"`
}

// PluginPaneInputPayload forwards one keypress from a viewer to the plugin
// owning the channel. KeyString is the canonical encoding (Bubble Tea's
// KeyMsg.String(): "a", "enter", "ctrl+c", "alt+left", ...); KeyType/Runes/
// Alt are kept for existing Bubble Tea v1 plugins that rebuild a tea.KeyMsg.
type PluginPaneInputPayload struct {
	ChannelID         uuid.UUID `json:"channel_id"`
	ViewerID          uuid.UUID `json:"viewer_id,omitempty"`
	KeyType           int       `json:"key_type"`
	Runes             []rune    `json:"runes,omitempty"`
	Alt               bool      `json:"alt,omitempty"`
	KeyString         string    `json:"key_string"`
	ViewerName        string    `json:"viewer_name,omitempty"`
	ViewerDisplayName string    `json:"viewer_display_name,omitempty"`
}

// PluginPaneLeavePayload is sent when a viewer navigates away from a
// plugin channel, or their connection closes.
type PluginPaneLeavePayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	ViewerID  uuid.UUID `json:"viewer_id,omitempty"`
}

// PluginPaneFramePayload is pushed by a plugin process with the rendered
// screen for one specific viewer -- or, with ViewerID left empty, for every
// current viewer of ChannelID (a shared view such as a passthrough
// terminal). The server only delivers frames for the plugin's own channels,
// to users actually viewing them. Escape codes other than SGR styling and
// OSC 8 links are stripped by the viewer's client.
type PluginPaneFramePayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	ViewerID  uuid.UUID `json:"viewer_id,omitempty"`
	Frame     string    `json:"frame"`
	Seq       int64     `json:"seq"` // Increase on every frame per viewer; the client drops frames with Seq <= last-applied
	// Epoch is stamped by the server (ignored if a plugin sets it) and
	// changes whenever the plugin reconnects, so the client knows a lower
	// Seq means a fresh stream rather than a stale frame.
	Epoch int64 `json:"epoch,omitempty"`
	// Images are drawn over the frame's text, each in a box of cells.
	// Concord picks how (Kitty graphics, Sixel, iTerm2 or half-blocks) for
	// each viewer's terminal. Asset is a path in the plugin's client/
	// folder, e.g. "assets/pieces/white-king.png".
	Images []PaneImage `json:"images,omitempty"`
	// Keys claims Concord's navigation keys (PaneKeyEsc, PaneKeyTab,
	// PaneKeyShiftTab) for as long as this frame is on screen. Unclaimed,
	// Esc and Shift+Tab leave the pane for the channel list and Tab moves on
	// to the member list, as in every other channel. Claim them only while
	// they mean something: a selection or a menu to cancel, a form's fields
	// to move between, a terminal program. Ctrl+] always leaves.
	Keys []string `json:"keys,omitempty"`
}

// The navigation keys a frame can claim (PluginPaneFramePayload.Keys).
const (
	PaneKeyEsc      = "esc"
	PaneKeyTab      = "tab"
	PaneKeyShiftTab = "shift+tab"
)

// PaneNavigationKeys are every key a frame can claim.
var PaneNavigationKeys = []string{PaneKeyEsc, PaneKeyTab, PaneKeyShiftTab}

// PaneImage places one image in a pane frame. Col and Row are the
// top-left cell (0-based, inside the pane); Cols and Rows its size in
// cells. The image is scaled to fit the box, keeping its aspect ratio.
type PaneImage struct {
	Asset string `json:"asset"`
	Col   int    `json:"col"`
	Row   int    `json:"row"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
}

// ── Plugin events ───────────────────────────────────────────────────────────

// PluginEventPayload is the generic envelope for anything that isn't pane
// rendering. Kind says what it is; see the PluginEvent* constants for the
// kinds Concord understands. Any other kind sent with ViewerID set is
// relayed unchanged to that viewer's client, as long as they're viewing one
// of your panes.
type PluginEventPayload struct {
	PluginID string          `json:"plugin_id"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
	ViewerID uuid.UUID       `json:"viewer_id,omitempty"`
}

// PluginEventPayload.Kind values Concord itself understands. Any other kind
// sent with ViewerID set is relayed unchanged to that viewer's client.
const (
	// Plugin → Concord: post Payload (PluginNotifyEventPayload) as a system
	// message in the plugin's configured activity channel.
	PluginEventNotify = "notify"
	// Plugin → one viewer: stop capturing keys for Payload's
	// (PluginPaneClosePayload) channel and hand focus back to Concord's own
	// navigation. The pane stays open and keeps receiving frames.
	PluginEventLeavePane = "leave_pane"
	// Plugin → one member (PluginNotifyUserPayload): a toast plus an unread
	// badge on the plugin's channel -- e.g. "Alex challenged you to chess".
	// Only delivered to members who can view that channel.
	PluginEventNotifyUser = "notify_user"
	// Plugin → Concord request (PluginMembersRequest), answered with the
	// same kind (PluginMembersResponse): who can see a channel and who's
	// online, e.g. for a challenge lobby.
	PluginEventMembers = "members"
	// Plugin → viewer(s) (PluginPaneTitlePayload): the text shown in the
	// pane's border title in place of the channel name, e.g.
	// "Chess — Jordan vs Alex". ViewerID empty = every viewer.
	PluginEventPaneTitle = "pane_title"
	// Plugin → viewer(s) (PluginPlaySoundPayload): play a sound from the
	// plugin's client/ folder. ViewerID empty = everyone viewing the channel.
	PluginEventPlaySound = "play_sound"
	// Plugin ↔ one viewer's client code (PluginClientMessagePayload):
	// messages between a plugin and the WebAssembly code it ships in its
	// client/ folder. A plugin sends it with ViewerID set; a viewer's
	// client code sends it to the plugin, and Concord fills in ViewerID
	// and the viewer's names.
	PluginEventClientMessage = "client_message"
	// Plugin → one player (PluginGameResultPayload): how a game ended for
	// them, so their client can count wins (Settings > About achievements).
	// The table kit sends it; ViewerID is the player.
	PluginEventGameResult = "game_result"
)

// PluginNotifyEventPayload is the Payload shape for PluginEventPayload{Kind: "notify"}.
type PluginNotifyEventPayload struct {
	Content string `json:"content"`
}

// PluginPaneClosePayload is the Payload shape for
// PluginEventPayload{Kind: "leave_pane"} — tells the named viewer's client
// to stop sending its keys to a plugin pane, e.g. because the plugin's own
// UI reached a state (its main view) where a designated "quit" keypress
// should hand control back to Concord's navigation.
type PluginPaneClosePayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
}

// PluginNotifyUserPayload is the Payload for Kind "notify_user".
type PluginNotifyUserPayload struct {
	UserID    uuid.UUID `json:"user_id"`
	ChannelID uuid.UUID `json:"channel_id"`
	Content   string    `json:"content"`
}

// PluginMembersRequest is the Payload a plugin sends with Kind "members".
// RequestID is echoed back so a plugin can match the reply.
type PluginMembersRequest struct {
	ChannelID uuid.UUID `json:"channel_id"`
	RequestID string    `json:"request_id,omitempty"`
}

// PluginMembersResponse is the Payload of the "members" reply.
type PluginMembersResponse struct {
	ChannelID uuid.UUID      `json:"channel_id"`
	RequestID string         `json:"request_id,omitempty"`
	Members   []PluginMember `json:"members"`
}

// PluginMember is one member who can view the requested channel.
type PluginMember struct {
	UserID      uuid.UUID `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Online      bool      `json:"online"`
	Viewing     bool      `json:"viewing"` // has this channel's pane open right now
}

// PluginPaneTitlePayload is the Payload for Kind "pane_title". An empty
// Title restores the channel's own name.
type PluginPaneTitlePayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	Title     string    `json:"title"`
}

// PluginPlaySoundPayload is the Payload for Kind "play_sound". Asset is a
// WAV or Ogg/Opus file in the plugin's client/ folder; Volume is 0 to 1
// (0 means 1), scaled again by the member's own plugin-sound setting.
type PluginPlaySoundPayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	Asset     string    `json:"asset"`
	Volume    float64   `json:"volume,omitempty"`
}

// PluginClientMessagePayload is the Payload for Kind "client_message".
// Data is whatever JSON the two sides agree on, at most
// MaxClientMessageBytes.
type PluginClientMessagePayload struct {
	ChannelID         uuid.UUID       `json:"channel_id"`
	Data              json.RawMessage `json:"data"`
	ViewerName        string          `json:"viewer_name,omitempty"`         // set by Concord, toward the plugin
	ViewerDisplayName string          `json:"viewer_display_name,omitempty"` // set by Concord, toward the plugin
}

// MaxClientMessageBytes caps a client_message's Data.
const MaxClientMessageBytes = 64 << 10

// ── Settings ────────────────────────────────────────────────────────────────

// PluginField describes one manifest-declared settings field (a
// [[server_config_field]] or a channel kind's [[channel_kind.create_field]]).
type PluginField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // text | number | boolean | select | channel_select | secret
	Options  []string `json:"options,omitempty"`
	Default  string   `json:"default,omitempty"`
	Required bool     `json:"required,omitempty"`
	Help     string   `json:"help,omitempty"`
}

// PluginInfo describes one installed plugin: its settings fields and their
// current values. A plugin receives its own (EventPluginConfigUpdate) on
// connect and whenever an admin saves; secrets arrive in plaintext.
type PluginInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Product      string            `json:"product,omitempty"`
	Version      string            `json:"version"`
	SourceURL    string            `json:"source_url,omitempty"`
	Enabled      bool              `json:"enabled"`
	Status       string            `json:"status"`
	LastError    string            `json:"last_error,omitempty"`
	ConfigFields []PluginField     `json:"config_fields,omitempty"`
	ConfigValues map[string]string `json:"config_values,omitempty"`
	// SecretsSet lists "secret" fields that have a value. Their values are
	// never sent to admins' clients (ConfigValues leaves them out there);
	// only the plugin itself receives them.
	SecretsSet []string `json:"secrets_set,omitempty"`
	// BaseID is set on an instance of another plugin (a second Mynah
	// persona); AllowsInstances on a plugin that can have several.
	BaseID          string `json:"base_id,omitempty"`
	AllowsInstances bool   `json:"allows_instances,omitempty"`
}

// PluginConfigListPayload is EventPluginConfigUpdate's data.
type PluginConfigListPayload struct {
	Plugins []PluginInfo `json:"plugins"`
}

// PluginGameResultPayload is the Payload for Kind "game_result".
type PluginGameResultPayload struct {
	Game     string `json:"game"`               // the game's name, e.g. "Chess"
	Result   string `json:"result"`             // "win", "loss" or "draw"
	Computer bool   `json:"computer,omitempty"` // against the computer
	Reason   string `json:"reason,omitempty"`   // e.g. "checkmate"
}
