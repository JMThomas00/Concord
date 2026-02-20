package client

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/themes"
)

// View represents different views/screens in the application
type View int

const (
	ViewIdentitySetup View = iota // First-run: set up local identity
	ViewLogin
	ViewRegister
	ViewMain
	ViewServerList
	ViewChannelList
	ViewSettings
	ViewAddServer
	ViewManageServers
	ViewThemeBrowser
)

// FocusArea represents which area of the UI has focus
type FocusArea int

const (
	FocusServerIcons FocusArea = iota // Server icons column (left-most)
	FocusChannelList                  // Channels column
	FocusChat
	FocusInput
	FocusUserList
	FocusMessageNav                   // Message navigation mode
)

// App represents the main application state
type App struct {
	// Window dimensions
	width  int
	height int

	// Current view
	view View

	// Focus state
	focus FocusArea

	// Theme
	theme  *themes.Theme
	styles *themes.Styles
	banner Banner // Static banner chosen at startup

	// Local identity (single identity across all servers)
	localIdentity *LocalIdentity

	// Multi-server connection management
	connMgr    *ConnectionManager      // Manages all server connections
	connEvents chan tea.Msg           // Event channel for async connection events

	// Client-side server list (from servers.json)
	clientServers       []*ClientServerInfo
	currentClientServer *ClientServerInfo
	serverIndex         int

	// Active server connection (for current view)
	activeConn *ServerConnection

	// Protocol server state (from READY message)
	currentServer *models.Server  // Currently selected protocol server
	protocolServerIndex int        // Index in activeConn.Servers

	// Channel state
	currentChannel      *models.Channel
	channelIndex        int
	channelTree         *ChannelTree          // Hierarchical channel tree
	collapsedCategories map[uuid.UUID]bool    // Per-server collapsed category state

	// Default user preferences
	defaultPreferences *DefaultPreferences

	// Configuration
	configMgr *ConfigManager
	uiConfig  *UIConfig

	// UI components
	input         textarea.Model
	chatViewport  viewport.Model
	sidebarScroll int

	// Login/Register form
	loginEmail           textinput.Model
	loginPassword        textinput.Model
	loginPasswordConfirm textinput.Model
	loginUsername        textinput.Model
	loginFocus           int
	registerLinkFocused  bool // Track if "Register New Account" link is focused
	loginError           string

	// Identity Setup form (first-run only)
	identityAlias           textinput.Model
	identityEmail           textinput.Model
	identityPassword        textinput.Model
	identityPasswordConfirm textinput.Model
	identityFocus           int
	identityError           string

	// Add Server form
	addServerName    textinput.Model
	addServerAddress textinput.Model
	addServerPort    textinput.Model
	addServerUseTLS  bool
	addServerFocus   int
	addServerError   string

	// Manage Servers view
	manageServersFocus  int
	pingResults         map[uuid.UUID]*PingResult
	editingServerID     *uuid.UUID // Set when editing an existing server
	editingServerIndex  int        // Index in clientServers of the server being edited

	// Status message
	statusMessage string
	statusError   bool

	// Command handler
	commandHandler *CommandHandler

	// Typing indicator
	typingUsers     []string
	typingExpiry    map[uuid.UUID]time.Time // userID → when the typing indicator expires
	typingFrame     int                     // current animation frame index
	lastTypingSent  time.Time               // when we last sent OpTypingStart

	// Theme browser state
	themeBrowserState *ThemeBrowserState

	// @mention autocomplete state
	mentionSuggestions []string
	mentionQuery       string
	showMentionPopup   bool

	// Unread tracking (client-side only)
	unreadCounts  map[uuid.UUID]map[uuid.UUID]int // clientServerID → channelID → count
	mentionCounts map[uuid.UUID]map[uuid.UUID]int // clientServerID → channelID → @mention count
	mutedChannels map[uuid.UUID]bool              // channelID → muted

	// AFK tracking
	lastActivityTime time.Time
	isAFK            bool

	// Message navigation state (two-level system)
	messageNavMode        bool      // Level 1: browsing messages
	messageNavIndex       int       // Which message is selected (0-based)
	inMessageEditMode     bool      // Level 2: navigating within a message
	messageCursorLine     int       // Cursor line within message (Level 2)
	messageCursorCol      int       // Cursor column within message (Level 2)
	messageSelectionStart *Position // Selection start (nil if no selection)
	messageSelectionEnd   *Position // Selection end

	// Link browser state
	linkBrowserState *LinkBrowserState
}

// Position represents a cursor position in a message (for Level 2 navigation)
type Position struct {
	Line int // Line number in the message
	Col  int // Column (character offset) within the line
}

// LinkBrowserState holds the state for the link browser modal
type LinkBrowserState struct {
	Links         []string         // Extracted URLs
	SelectedIndex int              // Cursor position
	SourceMessage *MessageDisplay  // Message the links came from (optional)
	PreviousMode  string           // "message_nav" or "main"
}

// MessageDisplay wraps a message with display information
type MessageDisplay struct {
	*models.Message
	AuthorName  string
	AuthorColor string
	IsOwn       bool
	ShowHeader  bool // Show author/timestamp (false for consecutive messages)
	IsWhisper   bool // Ephemeral DM from /whisper
	IsSystem    bool // Server-wide moderation/system announcement
}

// MemberDisplay wraps a member with display information
type MemberDisplay struct {
	User        *models.User
	Member      *models.ServerMember
	HighestRole *models.Role // highest hoisted role (nil = regular member)
	AvatarColor string       // hex color for circle avatar
}

// avatarPalette is a set of colors used as fallback avatar colors when a member has no hoisted role
var avatarPalette = []string{"#bd93f9", "#50fa7b", "#ff79c6", "#8be9fd", "#ffb86c", "#f1fa8c", "#ff5555"}

// buildMemberDisplay creates a MemberDisplay from a server member, its user, and a role map.
// It finds the member's highest hoisted role (by Position) to determine the avatar color and grouping.
func buildMemberDisplay(member *models.ServerMember, user *models.User, roleMap map[uuid.UUID]*models.Role) *MemberDisplay {
	var highestRole *models.Role
	for _, roleID := range member.RoleIDs {
		role, ok := roleMap[roleID]
		if !ok || role.IsDefault {
			continue
		}
		if role.IsHoisted && (highestRole == nil || role.Position > highestRole.Position) {
			highestRole = role
		}
	}

	avatarColor := ""
	if highestRole != nil && highestRole.Color != 0 {
		avatarColor = highestRole.GetColorHex()
	}
	if avatarColor == "" {
		// Deterministic fallback: hash username into palette
		hash := 0
		for _, c := range user.Username {
			hash = (hash*31 + int(c)) & 0x7fffffff
		}
		avatarColor = avatarPalette[hash%len(avatarPalette)]
	}

	return &MemberDisplay{
		User:        user,
		Member:      member,
		HighestRole: highestRole,
		AvatarColor: avatarColor,
	}
}

// containsMention reports whether content contains a @mention of alias.
func containsMention(content, alias string) bool {
	if alias == "" {
		return false
	}
	return strings.Contains(strings.ToLower(content), "@"+strings.ToLower(alias))
}

// loadMutedChannels converts the persisted []string of UUIDs into the runtime map.
func loadMutedChannels(cfg *AppConfig) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool)
	if cfg == nil {
		return m
	}
	for _, s := range cfg.UI.MutedChannels {
		if id, err := uuid.Parse(s); err == nil {
			m[id] = true
		}
	}
	return m
}

// getActiveServerID returns the server UUID for the currently active connection, or uuid.Nil.
func (a *App) getActiveServerID() uuid.UUID {
	if a.currentServer != nil {
		return a.currentServer.ID
	}
	return uuid.Nil
}

// saveMutedChannels persists the current mutedChannels map back to config.json.
func (a *App) saveMutedChannels() {
	if a.configMgr == nil {
		return
	}
	cfg, err := a.configMgr.LoadAppConfig()
	if err != nil || cfg == nil {
		cfg = &AppConfig{Version: 1}
	}
	slugs := make([]string, 0, len(a.mutedChannels))
	for id := range a.mutedChannels {
		slugs = append(slugs, id.String())
	}
	cfg.UI.MutedChannels = slugs
	_ = a.configMgr.SaveAppConfig(cfg)
}

// NewApp creates a new application instance
func NewApp(clientServers []*ClientServerInfo, defaultPrefs *DefaultPreferences, configMgr *ConfigManager, identity *LocalIdentity) *App {
	// Initialize textarea for chat
	input := textarea.New()
	input.Placeholder = "Type a message..."
	input.CharLimit = 2000
	input.SetWidth(50)
	input.SetHeight(4)  // 4 rows of text (matches layout slot: 2 borders + 4 content = 6 rows)
	input.ShowLineNumbers = false
	input.Prompt = "" // remove the default "> " prompt gutter character
	// Configure keybindings: Enter sends the message (handled in handleKeyPress).
	// Ctrl+Enter or Ctrl+J inserts a newline in the compose box.
	// - Ctrl+Enter: works in Windows Terminal and terminals with CSI u support
	// - Ctrl+J: sends ASCII 0x0A (LF), distinct from 0x0D (CR/Enter) in all terminals
	// - Shift+Enter: kept as fallback for kitty/modern terminal emulators
	input.KeyMap.InsertNewline.SetKeys("ctrl+enter", "ctrl+j", "shift+enter")

	// Initialize login inputs
	loginEmail := textinput.New()
	loginEmail.Placeholder = "Email"
	loginEmail.Focus()

	loginPassword := textinput.New()
	loginPassword.Placeholder = "Password"
	loginPassword.EchoMode = textinput.EchoPassword

	loginPasswordConfirm := textinput.New()
	loginPasswordConfirm.Placeholder = "Confirm Password"
	loginPasswordConfirm.EchoMode = textinput.EchoPassword

	loginUsername := textinput.New()
	loginUsername.Placeholder = "Alias (Display Name)"
	loginUsername.CharLimit = 32

	// Initialize add server inputs
	addServerName := textinput.New()
	addServerName.Placeholder = "Server Name"
	addServerName.CharLimit = 50

	addServerAddress := textinput.New()
	addServerAddress.Placeholder = "localhost"
	addServerAddress.CharLimit = 100

	addServerPort := textinput.New()
	addServerPort.Placeholder = "8080"
	addServerPort.CharLimit = 5

	// Load default theme
	theme := themes.GetDefaultTheme()
	styles := theme.BuildStyles()

	// Select random banner at startup (not on every render)
	banner := GetRandomBanner()

	// Create event channel for async connection events
	connEvents := make(chan tea.Msg, 10)

	// Create connection manager
	connMgr := NewConnectionManager(connEvents)

	// Add all client servers to connection manager
	for _, serverInfo := range clientServers {
		connMgr.AddServer(serverInfo)
	}

	// Select first client server if available
	var currentClientServer *ClientServerInfo
	if len(clientServers) > 0 {
		currentClientServer = clientServers[0]
	}

	// Load UI config
	appConfig, err := configMgr.LoadAppConfig()
	if err != nil {
		log.Printf("Warning: Failed to load app config: %v, using defaults", err)
		appConfig = &AppConfig{
			Version: 1,
			UI: UIConfig{
				Theme:               "dracula",
				ShowMembersList:     true,
				CollapsedCategories: make(map[string]map[string]bool),
			},
		}
	}

	// Determine startup view
	startView := ViewIdentitySetup
	if identity != nil {
		servers := configMgr.GetClientServers()
		if len(servers) == 0 {
			startView = ViewAddServer
		} else {
			startView = ViewLogin
		}
	}

	// Initialize identity setup form inputs
	identityAlias := textinput.New()
	identityAlias.Placeholder = "Alias"
	identityAlias.CharLimit = 32
	identityAlias.Focus()

	identityEmail := textinput.New()
	identityEmail.Placeholder = "Email"
	identityEmail.CharLimit = 255

	identityPassword := textinput.New()
	identityPassword.Placeholder = "Password (min 8 chars)"
	identityPassword.EchoMode = textinput.EchoPassword
	identityPassword.CharLimit = 128

	identityPasswordConfirm := textinput.New()
	identityPasswordConfirm.Placeholder = "Confirm Password"
	identityPasswordConfirm.EchoMode = textinput.EchoPassword
	identityPasswordConfirm.CharLimit = 128

	app := &App{
		view:                    startView,
		focus:                   FocusServerIcons, // Start on server list; user navigates into a channel before typing
		theme:                   theme,
		styles:                  styles,
		banner:                  banner,
		connMgr:                 connMgr,
		connEvents:              connEvents,
		localIdentity:           identity,
		clientServers:           clientServers,
		currentClientServer:     currentClientServer,
		serverIndex:             0,
		defaultPreferences:      defaultPrefs,
		configMgr:               configMgr,
		uiConfig:                &appConfig.UI,
		collapsedCategories:     make(map[uuid.UUID]bool),
		unreadCounts:            make(map[uuid.UUID]map[uuid.UUID]int),
		mentionCounts:           make(map[uuid.UUID]map[uuid.UUID]int),
		mutedChannels:           loadMutedChannels(appConfig),
		input:                   input,
		loginEmail:              loginEmail,
		loginPassword:           loginPassword,
		loginPasswordConfirm:    loginPasswordConfirm,
		loginUsername:           loginUsername,
		identityAlias:           identityAlias,
		identityEmail:           identityEmail,
		identityPassword:        identityPassword,
		identityPasswordConfirm: identityPasswordConfirm,
		addServerName:           addServerName,
		addServerAddress:        addServerAddress,
		addServerPort:           addServerPort,
		addServerUseTLS:         false,
	}

	// Initialize command handler
	app.commandHandler = NewCommandHandler(app)

	// Pre-fill login form with saved credentials or defaults
	app.initLoginView()

	return app
}

