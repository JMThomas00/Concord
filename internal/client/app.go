package client

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

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
	typingUsers []string

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
}

// MessageDisplay wraps a message with display information
type MessageDisplay struct {
	*models.Message
	AuthorName  string
	AuthorColor string
	IsOwn       bool
	ShowHeader  bool // Show author/timestamp (false for consecutive messages)
	IsWhisper   bool // Ephemeral DM from /whisper
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
	// Configure keybindings: disable default Enter behavior (we'll handle it manually)
	input.KeyMap.InsertNewline.SetKeys("shift+enter")

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
		focus:                   FocusInput,
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
	cmds := []tea.Cmd{textinput.Blink, a.waitForConnEvent()}
	// Auto-connect all known servers when identity is configured
	if a.localIdentity != nil {
		// Activate the textarea so the user can type immediately on launch
		a.input.Focus()
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
	case tea.KeyMsg:
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

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateViewportSize()

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
	switch a.view {
	case ViewIdentitySetup:
		return a.renderIdentitySetupView()
	case ViewLogin:
		return a.renderLoginView()
	case ViewRegister:
		return a.renderRegisterView()
	case ViewMain:
		return a.renderMainView()
	case ViewAddServer:
		return a.renderAddServerView()
	case ViewManageServers:
		return a.renderManageServersView()
	case ViewThemeBrowser:
		return a.renderThemeBrowserView()
	default:
		return "Unknown view"
	}
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
	case "ctrl+c", "ctrl+q":
		return tea.Quit

	case "ctrl+b":
		// Open Manage Servers from login or main view
		if a.view == ViewLogin || a.view == ViewMain {
			a.view = ViewManageServers
			a.manageServersFocus = 0
			if a.pingResults == nil {
				a.pingResults = make(map[uuid.UUID]*PingResult)
			}
			return nil
		}

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
			input := a.input.Value()
			if strings.HasPrefix(input, "/") {
				a.handleTabCompletion()
				return nil
			}
			// @mention completion: Tab selects first suggestion
			if a.showMentionPopup && len(a.mentionSuggestions) > 0 {
				a.completeMention(a.mentionSuggestions[0])
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
		// For main view
		if a.view == ViewMain {
			// If focused on server icons
			if a.focus == FocusServerIcons {
				// Check if (+) add button is selected
				if a.serverIndex >= len(a.clientServers) {
					// Open add server view
					a.view = ViewAddServer
					a.addServerFocus = 0
					a.addServerName.Focus()
					a.addServerError = ""
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

	case "up", "k":
		if a.focus == FocusServerIcons {
			a.navigateServerList(-1)
		} else if a.focus == FocusChannelList {
			a.navigateChannelList(-1)
		} else if a.focus == FocusChat {
			a.chatViewport.LineUp(1)
		}

	case "down", "j":
		if a.focus == FocusServerIcons {
			a.navigateServerList(1)
		} else if a.focus == FocusChannelList {
			a.navigateChannelList(1)
		} else if a.focus == FocusChat {
			a.chatViewport.LineDown(1)
		}

	case "left":
		if a.focus == FocusChannelList {
			a.handleCollapseCategory()
		}

	case "h":
		if a.focus == FocusChannelList {
			a.handleCollapseCategory()
		}

	case "right":
		if a.focus == FocusChannelList {
			a.handleExpandCategory()
		}

	case "l":
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

	// If not connected, show prompt
	if a.activeConn == nil || a.activeConn.GetState() != StateReady {
		a.statusMessage = "Unable to connect to server. Server may be offline."
		a.currentServer = nil
		a.currentChannel = nil
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

	for _, msg := range messages {
		// Check if this is a system message
		isSystemMsg := msg.AuthorName == "System"

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
			content.WriteString(lineStyle.Render(header))
			content.WriteString("\n")
		}

		// Render message content
		if isSystemMsg {
			systemStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Italic(true).
				Width(viewportWidth)
			content.WriteString(systemStyle.Render(msg.Content))
		} else if msg.IsWhisper {
			// Whisper: render with distinctive orange/pink color
			whisperStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Orange)).
				Italic(true).
				Width(viewportWidth)
			content.WriteString(whisperStyle.Render(msg.Content))
		} else {
			// Regular messages — highlight @mentions of the current user
			rendered := a.renderMessageContent(msg.Content, viewportWidth)
			content.WriteString(rendered)
		}
		content.WriteString("\n")
	}

	a.chatViewport.SetContent(content.String())
}

// renderMessageContent renders message text, highlighting @mentions of the current user.
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

	if alias == "" || !containsMention(text, alias) {
		return msgStyle.Width(width).Render(text)
	}

	// Split on @alias (case-insensitive)
	token := "@" + alias
	lower := strings.ToLower(text)
	var result strings.Builder
	remaining := text
	lowerRemaining := lower
	for {
		idx := strings.Index(lowerRemaining, strings.ToLower(token))
		if idx == -1 {
			result.WriteString(msgStyle.Render(remaining))
			break
		}
		if idx > 0 {
			result.WriteString(msgStyle.Render(remaining[:idx]))
		}
		result.WriteString(mentionStyle.Render(remaining[idx : idx+len(token)]))
		remaining = remaining[idx+len(token):]
		lowerRemaining = lowerRemaining[idx+len(token):]
	}
	// Wrap the whole thing in a width-constrained block
	return lipgloss.NewStyle().Width(width).Render(result.String())
}

// scrollToBottom scrolls the chat to the bottom
func (a *App) scrollToBottom() {
	a.chatViewport.GotoBottom()
}

// updateTypingIndicator updates the typing users list
func (a *App) updateTypingIndicator(users []string) {
	a.typingUsers = users
}

// updateViewportSize updates viewport dimensions based on window size
func (a *App) updateViewportSize() {
	// Calculate panel sizes to match renderMainView (4-column layout)
	availableWidth := a.width - 1

	// Fixed widths for 4-column layout
	serverIconsWidth := 10   // Server icons column
	channelsWidth := 30      // Channels list column
	membersWidth := 30       // Members list column
	chatWidth := availableWidth - serverIconsWidth - channelsWidth - membersWidth

	// Ensure chat has minimum width
	if chatWidth < 60 {
		// If terminal is too narrow, reduce members width
		membersWidth = 20
		chatWidth = availableWidth - serverIconsWidth - channelsWidth - membersWidth
	}

	// Set textarea width to match chat panel interior (minus borders)
	// chatWidth includes the panel borders, so subtract 4 (panel border 2 + input border 2)
	a.input.SetWidth(chatWidth - 4)
	a.input.SetHeight(4) // 4 lines of text (top+bottom border + 4 content = 6 rows total, matching layout slot)
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

	// If this is the active connection, update UI
	if a.activeConn != nil && a.activeConn.ServerID == serverID {
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
				// Select first channel if none selected
				if a.currentChannel == nil && len(payload.Channels) > 0 {
					a.channelIndex = 0
					a.selectChannel(0)
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

		// Add messages to connection
		if a.activeConn != nil {
			a.activeConn.mu.Lock()
			// Clear existing messages first
			a.activeConn.Messages[payload.ChannelID] = nil

			// Add historical messages
			for _, msgDisplay := range payload.Messages {
				display := &MessageDisplay{
					Message:     msgDisplay.Message,
					AuthorName:  msgDisplay.Author.Username,
					AuthorColor: a.theme.Colors.Purple, // TODO: Use user color from role
					IsOwn:       msgDisplay.Author.ID == a.activeConn.User.ID,
					ShowHeader:  true, // TODO: Implement message grouping
				}
				a.activeConn.Messages[payload.ChannelID] = append(
					a.activeConn.Messages[payload.ChannelID],
					display,
				)
			}
			a.activeConn.mu.Unlock()

			// Refresh chat if viewing this channel
			if a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
				a.updateChatContent()
				a.scrollToBottom()
			}
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
		sc.Members = append(sc.Members, display)
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

	case protocol.EventTypingStart:
		// TODO: Handle typing indicators

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
