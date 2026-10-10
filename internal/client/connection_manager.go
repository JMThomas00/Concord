package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// ConnectionState represents the state of a server connection
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateAuthenticating
	StateReady
	StateReconnecting
	StateError
)

// String returns a human-readable string representation of the connection state
func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting"
	case StateConnected:
		return "Connected"
	case StateAuthenticating:
		return "Authenticating"
	case StateReady:
		return "Ready"
	case StateReconnecting:
		return "Reconnecting"
	case StateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// ServerConnection wraps a Connection with per-server state
type ServerConnection struct {
	ServerID   uuid.UUID         // Client-side server tracking ID
	ServerInfo *ClientServerInfo // Client-side server metadata
	Connection *Connection       // WebSocket connection
	State      ConnectionState   // Current connection state
	LastError  error             // Last error encountered

	// Per-server data cache
	User    *models.User            // Authenticated user
	Token   string                  // Auth token
	Servers []*models.Server        // Servers from READY message

	// Connected server's own build identity, from READY -- see Server
	// Settings > About.
	ServerVersion   string
	ServerGitCommit string
	ServerBuildTime string
	// Plugin channel kinds this server offers: from READY, replaced by
	// PLUGIN_REGISTRY_UPDATE when plugins are installed or removed live.
	// App.pluginChannelKinds is the union across connections.
	PluginChannelKinds []protocol.PluginChannelKindInfo
	// PluginClients lists this server's plugin client parts (images, sounds),
	// from READY and PLUGIN_REGISTRY_UPDATE.
	PluginClients []protocol.PluginClientInfo
	// PluginBoards declares this server's plugin achievements and leaderboards
	// (READY, PLUGIN_REGISTRY_UPDATE); PluginRecords is your record for each
	// (PLUGIN_RECORDS), by plugin ID (plugin_records.go).
	PluginBoards  []protocol.PluginBoardInfo
	PluginRecords map[string]protocol.PluginRecord
	Channels map[uuid.UUID][]*models.Channel // Channels per protocol server
	Messages map[uuid.UUID][]*MessageDisplay // Messages per channel
	Members  []*MemberDisplay        // Members in current server
	Roles    map[uuid.UUID][]*models.Role    // Roles per protocol server
	PinnedMessages map[uuid.UUID][]*models.Message // Pinned messages per channel
	// Threads by their first message's ID (threads.go): the summary, and the
	// replies once the thread has been opened.
	Threads map[uuid.UUID]*threadState
	// The latest reply per thread, per channel, for the optional in-channel
	// line (threads_find.go).
	ThreadNotices map[uuid.UUID]map[uuid.UUID]threadNotice

	// Voice state — keyed by protocol server ID
	// VoiceStates maps userID → VoiceState for all users currently in voice on this connection's servers.
	VoiceStates           map[uuid.UUID]*models.VoiceState // userID → state
	CurrentVoiceChannelID uuid.UUID                        // local user's channel (zero = not in voice)
	VoiceSpeaking         map[uuid.UUID]bool               // userID → speaking (ephemeral)

	// Retry tracking
	RetryCount     int
	RetryStrategy  *ReconnectStrategy
	LastAttemptAt  time.Time

	mu sync.RWMutex
}

// NewServerConnection creates a new ServerConnection
func NewServerConnection(serverID uuid.UUID, serverInfo *ClientServerInfo) *ServerConnection {
	return &ServerConnection{
		ServerID:       serverID,
		ServerInfo:     serverInfo,
		State:          StateDisconnected,
		Channels:       make(map[uuid.UUID][]*models.Channel),
		Messages:       make(map[uuid.UUID][]*MessageDisplay),
		Members:        make([]*MemberDisplay, 0),
		Roles:          make(map[uuid.UUID][]*models.Role),
		PinnedMessages: make(map[uuid.UUID][]*models.Message),
		VoiceStates:    make(map[uuid.UUID]*models.VoiceState),
		VoiceSpeaking:  make(map[uuid.UUID]bool),
	}
}

// GetState returns the current connection state (thread-safe)
func (sc *ServerConnection) GetState() ConnectionState {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.State
}