// initLoginView initializes and pre-fills the login view
func (a *App) initLoginView() {
	// Reset form fields
	a.loginEmail.Reset()
	a.loginPassword.Reset()
	a.loginUsername.Reset()
	a.loginPasswordConfirm.Reset()
	a.loginError = ""
	a.loginFocus = 0
	a.registerLinkFocused = false

	// When local identity exists, pre-fill email and start on password field
	if a.localIdentity != nil {
		a.loginEmail.SetValue(a.localIdentity.Email)
		a.loginEmail.Blur()
		a.loginFocus = 1
		a.loginPassword.Focus()
		return
	}

	a.loginEmail.Focus()

	// Pre-fill email from saved credentials or default preferences
	if a.currentClientServer != nil && a.currentClientServer.SavedCredentials != nil {
		// Use saved credentials if available
		if a.currentClientServer.SavedCredentials.Email != "" {
			a.loginEmail.SetValue(a.currentClientServer.SavedCredentials.Email)
		}
	} else if a.defaultPreferences != nil {
		// Fall back to default preferences
		if a.defaultPreferences.Email != "" {
			a.loginEmail.SetValue(a.defaultPreferences.Email)
		}
	}
}

// Init implements tea.Model
func (a *App) Init() tea.Cmd {
	a.lastActivityTime = time.Now()
	cmds := []tea.Cmd{
		textinput.Blink,
		a.waitForConnEvent(),
		tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return afkCheckMsg{t} }),
		tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg { return typingTickMsg(t) }),
	}
	// Auto-connect all known servers when identity is configured
	if a.localIdentity != nil {
		// Start with focus on server icons; user navigates to a channel before typing.
		// (textarea focus is activated when the user switches to FocusInput)
		for _, server := range a.clientServers {
			cmds = append(cmds, a.autoConnectServer(server.ID))
		}
	}
	return tea.Batch(cmds...)
}

// waitForConnEvent creates a command that waits for connection events
func (a *App) waitForConnEvent() tea.Cmd {
	return func() tea.Msg {
		return <-a.connEvents // Blocks until event arrives
	}
}

// connectServerAsync initiates background connection without blocking UI
func (a *App) connectServerAsync(serverID uuid.UUID, token string) tea.Cmd {
	return func() tea.Msg {
		// Connect WebSocket
		if err := a.connMgr.ConnectServer(serverID); err != nil {
			return ConnectionFailedMsg{
				ServerID: serverID,
				Error:    err.Error(),
				Retry:    true,
			}
		}

		// Set active connection IMMEDIATELY after connect (before Identify)
		// This prevents race condition where READY arrives before activeConn is set
		a.activeConn = a.connMgr.GetConnection(serverID)
		if a.activeConn != nil {
			a.activeConn.mu.Lock()
			a.activeConn.Token = token
			a.activeConn.mu.Unlock()
		}

		// Authenticate (READY response will arrive asynchronously)
		if err := a.connMgr.Identify(serverID, token); err != nil {
			return ConnectionFailedMsg{
				ServerID: serverID,
				Error:    err.Error(),
				Retry:    false,
			}
		}

		return ConnectionReadyMsg{ServerID: serverID}
	}
}

// scheduleReconnect schedules a reconnection attempt with exponential backoff
func (a *App) scheduleReconnect(serverID uuid.UUID) tea.Cmd {
	sc := a.connMgr.GetConnection(serverID)
	if sc == nil {
		return nil
	}

	sc.mu.Lock()
	sc.RetryCount++
	attemptCount := sc.RetryCount

	if sc.RetryStrategy == nil {
		sc.RetryStrategy = DefaultReconnectStrategy()
	}
	strategy := sc.RetryStrategy
	token := sc.Token
	sc.mu.Unlock()

	if !strategy.ShouldRetry(attemptCount) {
		return func() tea.Msg {
			return ErrorMsg{Error: "Max reconnection attempts reached"}
		}
	}

	delay := strategy.NextDelay(attemptCount)

	return func() tea.Msg {
		// Notify about retry
		time.Sleep(delay)

		// Attempt reconnection
		return a.connectServerAsync(serverID, token)()
	}
}

// autoConnectServer performs HTTP-only login/register for a server in the background.
// WebSocket connection is handled separately by connectServerAsync (via AutoConnectMsg handler),
// which sets activeConn before calling Identify — preventing the READY event race condition.
func (a *App) autoConnectServer(serverID uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		id := a.localIdentity
		if id == nil {
			return AutoConnectMsg{ServerID: serverID, Err: fmt.Errorf("no local identity configured")}
		}

		// HTTP login/register only — WebSocket connection follows via connectServerAsync
		result := a.connMgr.AutoConnectHTTP(serverID, id.Email, id.Alias, id.Password)
		return AutoConnectMsg{
			ServerID: result.ServerID,
			UserID:   result.UserID,
			Email:    result.Email,
			Token:    result.Token,
			Err:      result.Err,
		}
	}
}

// Update implements tea.Model
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case afkCheckMsg:
		// Check if the user has been idle for 10 minutes
		if time.Since(a.lastActivityTime) >= 10*time.Minute && !a.isAFK && a.activeConn != nil {
			a.isAFK = true
			sc := a.activeConn
			if sc.Connection != nil {
				payload := &protocol.PresenceUpdatePayload{Status: models.StatusIdle}
				if wsMsg, err := protocol.NewMessage(protocol.OpPresenceUpdate, payload); err == nil {
					_ = sc.Connection.Send(wsMsg)
				}
			}
		}
		// Re-schedule the AFK check
		cmds = append(cmds, tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return afkCheckMsg{t} }))

	case typingTickMsg:
		// Advance animation frame and prune expired typing entries
		a.typingFrame++
		now := time.Time(msg)
		if a.typingExpiry != nil {
			changed := false
			for uid, exp := range a.typingExpiry {
				if now.After(exp) {
					delete(a.typingExpiry, uid)
					changed = true
				}
			}
			if changed {
				a.rebuildTypingUsers()
			}
		}
		cmds = append(cmds, tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg { return typingTickMsg(t) }))

	case tea.KeyMsg:
		// Any key press resets AFK state
		a.lastActivityTime = time.Now()
		if a.isAFK && a.activeConn != nil {
			a.isAFK = false
			sc := a.activeConn
			if sc.Connection != nil {
				payload := &protocol.PresenceUpdatePayload{Status: models.StatusOnline}
				if wsMsg, err := protocol.NewMessage(protocol.OpPresenceUpdate, payload); err == nil {
					_ = sc.Connection.Send(wsMsg)
				}
			}
		}
		// Store current view before handling key
		viewBeforeKey := a.view
		cmd := a.handleKeyPress(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		// If view changed, skip component updates (view transition handled)
		if a.view != viewBeforeKey {
			return a, tea.Batch(cmds...)
		}
		// Send typing indicator when composing (throttled to once per 4 seconds).
		// Only send if there's actual text in the input box — this way the indicator
		// automatically clears when the message is sent (input empties).
		if a.view == ViewMain && a.focus == FocusInput &&
			len(a.input.Value()) > 0 &&
			a.activeConn != nil && a.currentChannel != nil && a.currentClientServer != nil &&
			time.Since(a.lastTypingSent) > 4*time.Second {
			a.lastTypingSent = time.Now()
			serverID := a.currentClientServer.ID
			channelID := a.currentChannel.ID
			cmds = append(cmds, func() tea.Msg {
				_ = a.connMgr.SendTyping(serverID, channelID)
				return nil
			})
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// updateViewportSize sets chatViewport.Width/Height using the same layout
		// math as renderMainView/renderChatPanel, so updateChatContent below renders
		// with correct line widths (fixes blank chat on first load).
		a.updateViewportSize()
		if a.activeConn != nil && a.currentChannel != nil {
			a.updateChatContent()
			a.scrollToBottom()
		}

	case ServerScopedMsg:
		// Handle server-scoped messages from ConnectionManager
		cmd := a.handleServerScopedMessage(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Re-subscribe to connection events
		cmds = append(cmds, a.waitForConnEvent())

	case ConnectedMsg:
		a.statusMessage = "Connected to server"
		a.statusError = false
		// Re-subscribe to connection events
		cmds = append(cmds, a.waitForConnEvent())

	case DisconnectedMsg:
		a.statusMessage = "Disconnected from server"
		a.statusError = true
		// Re-subscribe to connection events
		cmds = append(cmds, a.waitForConnEvent())

	case LoginSuccessMsg:
		a.view = ViewMain
		a.focus = FocusInput
		a.input.Focus()
		a.statusMessage = "Connecting to server..."
		a.statusError = false

		// Initiate background connection
		cmds = append(cmds, a.connectServerAsync(msg.ServerID, msg.Token))

	case LoginErrorMsg:
		a.loginError = msg.Error
		a.statusError = true

	case AutoConnectMsg:
		if msg.Err != nil {
			log.Printf("Auto-connect failed for server %s: %v", msg.ServerID, msg.Err)
			a.statusMessage = fmt.Sprintf("Could not connect to server: %v", msg.Err)
			a.statusError = true
		} else if msg.Token != "" {
			// Save token to disk for future sessions
			go func() {
				if err := a.configMgr.SaveServerToken(msg.ServerID, msg.Email, msg.Token, msg.UserID); err != nil {
					log.Printf("Failed to save server token: %v", err)
				}
			}()
			// Select this server in UI if none is active yet (first to auth wins)
			if a.activeConn == nil {
				for i, cs := range a.clientServers {
					if cs.ID == msg.ServerID {
						a.currentClientServer = cs
						a.serverIndex = i
						break
					}
				}
			}
			// connectServerAsync sets activeConn BEFORE Identify, preventing
			// the race where READY arrives before activeConn is set
			cmds = append(cmds, a.connectServerAsync(msg.ServerID, msg.Token))
		}
		// Re-subscribe to connection events
		cmds = append(cmds, a.waitForConnEvent())

	case ConnectionReadyMsg:
		// Set connection state to ready
		sc := a.connMgr.GetConnection(msg.ServerID)
		if sc != nil {
			sc.SetState(StateReady)
		}

		// Update status bar
		a.statusMessage = "Connected to server"
		a.statusError = false
		// Re-subscribe to connection events
		cmds = append(cmds, a.waitForConnEvent())

	case ConnectionFailedMsg:
		sc := a.connMgr.GetConnection(msg.ServerID)
		if sc != nil {
			sc.SetState(StateError)
			sc.LastError = fmt.Errorf("%s", msg.Error)
			a.statusMessage = fmt.Sprintf("Connection failed: %s", msg.Error)
			a.statusError = true

			if msg.Retry {
				cmds = append(cmds, a.scheduleReconnect(msg.ServerID))
			}
		}

	case ConnectionRetryingMsg:
		a.statusMessage = fmt.Sprintf("Reconnecting (attempt %d)...", msg.AttemptCount)
		a.statusError = false

	case ServerPingResultMsg:
		// Update ping results
		if a.pingResults != nil {
			a.pingResults[msg.ServerID] = msg.Result
		}

	case ErrorMsg:
		a.statusMessage = msg.Error
		a.statusError = true
		// Re-subscribe to connection events
		cmds = append(cmds, a.waitForConnEvent())
	}

	// Update focused component
	switch a.view {
	case ViewIdentitySetup:
		cmd := a.updateIdentitySetupForm(msg)
		cmds = append(cmds, cmd)
	case ViewLogin, ViewRegister:
		cmd := a.updateLoginForm(msg)
		cmds = append(cmds, cmd)
	case ViewAddServer:
		cmd := a.updateAddServerForm(msg)
		cmds = append(cmds, cmd)
	case ViewMain:
		if a.focus == FocusInput {
			var cmd tea.Cmd
			a.input, cmd = a.input.Update(msg)
			cmds = append(cmds, cmd)
			a.updateMentionPopup()
		} else if a.focus == FocusChat {
			var cmd tea.Cmd
			a.chatViewport, cmd = a.chatViewport.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return a, tea.Batch(cmds...)
}

// View implements tea.Model
func (a *App) View() string {
	var baseView string
	switch a.view {
	case ViewIdentitySetup:
		baseView = a.renderIdentitySetupView()
	case ViewLogin:
		baseView = a.renderLoginView()
	case ViewRegister:
		baseView = a.renderRegisterView()
	case ViewMain:
		baseView = a.renderMainView()
	case ViewAddServer:
		baseView = a.renderAddServerView()
	case ViewManageServers:
		baseView = a.renderManageServersView()
	case ViewThemeBrowser:
		baseView = a.renderThemeBrowserView()
	default:
		baseView = "Unknown view"
	}

	// Render link browser overlay if active
	if a.linkBrowserState != nil {
		return a.renderLinkBrowserOverlay(baseView)
	}

	return baseView
}

// handleKeyPress handles keyboard input
func (a *App) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	// Route to view-specific handlers first
	if a.view == ViewIdentitySetup {
		return a.handleIdentitySetupKey(msg)
	}
	if a.view == ViewManageServers {
		return a.handleManageServersKey(msg)
	}
	if a.view == ViewThemeBrowser {
		return a.handleThemeBrowserKey(msg)
	}

	switch msg.String() {
	case "ctrl+q":
		return tea.Quit

	case "ctrl+t":
		// Open theme browser from login or main view
		if a.view == ViewLogin || a.view == ViewMain {
			a.openThemeBrowser(a.view)
			return nil
		}

	case "tab":
		if a.view == ViewAddServer {
			a.cycleAddServerFocus()
		} else if a.view == ViewMain && a.focus == FocusInput {
			// @mention completion takes priority — works inside slash commands too
			if a.showMentionPopup && len(a.mentionSuggestions) > 0 {
				a.completeMention(a.mentionSuggestions[0])
				return nil
			}
			input := a.input.Value()
			if strings.HasPrefix(input, "/") {
				a.handleTabCompletion()
				return nil
			}
			a.cycleFocus()
		} else {
			a.cycleFocus()
		}
		return nil

	case "shift+tab":
		if a.view == ViewAddServer {
			a.cycleAddServerFocusReverse()
		} else {
			a.cycleFocusReverse()
		}
		return nil

	case "enter":
		// In link browser: open selected link
		if a.linkBrowserState != nil {
			if a.linkBrowserState.SelectedIndex >= 0 && a.linkBrowserState.SelectedIndex < len(a.linkBrowserState.Links) {
				link := a.linkBrowserState.Links[a.linkBrowserState.SelectedIndex]
				a.closeLinkBrowser()
				return a.openURL(link)
			}
			return nil
		}
		if a.view == ViewLogin {
			if a.registerLinkFocused {
				// User pressed enter on "Register New Account" link
				a.view = ViewRegister
				a.loginFocus = 0
				a.registerLinkFocused = false
				a.loginEmail.Focus()
				a.loginEmail.Reset()
				a.loginPassword.Reset()
				a.loginUsername.Reset()
				a.loginPasswordConfirm.Reset()
				a.loginError = ""

				// Pre-fill with default preferences if available
				if a.defaultPreferences != nil {
					if a.defaultPreferences.Email != "" {
						a.loginEmail.SetValue(a.defaultPreferences.Email)
					}
					if a.defaultPreferences.Username != "" {
						a.loginUsername.SetValue(a.defaultPreferences.Username)
					}
				}
				return nil
			}
			return a.handleLoginSubmit()
		}
		if a.view == ViewRegister {
			return a.handleRegisterSubmit()
		}
		if a.view == ViewAddServer {
			return a.handleAddServerSubmit()
		}
		// In message navigation Level 1: Enter transitions to Level 2
		if a.messageNavMode && !a.inMessageEditMode {
			a.inMessageEditMode = true
			a.messageCursorLine = 0
			a.messageCursorCol = 0
			a.messageSelectionStart = nil
			a.messageSelectionEnd = nil
			// Refresh viewport to show Level 2 background color
			a.updateChatContent()
			return nil
		}
		// For main view
		if a.view == ViewMain {
			// If focused on server icons
			if a.focus == FocusServerIcons {
				// Check if "Manage Servers" button is selected
				if a.serverIndex >= len(a.clientServers) {
					a.view = ViewManageServers
					a.manageServersFocus = 0
					if a.pingResults == nil {
						a.pingResults = make(map[uuid.UUID]*PingResult)
					}
					return nil
				}
				// Otherwise select the server and go to login if not connected
				a.switchToClientServer(a.serverIndex)
				if a.activeConn == nil || a.activeConn.GetState() != StateReady {
					a.view = ViewLogin
					a.initLoginView()
				}
				return nil
			}
			// If focused on input, send message
			if a.focus == FocusInput {
				return a.handleSendMessage()
			}
		}

	case "esc":
		// Close link browser if active
		if a.linkBrowserState != nil {
			a.closeLinkBrowser()
			return nil
		}
		// Two-level escape: Level 2 → Level 1, Level 1 → Normal chat
		if a.messageNavMode {
			if a.inMessageEditMode {
				// Esc from Level 2: return to Level 1 (message selection)
				a.inMessageEditMode = false
				a.messageSelectionStart = nil
				a.messageSelectionEnd = nil
				a.messageCursorLine = 0
				a.messageCursorCol = 0
				// Refresh viewport to show Level 1 background color
				a.updateChatContent()
			} else {
				// Esc from Level 1: exit navigation mode entirely
				a.messageNavMode = false
				a.messageNavIndex = 0
				// Restore previous focus (input or chat)
				if a.focus == FocusMessageNav {
					a.focus = FocusInput
					a.input.Focus()
				}
				// Refresh viewport to clear highlighting
				a.updateChatContent()
				// Show terminal cursor again when exiting navigation
				return tea.ShowCursor
			}
			return nil
		}
		// Dismiss @mention popup if open
		if a.showMentionPopup {
			a.showMentionPopup = false
			a.mentionSuggestions = nil
			return nil
		}
		if a.view == ViewRegister {
			// Go back to login view
			a.view = ViewLogin
			a.loginUsername.Blur()
			a.loginPassword.Blur()
			a.loginPasswordConfirm.Blur()
			a.initLoginView()
		} else if a.view == ViewAddServer {
			// Cancel: return to manage servers if editing, otherwise main view
			a.addServerError = ""
			if a.editingServerID != nil {
				a.editingServerID = nil
				a.view = ViewManageServers
			} else {
				a.view = ViewMain
			}
		} else if a.view == ViewMain {
			a.focus = FocusServerIcons
			a.input.Blur()
		}

	case " ", "space":
		// Toggle TLS in Add Server view when on TLS field
		if a.view == ViewAddServer && a.addServerFocus == 3 {
			a.addServerUseTLS = !a.addServerUseTLS
		}

	case "alt+m":
		// Enter Level 1: Message Selection Mode
		if a.view == ViewMain && (a.focus == FocusChat || a.focus == FocusInput) &&
			a.activeConn != nil && a.currentChannel != nil {
			messages := a.activeConn.GetMessages(a.currentChannel.ID)
			if len(messages) > 0 {
				a.messageNavMode = true
				a.inMessageEditMode = false // Start in Level 1 (message selection)
				a.focus = FocusMessageNav
				a.messageNavIndex = len(messages) - 1 // Start at newest message
				a.messageSelectionStart = nil         // Clear any previous selection
				a.messageSelectionEnd = nil
				a.input.Blur()
				// Refresh viewport to show highlighting
				a.updateChatContent()
				// Hide terminal cursor when in navigation mode
				return tea.HideCursor
			}
		}

	case "c":
		// In link browser: copy selected link URL
		if a.linkBrowserState != nil {
			if a.linkBrowserState.SelectedIndex >= 0 && a.linkBrowserState.SelectedIndex < len(a.linkBrowserState.Links) {
				link := a.linkBrowserState.Links[a.linkBrowserState.SelectedIndex]
				if err := clipboard.WriteAll(link); err != nil {
					a.statusMessage = fmt.Sprintf("Failed to copy: %v", err)
					a.statusError = true
				} else {
					a.statusMessage = "Copied link URL"
					a.statusError = false
				}
			}
			return nil
		}
		// Copy selected message to clipboard (in message navigation mode)
		if a.messageNavMode {
			return a.copyMessageToClipboard()
		}

	case "C":
		// Uppercase C also copies in message navigation mode
		if a.messageNavMode {
			return a.copyMessageToClipboard()
		}

	case "ctrl+c":
		// Ctrl+C copies in message navigation mode
		if a.messageNavMode {
			return a.copyMessageToClipboard()
		}

	case "l":
		// In message navigation mode: open link browser if message has URLs
		if a.messageNavMode && a.activeConn != nil && a.currentChannel != nil {
			messages := a.activeConn.GetMessages(a.currentChannel.ID)
			if a.messageNavIndex >= 0 && a.messageNavIndex < len(messages) {
				msg := messages[a.messageNavIndex]
				links := a.extractLinksFromMessage(msg)
				if len(links) > 0 {
					a.openLinkBrowser(links, msg, "message_nav")
					// Temporarily exit message nav mode while in link browser
					a.messageNavMode = false
				} else {
					a.statusMessage = "No links found in selected message"
					a.statusError = false
				}
			}
			return nil
		}
		// In channel list: expand category (existing behavior)
		if a.focus == FocusChannelList {
			a.handleExpandCategory()
		}

	case "up":
		// Link browser navigation takes highest priority
		if a.linkBrowserState != nil {
			a.linkBrowserState.SelectedIndex--
			if a.linkBrowserState.SelectedIndex < 0 {
				a.linkBrowserState.SelectedIndex = len(a.linkBrowserState.Links) - 1
			}
			return nil
		}
		// Message navigation: Level 1 or Level 2
		if a.messageNavMode {
			if a.inMessageEditMode {
				// Level 2: Move cursor up one line within the message
				return a.moveCursorInMessage(0, -1, true) // dy = -1 (up), clear selection
			} else {
				// Level 1: Navigate to previous message
				return a.navigateMessage(-1)
			}
		}
		if a.focus == FocusServerIcons {
			a.navigateServerList(-1)
		} else if a.focus == FocusChannelList {
			a.navigateChannelList(-1)
		} else if a.focus == FocusChat {
			a.chatViewport.LineUp(1)
		}

	case "down":
		// Link browser navigation takes highest priority
		if a.linkBrowserState != nil {
			a.linkBrowserState.SelectedIndex++
			if a.linkBrowserState.SelectedIndex >= len(a.linkBrowserState.Links) {
				a.linkBrowserState.SelectedIndex = 0
			}
			return nil
		}
		// Message navigation: Level 1 or Level 2
		if a.messageNavMode {
			if a.inMessageEditMode {
				// Level 2: Move cursor down one line within the message
				return a.moveCursorInMessage(0, 1, true) // dy = 1 (down), clear selection
			} else {
				// Level 1: Navigate to next message
				return a.navigateMessage(1)
			}
		}
		if a.focus == FocusServerIcons {
			a.navigateServerList(1)
		} else if a.focus == FocusChannelList {
			a.navigateChannelList(1)
		} else if a.focus == FocusChat {
			a.chatViewport.LineDown(1)
		}

	case "shift+up":
		// Level 2: Select text while moving cursor up
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorWithSelection(0, -1) // dy = -1
		}
		if a.view == ViewMain && a.focus == FocusChannelList {
			return a.reorderChannel(-1)
		}

	case "shift+down":
		// Level 2: Select text while moving cursor down
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorWithSelection(0, 1) // dy = 1
		}
		if a.view == ViewMain && a.focus == FocusChannelList {
			return a.reorderChannel(1)
		}

	case "shift+left":
		// Level 2: Select text while moving cursor left
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorWithSelection(-1, 0) // dx = -1
		}

	case "shift+right":
		// Level 2: Select text while moving cursor right
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorWithSelection(1, 0) // dx = 1
		}

	case "left":
		// Level 2: Move cursor left one character
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorInMessage(-1, 0, true) // dx = -1 (left), clear selection
		}
		if a.focus == FocusChannelList {
			a.handleCollapseCategory()
		}

	case "h":
		if a.focus == FocusChannelList {
			a.handleCollapseCategory()
		}

	case "right":
		// Level 2: Move cursor right one character
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorInMessage(1, 0, true) // dx = 1 (right), clear selection
		}
		if a.focus == FocusChannelList {
			a.handleExpandCategory()
		}

	case "pgup":
		if a.focus == FocusChat {
			a.chatViewport.HalfViewUp()
		}

	case "pgdown":
		if a.focus == FocusChat {
			a.chatViewport.HalfViewDown()
		}

	case "ctrl+s":
		// Cycle through client servers (forward)
		if len(a.clientServers) > 0 {
			a.serverIndex = (a.serverIndex + 1) % len(a.clientServers)
			a.switchToClientServer(a.serverIndex)
		}

	case "ctrl+shift+s":
		// Cycle through client servers (backward)
		if len(a.clientServers) > 0 {
			a.serverIndex--
			if a.serverIndex < 0 {
				a.serverIndex = len(a.clientServers) - 1
			}
			a.switchToClientServer(a.serverIndex)
		}

	case "ctrl+n":
		// Open Add Server dialog
		if a.view == ViewMain {
			a.view = ViewAddServer
			a.initAddServerForm()
		}
	}

	return nil
}