// SetState sets the connection state (thread-safe)
func (sc *ServerConnection) SetState(state ConnectionState) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.State = state
}

// GetChannels returns channels for a protocol server (thread-safe)
func (sc *ServerConnection) GetChannels(protocolServerID uuid.UUID) []*models.Channel {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.Channels[protocolServerID]
}

// SetChannels sets channels for a protocol server (thread-safe)
func (sc *ServerConnection) SetChannels(protocolServerID uuid.UUID, channels []*models.Channel) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.Channels[protocolServerID] = channels
}

// GetMessages returns messages for a channel (thread-safe)
func (sc *ServerConnection) GetMessages(channelID uuid.UUID) []*MessageDisplay {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.Messages[channelID]
}

// AddMessage adds a message to a channel (thread-safe)
func (sc *ServerConnection) AddMessage(channelID uuid.UUID, msg *MessageDisplay) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Get existing messages
	messages := sc.Messages[channelID]

	// Limit to 1000 messages per channel (circular buffer)
	const maxMessages = 1000
	if len(messages) >= maxMessages {
		// Trim oldest half
		messages = messages[len(messages)/2:]
	}

	// Append new message
	messages = append(messages, msg)
	sc.Messages[channelID] = messages
}

// ClearMessages clears all messages for a channel (thread-safe)
func (sc *ServerConnection) ClearMessages(channelID uuid.UUID) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.Messages[channelID] = make([]*MessageDisplay, 0)
}

// ConnectionManager manages multiple server connections
type ConnectionManager struct {
	connections map[uuid.UUID]*ServerConnection
	eventChan   chan tea.Msg
	mu          sync.RWMutex
	done        chan struct{}
}

// NewConnectionManager creates a new ConnectionManager
func NewConnectionManager(eventChan chan tea.Msg) *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[uuid.UUID]*ServerConnection),
		eventChan:   eventChan,
		done:        make(chan struct{}),
	}
}

// AddServer adds a new server connection (but doesn't connect yet)
func (cm *ConnectionManager) AddServer(info *ClientServerInfo) (*ServerConnection, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if server already exists
	if _, exists := cm.connections[info.ID]; exists {
		return nil, fmt.Errorf("server %s already exists", info.ID)
	}

	// Create new server connection
	sc := NewServerConnection(info.ID, info)
	cm.connections[info.ID] = sc

	return sc, nil
}

// ConnectServer initiates a connection to a server
func (cm *ConnectionManager) ConnectServer(serverID uuid.UUID) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	// Don't reconnect if already connected
	if sc.GetState() == StateReady || sc.GetState() == StateConnecting {
		return nil
	}

	// Set state to connecting
	sc.SetState(StateConnecting)

	// Create new connection
	conn := NewConnection(sc.ServerInfo.GetWebSocketURL())

	// Set up callbacks to route events through ConnectionManager
	conn.onMessage = func(msg *protocol.Message) {
		cm.handleServerMessage(serverID, msg)
	}

	conn.onConnect = func() {
		cm.handleServerConnected(serverID)
	}

	conn.onDisconnect = func() {
		cm.handleServerDisconnected(serverID)
	}

	conn.onError = func(err error) {
		cm.handleServerError(serverID, err)
	}

	// Store connection
	sc.mu.Lock()
	sc.Connection = conn
	sc.mu.Unlock()

	// Initiate connection
	if err := conn.Connect(); err != nil {
		sc.SetState(StateError)
		sc.LastError = err
		return fmt.Errorf("failed to connect: %w", err)
	}

	return nil
}

// DisconnectServer disconnects from a server
func (cm *ConnectionManager) DisconnectServer(serverID uuid.UUID) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.Connection != nil {
		sc.Connection.Disconnect()
		sc.Connection = nil
	}

	sc.State = StateDisconnected
	return nil
}

// RemoveServer removes a server connection entirely
func (cm *ConnectionManager) RemoveServer(serverID uuid.UUID) error {
	// Disconnect first
	if err := cm.DisconnectServer(serverID); err != nil {
		return err
	}

	// Remove from map
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.connections, serverID)
	return nil
}

// GetConnection returns a server connection (thread-safe)
func (cm *ConnectionManager) GetConnection(serverID uuid.UUID) *ServerConnection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.connections[serverID]
}