// cycleFocus moves focus to the next area
func (a *App) cycleFocus() {
	if a.view == ViewLogin {
		if a.localIdentity != nil {
			// Local identity mode: email is locked, only cycle to password
			a.loginFocus = (a.loginFocus + 1) % 2 // 0=email(locked), 1=password
			switch a.loginFocus {
			case 0:
				a.loginEmail.Focus()
				a.loginPassword.Blur()
			case 1:
				a.loginEmail.Blur()
				a.loginPassword.Focus()
			}
			a.registerLinkFocused = false
			return
		}
		// Standard login view: email, password, register link
		if !a.registerLinkFocused {
			a.loginFocus = (a.loginFocus + 1) % 3 // 0=email, 1=password, 2=register link

			switch a.loginFocus {
			case 0:
				a.loginEmail.Focus()
				a.loginPassword.Blur()
				a.registerLinkFocused = false
			case 1:
				a.loginEmail.Blur()
				a.loginPassword.Focus()
				a.registerLinkFocused = false
			case 2:
				a.loginEmail.Blur()
				a.loginPassword.Blur()
				a.registerLinkFocused = true
			}
		} else {
			// Currently on register link, cycle back to email
			a.loginFocus = 0
			a.loginEmail.Focus()
			a.loginPassword.Blur()
			a.registerLinkFocused = false
		}
		return
	}

	if a.view == ViewRegister {
		// Register view: email, username, password, confirm password
		a.loginFocus = (a.loginFocus + 1) % 4

		switch a.loginFocus {
		case 0:
			a.loginEmail.Focus()
			a.loginUsername.Blur()
			a.loginPassword.Blur()
			a.loginPasswordConfirm.Blur()
		case 1:
			a.loginEmail.Blur()
			a.loginUsername.Focus()
			a.loginPassword.Blur()
			a.loginPasswordConfirm.Blur()
		case 2:
			a.loginEmail.Blur()
			a.loginUsername.Blur()
			a.loginPassword.Focus()
			a.loginPasswordConfirm.Blur()
		case 3:
			a.loginEmail.Blur()
			a.loginUsername.Blur()
			a.loginPassword.Blur()
			a.loginPasswordConfirm.Focus()
		}
		return
	}

	switch a.focus {
	case FocusServerIcons:
		a.focus = FocusChannelList
	case FocusChannelList:
		a.focus = FocusChat
	case FocusChat:
		a.focus = FocusInput
		a.input.Focus()
	case FocusInput:
		a.input.Blur()
		a.focus = FocusUserList
	case FocusUserList:
		a.focus = FocusServerIcons
	}
}

// cycleFocusReverse moves focus to the previous area
func (a *App) cycleFocusReverse() {
	switch a.focus {
	case FocusServerIcons:
		a.focus = FocusUserList
	case FocusChannelList:
		a.focus = FocusServerIcons
	case FocusChat:
		a.focus = FocusChannelList
	case FocusInput:
		a.focus = FocusChat
		a.input.Blur()
	case FocusUserList:
		a.focus = FocusInput
		a.input.Focus()
	}
}

// getCurrentChannels returns channels for the current protocol server
func (a *App) getCurrentChannels() []*models.Channel {
	if a.activeConn == nil || a.currentServer == nil {
		return []*models.Channel{}
	}
	return a.activeConn.GetChannels(a.currentServer.ID)
}

// navigateServerList navigates the client server list
func (a *App) navigateServerList(delta int) {
	// Total items = servers + add button (+)
	totalItems := len(a.clientServers) + 1

	a.serverIndex += delta
	if a.serverIndex < 0 {
		a.serverIndex = totalItems - 1
	} else if a.serverIndex >= totalItems {
		a.serverIndex = 0
	}

	// Only switch server if not on the (+) button
	if a.serverIndex < len(a.clientServers) {
		a.switchToClientServer(a.serverIndex)
	}
}

// navigateChannelList navigates the channel list for the current server
func (a *App) navigateChannelList(delta int) {
	if a.channelTree == nil || len(a.channelTree.FlatList) == 0 {
		return
	}

	// Find current channel in flat list
	currentIdx := -1
	for i, node := range a.channelTree.FlatList {
		if !node.IsCategory && a.currentChannel != nil && node.Channel.ID == a.currentChannel.ID {
			currentIdx = i
			break
		}
	}

	// Navigate to next/previous channel (skip categories)
	newIdx := currentIdx
	for {
		newIdx += delta

		// Wrap around
		if newIdx < 0 {
			newIdx = len(a.channelTree.FlatList) - 1
		} else if newIdx >= len(a.channelTree.FlatList) {
			newIdx = 0
		}

		// Stop if we've wrapped around back to start
		if newIdx == currentIdx {
			break
		}

		// Found a channel (not a category)
		if !a.channelTree.FlatList[newIdx].IsCategory {
			a.selectChannelByID(a.channelTree.FlatList[newIdx].Channel.ID)
			break
		}
	}
}

// reorderChannel moves the currently selected channel up (delta=-1) or down (delta=+1)
// within its sibling list. Positions are normalized across siblings then swapped, and
// two OpChannelUpdate messages are sent to persist the new order on the server.
func (a *App) reorderChannel(delta int) tea.Cmd {
	if a.activeConn == nil || a.currentServer == nil || a.channelTree == nil || a.currentChannel == nil {
		return nil
	}

	// Find the current channel's node in the tree
	node, ok := a.channelTree.NodeMap[a.currentChannel.ID]
	if !ok || node.Parent == nil {
		return nil
	}

	siblings := node.Parent.Children

	// Find current index in siblings
	currentIdx := -1
	for i, s := range siblings {
		if s == node {
			currentIdx = i
			break
		}
	}
	if currentIdx < 0 {
		return nil
	}

	targetIdx := currentIdx + delta
	if targetIdx < 0 || targetIdx >= len(siblings) {
		// Already at boundary — nothing to do
		return nil
	}
	targetNode := siblings[targetIdx]

	// Normalize sibling positions to distinct values (i*10) before swapping,
	// so equal-position channels (all new channels default to 0) still sort correctly.
	for i, sib := range siblings {
		sib.Channel.Position = i * 10
	}

	// Swap positions between the current and target nodes (they share pointers with sc.Channels)
	node.Channel.Position, targetNode.Channel.Position = targetNode.Channel.Position, node.Channel.Position

	// Swap in the siblings slice so RebuildFlatList reflects the new order immediately
	siblings[currentIdx], siblings[targetIdx] = siblings[targetIdx], siblings[currentIdx]

	// Re-sort sc.Channels by position so future loadChannelTree calls use the correct order
	serverID := a.currentServer.ID
	a.activeConn.mu.Lock()
	sort.Slice(a.activeConn.Channels[serverID], func(i, j int) bool {
		return a.activeConn.Channels[serverID][i].Position < a.activeConn.Channels[serverID][j].Position
	})
	a.activeConn.mu.Unlock()

	// Rebuild flat list for immediate rendering
	a.channelTree.RebuildFlatList(a.collapsedCategories)

	// Capture values before the goroutine closure
	currID := node.Channel.ID
	targetID := targetNode.Channel.ID
	currPos := node.Channel.Position
	targetPos := targetNode.Channel.Position
	pServerID := serverID

	return func() tea.Msg {
		conn := a.activeConn
		if conn == nil {
			return nil
		}
		req1 := &protocol.ChannelUpdateRequest{
			ServerID:  pServerID,
			ChannelID: currID,
			Position:  &currPos,
		}
		if msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req1); err == nil {
			_ = conn.Connection.Send(msg)
		}
		req2 := &protocol.ChannelUpdateRequest{
			ServerID:  pServerID,
			ChannelID: targetID,
			Position:  &targetPos,
		}
		if msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req2); err == nil {
			_ = conn.Connection.Send(msg)
		}
		return nil
	}
}

// navigateMessage navigates through messages in message navigation mode
func (a *App) navigateMessage(delta int) tea.Cmd {
	if !a.messageNavMode || a.activeConn == nil || a.currentChannel == nil {
		return nil
	}

	messages := a.activeConn.GetMessages(a.currentChannel.ID)
	if len(messages) == 0 {
		return nil
	}

	// Update index with wrapping
	a.messageNavIndex += delta
	if a.messageNavIndex < 0 {
		a.messageNavIndex = 0
	} else if a.messageNavIndex >= len(messages) {
		a.messageNavIndex = len(messages) - 1
	}

	// Refresh viewport to show new highlight position
	a.updateChatContent()

	return nil
}

// copyMessageToClipboard copies the selected message or selection to the system clipboard
func (a *App) copyMessageToClipboard() tea.Cmd {
	if !a.messageNavMode || a.activeConn == nil || a.currentChannel == nil {
		return nil
	}

	messages := a.activeConn.GetMessages(a.currentChannel.ID)
	if a.messageNavIndex < 0 || a.messageNavIndex >= len(messages) {
		return nil
	}

	var textToCopy string

	if a.inMessageEditMode {
		// Level 2: copy selection or entire message
		textToCopy = a.getSelectedText()
	} else {
		// Level 1: copy entire message
		msg := messages[a.messageNavIndex]
		textToCopy = msg.Content
	}

	// Copy plain text content to clipboard
	if err := clipboard.WriteAll(textToCopy); err != nil {
		a.statusMessage = fmt.Sprintf("Failed to copy: %v", err)
		a.statusError = true
		return nil
	}

	a.statusMessage = fmt.Sprintf("Copied %d characters", len(textToCopy))
	a.statusError = false

	return nil
}

// insertCursorIntoMessage inserts a visible cursor character and selection markers
// Uses plain text markers that will be styled later by renderMessageContent
func (a *App) insertCursorIntoMessage(content string) string {
	if !a.inMessageEditMode {
		return content
	}

	// Split content into lines
	lines := strings.Split(content, "\n")
	if a.messageCursorLine < 0 || a.messageCursorLine >= len(lines) {
		return content
	}

	// If there's an active selection, wrap it with markers
	if a.messageSelectionStart != nil && a.messageSelectionEnd != nil {
		// Normalize selection (ensure start is before end)
		start := a.messageSelectionStart
		end := a.messageSelectionEnd
		if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
			start, end = end, start
		}

		// Mark selection with special characters
		for lineIdx := range lines {
			line := lines[lineIdx]
			if lineIdx < start.Line || lineIdx > end.Line {
				continue
			}

			var startCol, endCol int
			if lineIdx == start.Line && lineIdx == end.Line {
				startCol = start.Col
				endCol = end.Col
			} else if lineIdx == start.Line {
				startCol = start.Col
				endCol = len(line)
			} else if lineIdx == end.Line {
				startCol = 0
				endCol = end.Col
			} else {
				startCol = 0
				endCol = len(line)
			}

			// Clamp to line bounds
			if startCol < 0 {
				startCol = 0
			}
			if endCol > len(line) {
				endCol = len(line)
			}
			if startCol >= len(line) {
				continue
			}

			// Wrap selected text with markers (will be styled in renderMessageContent)
			before := line[:startCol]
			selected := line[startCol:endCol]
			after := ""
			if endCol < len(line) {
				after = line[endCol:]
			}
			// Use visible brackets to show selection
			lines[lineIdx] = before + "[" + selected + "]" + after
		}
	}

	// Insert simple cursor character
	line := lines[a.messageCursorLine]
	col := a.messageCursorCol
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}

	// Use a simple cursor character
	cursor := "█"

	// Insert cursor
	before := line[:col]
	after := line[col:]
	lines[a.messageCursorLine] = before + cursor + after

	return strings.Join(lines, "\n")
}

// extractLinksFromMessage extracts all URLs from a message using the urlRegex
func (a *App) extractLinksFromMessage(msg *MessageDisplay) []string {
	matches := urlRegex.FindAllString(msg.Content, -1)
	return matches
}

// openLinkBrowser opens the link browser modal with the provided links
func (a *App) openLinkBrowser(links []string, sourceMsg *MessageDisplay, previousMode string) {
	if len(links) == 0 {
		a.statusMessage = "No links found in message"
		a.statusError = false
		return
	}

	a.linkBrowserState = &LinkBrowserState{
		Links:         links,
		SelectedIndex: 0,
		SourceMessage: sourceMsg,
		PreviousMode:  previousMode,
	}
}

// closeLinkBrowser closes the link browser and returns to the previous mode
func (a *App) closeLinkBrowser() {
	if a.linkBrowserState == nil {
		return
	}

	previousMode := a.linkBrowserState.PreviousMode
	a.linkBrowserState = nil

	// Restore previous mode
	if previousMode == "message_nav" {
		a.messageNavMode = true
		a.focus = FocusMessageNav
	}
}

// openURL opens a URL in the default browser
func (a *App) openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd

		// Determine the command based on the OS
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		case "darwin":
			cmd = exec.Command("open", url)
		default: // linux, bsd, etc.
			cmd = exec.Command("xdg-open", url)
		}

		// Execute the command (non-blocking)
		if err := cmd.Start(); err != nil {
			a.statusMessage = fmt.Sprintf("Failed to open URL: %v", err)
			a.statusError = true
			return nil
		}

		a.statusMessage = "Opened link in browser"
		a.statusError = false
		return nil
	}
}

// splitMessageIntoLines splits a message content into lines based on max width
// This accounts for word wrapping and returns an array of line strings
func (a *App) splitMessageIntoLines(content string, maxWidth int) []string {
	if content == "" {
		return []string{""}
	}

	// Simple implementation: split by newlines first, then word-wrap each line
	lines := strings.Split(content, "\n")
	result := []string{}

	for _, line := range lines {
		if len(line) <= maxWidth {
			result = append(result, line)
			continue
		}

		// Word wrap this line
		words := strings.Fields(line)
		currentLine := ""
		for _, word := range words {
			testLine := currentLine
			if testLine != "" {
				testLine += " "
			}
			testLine += word

			if len(testLine) > maxWidth {
				if currentLine != "" {
					result = append(result, currentLine)
					currentLine = word
				} else {
					// Single word longer than maxWidth - just add it
					result = append(result, word)
					currentLine = ""
				}
			} else {
				currentLine = testLine
			}
		}
		if currentLine != "" {
			result = append(result, currentLine)
		}
	}

	if len(result) == 0 {
		return []string{""}
	}
	return result
}

// moveCursorInMessage moves the cursor within a message (Level 2 navigation)
// dx: horizontal delta (-1 = left, +1 = right)
// dy: vertical delta (-1 = up, +1 = down)
// clearSelection: whether to clear any existing text selection
func (a *App) moveCursorInMessage(dx, dy int, clearSelection bool) tea.Cmd {
	if !a.messageNavMode || !a.inMessageEditMode || a.activeConn == nil || a.currentChannel == nil {
		return nil
	}

	messages := a.activeConn.GetMessages(a.currentChannel.ID)
	if a.messageNavIndex < 0 || a.messageNavIndex >= len(messages) {
		return nil
	}

	// Clear any existing selection when moving without Shift
	if clearSelection {
		a.messageSelectionStart = nil
		a.messageSelectionEnd = nil
	}

	msg := messages[a.messageNavIndex]
	lines := a.splitMessageIntoLines(msg.Content, a.chatViewport.Width-10) // Account for padding

	// Move cursor vertically
	if dy != 0 {
		a.messageCursorLine += dy
		// Clamp to valid line range
		if a.messageCursorLine < 0 {
			a.messageCursorLine = 0
		} else if a.messageCursorLine >= len(lines) {
			a.messageCursorLine = len(lines) - 1
		}
		// Clamp column to new line length
		if a.messageCursorCol >= len(lines[a.messageCursorLine]) {
			a.messageCursorCol = len(lines[a.messageCursorLine])
		}
	}

	// Move cursor horizontally
	if dx != 0 {
		a.messageCursorCol += dx
		currentLine := lines[a.messageCursorLine]

		// Handle wrapping to next/previous line
		if a.messageCursorCol < 0 && a.messageCursorLine > 0 {
			// Wrap to end of previous line
			a.messageCursorLine--
			a.messageCursorCol = len(lines[a.messageCursorLine])
		} else if a.messageCursorCol > len(currentLine) && a.messageCursorLine < len(lines)-1 {
			// Wrap to start of next line
			a.messageCursorLine++
			a.messageCursorCol = 0
		} else {
			// Clamp to current line bounds
			if a.messageCursorCol < 0 {
				a.messageCursorCol = 0
			} else if a.messageCursorCol > len(currentLine) {
				a.messageCursorCol = len(currentLine)
			}
		}
	}

	// Refresh viewport to show cursor at new position
	a.updateChatContent()

	return nil
}

// moveCursorWithSelection moves the cursor and updates the text selection (Level 2 with Shift)
func (a *App) moveCursorWithSelection(dx, dy int) tea.Cmd {
	if !a.messageNavMode || !a.inMessageEditMode {
		return nil
	}

	// If no selection exists, start one at current cursor position
	if a.messageSelectionStart == nil {
		a.messageSelectionStart = &Position{
			Line: a.messageCursorLine,
			Col:  a.messageCursorCol,
		}
	}

	// Move the cursor (don't clear selection - we're extending it)
	a.moveCursorInMessage(dx, dy, false)

	// Update selection end to new cursor position
	a.messageSelectionEnd = &Position{
		Line: a.messageCursorLine,
		Col:  a.messageCursorCol,
	}

	// Refresh viewport to show cursor and selection
	a.updateChatContent()

	return nil
}

// getSelectedText returns the selected text, or the entire message if no selection
func (a *App) getSelectedText() string {
	if !a.messageNavMode || a.activeConn == nil || a.currentChannel == nil {
		return ""
	}

	messages := a.activeConn.GetMessages(a.currentChannel.ID)
	if a.messageNavIndex < 0 || a.messageNavIndex >= len(messages) {
		return ""
	}

	msg := messages[a.messageNavIndex]

	// If no selection, return entire message
	if a.messageSelectionStart == nil || a.messageSelectionEnd == nil {
		return msg.Content
	}

	lines := a.splitMessageIntoLines(msg.Content, a.chatViewport.Width-10)

	// Normalize selection (ensure start is before end)
	start := a.messageSelectionStart
	end := a.messageSelectionEnd
	if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
		start, end = end, start
	}

	// Single line selection
	if start.Line == end.Line {
		line := lines[start.Line]
		startCol := start.Col
		endCol := end.Col
		if startCol < 0 {
			startCol = 0
		}
		if endCol > len(line) {
			endCol = len(line)
		}
		if startCol >= len(line) {
			return ""
		}
		return line[startCol:endCol]
	}

	// Multi-line selection
	var result strings.Builder

	// First line (from start.Col to end of line)
	if start.Line < len(lines) {
		line := lines[start.Line]
		startCol := start.Col
		if startCol < 0 {
			startCol = 0
		}
		if startCol < len(line) {
			result.WriteString(line[startCol:])
			result.WriteString("\n")
		}
	}

	// Middle lines (entire lines)
	for i := start.Line + 1; i < end.Line && i < len(lines); i++ {
		result.WriteString(lines[i])
		result.WriteString("\n")
	}

	// Last line (from start to end.Col)
	if end.Line < len(lines) {
		line := lines[end.Line]
		endCol := end.Col
		if endCol > len(line) {
			endCol = len(line)
		}
		if endCol > 0 {
			result.WriteString(line[:endCol])
		}
	}

	return result.String()
}

// selectChannelByID selects a channel by its UUID, requesting message history from the server
func (a *App) selectChannelByID(channelID uuid.UUID) {
	channels := a.getCurrentChannels()
	for i, ch := range channels {
		if ch.ID == channelID {
			a.selectChannel(i)
			return
		}
	}
}

// handleCollapseCategory collapses the current channel's parent category (or current category if on one)
func (a *App) handleCollapseCategory() {
	if a.channelTree == nil || a.currentChannel == nil {
		return
	}

	// Find current channel's node
	node := a.channelTree.NodeMap[a.currentChannel.ID]
	if node == nil {
		return
	}

	// If current channel has a parent category, collapse it
	if node.Parent != nil && node.Parent.IsCategory {
		categoryID := node.Parent.Channel.ID
		a.collapsedCategories[categoryID] = true
		a.channelTree.RebuildFlatList(a.collapsedCategories)
		a.saveCollapsedState()
	}
}

// handleExpandCategory expands the current channel's parent category (or current category if on one)
func (a *App) handleExpandCategory() {
	if a.channelTree == nil || a.currentChannel == nil {
		return
	}

	// Find current channel's node
	node := a.channelTree.NodeMap[a.currentChannel.ID]
	if node == nil {
		return
	}

	// If current channel has a parent category, expand it
	if node.Parent != nil && node.Parent.IsCategory {
		categoryID := node.Parent.Channel.ID
		a.collapsedCategories[categoryID] = false
		a.channelTree.RebuildFlatList(a.collapsedCategories)
		a.saveCollapsedState()
	}
}

// saveServersOrder persists the current in-memory server order to disk
func (a *App) saveServersOrder() {
	config, err := a.configMgr.LoadServers()
	if err != nil {
		log.Printf("Failed to load servers for reorder: %v", err)
		return
	}
	config.Servers = a.clientServers
	if err := a.configMgr.SaveServers(config); err != nil {
		log.Printf("Failed to save server order: %v", err)
	}
}

// switchToClientServer switches to a different client server
func (a *App) switchToClientServer(index int) {
	if index < 0 || index >= len(a.clientServers) {
		return
	}

	a.serverIndex = index
	a.currentClientServer = a.clientServers[index]

	// Get or create connection
	a.activeConn = a.connMgr.GetConnection(a.currentClientServer.ID)

	// If not connected, clear stale state and show prompt
	if a.activeConn == nil || a.activeConn.GetState() != StateReady {
		a.statusMessage = "Unable to connect to server. Server may be offline."
		a.currentServer = nil
		a.currentChannel = nil
		a.channelTree = nil
		return
	}

	// Load first protocol server from connection
	a.activeConn.mu.RLock()
	if len(a.activeConn.Servers) > 0 {
		a.currentServer = a.activeConn.Servers[0]
		a.protocolServerIndex = 0
	}
	a.activeConn.mu.RUnlock()

	a.loadChannelsForServer()
}

// loadChannelsForServer loads channels for the current protocol server
func (a *App) loadChannelsForServer() {
	if a.activeConn == nil || a.currentServer == nil {
		return
	}

	channels := a.activeConn.GetChannels(a.currentServer.ID)

	// Build channel tree
	a.loadChannelTree()

	if len(channels) > 0 {
		a.channelIndex = 0
		a.selectChannel(0)
	} else {
		a.currentChannel = nil
		a.channelIndex = 0
		// TODO: Request channel list from server
	}
}

// loadChannelTree builds the channel tree from the current server's channels
func (a *App) loadChannelTree() {
	if a.activeConn == nil || a.currentServer == nil {
		a.channelTree = nil
		return
	}

	// Get channels for current server
	channels := a.activeConn.GetChannels(a.currentServer.ID)

	// Build tree
	a.channelTree = BuildChannelTree(channels)

	// Load collapsed state from config
	if a.uiConfig != nil && a.uiConfig.CollapsedCategories != nil {
		serverKey := a.activeConn.ServerID.String()
		if collapsed, ok := a.uiConfig.CollapsedCategories[serverKey]; ok {
			// Clear existing collapsed state
			a.collapsedCategories = make(map[uuid.UUID]bool)

			// Load from config
			for categoryIDStr, isCollapsed := range collapsed {
				if categoryID, err := uuid.Parse(categoryIDStr); err == nil {
					a.collapsedCategories[categoryID] = isCollapsed
				}
			}
		}
	}

	// Rebuild flat list with collapsed state
	if a.channelTree != nil {
		a.channelTree.RebuildFlatList(a.collapsedCategories)
	}
}

// saveCollapsedState persists the current collapsed state to config
func (a *App) saveCollapsedState() {
	if a.configMgr == nil || a.uiConfig == nil || a.activeConn == nil {
		return
	}

	// Initialize collapsed categories map if needed
	if a.uiConfig.CollapsedCategories == nil {
		a.uiConfig.CollapsedCategories = make(map[string]map[string]bool)
	}

	serverKey := a.activeConn.ServerID.String()

	// Convert map[uuid.UUID]bool to map[string]bool
	collapsed := make(map[string]bool)
	for categoryID, isCollapsed := range a.collapsedCategories {
		collapsed[categoryID.String()] = isCollapsed
	}

	// Save to config
	a.uiConfig.CollapsedCategories[serverKey] = collapsed

	// Write to disk
	appConfig := &AppConfig{
		Version: 1,
		UI:      *a.uiConfig,
	}

	if err := a.configMgr.SaveAppConfig(appConfig); err != nil {
		log.Printf("Failed to save collapsed state: %v", err)
	}
}

// selectChannel selects a channel
func (a *App) selectChannel(index int) {
	channels := a.getCurrentChannels()
	if index < 0 || index >= len(channels) {
		return
	}
	a.channelIndex = index
	a.currentChannel = channels[index]

	// Clear typing indicators from the previous channel
	a.clearTypingState()

	// Clear unread counts for this channel
	if a.currentClientServer != nil && a.currentChannel != nil {
		serverID := a.currentClientServer.ID
		if a.unreadCounts[serverID] != nil {
			delete(a.unreadCounts[serverID], a.currentChannel.ID)
		}
		if a.mentionCounts[serverID] != nil {
			delete(a.mentionCounts[serverID], a.currentChannel.ID)
		}
	}

	// Clear messages for this channel (they'll be loaded from server)
	if a.activeConn != nil && a.currentChannel != nil {
		a.activeConn.ClearMessages(a.currentChannel.ID)
	}

	// Request message history from server
	if a.activeConn != nil && a.currentChannel != nil {
		req := &protocol.MessageHistoryRequest{
			ChannelID: a.currentChannel.ID,
			Limit:     200,
		}

		log.Printf("selectChannel: requesting history for channel=%s", a.currentChannel.ID)
		msg, err := protocol.NewMessage(protocol.OpRequestMessages, req)
		if err != nil {
			log.Printf("Failed to create message request: %v", err)
		} else {
			if err := a.activeConn.Connection.Send(msg); err != nil {
				log.Printf("Failed to request messages: %v", err)
			} else {
				log.Printf("selectChannel: OpRequestMessages sent for channel=%s", a.currentChannel.ID)
			}
		}
	}

	a.updateChatContent()
}