// GetAllConnections returns all server connections sorted by Order (thread-safe)
func (cm *ConnectionManager) GetAllConnections() []*ServerConnection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	connections := make([]*ServerConnection, 0, len(cm.connections))
	for _, conn := range cm.connections {
		connections = append(connections, conn)
	}

	// Sort by Order field for stable display order
	// Use ID as secondary sort key for stable ordering when Order values are equal
	sort.Slice(connections, func(i, j int) bool {
		if connections[i].ServerInfo.Order == connections[j].ServerInfo.Order {
			// Secondary sort by ID (lexicographic) for stable ordering
			return connections[i].ServerInfo.ID.String() < connections[j].ServerInfo.ID.String()
		}
		return connections[i].ServerInfo.Order < connections[j].ServerInfo.Order
	})

	return connections
}

// GetConnectedServers returns all connected servers (thread-safe)
func (cm *ConnectionManager) GetConnectedServers() []*ServerConnection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	connected := make([]*ServerConnection, 0)
	for _, sc := range cm.connections {
		if sc.GetState() == StateReady {
			connected = append(connected, sc)
		}
	}
	return connected
}

// SendMessage sends a message to a channel on a specific server
func (cm *ConnectionManager) SendMessage(serverID, channelID uuid.UUID, content string, replyToID *uuid.UUID) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	if sc.GetState() != StateReady {
		return fmt.Errorf("server %s not ready", serverID)
	}

	sc.mu.RLock()
	conn := sc.Connection
	sc.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no connection for server %s", serverID)
	}

	return conn.SendMessage(channelID, content, replyToID)
}

// SendThreadMessage posts a message into a thread on a specific server.
func (cm *ConnectionManager) SendThreadMessage(serverID, channelID, threadID uuid.UUID, content string) error {
	sc := cm.GetConnection(serverID)
	if sc == nil || sc.GetState() != StateReady {
		return fmt.Errorf("server %s not ready", serverID)
	}
	sc.mu.RLock()
	conn := sc.Connection
	sc.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("no connection for server %s", serverID)
	}
	return conn.SendThreadMessage(channelID, threadID, content)
}

// SendTyping sends a typing indicator to a channel on a specific server
func (cm *ConnectionManager) SendTyping(serverID, channelID uuid.UUID) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	if sc.GetState() != StateReady {
		return nil // Silently ignore if not ready
	}

	sc.mu.RLock()
	conn := sc.Connection
	sc.mu.RUnlock()

	if conn == nil {
		return nil
	}

	return conn.SendTyping(channelID)
}

// SendTypingStop mirrors SendTyping's shape for the "stopped without
// sending" signal -- see OpTypingStop's doc comment in
// internal/protocol/messages.go.
func (cm *ConnectionManager) SendTypingStop(serverID, channelID uuid.UUID) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	if sc.GetState() != StateReady {
		return nil // Silently ignore if not ready
	}

	sc.mu.RLock()
	conn := sc.Connection
	sc.mu.RUnlock()

	if conn == nil {
		return nil
	}

	return conn.SendTypingStop(channelID)
}

// SendVoiceStateUpdate sends a voice state update (join/leave/mute/deafen) to a server.
func (cm *ConnectionManager) SendVoiceStateUpdate(serverID uuid.UUID, payload *protocol.VoiceStateUpdatePayload) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	sc.mu.RLock()
	conn := sc.Connection
	sc.mu.RUnlock()

	if conn == nil || sc.GetState() != StateReady {
		return fmt.Errorf("server %s not ready", serverID)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := &protocol.Message{Op: protocol.OpVoiceStateUpdate, Data: data}
	return conn.Send(msg)
}

// SendRaw sends a raw protocol message to a specific server
func (cm *ConnectionManager) SendRaw(serverID uuid.UUID, msg *protocol.Message) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	if sc.GetState() != StateReady {
		return fmt.Errorf("server %s not ready", serverID)
	}

	sc.mu.RLock()
	conn := sc.Connection
	sc.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no connection for server %s", serverID)
	}

	return conn.Send(msg)
}