// addMessage adds a message to the display for the active connection
func (a *App) addMessage(msg *models.Message, author *models.User) {
	if a.activeConn == nil || a.currentChannel == nil {
		return
	}

	// Get current user from active connection
	var currentUser *models.User
	a.activeConn.mu.RLock()
	currentUser = a.activeConn.User
	a.activeConn.mu.RUnlock()

	isOwn := currentUser != nil && msg.AuthorID == currentUser.ID

	// Get existing messages for this channel
	messages := a.activeConn.GetMessages(a.currentChannel.ID)

	// Check if we should show header (different author or time gap)
	showHeader := true
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.AuthorID == msg.AuthorID {
			// Same author, check time gap
			gap := msg.CreatedAt.Sub(lastMsg.CreatedAt)
			if gap.Minutes() < 5 {
				showHeader = false
			}
		}
	}

	display := &MessageDisplay{
		Message:     msg,
		AuthorName:  author.GetDisplayName(),
		AuthorColor: a.theme.Colors.Cyan, // TODO: Use role color
		IsOwn:       isOwn,
		ShowHeader:  showHeader,
	}

	// Add message to active connection's channel
	a.activeConn.AddMessage(a.currentChannel.ID, display)
	a.updateChatContent()
}

// updateChatContent rebuilds the chat viewport content from active connection
func (a *App) updateChatContent() {
	var content strings.Builder

	if a.activeConn == nil || a.currentChannel == nil {
		a.chatViewport.SetContent("")
		return
	}

	// Get messages for current channel
	messages := a.activeConn.GetMessages(a.currentChannel.ID)

	// Get viewport width for full-width backgrounds
	viewportWidth := a.chatViewport.Width

	for i, msg := range messages {
		// Check if this message is selected in navigation mode
		// Level 1: Highlight entire message with selection background
		// Level 2: Highlight with cursor indicator (editing mode)
		isSelected := a.messageNavMode && !a.inMessageEditMode && i == a.messageNavIndex
		isInLevel2 := a.messageNavMode && a.inMessageEditMode && i == a.messageNavIndex

		// Check if this is a system message (either explicit flag or legacy system author)
		isSystemMsg := msg.IsSystem || msg.AuthorName == "System"

		// Create highlight style for selected message (Level 1 only)
		var highlightStyle lipgloss.Style
		if isSelected {
			// Level 1: Full selection background
			highlightStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(a.theme.Colors.Selection)).
				Width(viewportWidth)
		}
		// Level 2: No background - just show cursor inline
		// The cursor and selection will be rendered within the message content

		// In Level 2, prepare to insert cursor and selection into message content
		messageContentWithCursor := msg.Content
		if isInLevel2 && !isSystemMsg {
			messageContentWithCursor = a.insertCursorIntoMessage(msg.Content)
		}

		if msg.ShowHeader && !isSystemMsg {
			// Render author line with full width background (non-system messages)
			authorStyle := a.styles.UsernameOther
			if msg.IsOwn {
				authorStyle = a.styles.UsernameSelf
			}
			timestamp := msg.CreatedAt.Format("15:04")

			// Render author name with its style
			authorText := authorStyle.Render(msg.AuthorName)
			// Render timestamp with background
			timestampStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Semantic.ChatTimestamp)).
				Faint(true)
			timestampText := timestampStyle.Render(timestamp)

			header := fmt.Sprintf("%s  %s", authorText, timestampText)

			// Apply full width background to entire line
			lineStyle := lipgloss.NewStyle().
				Width(viewportWidth)
			headerLine := lineStyle.Render(header)

			// Wrap in highlight if selected (Level 1 or Level 2)
			if isSelected || isInLevel2 {
				headerLine = highlightStyle.Render(headerLine)
			}
			content.WriteString(headerLine)
			content.WriteString("\n")
		}

		// Render message content
		var contentLine string
		if isSystemMsg {
			// Render as a centered announcement: ─── message text ───
			barStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Faint(true)
			textStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
				Italic(true).
				Bold(true)
			msgText := textStyle.Render(" " + msg.Content + " ")
			msgVisLen := lipgloss.Width(msgText)
			remaining := viewportWidth - msgVisLen
			if remaining < 0 {
				remaining = 0
			}
			leftBar := strings.Repeat("─", remaining/2)
			rightBar := strings.Repeat("─", remaining-remaining/2)
			line := barStyle.Render(leftBar) + msgText + barStyle.Render(rightBar)
			contentLine = lipgloss.NewStyle().Width(viewportWidth).Render(line)
		} else if msg.IsWhisper {
			// Whisper: render with distinctive orange/pink color
			whisperStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Orange)).
				Italic(true).
				Width(viewportWidth)
			contentLine = whisperStyle.Render(messageContentWithCursor)
		} else {
			// Regular messages — highlight @mentions of the current user
			contentLine = a.renderMessageContent(messageContentWithCursor, viewportWidth)
		}

		// Wrap content in highlight if selected (Level 1 or Level 2)
		if isSelected || isInLevel2 {
			contentLine = highlightStyle.Render(contentLine)
		}
		content.WriteString(contentLine)
		content.WriteString("\n")
	}

	a.chatViewport.SetContent(content.String())
}

// renderMessageContent renders message text, highlighting @mentions of the current user.
// urlRegex matches http and https URLs.
var urlRegex = regexp.MustCompile(`https?://[^\s<>"{}|\\^` + "`" + `\[\]]+`)

// osc8Link wraps text in an OSC 8 terminal hyperlink.
// Uses ESC\ (ST, String Terminator, 0x1B 0x5C) which Windows Terminal requires
// for reliable Ctrl+Click support. Hold Ctrl and left-click the link to open it.
func osc8Link(url, styledText string) string {
	const st = "\033\\"
	return "\033]8;;" + url + st + styledText + "\033]8;;" + st
}

func (a *App) renderMessageContent(text string, width int) string {
	// Determine current user's alias
	var alias string
	if a.activeConn != nil && a.activeConn.User != nil {
		alias = a.activeConn.User.Username
	}

	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Semantic.ChatFg))
	mentionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Semantic.ChatMention)).
		Bold(true)
	linkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Underline(true)

	// renderLine processes a single line (no \n) applying URL and @mention styling.
	renderLine := func(line string) string {
		var out strings.Builder
		seg := line
		for len(seg) > 0 {
			urlLoc := urlRegex.FindStringIndex(seg)
			mentionLoc := []int{-1, -1}
			if alias != "" {
				token := "@" + alias
				if idx := strings.Index(strings.ToLower(seg), strings.ToLower(token)); idx != -1 {
					mentionLoc = []int{idx, idx + len(token)}
				}
			}

			useURL := urlLoc != nil
			useMention := mentionLoc[0] != -1
			if useURL && useMention {
				if mentionLoc[0] < urlLoc[0] {
					useURL = false
				} else {
					useMention = false
				}
			}

			switch {
			case useMention:
				if mentionLoc[0] > 0 {
					out.WriteString(msgStyle.Render(seg[:mentionLoc[0]]))
				}
				out.WriteString(mentionStyle.Render(seg[mentionLoc[0]:mentionLoc[1]]))
				seg = seg[mentionLoc[1]:]
			case useURL:
				if urlLoc[0] > 0 {
					out.WriteString(msgStyle.Render(seg[:urlLoc[0]]))
				}
				rawURL := seg[urlLoc[0]:urlLoc[1]]
				out.WriteString(osc8Link(rawURL, linkStyle.Render(rawURL)))
				seg = seg[urlLoc[1]:]
			default:
				out.WriteString(msgStyle.Render(seg))
				seg = ""
			}
		}
		return out.String()
	}

	// Process each newline-separated line independently and apply Width() per line.
	// Applying Width() to the whole multi-line string causes lipgloss to
	// miscount visible characters when OSC 8 sequences are on a continuation
	// line, which shifts the link far to the right with blank space before it.
	lines := strings.Split(text, "\n")
	renderedLines := make([]string, len(lines))
	for i, line := range lines {
		renderedLines[i] = lipgloss.NewStyle().Width(width).Render(renderLine(line))
	}
	return strings.Join(renderedLines, "\n")
}

// scrollToBottom scrolls the chat to the bottom
func (a *App) scrollToBottom() {
	a.chatViewport.GotoBottom()
}

// roleLevel categorizes the current user's highest permission level on the active server.
type roleLevel int

const (
	roleLevelMember roleLevel = iota
	roleLevelMod
	roleLevelAdmin
)

// currentUserRoleLevel returns the effective permission level for the logged-in user
// on the currently active server connection.
func (a *App) currentUserRoleLevel() roleLevel {
	if a.activeConn == nil || a.activeConn.User == nil {
		return roleLevelMember
	}
	userID := a.activeConn.User.ID

	a.activeConn.mu.RLock()
	members := a.activeConn.Members
	a.activeConn.mu.RUnlock()

	for _, m := range members {
		if m.User == nil || m.User.ID != userID {
			continue
		}
		if m.HighestRole == nil {
			return roleLevelMember
		}
		if m.HighestRole.HasPermission(models.PermissionAdministrator) {
			return roleLevelAdmin
		}
		if m.HighestRole.HasPermission(models.PermissionKickMembers) ||
			m.HighestRole.HasPermission(models.PermissionMuteMembers) ||
			m.HighestRole.HasPermission(models.PermissionManageChannels) {
			return roleLevelMod
		}
		return roleLevelMember
	}
	return roleLevelMember
}

// updateTypingIndicator updates the typing users list
func (a *App) updateTypingIndicator(users []string) {
	a.typingUsers = users
}

// rebuildTypingUsers refreshes a.typingUsers from the current typingExpiry map,
// resolving user IDs to display names via the active connection's member list.
func (a *App) rebuildTypingUsers() {
	if len(a.typingExpiry) == 0 {
		a.typingUsers = nil
		return
	}
	// Build a userID → username map from the members list
	nameMap := make(map[uuid.UUID]string)
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		for _, m := range a.activeConn.Members {
			if m.User != nil {
				nameMap[m.User.ID] = m.User.Username
			}
		}
		a.activeConn.mu.RUnlock()
	}
	users := make([]string, 0, len(a.typingExpiry))
	for uid := range a.typingExpiry {
		if name, ok := nameMap[uid]; ok {
			users = append(users, name)
		} else {
			users = append(users, uid.String()[:8])
		}
	}
	sort.Strings(users)
	a.typingUsers = users
}

// clearTypingState resets all typing indicators for the current channel.
// Called on channel switch so stale indicators don't bleed across channels.
func (a *App) clearTypingState() {
	a.typingExpiry = nil
	a.typingUsers = nil
}

// updateViewportSize updates viewport and textarea dimensions based on window size.
// Must use the same column widths and height math as renderMainView / renderChatPanel
// so that chatViewport.Width is correct before updateChatContent() is called.
func (a *App) updateViewportSize() {
	// Must match renderMainView exactly
	availableWidth := a.width - 1
	serverIconsWidth := 22
	channelsWidth := 26
	membersWidth := 30
	chatWidth := availableWidth - serverIconsWidth - channelsWidth - membersWidth
	if chatWidth < 60 {
		membersWidth = 20
		chatWidth = availableWidth - serverIconsWidth - channelsWidth - membersWidth
	}

	// Interior of the chat panel (panel has a 1-char border on each side)
	interiorWidth := chatWidth - 2

	// Set textarea width — matches renderChatPanel line 919
	a.input.SetWidth(interiorWidth - 2)
	a.input.SetHeight(4)

	// Set viewport dimensions — matches renderChatPanel lines 922-924
	// panelHeight = a.height - 2 (status bar + top padding)
	// inputHeight = 6, headerHeight = 2
	panelHeight := a.height - 2
	inputHeight := 6
	headerHeight := 2
	chatHeight := panelHeight - inputHeight - headerHeight
	if interiorWidth > 0 {
		a.chatViewport.Width = interiorWidth
	}
	if chatHeight > 2 {
		a.chatViewport.Height = chatHeight - 2
	}
}

// handleSendMessage sends the current input as a message
func (a *App) handleSendMessage() tea.Cmd {
	content := strings.TrimSpace(a.input.Value())
	if content == "" {
		return nil
	}

	a.input.Reset()

	// Check if this is a slash command
	if strings.HasPrefix(content, "/") {
		return a.handleSlashCommand(content)
	}

	// Create and send message via connection manager
	if a.activeConn != nil && a.currentChannel != nil && a.currentClientServer != nil {
		serverID := a.currentClientServer.ID
		channelID := a.currentChannel.ID

		return func() tea.Msg {
			if err := a.connMgr.SendMessage(serverID, channelID, content); err != nil {
				return ErrorMsg{Error: fmt.Sprintf("Failed to send message: %v", err)}
			}
			return nil
		}
	}

	return nil
}

// handleSlashCommand processes slash commands
func (a *App) handleSlashCommand(input string) tea.Cmd {
	cmd, err := ParseCommand(input)
	if err != nil {
		a.statusMessage = fmt.Sprintf("Invalid command: %v", err)
		a.statusError = true
		return nil
	}

	result, err := a.commandHandler.Execute(cmd)
	if err != nil {
		a.statusMessage = fmt.Sprintf("Command failed: %v", err)
		a.statusError = true
		return nil
	}

	// Special handling for help command - display in chat area
	if cmd.Name == "help" {
		a.displayLocalSystemMessage(result)
		a.statusMessage = ""
		a.statusError = false
	} else {
		a.statusMessage = result
		a.statusError = false
	}
	return nil
}

// displayLocalSystemMessage displays a system message in the chat viewport (local only, not broadcast)
func (a *App) displayLocalSystemMessage(content string) {
	if a.activeConn == nil || a.currentChannel == nil {
		return
	}

	// Create a local system message
	systemMsg := &MessageDisplay{
		Message: &models.Message{
			ID:        uuid.New(),
			ChannelID: a.currentChannel.ID,
			Content:   content,
			CreatedAt: time.Now(),
		},
		AuthorName:  "System",
		AuthorColor: "#888888",
		IsOwn:       false,
		ShowHeader:  true,
	}

	// Add to connection's message list
	a.activeConn.AddMessage(a.currentChannel.ID, systemMsg)

	// Update chat content and scroll to bottom
	a.updateChatContent()
	a.scrollToBottom()
}

// updateMentionPopup checks the current input for a trailing @query and updates the popup.
// Call this whenever the input value changes.
func (a *App) updateMentionPopup() {
	val := a.input.Value()
	// Find the last '@' that is preceded by whitespace (or is at position 0)
	atIdx := -1
	for i := len(val) - 1; i >= 0; i-- {
		if val[i] == '@' {
			if i == 0 || val[i-1] == ' ' {
				atIdx = i
				break
			}
		} else if val[i] == ' ' {
			break // crossed a word boundary without finding @
		}
	}
	if atIdx == -1 {
		a.showMentionPopup = false
		a.mentionSuggestions = nil
		a.mentionQuery = ""
		return
	}
	query := strings.ToLower(val[atIdx+1:])
	// Don't show popup after completed mention (query contains space)
	if strings.Contains(query, " ") {
		a.showMentionPopup = false
		a.mentionSuggestions = nil
		return
	}
	a.mentionQuery = query
	// Filter members
	var suggestions []string
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		for _, m := range a.activeConn.Members {
			if m.User != nil && strings.HasPrefix(strings.ToLower(m.User.Username), query) {
				suggestions = append(suggestions, m.User.Username)
				if len(suggestions) >= 5 {
					break
				}
			}
		}
		a.activeConn.mu.RUnlock()
	}
	a.mentionSuggestions = suggestions
	a.showMentionPopup = len(suggestions) > 0
}

// completeMention replaces the current @query in the input with the chosen username.
func (a *App) completeMention(username string) {
	val := a.input.Value()
	// Find last @ position (same logic as updateMentionPopup)
	atIdx := -1
	for i := len(val) - 1; i >= 0; i-- {
		if val[i] == '@' {
			if i == 0 || val[i-1] == ' ' {
				atIdx = i
				break
			}
		} else if val[i] == ' ' {
			break
		}
	}
	if atIdx == -1 {
		return
	}
	newVal := val[:atIdx] + "@" + username + " "
	a.input.SetValue(newVal)
	a.showMentionPopup = false
	a.mentionSuggestions = nil
}

// handleTabCompletion provides autocomplete for slash commands
func (a *App) handleTabCompletion() {
	input := a.input.Value()
	if !strings.HasPrefix(input, "/") {
		return
	}

	// Remove the leading slash and get the partial command
	partial := strings.TrimPrefix(input, "/")

	// Available commands
	commands := []string{
		"create-channel",
		"create-category",
		"delete-channel",
		"delete-category",
		"rename-channel",
		"move-channel",
		"theme",
		"mute",
		"unmute",
		"role",
		"kick",
		"ban",
		"whisper",
		"help",
	}

	// Find matching commands
	var matches []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, partial) {
			matches = append(matches, cmd)
		}
	}

	// If exactly one match, complete it
	if len(matches) == 1 {
		a.input.SetValue("/" + matches[0] + " ")
		a.input.CursorEnd()
	} else if len(matches) > 1 {
		// Multiple matches - show them in status bar
		a.statusMessage = "Available: " + strings.Join(matches, ", ")
		a.statusError = false
	}
}

// --- Message types for tea.Cmd ---

// AutoConnectMsg carries the result of an auto-connect attempt
type AutoConnectMsg struct {
	ServerID uuid.UUID
	UserID   uuid.UUID
	Email    string
	Token    string
	Err      error
}

// ConnectedMsg indicates successful connection
type ConnectedMsg struct{}

// DisconnectedMsg indicates disconnection
type DisconnectedMsg struct{}

// LoginSuccessMsg indicates successful login
type LoginSuccessMsg struct {
	User     *models.User
	Token    string
	ServerID uuid.UUID
	Servers  []*models.Server
}

// LoginErrorMsg indicates login failure
type LoginErrorMsg struct {
	Error string
}

// ConnectionReadyMsg indicates WebSocket connection is ready
type ConnectionReadyMsg struct {
	ServerID uuid.UUID
}

// afkCheckMsg is fired periodically to check for AFK inactivity
type afkCheckMsg struct{ t time.Time }

// typingTickMsg drives the typing indicator animation and expiry pruning
type typingTickMsg time.Time

// ConnectionFailedMsg indicates connection failed
type ConnectionFailedMsg struct {
	ServerID uuid.UUID
	Error    string
	Retry    bool // Whether to retry automatically
}

// ConnectionRetryingMsg indicates reconnection attempt
type ConnectionRetryingMsg struct {
	ServerID     uuid.UUID
	AttemptCount int
	NextDelay    time.Duration
}

// MessageReceivedMsg indicates a new message
type MessageReceivedMsg struct {
	Message *models.Message
	Author  *models.User
}

// TypingMsg indicates typing users
type TypingMsg struct {
	ChannelID uuid.UUID
	Users     []string
}

// ServerDataMsg contains server data
type ServerDataMsg struct {
	Server   *models.Server
	Channels []*models.Channel
	Roles    []*models.Role
	Members  []*models.ServerMember
}

// ErrorMsg indicates an error
type ErrorMsg struct {
	Error string
}

// ProtocolMsg wraps protocol messages from the WebSocket
type ProtocolMsg struct {
	Message *protocol.Message
}

// SetTheme sets the application theme
func (a *App) SetTheme(theme *themes.Theme) {
	a.theme = theme
	a.styles = theme.BuildStyles()
}

// SetToken sets the authentication token for the active connection
func (a *App) SetToken(token string) {
	if a.activeConn != nil {
		a.activeConn.mu.Lock()
		a.activeConn.Token = token
		a.activeConn.mu.Unlock()
	}
}

// handleServerScopedMessage processes server-scoped messages from ConnectionManager
func (a *App) handleServerScopedMessage(scopedMsg ServerScopedMsg) tea.Cmd {
	serverID := scopedMsg.ServerID

	// Get the server connection
	sc := a.connMgr.GetConnection(serverID)
	if sc == nil {
		return nil
	}

	// Handle different message types
	switch msg := scopedMsg.Msg.(type) {
	case ProtocolMsg:
		return a.handleProtocolMessage(serverID, msg.Message)

	case ConnectedMsg:
		sc.SetState(StateConnected)
		if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
			a.statusMessage = fmt.Sprintf("Connected to %s", a.currentClientServer.Name)
			a.statusError = false
		}

	case DisconnectedMsg:
		sc.SetState(StateDisconnected)
		if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
			a.statusMessage = fmt.Sprintf("Disconnected from %s", a.currentClientServer.Name)
			a.statusError = true
		}

	case ErrorMsg:
		sc.SetState(StateError)
		sc.LastError = fmt.Errorf("%s", msg.Error)
		if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
			a.statusMessage = fmt.Sprintf("Error: %s", msg.Error)
			a.statusError = true
		}
	}

	return nil
}

// handleProtocolMessage processes incoming WebSocket protocol messages for a specific server
func (a *App) handleProtocolMessage(serverID uuid.UUID, msg *protocol.Message) tea.Cmd {
	sc := a.connMgr.GetConnection(serverID)
	if sc == nil {
		return nil
	}

	switch msg.Op {
	case protocol.OpReady:
		// Parse READY payload
		var payload protocol.ReadyPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			a.statusMessage = "Failed to parse server data"
			a.statusError = true
			return nil
		}

		// Update server connection state with server data
		return a.handleReady(serverID, &payload)

	case protocol.OpDispatch:
		// Handle real-time events (messages, typing, etc.)
		return a.handleDispatch(serverID, msg)

	case protocol.OpInvalidSession:
		// Token was rejected by server
		sc.SetState(StateError)
		if a.localIdentity != nil {
			// We have a local identity — retry with HTTP login (which is synchronous and reliable)
			return a.autoConnectServer(serverID)
		}
		// No local identity — fall back to login screen
		if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
			a.view = ViewLogin
			a.initLoginView()
			a.loginError = "Session invalid, please log in again"
		}
		return nil
	}

	return nil
}

// handleReady processes the READY message from the server
func (a *App) handleReady(serverID uuid.UUID, payload *protocol.ReadyPayload) tea.Cmd {
	sc := a.connMgr.GetConnection(serverID)
	if sc == nil {
		return nil
	}

	// Store user and protocol servers in server connection
	sc.mu.Lock()
	sc.User = payload.User
	sc.Servers = payload.Servers
	sc.mu.Unlock()

	// Mark as ready
	sc.SetState(StateReady)

	// Update UI if this is the server the user currently has selected.
	// We also set a.activeConn here to correct any race from connectServerAsync goroutines
	// (multiple servers connecting in parallel can overwrite a.activeConn from different goroutines).
	if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
		a.activeConn = sc // ensure activeConn points to the selected server
		// Select first protocol server if available
		if len(payload.Servers) > 0 {
			a.currentServer = payload.Servers[0]
			a.protocolServerIndex = 0
			a.loadChannelsForServer()
		}

		a.statusMessage = "Ready"
		a.statusError = false
	}

	return nil
}