// Login authenticates with a server using email/password
func (cm *ConnectionManager) Login(serverID uuid.UUID, email, password string) (*models.User, string, error) {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return nil, "", fmt.Errorf("server %s not found", serverID)
	}

	sc.mu.RLock()
	conn := sc.Connection
	httpURL := sc.ServerInfo.GetHTTPURL()
	sc.mu.RUnlock()

	if conn == nil {
		// Create temporary connection for login
		conn = NewConnection(sc.ServerInfo.GetWebSocketURL())
		conn.serverAddr = httpURL
	}

	user, token, err := conn.Login(email, password)
	if err != nil {
		return nil, "", err
	}

	// Store user and token
	sc.mu.Lock()
	sc.User = user
	sc.Token = token
	sc.mu.Unlock()

	return user, token, nil
}

// Register creates a new account on a server
func (cm *ConnectionManager) Register(serverID uuid.UUID, username, email, password string) (*models.User, string, error) {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return nil, "", fmt.Errorf("server %s not found", serverID)
	}

	sc.mu.RLock()
	httpURL := sc.ServerInfo.GetHTTPURL()
	sc.mu.RUnlock()

	// Create temporary connection for registration
	conn := NewConnection(sc.ServerInfo.GetWebSocketURL())
	conn.serverAddr = httpURL

	user, token, err := conn.Register(username, email, password)
	if err != nil {
		return nil, "", err
	}

	// Store user and token
	sc.mu.Lock()
	sc.User = user
	sc.Token = token
	sc.mu.Unlock()

	return user, token, nil
}

// Identify authenticates an existing connection with a token
func (cm *ConnectionManager) Identify(serverID uuid.UUID, token string) error {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return fmt.Errorf("server %s not found", serverID)
	}

	sc.mu.RLock()
	conn := sc.Connection
	sc.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no connection for server %s", serverID)
	}

	sc.SetState(StateAuthenticating)
	return conn.Identify(token)
}

// AutoConnectResult holds the result of an auto-connect attempt
type AutoConnectResult struct {
	ServerID uuid.UUID
	UserID   uuid.UUID
	Username string // the account's name on the server
	Email    string // the email it signed in (or must verify) with
	Token    string
	Err      error
	// NeedsVerification: the account exists but must enter the code the
	// server emailed to Email before it can sign in.
	NeedsVerification bool
}

// AutoConnect performs the full token→login→register flow for a server.
// It is designed to be called inside a tea.Cmd goroutine.
// Flow: if saved token exists, connect+identify; on failure try login; on 401 try register.
func (cm *ConnectionManager) AutoConnect(serverID uuid.UUID, savedToken, email, alias, password string) AutoConnectResult {
	result := AutoConnectResult{ServerID: serverID}

	// Step 1: if we have a saved token, try connecting with it first
	if savedToken != "" {
		if err := cm.ConnectServer(serverID); err == nil {
			if err := cm.Identify(serverID, savedToken); err == nil {
				// Token identify sent — actual READY arrives asynchronously.
				// Return success; the app will handle OpReady/OpInvalidSession.
				result.Token = savedToken
				return result
			}
		}
		// Connection or identify failed — disconnect cleanly before retrying
		_ = cm.DisconnectServer(serverID)
	}

	// Step 2: try HTTP login with stored password
	user, token, err := cm.Login(serverID, email, password)
	if err == nil {
		result.UserID = user.ID
		result.Email = email
		result.Token = token

		// Connect WebSocket and identify
		if connErr := cm.ConnectServer(serverID); connErr != nil {
			result.Err = connErr
			return result
		}
		if identErr := cm.Identify(serverID, token); identErr != nil {
			result.Err = identErr
			return result
		}
		return result
	}

	// Step 3: login failed — try auto-register (server may not have this account yet)
	user, token, err = cm.Register(serverID, alias, email, password)
	if err != nil {
		result.Err = fmt.Errorf("auto-connect failed: %w", err)
		return result
	}
	result.UserID = user.ID
	result.Email = email
	result.Token = token

	if connErr := cm.ConnectServer(serverID); connErr != nil {
		result.Err = connErr
		return result
	}
	if identErr := cm.Identify(serverID, token); identErr != nil {
		result.Err = identErr
		return result
	}
	return result
}