// handleDispatch processes dispatch events
func (a *App) handleDispatch(serverID uuid.UUID, msg *protocol.Message) tea.Cmd {
	sc := a.connMgr.GetConnection(serverID)
	if sc == nil {
		return nil
	}

	switch msg.Type {
	case protocol.EventReady:
		// Parse READY payload
		var payload protocol.ReadyPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse READY payload: %v", err)
			return nil
		}

		// Store servers in the server connection
		sc.mu.Lock()
		sc.Servers = payload.Servers
		sc.mu.Unlock()

		log.Printf("READY received: User=%s, %d servers available", payload.User.Username, len(payload.Servers))

		// Set current server to first server if not already set
		if a.currentServer == nil && len(payload.Servers) > 0 {
			a.currentServer = payload.Servers[0]
			a.protocolServerIndex = 0
			log.Printf("Set current server to: %s (ID=%s)", a.currentServer.Name, a.currentServer.ID)
		}

		// SERVER_CREATE events will follow with channels for each server

	case protocol.EventServerCreate:
		// Parse server create payload
		var payload protocol.ServerCreatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse SERVER_CREATE payload: %v", err)
			return nil
		}

		// Store channels in server connection
		sc.SetChannels(payload.Server.ID, payload.Channels)

		// Build lookup maps for roles and users
		roleMap := make(map[uuid.UUID]*models.Role, len(payload.Roles))
		for _, r := range payload.Roles {
			roleMap[r.ID] = r
		}
		userMap := make(map[uuid.UUID]*models.User, len(payload.Users))
		for _, u := range payload.Users {
			userMap[u.ID] = u
		}

		// Build MemberDisplay list and store in connection
		displays := make([]*MemberDisplay, 0, len(payload.Members))
		for _, m := range payload.Members {
			user, ok := userMap[m.UserID]
			if !ok {
				continue
			}
			displays = append(displays, buildMemberDisplay(m, user, roleMap))
		}

		sc.mu.Lock()
		sc.Roles[payload.Server.ID] = payload.Roles
		sc.Members = displays
		sc.mu.Unlock()

		log.Printf("Received SERVER_CREATE for %s: %d channels, %d members, %d roles",
			payload.Server.Name, len(payload.Channels), len(displays), len(payload.Roles))

		// If this is the active connection and current server, update UI
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			if a.currentServer != nil && a.currentServer.ID == payload.Server.ID {
				a.loadChannelTree()
				if a.currentChannel == nil && len(payload.Channels) > 0 {
					// First connection: select the first channel
					a.channelIndex = 0
					a.selectChannel(0)
				} else if a.currentChannel != nil {
					// Reconnect: re-request history for the channel we were viewing
					// (message history may be stale or empty after a forced disconnect)
					a.selectChannelByID(a.currentChannel.ID)
				}
			}
		}

	case protocol.EventMessageCreate:
		// Parse message payload
		var payload protocol.MessageCreatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse MESSAGE_CREATE payload: %v", err)
			return nil
		}

		// Create display message
		display := &MessageDisplay{
			Message:     payload.Message,
			AuthorName:  payload.Author.Username,
			AuthorColor: a.theme.Colors.Purple, // TODO: Use user color from role
			IsOwn:       payload.Author.ID == sc.User.ID,
			ShowHeader:  true, // TODO: Implement message grouping
		}

		// Add message to connection's message history
		sc.AddMessage(payload.Message.ChannelID, display)

		// Unread tracking: increment if this channel is not currently viewed
		isCurrentChannel := a.currentChannel != nil && a.currentChannel.ID == payload.Message.ChannelID
		if !isCurrentChannel && !a.mutedChannels[payload.Message.ChannelID] {
			if a.unreadCounts[serverID] == nil {
				a.unreadCounts[serverID] = make(map[uuid.UUID]int)
			}
			a.unreadCounts[serverID][payload.Message.ChannelID]++
			// Check for @mention using the authenticated user's alias
			if sc.User != nil && containsMention(payload.Message.Content, sc.User.Username) {
				if a.mentionCounts[serverID] == nil {
					a.mentionCounts[serverID] = make(map[uuid.UUID]int)
				}
				a.mentionCounts[serverID][payload.Message.ChannelID]++
			}
		}

		log.Printf("MESSAGE_CREATE: channel=%s, author=%s, activeConn=%v, currentChannel=%v",
			payload.Message.ChannelID, payload.Author.Username,
			a.activeConn != nil,
			a.currentChannel != nil)

		// Update UI if this is for the active connection and current channel
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			if a.currentChannel != nil && a.currentChannel.ID == payload.Message.ChannelID {
				// Message is for currently viewed channel - update chat viewport
				log.Printf("Updating chat content for message in current channel")
				a.updateChatContent()
				a.scrollToBottom()
			} else if a.currentChannel != nil {
				log.Printf("Message not for current channel: msg=%s, current=%s",
					payload.Message.ChannelID, a.currentChannel.ID)
			}
		}

	case protocol.EventMessagesHistory:
		// Parse message history payload
		var payload protocol.MessageHistoryPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse MESSAGES_HISTORY payload: %v", err)
			return nil
		}

		// Store messages in the server-scoped connection (not a.activeConn which may
		// point to a different server if the user switched servers between request/response)
		sc.mu.Lock()
		// Clear existing messages first
		sc.Messages[payload.ChannelID] = nil

		// Determine current user ID for IsOwn flag
		var currentUserID uuid.UUID
		if sc.User != nil {
			currentUserID = sc.User.ID
		}

		// Add historical messages
		for _, msgDisplay := range payload.Messages {
			isSystem := msgDisplay.Type == models.MessageTypeSystem
			display := &MessageDisplay{
				Message:     msgDisplay.Message,
				AuthorName:  msgDisplay.Author.Username,
				AuthorColor: a.theme.Colors.Purple, // TODO: Use user color from role
				IsOwn:       msgDisplay.Author.ID == currentUserID,
				ShowHeader:  !isSystem,
				IsSystem:    isSystem,
			}
			sc.Messages[payload.ChannelID] = append(
				sc.Messages[payload.ChannelID],
				display,
			)
		}

		// Populate pinned messages for this channel
		if payload.PinnedMessages != nil {
			sc.PinnedMessages[payload.ChannelID] = payload.PinnedMessages
		}
		sc.mu.Unlock()

		// Refresh chat if we're currently viewing this channel on this server
		if a.activeConn != nil && a.activeConn.ServerID == serverID &&
			a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
			a.updateChatContent()
			a.scrollToBottom()
		}

	case protocol.EventPresenceUpdate:
		var payload protocol.PresenceUpdateEventPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse PRESENCE_UPDATE payload: %v", err)
			return nil
		}
		sc.mu.Lock()
		for _, m := range sc.Members {
			if m.User.ID == payload.User.ID {
				m.User.Status = payload.Status
				break
			}
		}
		sc.mu.Unlock()

	case protocol.EventServerMemberAdd:
		var payload protocol.ServerMemberAddPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse SERVER_MEMBER_ADD payload: %v", err)
			return nil
		}
		sc.mu.RLock()
		roles := sc.Roles[payload.ServerID]
		sc.mu.RUnlock()
		roleMap := make(map[uuid.UUID]*models.Role, len(roles))
		for _, r := range roles {
			roleMap[r.ID] = r
		}
		display := buildMemberDisplay(payload.Member, payload.User, roleMap)
		sc.mu.Lock()
		// Upsert: update the existing entry if the user is already in the list
		// (e.g. they were shown as offline and have just reconnected), otherwise append.
		found := false
		for i, m := range sc.Members {
			if m.User != nil && m.User.ID == payload.User.ID {
				sc.Members[i] = display
				found = true
				break
			}
		}
		if !found {
			sc.Members = append(sc.Members, display)
		}
		sc.mu.Unlock()

	case protocol.EventServerMemberRemove:
		var payload protocol.ServerMemberRemovePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse SERVER_MEMBER_REMOVE payload: %v", err)
			return nil
		}
		sc.mu.Lock()
		for i, m := range sc.Members {
			if m.User.ID == payload.User.ID {
				sc.Members = append(sc.Members[:i], sc.Members[i+1:]...)
				break
			}
		}
		sc.mu.Unlock()

	case protocol.EventServerMemberUpdate:
		var payload protocol.ServerMemberUpdatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse SERVER_MEMBER_UPDATE payload: %v", err)
			return nil
		}
		// Rebuild role map from payload roles
		roleMap := make(map[uuid.UUID]*models.Role, len(payload.Roles))
		for _, r := range payload.Roles {
			roleMap[r.ID] = r
		}

		// If this update affects the current user, check for mute state changes
		if sc.User != nil && payload.User.ID == sc.User.ID {
			sc.mu.RLock()
			var prevMuted bool
			for _, m := range sc.Members {
				if m.User != nil && m.User.ID == sc.User.ID {
					prevMuted = m.Member != nil && m.Member.IsMuted
					break
				}
			}
			sc.mu.RUnlock()
			if payload.Member.IsMuted && !prevMuted {
				a.displayLocalSystemMessage("You have been muted by a moderator. You cannot send messages.")
			} else if !payload.Member.IsMuted && prevMuted {
				a.displayLocalSystemMessage("You have been unmuted.")
			}
		}

		sc.mu.Lock()
		for i, m := range sc.Members {
			if m.User != nil && m.User.ID == payload.User.ID {
				sc.Members[i] = buildMemberDisplay(payload.Member, payload.User, roleMap)
				break
			}
		}
		sc.mu.Unlock()

	case protocol.EventWhisperCreate:
		var whisperPayload protocol.WhisperCreatePayload
		if err := json.Unmarshal(msg.Data, &whisperPayload); err != nil {
			log.Printf("Failed to parse WHISPER_CREATE payload: %v", err)
			return nil
		}
		var chID uuid.UUID
		if a.currentChannel != nil {
			chID = a.currentChannel.ID
		}
		display := &MessageDisplay{
			Message: &models.Message{
				ID:        uuid.New(),
				ChannelID: chID,
				AuthorID:  whisperPayload.FromUser.ID,
				Content:   "[DM] " + whisperPayload.Content,
				CreatedAt: whisperPayload.Timestamp,
			},
			AuthorName:  whisperPayload.FromUser.Username,
			AuthorColor: a.theme.Colors.Orange,
			IsOwn:       sc.User != nil && whisperPayload.FromUser.ID == sc.User.ID,
			ShowHeader:  true,
			IsWhisper:   true,
		}
		if a.activeConn != nil && a.currentChannel != nil {
			a.activeConn.AddMessage(a.currentChannel.ID, display)
			a.updateChatContent()
			a.scrollToBottom()
		}

	case protocol.EventMessagePin:
		var payload protocol.MessagePinPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse MESSAGE_PIN payload: %v", err)
			return nil
		}
		if payload.Message != nil {
			sc.mu.Lock()
			pinned := sc.PinnedMessages[payload.ChannelID]
			// Add if not already present
			alreadyPinned := false
			for _, pm := range pinned {
				if pm.ID == payload.Message.ID {
					alreadyPinned = true
					break
				}
			}
			if !alreadyPinned {
				sc.PinnedMessages[payload.ChannelID] = append(pinned, payload.Message)
			}
			sc.mu.Unlock()
			if a.activeConn != nil && a.activeConn.ServerID == serverID &&
				a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
				a.updateChatContent()
			}
		}

	case protocol.EventMessageUnpin:
		var payload protocol.MessagePinPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse MESSAGE_UNPIN payload: %v", err)
			return nil
		}
		if payload.Message != nil {
			sc.mu.Lock()
			pinned := sc.PinnedMessages[payload.ChannelID]
			filtered := pinned[:0]
			for _, pm := range pinned {
				if pm.ID != payload.Message.ID {
					filtered = append(filtered, pm)
				}
			}
			sc.PinnedMessages[payload.ChannelID] = filtered
			sc.mu.Unlock()
			if a.activeConn != nil && a.activeConn.ServerID == serverID &&
				a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
				a.updateChatContent()
			}
		}

	case protocol.EventSystemMessage:
		var payload protocol.SystemMessagePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse SYSTEM_MESSAGE payload: %v", err)
			return nil
		}
		// Show in whatever channel is currently viewed on this server
		var chID uuid.UUID
		if a.currentChannel != nil {
			chID = a.currentChannel.ID
		}
		display := &MessageDisplay{
			Message: &models.Message{
				ID:        uuid.New(),
				ChannelID: chID,
				Content:   payload.Content,
				CreatedAt: payload.Timestamp,
			},
			IsSystem:   true,
			ShowHeader: false,
		}
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			sc.AddMessage(chID, display)
			if a.currentChannel != nil && a.currentChannel.ID == chID {
				a.updateChatContent()
				a.scrollToBottom()
			}
		}

	case protocol.EventTypingStart:
		var typingPayload protocol.TypingStartEventPayload
		if err := json.Unmarshal(msg.Data, &typingPayload); err != nil {
			return nil
		}
		// Only show for the channel the user is currently viewing on this server
		if a.currentChannel == nil || typingPayload.ChannelID != a.currentChannel.ID {
			return nil
		}
		// Never show our own typing indicator
		if sc.User != nil && typingPayload.UserID == sc.User.ID {
			return nil
		}
		if a.typingExpiry == nil {
			a.typingExpiry = make(map[uuid.UUID]time.Time)
		}
		a.typingExpiry[typingPayload.UserID] = time.Now().Add(10 * time.Second)
		a.rebuildTypingUsers()

	case protocol.EventChannelCreate:
		// Parse channel payload
		var payload protocol.ChannelCreatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse CHANNEL_CREATE payload: %v", err)
			return nil
		}

		// Add to server connection's channels
		sc.mu.Lock()
		protocolServerID := payload.Channel.ServerID
		sc.Channels[protocolServerID] = append(sc.Channels[protocolServerID], payload.Channel)
		sc.mu.Unlock()

		log.Printf("CHANNEL_CREATE: channel=%s, protocolServer=%s, clientServer=%s",
			payload.Channel.Name, protocolServerID, serverID)

		// Update tree if this is the active connection AND the channel is for current protocol server
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			if a.currentServer != nil && a.currentServer.ID == protocolServerID {
				if a.channelTree != nil {
					a.channelTree.AddChannel(payload.Channel)
					a.channelTree.RebuildFlatList(a.collapsedCategories)
				}
			}
		}

	case protocol.EventChannelUpdate:
		// Parse channel payload
		var payload protocol.ChannelUpdatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse CHANNEL_UPDATE payload: %v", err)
			return nil
		}

		// Update in server connection's channels
		sc.mu.Lock()
		protocolServerID := payload.Channel.ServerID
		for i, ch := range sc.Channels[protocolServerID] {
			if ch.ID == payload.Channel.ID {
				sc.Channels[protocolServerID][i] = payload.Channel
				break
			}
		}
		sc.mu.Unlock()

		// Update tree if this is the active connection AND the channel is for current protocol server
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			if a.currentServer != nil && a.currentServer.ID == protocolServerID {
				if a.channelTree != nil {
					a.channelTree.UpdateChannel(payload.Channel)
					a.channelTree.RebuildFlatList(a.collapsedCategories)
				}
			}
		}

	case protocol.EventChannelDelete:
		// Parse channel delete payload
		var payload protocol.ChannelDeletePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse CHANNEL_DELETE payload: %v", err)
			return nil
		}

		// Remove from server connection's channels
		sc.mu.Lock()
		protocolServerID := payload.ServerID
		for i, ch := range sc.Channels[protocolServerID] {
			if ch.ID == payload.ChannelID {
				sc.Channels[protocolServerID] = append(sc.Channels[protocolServerID][:i], sc.Channels[protocolServerID][i+1:]...)
				break
			}
		}
		sc.mu.Unlock()

		// Update tree if this is the active connection AND the channel is for current protocol server
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			if a.currentServer != nil && a.currentServer.ID == protocolServerID {
				if a.channelTree != nil {
					a.channelTree.RemoveChannel(payload.ChannelID)
					a.channelTree.RebuildFlatList(a.collapsedCategories)

					// If deleted channel was the current channel, select another
					if a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
						// Select first available channel
						if len(a.channelTree.FlatList) > 0 {
							for _, node := range a.channelTree.FlatList {
								if !node.IsCategory {
									a.currentChannel = node.Channel
									a.updateChatContent()
									break
								}
							}
						} else {
							a.currentChannel = nil
						}
					}
				}
			}
		}
	}

	return nil
}