// AutoConnectHTTP performs only the HTTP login→register flow (no WebSocket).
// Use this when the WebSocket connection will be handled separately by connectServerAsync,
// which correctly sets activeConn before calling Identify to prevent the READY race condition.
//
// It signs in as the profile: by the email this profile last used on the
// server (saved), else the profile's email. It only registers a new account
// on a server this profile has never signed in to, and only when the
// server says the sign-in details are wrong: anywhere else a failure is
// reported, so a changed password or email never quietly makes a second
// account.
func (cm *ConnectionManager) AutoConnectHTTP(serverID uuid.UUID, profile *LocalIdentity, saved *SavedCredentials) AutoConnectResult {
	result := AutoConnectResult{ServerID: serverID}
	email := profile.Email
	known := saved.belongsTo(profile)
	if known && saved.Email != "" {
		email = saved.Email
	}
	result.Email = email

	done := func(user *models.User, token string) AutoConnectResult {
		result.UserID, result.Token = user.ID, token
		result.Username = user.Username
		return result
	}
	needsCode := func(err error) bool {
		if e, ok := IsVerificationRequired(err); ok {
			result.NeedsVerification = true
			result.Err = e
			if e.Email != "" {
				result.Email = e.Email
			}
			return true
		}
		return false
	}

	user, token, err := cm.Login(serverID, email, profile.Password)
	if err == nil {
		return done(user, token)
	}
	if needsCode(err) {
		return result
	}
	apiErr, isAPI := err.(*APIError)
	if !isAPI || apiErr.Status != http.StatusUnauthorized {
		result.Err = err
		return result
	}
	if known {
		result.Err = fmt.Errorf("couldn't sign in as %s: the password doesn't match (reset it with Forgot password on the login screen)", email)
		return result
	}

	// Never signed in here as this profile: make the account.
	user, token, err = cm.Register(serverID, profile.Alias, profile.Email, profile.Password)
	if err == nil {
		return done(user, token)
	}
	if needsCode(err) {
		return result
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusConflict {
		result.Err = fmt.Errorf("%s already has an account on this server with a different password (Forgot password on the login screen resets it)", profile.Email)
		return result
	}
	result.Err = err
	return result
}

// Event handlers

// handleServerMessage wraps protocol messages with server context
func (cm *ConnectionManager) handleServerMessage(serverID uuid.UUID, msg *protocol.Message) {
	wrapped := ServerScopedMsg{
		ServerID: serverID,
		Msg:      ProtocolMsg{Message: msg},
	}
	cm.eventChan <- wrapped
}

// handleServerConnected handles connection established event
func (cm *ConnectionManager) handleServerConnected(serverID uuid.UUID) {
	sc := cm.GetConnection(serverID)
	if sc != nil {
		sc.SetState(StateConnected)
	}

	wrapped := ServerScopedMsg{
		ServerID: serverID,
		Msg:      ConnectedMsg{},
	}
	cm.eventChan <- wrapped
}

// handleServerDisconnected handles disconnection event
func (cm *ConnectionManager) handleServerDisconnected(serverID uuid.UUID) {
	sc := cm.GetConnection(serverID)
	if sc != nil {
		sc.SetState(StateDisconnected)
	}

	wrapped := ServerScopedMsg{
		ServerID: serverID,
		Msg:      DisconnectedMsg{},
	}
	cm.eventChan <- wrapped
}

// handleServerError handles error event
func (cm *ConnectionManager) handleServerError(serverID uuid.UUID, err error) {
	sc := cm.GetConnection(serverID)
	if sc != nil {
		sc.SetState(StateError)
		sc.LastError = err
	}

	wrapped := ServerScopedMsg{
		ServerID: serverID,
		Msg:      ErrorMsg{Error: err.Error()},
	}
	cm.eventChan <- wrapped
}

// ServerScopedMsg wraps a tea.Msg with server context
type ServerScopedMsg struct {
	ServerID uuid.UUID
	Msg      tea.Msg
}

// Shutdown closes all connections and cleans up
func (cm *ConnectionManager) Shutdown() {
	close(cm.done)

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for serverID := range cm.connections {
		cm.DisconnectServer(serverID)
	}
}
