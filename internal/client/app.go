package client

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	ViewToS           View = iota // First-run: Terms of Service acceptance
	ViewIdentitySetup               // First-run: set up local identity
	ViewLogin
	ViewRegister
	ViewMain
	ViewServerList
	ViewChannelList
	ViewSettings
	ViewServerManagement
	ViewAddServer
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

	// Terms of Service acceptance state
	tosState *ToSState

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

	// Manage Servers (Settings sub-page)
	pingResults         map[uuid.UUID]*PingResult
	editingServerID     *uuid.UUID // Set when editing an existing server
	editingServerIndex  int        // Index in clientServers of the server being edited
	deleteConfirmServerID *uuid.UUID // Server awaiting delete confirmation

	// Status message
	statusMessage string
	statusError   bool

	// Command handler
	commandHandler *CommandHandler

	// Typing indicator
	typingUsers     []typingDisplayUser
	typingExpiry    map[uuid.UUID]time.Time // userID → when the typing indicator expires
	typingUsernames map[uuid.UUID]string    // userID → username carried on the event itself, for typists (e.g. plugin service accounts) with no ServerMember row to resolve from
	typingIsBot     map[uuid.UUID]bool      // userID → whether the typist is a plugin's own service account ("is thinking" vs. "is typing")
	typingFrame     int                     // current animation frame index
	lastTypingSent  time.Time               // when we last sent OpTypingStart

	// Theme browser state
	themeBrowserState *ThemeBrowserState

	// Settings view state
	settingsState *SettingsState

	// Server Management view state
	serverManagementState *ServerManagementState

	// @mention autocomplete state
	mentionSuggestions []string
	mentionQuery       string
	showMentionPopup   bool

	// Unread tracking (client-side only)
	unreadCounts  map[uuid.UUID]map[uuid.UUID]int // clientServerID → channelID → count
	mentionCounts map[uuid.UUID]map[uuid.UUID]int // clientServerID → channelID → @mention count
	mutedChannels map[uuid.UUID]bool              // channelID → muted
	mutedServers  map[uuid.UUID]bool              // clientServerID → muted

	// Notification settings (mirrors UIConfig.Notifications, kept in sync)
	notifConfig NotificationConfig

	// Audio settings (mirrors UIConfig.Audio, kept in sync)
	audioConfig AudioConfig

	// Voice engine state (nil when not in a voice channel)
	voiceEngine   *VoiceEngine
	voiceSigOut   chan VoiceSignalOut // engine → server: WebRTC signals
	voiceEventOut chan interface{}    // engine → bubbletea: state events
	voiceQuit     chan struct{}       // closed by stopVoiceEngine to unblock waiting cmds
	voiceQuality  map[uuid.UUID]int     // userID → latest ICE RTT ms (-1 = unknown)
	voiceLevels   map[uuid.UUID]float32 // userID → latest RMS output level (0.0–1.0)

	// File transfer engine state — lazily created on first use and kept alive
	// for the app's lifetime (unlike voice, transfers aren't tied to joining
	// a channel). Only ever sends/receives over a.activeConn, mirroring
	// voice's existing single-active-connection simplification.
	fileTransferEngine   *FileTransferEngine
	fileTransferSigOut   chan FileTransferSignalOut
	fileTransferEventOut chan interface{}
	// pendingFileTransferCmd holds the wait-pump Cmd from the most recent
	// ensureFileTransferEngine() call that created a new engine. Slash
	// command handlers (commands.go) can't return a tea.Cmd directly, so
	// handleSlashCommand collects this after Execute() runs.
	pendingFileTransferCmd tea.Cmd

	// Server list panel animation
	serverListAnimWidth int  // current animated width (22 expanded, 8 collapsed)
	serverListAnimating  bool

	// Members panel animation
	membersAnimWidth int  // current animated width (30 expanded, 8 collapsed)
	membersAnimating  bool

	// Full-panel slide animations
	settingsAnimFrame   int  // 0=hidden, panelAnimMaxFrames=fully visible
	settingsAnimClosing bool // true while sliding out
	settingsAnimating   bool

	srvMgmtAnimFrame   int
	srvMgmtAnimClosing bool
	srvMgmtAnimating   bool

	// AFK tracking
	lastActivityTime time.Time
	isAFK            bool

	// Message navigation state (two-level system)
	messageNavMode        bool      // Level 1: browsing messages
	messageNavIndex       int       // Which message is selected (0-based)
	messageLineOffsets    []int     // message index -> its starting line in the last content updateChatContent() rendered; the only source of truth calculateMessageLinePosition uses, so it can never drift from the actual (word-wrapped) render again
	inMessageEditMode     bool      // Level 2: navigating within a message
	messageCursorLine     int       // Cursor line within message (Level 2)
	messageCursorCol      int       // Cursor column within message (Level 2)
	messageSelectionStart *Position // Selection start (nil if no selection)
	messageSelectionEnd   *Position // Selection end

	// Reply state (Teams-style quoted replies)
	replyTarget *MessageDisplay // Message being replied to
	replyQuote  string          // Ellipsized first line for display

	// Edit message state
	editingMessageID  *uuid.UUID // Message being edited
	editingChannelID  *uuid.UUID // Channel of message being edited

	// Delete message state (confirmation)
	deleteConfirmMessageID *uuid.UUID // Message awaiting delete confirmation
	deleteConfirmChannelID *uuid.UUID // Channel of message to delete

	// Link browser state
	linkBrowserState *LinkBrowserState

	// Help modal state
	helpModalState *HelpModalState

	// Member panel navigation state
	selectedMemberIndex int                // Index in flattened member list
	memberContextMenu   *MemberContextMenu // Context menu state (nil when closed)

	// Hub Browser overlay
	showHubBrowser bool
	hubBrowser     HubBrowserState

	// Plugin platform: kinds advertised at READY (keyed "pluginID:kind"), and
	// the active remote-pane state when currentChannel is plugin-provided.
	// The client never needs plugin-specific code — see plugin_pane.go.
	pluginChannelKinds map[string]protocol.PluginChannelKindInfo
	pluginPane         *PluginPaneState
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

// HelpModalState holds the state for the help modal overlay
type HelpModalState struct {
	Content      string // Multi-line help text to display
	ScrollOffset int    // For future scrolling support (start at 0)
}

// MemberContextMenu holds the state for the member action context menu
type MemberContextMenu struct {
	TargetMember  *MemberDisplay
	Actions       []MemberAction
	SelectedIndex int
	VolumeSlider  *VolumeSliderState // non-nil when in per-user volume adjust mode
	AnimFrame     int                // 0 = just opened, counts up to contextMenuMaxFrames
}

// VolumeSliderState holds the in-progress per-user volume adjustment.
type VolumeSliderState struct {
	Volume float64 // 0.0–2.0; adjusted with ←/→, applied live
}

// MemberAction represents a single action in the context menu
type MemberAction struct {
	Label        string
	Key          string // Keyboard shortcut (e.g., "W", "K")
	Handler      func(*App, *MemberDisplay) tea.Cmd
	RequiresPerm models.Permission // 0 = no permission required
}

// MessageDisplay wraps a message with display information
type MessageDisplay struct {
	*models.Message
	AuthorName    string
	AuthorColor   string
	RecipientName string // For whispers only
	IsOwn         bool
	ShowHeader    bool // Show author/timestamp (false for consecutive messages)
	IsWhisper     bool // Ephemeral DM from /whisper
	IsSystem      bool // Server-wide moderation/system announcement
	IsDeleted     bool // Soft-deleted; rendered as [message deleted] placeholder
	IsBotAuthor   bool // Author is a plugin's own service account (models.User.IsServiceAccount) — never grouped under a shared header with an adjacent message, even the same author's own within the grouping window, since e.g. two of Mynah's replies landing close together answer two different questions and reading as one merged reply is actively misleading
}

// MemberDisplay wraps a member with display information
type MemberDisplay struct {
	User        *models.User
	Member      *models.ServerMember
	HighestRole *models.Role // highest hoisted role (nil = regular member)
	AvatarColor string       // hex color for circle avatar
	IsBanned    bool         // Is user banned from server
	IsMuted     bool         // Is user server-muted
	KickCount   int          // Number of times kicked
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
		IsBanned:    member.IsBanned,
		IsMuted:     member.IsMuted,
		KickCount:   member.KickCount,
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

// loadMutedServers converts the persisted []string of UUIDs into the runtime map.
func loadMutedServers(cfg *AppConfig) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool)
	if cfg == nil {
		return m
	}
	for _, s := range cfg.UI.MutedServers {
		if id, err := uuid.Parse(s); err == nil {
			m[id] = true
		}
	}
	return m
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

// saveMutedServers persists the current mutedServers map back to config.json.
func (a *App) saveMutedServers() {
	if a.configMgr == nil {
		return
	}
	cfg, err := a.configMgr.LoadAppConfig()
	if err != nil || cfg == nil {
		cfg = &AppConfig{Version: 1}
	}
	slugs := make([]string, 0, len(a.mutedServers))
	for id := range a.mutedServers {
		slugs = append(slugs, id.String())
	}
	cfg.UI.MutedServers = slugs
	_ = a.configMgr.SaveAppConfig(cfg)
}

// saveNotifConfig persists the current notification config back to config.json.
func (a *App) saveNotifConfig() {
	if a.configMgr == nil {
		return
	}
	cfg, err := a.configMgr.LoadAppConfig()
	if err != nil || cfg == nil {
		cfg = &AppConfig{Version: 1}
	}
	cfg.UI.Notifications = a.notifConfig
	_ = a.configMgr.SaveAppConfig(cfg)
}

// serverListAnimTick returns a Cmd that fires one animation frame (16ms ≈ 60fps).
func serverListAnimTick() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg {
		return serverListAnimTickMsg{}
	})
}

// membersAnimTick returns a Cmd that fires one members panel animation frame.
func membersAnimTick() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg {
		return membersAnimTickMsg{}
	})
}

// toggleMembersList flips the collapsed state and starts the slide animation.
func (a *App) toggleMembersList() tea.Cmd {
	if a.uiConfig == nil {
		return nil
	}
	a.uiConfig.Display.MembersListCollapsed = !a.uiConfig.Display.MembersListCollapsed
	a.membersAnimating = true
	a.saveDisplayConfig()
	return membersAnimTick()
}

// toggleServerList flips the collapsed state and starts the slide animation.
func (a *App) toggleServerList() tea.Cmd {
	if a.uiConfig == nil {
		return nil
	}
	a.uiConfig.Display.ServerListCollapsed = !a.uiConfig.Display.ServerListCollapsed
	a.serverListAnimating = true
	a.saveDisplayConfig()
	return serverListAnimTick()
}

// renderViewByID renders the background view used during panel slide animations.
func (a *App) renderViewByID(v View) string {
	switch v {
	case ViewMain:
		return a.renderMainView()
	case ViewLogin:
		return a.renderLoginView()
	default:
		return a.renderMainView()
	}
}

// saveDisplayConfig persists the current display config and ShowMembersList back to config.json.
func (a *App) saveDisplayConfig() {
	if a.configMgr == nil || a.uiConfig == nil {
		return
	}
	cfg, err := a.configMgr.LoadAppConfig()
	if err != nil || cfg == nil {
		cfg = &AppConfig{Version: 1}
	}
	cfg.UI.Display = a.uiConfig.Display
	cfg.UI.ShowMembersList = a.uiConfig.ShowMembersList
	_ = a.configMgr.SaveAppConfig(cfg)
}

// saveAudioConfig persists the current audio config back to config.json.
func (a *App) saveAudioConfig() {
	if a.configMgr == nil {
		return
	}
	cfg, err := a.configMgr.LoadAppConfig()
	if err != nil || cfg == nil {
		cfg = &AppConfig{Version: 1}
	}
	cfg.UI.Audio = a.audioConfig
	_ = a.configMgr.SaveAppConfig(cfg)
}

// formatTimestamp formats a message timestamp according to the current display config.
func (a *App) formatTimestamp(t time.Time) string {
	if a.uiConfig == nil {
		return t.Format("01/02/06 15:04")
	}
	cfg := a.uiConfig.Display
	if cfg.TimestampStyle == "relative" {
		now := time.Now()
		today := now.Truncate(24 * time.Hour)
		msgDay := t.Truncate(24 * time.Hour)
		var timePart string
		if cfg.TimestampFormat == "12h" {
			timePart = t.Format("3:04 PM")
		} else {
			timePart = t.Format("15:04")
		}
		switch {
		case msgDay.Equal(today):
			return "Today at " + timePart
		case msgDay.Equal(today.Add(-24 * time.Hour)):
			return "Yesterday at " + timePart
		default:
			return t.Format("Jan 2") + " at " + timePart
		}
	}
	// Absolute
	if cfg.TimestampFormat == "12h" {
		return t.Format("01/02/06 3:04 PM")
	}
	return t.Format("01/02/06 15:04")
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
	// Remove background color to match terminal background
	input.FocusedStyle.Base = input.FocusedStyle.Base.Background(lipgloss.NoColor{})
	input.FocusedStyle.CursorLine = input.FocusedStyle.CursorLine.Background(lipgloss.NoColor{})
	input.BlurredStyle.Base = input.BlurredStyle.Base.Background(lipgloss.NoColor{})
	input.BlurredStyle.CursorLine = input.BlurredStyle.CursorLine.Background(lipgloss.NoColor{})

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
				LastBannerIndex:     -1,
			},
		}
	}

	// Select random banner at startup (not on every render)
	// Use last banner index from config to ensure no consecutive repeats across launches
	banner, newBannerIndex := GetRandomBanner(appConfig.UI.LastBannerIndex)

	// Save the new banner index back to config
	appConfig.UI.LastBannerIndex = newBannerIndex
	if err := configMgr.SaveAppConfig(appConfig); err != nil {
		log.Printf("Warning: Failed to save banner index: %v", err)
	}

	// Determine startup view
	var startView View

	// Check ToS acceptance first
	if !appConfig.TermsAccepted {
		startView = ViewToS
	} else if identity != nil {
		servers := configMgr.GetClientServers()
		if len(servers) == 0 {
			startView = ViewAddServer
		} else {
			startView = ViewLogin
		}
	} else {
		startView = ViewIdentitySetup
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
		mutedServers:            loadMutedServers(appConfig),
		notifConfig:             appConfig.UI.Notifications,
		audioConfig:             defaultAudioConfig(appConfig.UI.Audio),
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

	// Initialize server list animation width based on saved collapsed state
	if appConfig.UI.Display.ServerListCollapsed {
		app.serverListAnimWidth = 10
	} else {
		app.serverListAnimWidth = 22
	}

	// Initialize members panel animation width based on saved collapsed state
	if appConfig.UI.Display.MembersListCollapsed {
		app.membersAnimWidth = 10
	} else {
		app.membersAnimWidth = 30
	}

	// Initialize command handler
	app.commandHandler = NewCommandHandler(app)

	// Pre-fill login form with saved credentials or defaults
	app.initLoginView()

	// Initialize ToS view if needed
	if startView == ViewToS {
		app.initToSView()
	}

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
		tea.Tick(a.typingTickDuration(), func(t time.Time) tea.Msg { return typingTickMsg(t) }),
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

		// Set active connection ONLY if it's nil (first connection)
		// OR if this is the server the user currently has selected.
		// This prevents background connections from overwriting activeConn.
		shouldSetActive := a.activeConn == nil ||
			(a.currentClientServer != nil && a.currentClientServer.ID == serverID)

		if shouldSetActive {
			a.activeConn = a.connMgr.GetConnection(serverID)
		}

		// Set token on the connection we just made (not necessarily activeConn)
		conn := a.connMgr.GetConnection(serverID)
		if conn != nil {
			conn.mu.Lock()
			conn.Token = token
			conn.mu.Unlock()
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

	// Once past the initial backoff ramp, cap RetryCount so delay stays at MaxDelay
	// rather than climbing forever. Never give up — keep polling until the server returns.
	if !strategy.ShouldRetry(attemptCount) {
		sc.mu.Lock()
		sc.RetryCount = strategy.MaxRetries
		sc.mu.Unlock()
		attemptCount = strategy.MaxRetries
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

	case exitNavModeMsg:
		a.messageNavMode = false
		if msg.setFocus {
			a.focus = FocusInput
			a.input.Focus()
		}
		if msg.enableEditMode {
			a.inMessageEditMode = true
		}
		a.updateChatContent()
		return a, tea.ShowCursor

	case typingTickMsg:
		// Advance animation frame and prune expired typing entries
		a.typingFrame++
		now := time.Time(msg)
		if a.typingExpiry != nil {
			changed := false
			for uid, exp := range a.typingExpiry {
				if now.After(exp) {
					delete(a.typingExpiry, uid)
					delete(a.typingUsernames, uid)
					delete(a.typingIsBot, uid)
					changed = true
				}
			}
			if changed {
				a.rebuildTypingUsers()
			}
		}
		cmds = append(cmds, tea.Tick(a.typingTickDuration(), func(t time.Time) tea.Msg { return typingTickMsg(t) }))

	case serverListAnimTickMsg:
		target := 22
		if a.uiConfig != nil && a.uiConfig.Display.ServerListCollapsed {
			target = 10
		}
		if a.serverListAnimWidth < target {
			a.serverListAnimWidth = min(a.serverListAnimWidth+2, target)
		} else if a.serverListAnimWidth > target {
			a.serverListAnimWidth = max(a.serverListAnimWidth-2, target)
		}
		a.updateViewportSize()
		if a.activeConn != nil && a.currentChannel != nil {
			a.updateChatContent()
		}
		if a.serverListAnimWidth != target {
			cmds = append(cmds, serverListAnimTick())
		} else {
			a.serverListAnimating = false
		}

	case membersAnimTickMsg:
		target := 30
		if a.uiConfig != nil && a.uiConfig.Display.MembersListCollapsed {
			target = 10
		}
		if a.membersAnimWidth < target {
			a.membersAnimWidth = min(a.membersAnimWidth+2, target)
		} else if a.membersAnimWidth > target {
			a.membersAnimWidth = max(a.membersAnimWidth-2, target)
		}
		a.updateViewportSize()
		if a.activeConn != nil && a.currentChannel != nil {
			a.updateChatContent()
		}
		if a.membersAnimWidth != target {
			cmds = append(cmds, membersAnimTick())
		} else {
			a.membersAnimating = false
		}

	case contextMenuAnimTickMsg:
		if a.memberContextMenu != nil && a.memberContextMenu.AnimFrame < contextMenuMaxFrames {
			a.memberContextMenu.AnimFrame++
			if a.memberContextMenu.AnimFrame < contextMenuMaxFrames {
				cmds = append(cmds, contextMenuAnimTick())
			}
		}

	case settingsPanelAnimTickMsg:
		if a.settingsAnimating {
			if !a.settingsAnimClosing {
				a.settingsAnimFrame++
				if a.settingsAnimFrame < panelAnimMaxFrames {
					cmds = append(cmds, settingsPanelAnimTick())
				} else {
					a.settingsAnimating = false
				}
			} else {
				a.settingsAnimFrame--
				if a.settingsAnimFrame > 0 {
					cmds = append(cmds, settingsPanelAnimTick())
				} else {
					a.settingsAnimating = false
					a.settingsAnimClosing = false
					if a.settingsState != nil {
						a.view = a.settingsState.PreviousView
						a.settingsState = nil
					}
				}
			}
		}

	case srvMgmtPanelAnimTickMsg:
		if a.srvMgmtAnimating {
			if !a.srvMgmtAnimClosing {
				a.srvMgmtAnimFrame++
				if a.srvMgmtAnimFrame < panelAnimMaxFrames {
					cmds = append(cmds, srvMgmtPanelAnimTick())
				} else {
					a.srvMgmtAnimating = false
				}
			} else {
				a.srvMgmtAnimFrame--
				if a.srvMgmtAnimFrame > 0 {
					cmds = append(cmds, srvMgmtPanelAnimTick())
				} else {
					a.srvMgmtAnimating = false
					a.srvMgmtAnimClosing = false
					if a.serverManagementState != nil {
						a.view = a.serverManagementState.PreviousView
						a.serverManagementState = nil
					}
				}
			}
		}

	case chatReflowMsg:
		a.updateViewportSize()
		a.updateChatContent()

	case hubServersLoadedMsg, hubLoadErrorMsg, hubJoinResponseMsg, hubJoinVerifiedMsg,
		hubJoinErrorMsg, hubHealthCheckMsg, hubHealthCheckErrMsg, hubPeersLoadedMsg,
		hubPeersLoadErrMsg:
		if handled, cmd := a.handleHubMsg(msg); handled {
			return a, cmd
		}

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
			time.Since(a.lastTypingSent) > 2*time.Second {
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
		a.resizePluginPane()

	case tea.MouseMsg:
		// Handle mouse wheel scrolling over chat viewport (regardless of focus)
		if a.view == ViewMain && (msg.Type == tea.MouseWheelUp || msg.Type == tea.MouseWheelDown) {
			if a.isCursorOverChatViewport(msg.X, msg.Y) {
				var cmd tea.Cmd
				a.chatViewport, cmd = a.chatViewport.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
		// Help & Guide page mouse wheel scrolling (works regardless of FocusOnForm)
		if a.view == ViewSettings && (msg.Type == tea.MouseWheelUp || msg.Type == tea.MouseWheelDown) {
			s := a.settingsState
			if s != nil && s.SelectedCategory == len(s.Categories)-1 {
				if msg.Type == tea.MouseWheelUp {
					if s.HelpScrollOffset > 0 {
						s.HelpScrollOffset--
					}
				} else {
					s.HelpScrollOffset++
				}
			}
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
		// A dropped connection can't relay a leave_pane signal — the
		// plugin has no way to ask Concord to release the pane over a
		// connection that no longer exists, and 'q' has nothing to reach
		// either. Without this, a viewer who'd focused a plugin pane
		// before the connection dropped would be stuck: every key
		// captured by the pane guards in handleKeyPress/Update, no
		// escape route left. Release it here instead, the same way a
		// clean leave_pane would.
		if a.pluginPane != nil {
			a.leavePluginPane()
			a.focus = FocusChannelList
		}
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

	case VoiceEngineReadyMsg:
		a.statusMessage = "Voice engine ready"
		a.statusError = false
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceEvent())
			cmds = append(cmds, a.waitForVoiceSignal())
		}

	case VoiceEngineErrorMsg:
		a.statusMessage = fmt.Sprintf("Voice error: %v", msg.Err)
		a.statusError = true
		a.stopVoiceEngine()

	case VoiceConnectedMsg:
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceEvent())
		}

	case VoiceDisconnectedMsg:
		a.stopVoiceEngine()

	case VoicePeerConnectedMsg:
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceEvent())
		}

	case VoicePeerDisconnectedMsg:
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceEvent())
		}

	case VoiceQualityMsg:
		if a.voiceQuality == nil {
			a.voiceQuality = make(map[uuid.UUID]int)
		}
		a.voiceQuality[msg.UserID] = msg.LatencyMs
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceEvent())
		}

	case VoiceLevelMsg:
		if a.voiceLevels == nil {
			a.voiceLevels = make(map[uuid.UUID]float32)
		}
		a.voiceLevels[msg.UserID] = msg.Level
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceEvent())
		}

	case voiceLocalSpeakingMsg:
		// Forward local speaking state to server so others see the indicator.
		if a.activeConn != nil && a.activeConn.Connection != nil {
			chID := a.activeConn.CurrentVoiceChannelID
			if chID != uuid.Nil {
				payload := &protocol.VoiceSpeakingPayload{
					ChannelID:  chID,
					IsSpeaking: msg.speaking,
				}
				if wsMsg, err := protocol.NewMessage(protocol.OpVoiceSpeaking, payload); err == nil {
					_ = a.activeConn.Connection.Send(wsMsg)
				}
			}
		}
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceEvent())
		}

	case VoiceSignalOut:
		// Forward WebRTC signal (offer/answer/candidate) to the target peer via server relay.
		if a.activeConn != nil && a.activeConn.Connection != nil {
			payload := &protocol.VoiceSignalPayload{
				TargetUserID: msg.TargetUserID,
				ChannelID:    msg.ChannelID,
				Type:         msg.Type,
				SDP:          msg.SDP,
				Candidate:    msg.Candidate,
			}
			if wsMsg, err := protocol.NewMessage(protocol.OpVoiceSignal, payload); err == nil {
				_ = a.activeConn.Connection.Send(wsMsg)
			}
		}
		if a.voiceEngine != nil {
			cmds = append(cmds, a.waitForVoiceSignal())
		}

	case FileTransferSignalOut:
		// Forward a file transfer signal (request/offer/answer/candidate) to
		// the target peer via server relay. Mirrors VoiceSignalOut above.
		if a.activeConn != nil && a.activeConn.Connection != nil {
			payload := &protocol.FileTransferSignalPayload{
				TargetUserID: msg.TargetUserID,
				AttachmentID: msg.AttachmentID,
				Type:         msg.Type,
				SDP:          msg.SDP,
				Candidate:    msg.Candidate,
			}
			if wsMsg, err := protocol.NewMessage(protocol.OpFileTransferSignal, payload); err == nil {
				_ = a.activeConn.Connection.Send(wsMsg)
			}
		}
		if a.fileTransferEngine != nil {
			cmds = append(cmds, a.waitForFileTransferSignal())
		}

	case FileTransferProgressMsg:
		pct := 0
		if msg.TotalBytes > 0 {
			pct = int(msg.BytesDone * 100 / msg.TotalBytes)
		}
		verb := "Sending"
		if msg.Direction == TransferReceiving {
			verb = "Downloading"
		}
		a.statusMessage = fmt.Sprintf("%s file: %d%%", verb, pct)
		a.statusError = false
		if a.fileTransferEngine != nil {
			cmds = append(cmds, a.waitForFileTransferEvent())
		}

	case FileTransferDoneMsg:
		if msg.Err != nil {
			verb := "send"
			if msg.Direction == TransferReceiving {
				verb = "download"
			}
			a.statusMessage = fmt.Sprintf("File %s failed (%s): %v", verb, msg.Filename, msg.Err)
			a.statusError = true
		} else if msg.Direction == TransferReceiving {
			a.statusMessage = fmt.Sprintf("Downloaded %s to %s", msg.Filename, msg.DestPath)
			a.statusError = false
		} else {
			a.statusMessage = fmt.Sprintf("Finished sending %s", msg.Filename)
			a.statusError = false
		}
		if a.fileTransferEngine != nil {
			cmds = append(cmds, a.waitForFileTransferEvent())
		}

	case FileTransferOfflineMsg:
		a.statusMessage = "Can't download: the sender is offline"
		a.statusError = true
		if a.fileTransferEngine != nil {
			cmds = append(cmds, a.waitForFileTransferEvent())
		}

	case FileTransferRejectedMsg:
		a.statusMessage = "Download request was declined (file may no longer be available)"
		a.statusError = true
		if a.fileTransferEngine != nil {
			cmds = append(cmds, a.waitForFileTransferEvent())
		}

	case ErrorMsg:
		a.statusMessage = msg.Error
		a.statusError = true
		// Re-subscribe to connection events
		cmds = append(cmds, a.waitForConnEvent())
	}

	// Update focused component
	switch a.view {
	case ViewToS:
		var cmd tea.Cmd
		a.tosState.viewport, cmd = a.tosState.viewport.Update(msg)
		a.tosState.updateScrollState()
		cmds = append(cmds, cmd)
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
		if keyMsg, ok := msg.(tea.KeyMsg); ok && a.focus == FocusChat && a.pluginPane != nil && a.currentChannel != nil && a.pluginPane.ChannelID == a.currentChannel.ID {
			// A remote-pane plugin channel replaces both the chat viewport and
			// the message entry field — every key not already claimed by a
			// global keybind above forwards to the owning plugin process.
			// Gated on FocusChat for the same reason as the guard at the top
			// of handleKeyPress: enterPluginPane fires on every arrow-key step
			// while merely browsing the channel list (FocusChannelList), and
			// without this check the same keypress that moves Concord's own
			// list cursor would also leak into the plugin's board as a
			// spurious cursor move.
			a.forwardPluginPaneInput(keyMsg)
		} else if a.focus == FocusInput && !a.messageNavMode {
			// Only pass keys to textarea if BOTH focus is on input AND not in message nav mode
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
	// Hub browser is a full-screen overlay; render it before the normal view switch.
	if a.showHubBrowser {
		return a.renderHubBrowserView()
	}

	var baseView string
	switch a.view {
	case ViewToS:
		baseView = a.renderToSView()
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
	case ViewSettings:
		baseView = a.renderSettingsView()
		if a.settingsAnimating {
			t := float64(a.settingsAnimFrame) / float64(panelAnimMaxFrames)
			animWidth := int(easeInOutCubic(t) * float64(a.width))
			if animWidth < a.width {
				baseView = clipPanelLeft(baseView, animWidth)
			}
		}
	case ViewServerManagement:
		baseView = a.renderServerManagementView()
		if a.srvMgmtAnimating {
			t := float64(a.srvMgmtAnimFrame) / float64(panelAnimMaxFrames)
			animWidth := int(easeInOutCubic(t) * float64(a.width))
			if animWidth < a.width {
				baseView = clipPanelRight(baseView, animWidth, a.width)
			}
		}
	case ViewThemeBrowser:
		baseView = a.renderThemeBrowserView()
	default:
		baseView = "Unknown view"
	}

	// Render link browser overlay if active
	if a.linkBrowserState != nil {
		return a.renderLinkBrowserOverlay(baseView)
	}

	// Render help modal overlay if active
	if a.helpModalState != nil {
		return a.renderHelpModalOverlay(baseView)
	}

	// Render member context menu overlay if active
	if a.memberContextMenu != nil {
		return a.renderMemberContextMenuOverlay(baseView)
	}

	return baseView
}

// handleKeyPress handles keyboard input
func (a *App) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	// A remote-pane plugin channel captures every key, full stop — every
	// global keybind below (tab cycling focus, [ / ] toggling panels,
	// ctrl+s opening Settings, etc.) would otherwise fire on the very same
	// keypress the pane also receives via forwardPluginPaneInput later in
	// Update(), corrupting both at once: e.g. tab simultaneously cycling
	// Concord's own focus AND advancing a field inside the plugin's form.
	// There's no client-side escape key reserved for leaving the pane —
	// the plugin itself decides when that's safe (e.g. only from its own
	// main view, not mid-form) and asks Concord to leave via
	// EventPluginEvent{Kind: "leave_pane"} (see the EventPluginEvent case
	// in the connection-event dispatch switch, not this function).
	//
	// Gated on a.focus == FocusChat, not just a.pluginPane being set:
	// navigateChannelList calls selectChannel — and therefore
	// enterPluginPane — on every arrow-key step while merely browsing the
	// channel list (a.focus == FocusChannelList), the same as it does for
	// an ordinary text channel's preview. Capturing input at that point
	// would hijack the very same arrow keys mid-browse, making it
	// impossible to navigate past a plugin channel without first "escaping"
	// it. Tab into FocusChat (the same deliberate step needed to scroll an
	// ordinary channel's messages) before the pane starts capturing
	// everything.
	if a.view == ViewMain && a.focus == FocusChat && a.pluginPane != nil && a.currentChannel != nil && a.pluginPane.ChannelID == a.currentChannel.ID {
		return nil
	}

	// Hub browser intercepts all keys when visible
	if a.showHubBrowser {
		return a.handleHubBrowserKey(msg)
	}

	// Route to view-specific handlers first
	if a.view == ViewToS {
		return a.handleToSKey(msg)
	}
	if a.view == ViewIdentitySetup {
		return a.handleIdentitySetupKey(msg)
	}
	if a.view == ViewThemeBrowser {
		return a.handleThemeBrowserKey(msg)
	}
	if a.view == ViewSettings {
		return a.handleSettingsKey(msg)
	}
	if a.view == ViewServerManagement {
		return a.handleServerManagementKey(msg)
	}

	// PTT toggle: intercept before view-specific key routing.
	if a.voiceEngine != nil && a.audioConfig.PTTEnabled && a.audioConfig.PTTKey != "" {
		if msg.String() == a.audioConfig.PTTKey {
			a.voiceEngine.TogglePTT()
			return nil
		}
	}

	// Member context menu letter shortcuts: when the action list is visible (not slider mode),
	// pressing the key shown next to an action directly executes it.
	if a.memberContextMenu != nil && a.memberContextMenu.VolumeSlider == nil {
		pressedKey := strings.ToUpper(msg.String())
		for i, action := range a.memberContextMenu.Actions {
			if strings.ToUpper(action.Key) == pressedKey {
				a.memberContextMenu.SelectedIndex = i
				return a.executeMemberAction()
			}
		}
	}

	switch msg.String() {
	case "ctrl+q":
		return tea.Quit

	case "ctrl+m":
		// Toggle self-mute in voice channel
		if a.view == ViewMain && a.activeConn != nil && a.currentClientServer != nil {
			a.activeConn.mu.RLock()
			vs := a.activeConn.VoiceStates[a.activeConn.User.ID]
			a.activeConn.mu.RUnlock()
			if vs != nil {
				payload := &protocol.VoiceStateUpdatePayload{
					ServerID:       vs.ServerID,
					ChannelID:      &vs.ChannelID,
					IsSelfMuted:    !vs.IsSelfMuted,
					IsSelfDeafened: vs.IsSelfDeafened,
				}
				_ = a.connMgr.SendVoiceStateUpdate(a.currentClientServer.ID, payload)
			}
		}
		return nil

	case "ctrl+d":
		// Toggle self-deafen in voice channel
		if a.view == ViewMain && a.activeConn != nil && a.currentClientServer != nil {
			a.activeConn.mu.RLock()
			vs := a.activeConn.VoiceStates[a.activeConn.User.ID]
			a.activeConn.mu.RUnlock()
			if vs != nil {
				payload := &protocol.VoiceStateUpdatePayload{
					ServerID:       vs.ServerID,
					ChannelID:      &vs.ChannelID,
					IsSelfMuted:    vs.IsSelfMuted,
					IsSelfDeafened: !vs.IsSelfDeafened,
				}
				_ = a.connMgr.SendVoiceStateUpdate(a.currentClientServer.ID, payload)
			}
		}
		return nil

	case "ctrl+s":
		// Context-aware: Open Settings from login/main view, otherwise cycle servers
		if a.view == ViewLogin || a.view == ViewMain {
			return a.openSettings(a.view)
		}
		// Cycle through client servers (forward) when not in login/main view
		if len(a.clientServers) > 0 {
			a.serverIndex = (a.serverIndex + 1) % len(a.clientServers)
			a.switchToClientServer(a.serverIndex)
		}

	case "ctrl+b":
		// Open Server Management from main view (admin only)
		if a.view == ViewMain {
			// Check if user has admin permissions
			if a.currentUserRoleLevel() >= roleLevelAdmin {
				return a.openServerManagement(ViewMain, 0)
			}
		}

	case "ctrl+t":
		// Open theme browser from login or main view
		if a.view == ViewLogin || a.view == ViewMain {
			a.openThemeBrowser(a.view)
			return nil
		}

	case "ctrl+g":
		// Open the Grapevine Hub Browser (public server discovery).
		// Only where Settings > Manage Servers (the primary entry, key B)
		// is not reachable — i.e. before the user has any server to log
		// into. From the main view, use Settings > Manage Servers.
		switch a.view {
		case ViewLogin, ViewAddServer:
			return a.openHubBrowser()
		}

	case "[":
		// Toggle server list panel collapse/expand with animation (not while typing)
		if a.view == ViewMain && a.focus != FocusInput {
			return a.toggleServerList()
		}

	case "]":
		// Toggle members panel collapse/expand with animation (not while typing)
		if a.view == ViewMain && a.focus != FocusInput {
			return a.toggleMembersList()
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
		// Delete confirmation: send delete request
		if a.deleteConfirmMessageID != nil && a.deleteConfirmChannelID != nil {
			messageID := *a.deleteConfirmMessageID
			channelID := *a.deleteConfirmChannelID
			serverID := a.currentClientServer.ID

			// Clear confirmation state
			a.deleteConfirmMessageID = nil
			a.deleteConfirmChannelID = nil

			// Send delete request
			payload := &protocol.DeleteMessagePayload{
				MessageID: messageID,
				ChannelID: channelID,
			}

			data, err := json.Marshal(payload)
			if err != nil {
				a.statusMessage = fmt.Sprintf("Failed to marshal delete payload: %v", err)
				a.statusError = true
				return nil
			}

			msg := &protocol.Message{
				Op:   protocol.OpDeleteMessage,
				Data: data,
			}

			if err := a.connMgr.SendRaw(serverID, msg); err != nil {
				a.statusMessage = fmt.Sprintf("Failed to delete message: %v", err)
				a.statusError = true
				return nil
			}

			a.statusMessage = "Message deleted"
			a.statusError = false

			// Exit message nav mode
			a.messageNavMode = false
			a.focus = FocusInput
			a.updateChatContent()
			return nil
		}

		// In link browser: open selected link
		if a.linkBrowserState != nil {
			if a.linkBrowserState.SelectedIndex >= 0 && a.linkBrowserState.SelectedIndex < len(a.linkBrowserState.Links) {
				link := a.linkBrowserState.Links[a.linkBrowserState.SelectedIndex]
				a.closeLinkBrowser()
				return a.openURL(link)
			}
			return nil
		}
		// In member context menu: execute selected action
		if a.memberContextMenu != nil {
			return a.executeMemberAction()
		}
		// In member list: open context menu for selected member
		if a.view == ViewMain && a.focus == FocusUserList {
			return a.openMemberContextMenu()
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
				// Select the server and go to login if not connected
				if a.serverIndex < len(a.clientServers) {
					a.switchToClientServer(a.serverIndex)
					if a.activeConn == nil || a.activeConn.GetState() != StateReady {
						a.view = ViewLogin
						a.initLoginView()
					}
				}
				return nil
			}
			// If focused on channel list and selected channel is a voice channel,
			// Enter joins the voice channel (or leaves if already in it).
			if a.focus == FocusChannelList && a.currentChannel != nil &&
				a.currentChannel.Type == models.ChannelTypeVoice &&
				a.activeConn != nil && a.currentServer != nil && a.currentClientServer != nil {
				a.activeConn.mu.RLock()
				alreadyInThisChannel := a.activeConn.CurrentVoiceChannelID == a.currentChannel.ID
				a.activeConn.mu.RUnlock()

				if alreadyInThisChannel {
					// Leave voice
					payload := &protocol.VoiceStateUpdatePayload{
						ServerID:  a.currentServer.ID,
						ChannelID: nil,
					}
					_ = a.connMgr.SendVoiceStateUpdate(a.currentClientServer.ID, payload)
					a.stopVoiceEngine()
					a.statusMessage = "Left voice channel."
				} else {
					// Join voice
					chID := a.currentChannel.ID
					payload := &protocol.VoiceStateUpdatePayload{
						ServerID:  a.currentServer.ID,
						ChannelID: &chID,
					}
					_ = a.connMgr.SendVoiceStateUpdate(a.currentClientServer.ID, payload)
					a.statusMessage = fmt.Sprintf("Joined voice: %s", a.currentChannel.Name)
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
		// Close help modal if active
		if a.helpModalState != nil {
			a.closeHelpModal()
			return nil
		}
		// Volume slider: Esc exits slider mode back to the action list.
		if a.memberContextMenu != nil && a.memberContextMenu.VolumeSlider != nil {
			a.memberContextMenu.VolumeSlider = nil
			return nil
		}
		// Close member context menu if active
		if a.memberContextMenu != nil {
			a.closeMemberContextMenu()
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
		// Clear reply state if active
		if a.replyTarget != nil {
			a.replyTarget = nil
			a.replyQuote = ""
			a.statusMessage = "Reply cancelled"
			a.statusError = false
			return nil
		}
		// Cancel delete confirmation if active
		if a.deleteConfirmMessageID != nil {
			a.deleteConfirmMessageID = nil
			a.deleteConfirmChannelID = nil
			a.statusMessage = "Delete cancelled"
			a.statusError = false
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
			// Cancel: return to appropriate view based on context
			a.addServerError = ""
			a.editingServerID = nil
			if a.settingsState != nil {
				// Return to Settings if opened from there
				a.view = ViewSettings
			} else if a.serverManagementState != nil {
				// Return to Server Management if opened from there
				a.view = ViewServerManagement
			} else {
				// Otherwise return to main view
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

				// PRE-CALCULATE scroll position for newest message
				linePos := a.calculateMessageLinePosition(a.messageNavIndex)
				viewportHeight := a.chatViewport.Height
				targetOffset := linePos - (viewportHeight / 3)
				if targetOffset < 0 {
					targetOffset = 0
				}

				// Refresh viewport to show highlighting
				a.updateChatContent()

				// Apply scroll position
				a.chatViewport.SetYOffset(targetOffset)

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

	case "r", "R":
		// Reply to selected message (Teams-style quoted reply)
		if a.messageNavMode && !a.inMessageEditMode && a.messageNavIndex >= 0 {
			if a.activeConn == nil || a.currentChannel == nil {
				return nil
			}
			messages := a.activeConn.GetMessages(a.currentChannel.ID)
			if a.messageNavIndex < len(messages) {
				msg := messages[a.messageNavIndex]

				// Set reply target (quote will show above input, styled)
				a.replyTarget = msg
				a.replyQuote = extractFirstLineWithEllipsis(msg.Content, 60)

				// Clear input and return command to exit nav mode AFTER component updates
				a.input.SetValue("")  // Clear any stray input
				return func() tea.Msg {
					return exitNavModeMsg{setFocus: true}
				}
			}
		}

	case "e", "E":
		// Edit selected message (own messages only, or admin/mod with permission)
		if a.messageNavMode && !a.inMessageEditMode && a.messageNavIndex >= 0 {
			if a.activeConn == nil || a.currentChannel == nil || a.activeConn.User == nil {
				return nil
			}
			messages := a.activeConn.GetMessages(a.currentChannel.ID)
			if a.messageNavIndex < len(messages) {
				msg := messages[a.messageNavIndex]

				// Check if user can edit (own message only for now - permissions check TODO)
				if msg.AuthorID != a.activeConn.User.ID {
					a.statusMessage = "You can only edit your own messages"
					a.statusError = true
					return nil
				}

				// Pre-fill input with message content
				a.input.SetValue(msg.Content)  // This replaces any content, so should be clean

				// Track editing state
				a.editingMessageID = &msg.ID
				a.editingChannelID = &msg.ChannelID

				// Return command to exit nav mode AFTER component updates
				return func() tea.Msg {
					return exitNavModeMsg{setFocus: true, enableEditMode: true}
				}
			}
		}

	case "d", "D":
		// Delete selected message (with confirmation)
		if a.messageNavMode && !a.inMessageEditMode && a.messageNavIndex >= 0 {
			if a.activeConn == nil || a.currentChannel == nil || a.activeConn.User == nil {
				return nil
			}
			messages := a.activeConn.GetMessages(a.currentChannel.ID)
			if a.messageNavIndex < len(messages) {
				msg := messages[a.messageNavIndex]

				// Check permission: own message (24h window) or manage messages permission
				isOwn := msg.AuthorID == a.activeConn.User.ID
				withinWindow := time.Since(msg.CreatedAt) < 24*time.Hour
				hasManageMessages := a.hasPermission(a.activeConn.User, models.PermissionManageMessages)

				// Can delete if: (own message within 24h) OR (has manage messages permission)
				canDelete := (isOwn && withinWindow) || hasManageMessages

				if !canDelete {
					if isOwn && !withinWindow {
						a.statusMessage = "You can only delete your own messages within 24 hours"
					} else {
						a.statusMessage = "You don't have permission to delete this message"
					}
					a.statusError = true
					return nil
				}

				// Enter confirmation mode
				a.deleteConfirmMessageID = &msg.ID
				a.deleteConfirmChannelID = &msg.ChannelID
				a.statusMessage = "Delete this message? [Enter] Confirm [Esc] Cancel"
				a.statusError = false
				a.updateChatContent()
			}
		}

	case "s":
		// Open Settings when focused on server list
		if a.view == ViewMain && a.focus == FocusServerIcons {
			return a.openSettings(ViewMain)
		}

	case "l":
		// In message navigation mode: open link(s) in selected message
		if a.messageNavMode && a.activeConn != nil && a.currentChannel != nil {
			messages := a.activeConn.GetMessages(a.currentChannel.ID)
			if a.messageNavIndex >= 0 && a.messageNavIndex < len(messages) {
				msg := messages[a.messageNavIndex]
				links := a.extractLinksFromMessage(msg)

				if len(links) == 0 {
					// No links: show status message
					a.statusMessage = "No links found in selected message"
					a.statusError = false
				} else if len(links) == 1 {
					// Single link: open directly without link browser
					cmd := a.openURL(links[0])
					a.statusMessage = "Opened link in browser"
					a.statusError = false
					return cmd
				} else {
					// Multiple links: show link browser
					a.openLinkBrowser(links, msg, "message_nav")
					// Temporarily exit message nav mode while in link browser
					a.messageNavMode = false
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
		// Member context menu navigation (blocked while volume slider is active)
		if a.memberContextMenu != nil && a.memberContextMenu.VolumeSlider == nil {
			a.memberContextMenu.SelectedIndex--
			if a.memberContextMenu.SelectedIndex < 0 {
				a.memberContextMenu.SelectedIndex = len(a.memberContextMenu.Actions) - 1
			}
			return nil
		} else if a.memberContextMenu != nil {
			return nil // absorb key in slider mode
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
		} else if a.focus == FocusUserList {
			a.navigateMemberList(-1)
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
		// Member context menu navigation (blocked while volume slider is active)
		if a.memberContextMenu != nil && a.memberContextMenu.VolumeSlider == nil {
			a.memberContextMenu.SelectedIndex++
			if a.memberContextMenu.SelectedIndex >= len(a.memberContextMenu.Actions) {
				a.memberContextMenu.SelectedIndex = 0
			}
			return nil
		} else if a.memberContextMenu != nil {
			return nil // absorb key in slider mode
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
		} else if a.focus == FocusUserList {
			a.navigateMemberList(1)
		} else if a.focus == FocusChat {
			a.chatViewport.LineDown(1)
		}

	case "shift+up":
		// Level 2: Select text while moving cursor up
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorWithSelection(0, -1) // dy = -1
		}

	case "shift+down":
		// Level 2: Select text while moving cursor down
		if a.messageNavMode && a.inMessageEditMode {
			return a.moveCursorWithSelection(0, 1) // dy = 1
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
		// Volume slider: decrease by 1%
		if a.memberContextMenu != nil && a.memberContextMenu.VolumeSlider != nil {
			a.memberContextMenu.VolumeSlider.Volume -= 0.01
			if a.memberContextMenu.VolumeSlider.Volume < 0 {
				a.memberContextMenu.VolumeSlider.Volume = 0
			}
			a.applyVolumeAdjust()
			return nil
		}
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
		// Volume slider: increase by 1%
		if a.memberContextMenu != nil && a.memberContextMenu.VolumeSlider != nil {
			a.memberContextMenu.VolumeSlider.Volume += 0.01
			if a.memberContextMenu.VolumeSlider.Volume > 2.0 {
				a.memberContextMenu.VolumeSlider.Volume = 2.0
			}
			a.applyVolumeAdjust()
			return nil
		}
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
	// Navigate through servers only (removed "Manage Servers" button)
	totalItems := len(a.clientServers)
	if totalItems == 0 {
		return
	}

	a.serverIndex += delta
	if a.serverIndex < 0 {
		a.serverIndex = totalItems - 1
	} else if a.serverIndex >= totalItems {
		a.serverIndex = 0
	}

	a.switchToClientServer(a.serverIndex)
}

// navigateChannelList navigates the channel list for the current server
func (a *App) navigateChannelList(delta int) {
	if a.channelTree == nil || len(a.channelTree.FlatList) == 0 {
		return
	}

	// Find current channel in flat list (including categories)
	currentIdx := -1
	for i, node := range a.channelTree.FlatList {
		if a.currentChannel != nil && node.Channel.ID == a.currentChannel.ID {
			currentIdx = i
			break
		}
	}

	// Navigate to next/previous item (including categories)
	newIdx := currentIdx + delta

	// Wrap around
	if newIdx < 0 {
		newIdx = len(a.channelTree.FlatList) - 1
	} else if newIdx >= len(a.channelTree.FlatList) {
		newIdx = 0
	}

	// Select the item at newIdx (could be channel or category)
	if newIdx >= 0 && newIdx < len(a.channelTree.FlatList) {
		a.selectChannelByID(a.channelTree.FlatList[newIdx].Channel.ID)
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

	// Normalize sibling SortOrder to distinct values (i*10) before swapping,
	// so equal-SortOrder channels (all new channels default to 0) still sort correctly.
	for i, sib := range siblings {
		sib.Channel.SortOrder = i * 10
	}

	// Swap SortOrder between the current and target nodes (they share pointers with sc.Channels)
	node.Channel.SortOrder, targetNode.Channel.SortOrder = targetNode.Channel.SortOrder, node.Channel.SortOrder

	// Swap in the siblings slice so RebuildFlatList reflects the new order immediately
	siblings[currentIdx], siblings[targetIdx] = siblings[targetIdx], siblings[currentIdx]

	// Re-sort sc.Channels by SortOrder so future loadChannelTree calls use the correct order
	serverID := a.currentServer.ID
	a.activeConn.mu.Lock()
	sort.Slice(a.activeConn.Channels[serverID], func(i, j int) bool {
		return a.activeConn.Channels[serverID][i].SortOrder < a.activeConn.Channels[serverID][j].SortOrder
	})
	a.activeConn.mu.Unlock()

	// Rebuild flat list for immediate rendering
	a.channelTree.RebuildFlatList(a.collapsedCategories)

	// Capture values before the goroutine closure
	currID := node.Channel.ID
	targetID := targetNode.Channel.ID
	currSortOrder := node.Channel.SortOrder
	targetSortOrder := targetNode.Channel.SortOrder
	pServerID := serverID

	return func() tea.Msg {
		conn := a.activeConn
		if conn == nil {
			return nil
		}
		req1 := &protocol.ChannelUpdateRequest{
			ServerID:  pServerID,
			ChannelID: currID,
			SortOrder: &currSortOrder,
		}
		if msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req1); err == nil {
			_ = conn.Connection.Send(msg)
		}
		req2 := &protocol.ChannelUpdateRequest{
			ServerID:  pServerID,
			ChannelID: targetID,
			SortOrder: &targetSortOrder,
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

	// PRE-CALCULATE scroll position BEFORE updating content
	linePos := a.calculateMessageLinePosition(a.messageNavIndex)
	viewportHeight := a.chatViewport.Height
	targetOffset := linePos - (viewportHeight / 3)
	if targetOffset < 0 {
		targetOffset = 0
	}

	// Refresh viewport to show new highlight position
	a.updateChatContent()

	// IMMEDIATELY apply calculated offset after SetContent
	// SetYOffset clamps to valid range internally
	a.chatViewport.SetYOffset(targetOffset)

	return nil
}

// extractFirstLineWithEllipsis extracts the first line of a message with ellipsis if truncated or multi-line
func extractFirstLineWithEllipsis(content string, maxLen int) string {
	lines := strings.Split(content, "\n")
	firstLine := lines[0]

	// Truncate if exceeds maxLen
	if len(firstLine) > maxLen {
		return firstLine[:maxLen] + "..."
	}

	// Add ellipsis if there are multiple lines
	if len(lines) > 1 {
		return firstLine + "..."
	}

	return firstLine
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

// buildFlatMemberList creates a flat list of members in the exact order rendered by
// renderUserList: voice channel groups first, then role sections, then regular members.
// This guarantees selectedMemberIndex always matches the highlighted row.
func (a *App) buildFlatMemberList() []*MemberDisplay {
	if a.activeConn == nil {
		return nil
	}

	a.activeConn.mu.RLock()
	members := a.activeConn.Members
	voiceStates := a.activeConn.VoiceStates
	channels := a.activeConn.Channels
	a.activeConn.mu.RUnlock()

	var flatList []*MemberDisplay

	// ── 1. Voice channel groups (rendered first) ──────────────────────────────
	type voiceGroup struct {
		channelID uuid.UUID
		position  int
		name      string
		members   []*MemberDisplay
	}
	voiceUserSet := make(map[uuid.UUID]struct{})
	groupMap := make(map[uuid.UUID]*voiceGroup)

	for userID, vs := range voiceStates {
		voiceUserSet[userID] = struct{}{}
		if _, exists := groupMap[vs.ChannelID]; !exists {
			name := vs.ChannelID.String()[:8] // fallback if channel not found
			pos := 0
			for _, chList := range channels {
				for _, ch := range chList {
					if ch.ID == vs.ChannelID {
						name = ch.Name
						pos = ch.Position
						break
					}
				}
			}
			groupMap[vs.ChannelID] = &voiceGroup{channelID: vs.ChannelID, position: pos, name: name}
		}
	}
	for _, m := range members {
		if vs, ok := voiceStates[m.User.ID]; ok {
			if grp, ok := groupMap[vs.ChannelID]; ok {
				grp.members = append(grp.members, m)
			}
		}
	}
	var voiceGroups []voiceGroup
	for _, g := range groupMap {
		voiceGroups = append(voiceGroups, *g)
	}
	sort.Slice(voiceGroups, func(i, j int) bool {
		if voiceGroups[i].position != voiceGroups[j].position {
			return voiceGroups[i].position < voiceGroups[j].position
		}
		return voiceGroups[i].name < voiceGroups[j].name
	})
	for i := range voiceGroups {
		sort.Slice(voiceGroups[i].members, func(a, b int) bool {
			return voiceGroups[i].members[a].User.Username < voiceGroups[i].members[b].User.Username
		})
		flatList = append(flatList, voiceGroups[i].members...)
	}

	// ── 2. Role sections (same insertion-sort as renderUserList) ──────────────
	type roleSect struct {
		role    *models.Role
		members []*MemberDisplay
	}
	roleSectionMap := make(map[uuid.UUID]*roleSect)
	var roleSectionOrder []uuid.UUID
	var regularMembers []*MemberDisplay

	for _, m := range members {
		if _, inVoice := voiceUserSet[m.User.ID]; inVoice {
			continue // already added in voice groups above
		}
		if m.HighestRole != nil {
			rs, exists := roleSectionMap[m.HighestRole.ID]
			if !exists {
				rs = &roleSect{role: m.HighestRole}
				roleSectionMap[m.HighestRole.ID] = rs
				roleSectionOrder = append(roleSectionOrder, m.HighestRole.ID)
			}
			rs.members = append(rs.members, m)
		} else {
			regularMembers = append(regularMembers, m)
		}
	}

	// Insertion sort matching renderUserList: DisplayOrder ASC, secondary role name ASC.
	for i := 1; i < len(roleSectionOrder); i++ {
		for j := i; j > 0; j-- {
			curr := roleSectionMap[roleSectionOrder[j]]
			prev := roleSectionMap[roleSectionOrder[j-1]]
			currOrder := curr.role.DisplayOrder
			prevOrder := prev.role.DisplayOrder
			shouldSwap := false
			if currOrder < prevOrder {
				shouldSwap = true
			} else if currOrder == prevOrder {
				shouldSwap = curr.role.Name < prev.role.Name
			}
			if shouldSwap {
				roleSectionOrder[j], roleSectionOrder[j-1] = roleSectionOrder[j-1], roleSectionOrder[j]
			} else {
				break
			}
		}
	}
	for _, roleID := range roleSectionOrder {
		rs := roleSectionMap[roleID]
		sort.Slice(rs.members, func(i, j int) bool {
			return rs.members[i].User.Username < rs.members[j].User.Username
		})
		flatList = append(flatList, rs.members...)
	}

	// ── 3. Regular members (no role, not in voice) ────────────────────────────
	sort.Slice(regularMembers, func(i, j int) bool {
		return regularMembers[i].User.Username < regularMembers[j].User.Username
	})
	flatList = append(flatList, regularMembers...)

	return flatList
}

// navigateMemberList navigates through the member list
func (a *App) navigateMemberList(delta int) {
	flatMembers := a.buildFlatMemberList()
	if len(flatMembers) == 0 {
		return
	}

	a.selectedMemberIndex += delta

	// Wrap around
	if a.selectedMemberIndex < 0 {
		a.selectedMemberIndex = len(flatMembers) - 1
	} else if a.selectedMemberIndex >= len(flatMembers) {
		a.selectedMemberIndex = 0
	}
}

// closeMemberContextMenu closes the member context menu
func (a *App) closeMemberContextMenu() {
	a.memberContextMenu = nil
}

// openMemberContextMenu opens the context menu for the selected member
func (a *App) openMemberContextMenu() tea.Cmd {
	activeConn := a.getActiveConnection()
	if activeConn == nil {
		return nil
	}

	flatMembers := a.buildFlatMemberList()
	if a.selectedMemberIndex >= len(flatMembers) {
		return nil
	}
	targetMember := flatMembers[a.selectedMemberIndex]

	// Build actions based on current user's permissions
	actions := []MemberAction{
		{Label: "Whisper", Key: "W", Handler: handleWhisperAction, RequiresPerm: 0},
	}

	// Check current user's permissions
	currentUser := activeConn.User
	if currentUser == nil {
		a.memberContextMenu = &MemberContextMenu{
			TargetMember:  targetMember,
			Actions:       actions,
			SelectedIndex: 0,
		}
		return contextMenuAnimTick()
	}

	// Check if user can moderate this member
	canModerate := a.canModerate(targetMember)

	// Add moderation actions if user has permission and can moderate this member
	if canModerate && a.hasPermission(currentUser, models.PermissionKickMembers) {
		muteAction := MemberAction{Label: "Mute", Key: "M", Handler: handleMuteAction, RequiresPerm: models.PermissionKickMembers}
		if targetMember.IsMuted {
			muteAction = MemberAction{Label: "Unmute", Key: "M", Handler: handleUnmuteAction, RequiresPerm: models.PermissionKickMembers}
		}
		actions = append(actions,
			muteAction,
			MemberAction{Label: "Kick", Key: "K", Handler: handleKickAction, RequiresPerm: models.PermissionKickMembers},
			MemberAction{Label: "Timeout", Key: "T", Handler: handleTimeoutAction, RequiresPerm: models.PermissionKickMembers},
		)
	}

	if canModerate && a.hasPermission(currentUser, models.PermissionBanMembers) {
		if targetMember.IsBanned {
			actions = append(actions,
				MemberAction{Label: "Unban", Key: "N", Handler: handleUnbanAction, RequiresPerm: models.PermissionBanMembers},
			)
		} else {
			actions = append(actions,
				MemberAction{Label: "Ban", Key: "B", Handler: handleBanAction, RequiresPerm: models.PermissionBanMembers},
			)
		}
	}

	if a.hasPermission(currentUser, models.PermissionManageRoles) {
		actions = append(actions,
			MemberAction{Label: "Assign Role", Key: "R", Handler: handleRoleAction, RequiresPerm: models.PermissionManageRoles},
			MemberAction{Label: "Remove Role", Key: "E", Handler: handleRemoveRoleAction, RequiresPerm: models.PermissionManageRoles},
		)
	}

	// Voice actions — only shown when target user is currently in a voice channel.
	var isTargetInVoice bool
	var isServerMuted bool
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		if vs, ok := a.activeConn.VoiceStates[targetMember.User.ID]; ok {
			isTargetInVoice = true
			isServerMuted = vs.IsServerMuted || vs.IsServerDeafened
		}
		a.activeConn.mu.RUnlock()
	}
	if isTargetInVoice {
		actions = append(actions,
			MemberAction{Label: "Adjust Volume", Key: "V", Handler: handleVolumeAdjustAction, RequiresPerm: 0},
		)
		if canModerate && a.hasPermission(currentUser, models.PermissionMuteMembers) {
			actions = append(actions,
				MemberAction{Label: "Move Voice", Key: "O", Handler: handleMoveVoiceAction, RequiresPerm: models.PermissionMuteMembers},
			)
			if isServerMuted {
				actions = append(actions,
					MemberAction{Label: "Voice Unmute", Key: "U", Handler: handleVoiceUnmuteAction, RequiresPerm: models.PermissionMuteMembers},
				)
			} else {
				actions = append(actions,
					MemberAction{Label: "Voice Mute", Key: "X", Handler: handleVoiceMuteAction, RequiresPerm: models.PermissionMuteMembers},
					MemberAction{Label: "Voice Deafen", Key: "D", Handler: handleVoiceDeafenAction, RequiresPerm: models.PermissionMuteMembers},
				)
			}
		}
	}

	a.memberContextMenu = &MemberContextMenu{
		TargetMember:  targetMember,
		Actions:       actions,
		SelectedIndex: 0,
		AnimFrame:     0,
	}

	return contextMenuAnimTick()
}

// executeMemberAction executes the selected action from the context menu.
// When in volume slider mode, Enter confirms and saves.
func (a *App) executeMemberAction() tea.Cmd {
	if a.memberContextMenu == nil {
		return nil
	}

	// Volume slider: Enter confirms and closes the menu.
	if a.memberContextMenu.VolumeSlider != nil {
		return a.confirmVolumeAdjust()
	}

	action := a.memberContextMenu.Actions[a.memberContextMenu.SelectedIndex]
	cmd := action.Handler(a, a.memberContextMenu.TargetMember)
	// Only close if the handler didn't enter a sub-mode (e.g. volume slider).
	if a.memberContextMenu != nil && a.memberContextMenu.VolumeSlider == nil {
		a.memberContextMenu = nil
	}
	return cmd
}

// confirmVolumeAdjust saves the current slider value and closes the menu.
func (a *App) confirmVolumeAdjust() tea.Cmd {
	a.applyVolumeAdjust()
	a.memberContextMenu = nil
	return nil
}

// applyVolumeAdjust applies the slider's current volume to the engine and config.
func (a *App) applyVolumeAdjust() {
	if a.memberContextMenu == nil || a.memberContextMenu.VolumeSlider == nil {
		return
	}
	userID := a.memberContextMenu.TargetMember.User.ID
	vol := a.memberContextMenu.VolumeSlider.Volume
	if a.voiceEngine != nil {
		a.voiceEngine.SetUserVolume(userID, vol)
	}
	if a.audioConfig.PerUserVolumes == nil {
		a.audioConfig.PerUserVolumes = make(map[string]float64)
	}
	a.audioConfig.PerUserVolumes[userID.String()] = vol
	a.saveAudioConfig()
}

// getActiveConnection returns the active server connection
func (a *App) getActiveConnection() *ServerConnection {
	return a.activeConn
}

// hasPermission checks if a user has a specific permission
func (a *App) hasPermission(user *models.User, perm models.Permission) bool {
	if user == nil || a.activeConn == nil {
		return false
	}

	// Find the user's ServerMember to get their roles
	var member *models.ServerMember
	for _, m := range a.activeConn.Members {
		if m.User.ID == user.ID {
			member = m.Member
			break
		}
	}

	if member == nil {
		return false
	}

	// Check all roles the member has
	for _, roleID := range member.RoleIDs {
		for _, roleList := range a.activeConn.Roles {
			for _, r := range roleList {
				if r.ID == roleID && r.HasPermission(perm) {
					return true
				}
			}
		}
	}

	return false
}

// hasPermissionForUser is identical to hasPermission but accepts any *models.User (not just the local user).
func (a *App) hasPermissionForUser(user *models.User, perm models.Permission) bool {
	return a.hasPermission(user, perm)
}

// canModerate checks if current user can moderate the target member
func (a *App) canModerate(targetMember *MemberDisplay) bool {
	if a.activeConn == nil || a.activeConn.User == nil {
		return false
	}

	currentUser := a.activeConn.User

	// Can't moderate yourself
	if currentUser.ID == targetMember.User.ID {
		return false
	}

	// Administrator bypasses role hierarchy — can moderate anyone below
	if a.hasPermission(currentUser, models.PermissionAdministrator) {
		// Admins cannot moderate other admins unless they themselves are also admin
		// (both are admin → neither can moderate the other unless one is higher)
		targetIsAdmin := a.hasPermissionForUser(targetMember.User, models.PermissionAdministrator)
		if !targetIsAdmin {
			return true
		}
	}

	// Get highest role positions
	currentHighest := a.getHighestRolePosition(currentUser)
	targetHighest := a.getHighestRolePosition(targetMember.User)

	// Can only moderate members with lower role position
	return currentHighest > targetHighest
}

// getHighestRolePosition returns the highest role position for a user
func (a *App) getHighestRolePosition(user *models.User) int {
	if a.activeConn == nil || user == nil {
		return 0
	}

	// Find the user's ServerMember to get their roles
	var member *models.ServerMember
	for _, m := range a.activeConn.Members {
		if m.User.ID == user.ID {
			member = m.Member
			break
		}
	}

	if member == nil {
		return 0
	}

	highest := 0
	for _, roleID := range member.RoleIDs {
		for _, roleList := range a.activeConn.Roles {
			for _, r := range roleList {
				if r.ID == roleID && r.Position > highest {
					highest = r.Position
				}
			}
		}
	}

	return highest
}

// Action handlers for member context menu

// handleWhisperAction pre-fills the input with a whisper command
func handleWhisperAction(a *App, member *MemberDisplay) tea.Cmd {
	// Pre-fill input with "/whisper @username "
	a.input.SetValue(fmt.Sprintf("/whisper @%s ", member.User.Username))
	a.focus = FocusInput
	a.input.Focus()
	a.statusMessage = "Type your whisper message"
	a.statusError = false
	return nil
}

// handleMuteAction mutes a member
func handleMuteAction(a *App, member *MemberDisplay) tea.Cmd {
	// Execute mute command via command handler
	ch := NewCommandHandler(a)
	msg, err := ch.Execute(&Command{
		Name: "mute",
		Args: []string{fmt.Sprintf("@%s", member.User.Username), "60"}, // 60 min default
	})

	if err != nil {
		a.statusMessage = err.Error()
		a.statusError = true
	} else {
		a.statusMessage = msg
		a.statusError = false
	}
	return nil
}

// handleUnmuteAction unmutes a text-muted member
func handleUnmuteAction(a *App, member *MemberDisplay) tea.Cmd {
	ch := NewCommandHandler(a)
	msg, err := ch.Execute(&Command{
		Name: "unmute",
		Args: []string{fmt.Sprintf("@%s", member.User.Username)},
	})
	if err != nil {
		a.statusMessage = err.Error()
		a.statusError = true
	} else {
		a.statusMessage = msg
		a.statusError = false
	}
	return nil
}

// handleUnbanAction lifts a ban from a member
func handleUnbanAction(a *App, member *MemberDisplay) tea.Cmd {
	ch := NewCommandHandler(a)
	msg, err := ch.Execute(&Command{
		Name: "unban",
		Args: []string{member.User.Username},
	})
	if err != nil {
		a.statusMessage = err.Error()
		a.statusError = true
	} else {
		a.statusMessage = msg
		a.statusError = false
	}
	return nil
}

// handleRemoveRoleAction pre-fills the input for role removal
func handleRemoveRoleAction(a *App, member *MemberDisplay) tea.Cmd {
	a.input.SetValue(fmt.Sprintf("/role remove @%s ", member.User.Username))
	a.focus = FocusInput
	a.input.Focus()
	a.statusMessage = "Enter role name to remove"
	a.statusError = false
	return nil
}

// handleMoveVoiceAction pre-fills the input to move a user to another voice channel
func handleMoveVoiceAction(a *App, member *MemberDisplay) tea.Cmd {
	a.input.SetValue(fmt.Sprintf("/move-voice @%s #", member.User.Username))
	a.focus = FocusInput
	a.input.Focus()
	a.statusMessage = "Enter destination voice channel name"
	a.statusError = false
	return nil
}

// handleKickAction kicks a member from the server
func handleKickAction(a *App, member *MemberDisplay) tea.Cmd {
	ch := NewCommandHandler(a)
	msg, err := ch.Execute(&Command{
		Name: "kick",
		Args: []string{fmt.Sprintf("@%s", member.User.Username)},
	})

	if err != nil {
		a.statusMessage = err.Error()
		a.statusError = true
	} else {
		a.statusMessage = msg
		a.statusError = false
	}
	return nil
}

// handleBanAction bans a member from the server
func handleBanAction(a *App, member *MemberDisplay) tea.Cmd {
	ch := NewCommandHandler(a)
	msg, err := ch.Execute(&Command{
		Name: "ban",
		Args: []string{fmt.Sprintf("@%s", member.User.Username)},
	})

	if err != nil {
		a.statusMessage = err.Error()
		a.statusError = true
	} else {
		a.statusMessage = msg
		a.statusError = false
	}
	return nil
}

// handleTimeoutAction pre-fills the input for timeout
func handleTimeoutAction(a *App, member *MemberDisplay) tea.Cmd {
	// Pre-fill input with "/timeout @username " so user can add duration
	a.input.SetValue(fmt.Sprintf("/timeout @%s ", member.User.Username))
	a.focus = FocusInput
	a.input.Focus()
	a.statusMessage = "Enter timeout duration in minutes"
	a.statusError = false
	return nil
}

// handleRoleAction pre-fills the input for role assignment
func handleRoleAction(a *App, member *MemberDisplay) tea.Cmd {
	// Pre-fill input for role assignment
	a.input.SetValue(fmt.Sprintf("/role assign @%s ", member.User.Username))
	a.focus = FocusInput
	a.input.Focus()
	a.statusMessage = "Enter role name to assign"
	a.statusError = false
	return nil
}

// ── Voice context-menu actions ─────────────────────────────────────────────

// handleVolumeAdjustAction opens the inline volume slider for the target user.
func handleVolumeAdjustAction(a *App, member *MemberDisplay) tea.Cmd {
	if a.memberContextMenu == nil {
		return nil
	}
	vol := 1.0
	if v, ok := a.audioConfig.PerUserVolumes[member.User.ID.String()]; ok {
		vol = v
	}
	a.memberContextMenu.VolumeSlider = &VolumeSliderState{Volume: vol}
	return nil
}

// handleVoiceMuteAction server-mutes the target user in voice.
func handleVoiceMuteAction(a *App, member *MemberDisplay) tea.Cmd {
	return sendVoiceServerMute(a, member.User.ID, true, false)
}

// handleVoiceDeafenAction server-deafens the target user in voice.
func handleVoiceDeafenAction(a *App, member *MemberDisplay) tea.Cmd {
	return sendVoiceServerMute(a, member.User.ID, true, true)
}

// handleVoiceUnmuteAction lifts server mute/deafen from the target user.
func handleVoiceUnmuteAction(a *App, member *MemberDisplay) tea.Cmd {
	return sendVoiceServerMute(a, member.User.ID, false, false)
}

func sendVoiceServerMute(a *App, userID uuid.UUID, muted, deafened bool) tea.Cmd {
	if a.activeConn == nil || a.activeConn.Connection == nil || a.currentServer == nil {
		return nil
	}
	conn := a.activeConn.Connection
	serverID := a.currentServer.ID
	return func() tea.Msg {
		payload := &protocol.VoiceServerMutePayload{
			ServerID: serverID,
			UserID:   userID,
			Muted:    muted,
			Deafened: deafened,
		}
		if wsMsg, err := protocol.NewMessage(protocol.OpVoiceServerMute, payload); err == nil {
			_ = conn.Send(wsMsg)
		}
		return nil
	}
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

// openHelpModal opens the help modal with the provided content
func (a *App) openHelpModal(content string) {
	a.helpModalState = &HelpModalState{
		Content:      content,
		ScrollOffset: 0,
	}
}

// closeHelpModal closes the help modal
func (a *App) closeHelpModal() {
	a.helpModalState = nil
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

	// If current node IS a category, collapse it
	if node.IsCategory {
		categoryID := node.Channel.ID
		a.collapsedCategories[categoryID] = true
		a.channelTree.RebuildFlatList(a.collapsedCategories)
		a.saveCollapsedState()
		return
	}

	// If current channel has a parent category, collapse it
	if node.Parent != nil && node.Parent.IsCategory {
		categoryID := node.Parent.Channel.ID
		a.collapsedCategories[categoryID] = true
		a.channelTree.RebuildFlatList(a.collapsedCategories)
		a.saveCollapsedState()

		// Move selection to the category since the child is now hidden
		a.currentChannel = node.Parent.Channel
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

	// If current node IS a category, expand it
	if node.IsCategory {
		categoryID := node.Channel.ID
		a.collapsedCategories[categoryID] = false
		a.channelTree.RebuildFlatList(a.collapsedCategories)
		a.saveCollapsedState()
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

	// Stop voice engine when leaving the current server.
	a.stopVoiceEngine()

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

	// Leave whatever plugin pane was active before switching.
	a.leavePluginPane()

	if a.currentChannel != nil && a.isRemotePaneChannel(a.currentChannel) {
		a.enterPluginPane(a.currentChannel)
		a.updateChatContent()
		return
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
			gapMins := float64(5)
			if a.uiConfig != nil && a.uiConfig.Display.GroupingGapMins > 0 {
				gapMins = float64(a.uiConfig.Display.GroupingGapMins)
			}
			if gap.Minutes() < gapMins {
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
		IsBotAuthor: author != nil && author.IsServiceAccount,
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

	// Track last rendered date for date separators
	var lastRenderedDate time.Time

	// lineOffsets/runningLines record each message's real starting line as
	// it's actually rendered (word-wrapping included, via the .Width() calls
	// below) — calculateMessageLinePosition reads this back instead of
	// re-deriving line counts itself, which is what let it silently drift
	// out of sync with word-wrapped content (found 2026-08-19: message
	// navigation's scroll-into-view undershot by however many extra visual
	// lines long messages like Alice's recipe replies actually wrapped to).
	lineOffsets := make([]int, len(messages))
	runningLines := 0

	for i, msg := range messages {
		msgStartLen := content.Len()

		// Check if this message is selected in navigation mode
		// Level 1: Highlight entire message with selection background
		// Level 2: Highlight with cursor indicator (editing mode)
		isSelected := a.messageNavMode && !a.inMessageEditMode && i == a.messageNavIndex
		isInLevel2 := a.messageNavMode && a.inMessageEditMode && i == a.messageNavIndex

		// Check if this is a system message (either explicit flag or legacy system author)
		isSystemMsg := msg.IsSystem || msg.AuthorName == "System"

		// In Level 2, prepare to insert cursor and selection into message content
		messageContentWithCursor := msg.Content
		if isInLevel2 && !isSystemMsg {
			messageContentWithCursor = a.insertCursorIntoMessage(msg.Content)
		}

		// Note: Reply quotes are now embedded inline in message content (press 'r' to reply)
		// No separate reply indicator rendering needed

		// Compute showHeader dynamically so GroupingGapMins changes take effect immediately
		showHeader := true
		if i > 0 && !isSystemMsg && !msg.IsBotAuthor {
			prev := messages[i-1]
			prevIsSystem := prev.IsSystem || prev.AuthorName == "System"
			if !prevIsSystem && prev.AuthorID == msg.AuthorID {
				gap := msg.CreatedAt.Sub(prev.CreatedAt)
				gapMins := float64(5)
				if a.uiConfig != nil && a.uiConfig.Display.GroupingGapMins > 0 {
					gapMins = float64(a.uiConfig.Display.GroupingGapMins)
				}
				if gap.Minutes() < gapMins {
					showHeader = false
				}
			}
		}

		// Date separator: render a ──── Day ──── divider between days when enabled
		if a.uiConfig != nil && a.uiConfig.Display.ShowDateSeps && !isSystemMsg {
			msgDate := msg.CreatedAt.Truncate(24 * time.Hour)
			if !lastRenderedDate.IsZero() && !msgDate.Equal(lastRenderedDate) {
				now := time.Now()
				today := now.Truncate(24 * time.Hour)
				yesterday := today.Add(-24 * time.Hour)
				var dateLabel string
				switch {
				case msgDate.Equal(today):
					dateLabel = "Today"
				case msgDate.Equal(yesterday):
					dateLabel = "Yesterday"
				default:
					dateLabel = msg.CreatedAt.Format("January 2, 2006")
				}
				barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
				barLen := (viewportWidth - len([]rune(dateLabel)) - 4) / 2
				if barLen < 4 {
					barLen = 4
				}
				bar := strings.Repeat("─", barLen)
				sepLine := barStyle.Render(bar + " " + dateLabel + " " + bar)
				content.WriteString(lipgloss.PlaceHorizontal(viewportWidth, lipgloss.Center, sepLine))
				content.WriteString("\n")
			}
			lastRenderedDate = msgDate
		}

		if showHeader && !isSystemMsg {
			// Render author line with full width background (non-system messages)
			authorStyle := a.styles.UsernameOther
			if msg.IsOwn {
				authorStyle = a.styles.UsernameSelf
			}
			timestamp := a.formatTimestamp(msg.CreatedAt)

			// Render author name — optionally preceded by a colored avatar circle
			var authorText string
			var plainAuthor string
			if a.uiConfig != nil && a.uiConfig.Display.ShowAvatars {
				initial := "?"
				if len([]rune(msg.AuthorName)) > 0 {
					initial = strings.ToUpper(string([]rune(msg.AuthorName)[:1]))
				}
				avatarColor := a.theme.Colors.Purple
				if msg.IsWhisper {
					avatarColor = a.theme.Colors.Orange
				} else if msg.IsSystem {
					avatarColor = a.theme.Colors.Comment
				}
				avatarStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(avatarColor)).
					Bold(true)
				circle := avatarStyle.Render("(" + initial + ")")
				authorText = circle + " " + authorStyle.Render(msg.AuthorName)
				plainAuthor = "(" + initial + ") " + msg.AuthorName
			} else {
				authorText = authorStyle.Render(msg.AuthorName)
				plainAuthor = msg.AuthorName
			}

			// Add [DM] indicator and recipient for whisper messages
			dmIndicator := ""
			if msg.IsWhisper {
				dmStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Orange)).
					Bold(true)
				recipientStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Purple))

				recipientText := ""
				if msg.RecipientName != "" {
					recipientText = " " + recipientStyle.Render(msg.RecipientName)
				}
				dmIndicator = " " + dmStyle.Render("[DM]") + recipientText
			}

			// Render timestamp
			timestampStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Semantic.ChatTimestamp))
			timestampText := timestampStyle.Render(timestamp)

			// Add (edited) indicator if message was edited
			editedIndicator := ""
			if msg.EditedAt != nil {
				editedStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Semantic.ChatTimestamp)).
					Italic(true)
				editedIndicator = " " + editedStyle.Render("(edited)")
			}

			header := fmt.Sprintf("%s%s  %s%s", authorText, dmIndicator, timestampText, editedIndicator)

			// Apply alignment and highlighting
			var headerLine string
			if isSelected || isInLevel2 {
				// Build header from PLAIN TEXT (no pre-applied colors)
				// This prevents ANSI code interference when applying background highlight
				plainHeader := plainAuthor
				if msg.IsWhisper {
					// Add plain text [DM] indicator
					plainHeader += " [DM]"
					if msg.RecipientName != "" {
						plainHeader += " " + msg.RecipientName
					}
				}
				plainHeader += "  " + timestamp
				if msg.EditedAt != nil {
					plainHeader += " (edited)"
				}

				// Apply highlight style with background - foreground will be theme color
				if msg.IsOwn {
					// Right-align with highlight and symmetric right padding
					headerStyle := lipgloss.NewStyle().
						Background(lipgloss.Color(a.theme.Colors.Selection)).
						Width(viewportWidth).
						Align(lipgloss.Right).
						PaddingRight(2)
					headerLine = headerStyle.Render(plainHeader)
				} else {
					// Left-align with highlight and left padding
					headerStyle := lipgloss.NewStyle().
						Background(lipgloss.Color(a.theme.Colors.Selection)).
						Width(viewportWidth).
						PaddingLeft(2)
					headerLine = headerStyle.Render(plainHeader)
				}
			} else {
				// No highlight
				if msg.IsOwn {
					// Right-align with symmetric right padding
					lineStyle := lipgloss.NewStyle().
						Width(viewportWidth).
						Align(lipgloss.Right).
						PaddingRight(2)
					headerLine = lineStyle.Render(header)
				} else {
					// Left-align with left padding to match right side visual spacing
					lineStyle := lipgloss.NewStyle().Width(viewportWidth).PaddingLeft(2)
					headerLine = lineStyle.Render(header)
				}
			}
			content.WriteString(headerLine)
			content.WriteString("\n")
		}

		// Render message content
		var contentLine string
		if msg.IsDeleted {
			deletedStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Italic(true)
			contentLine = deletedStyle.Render("[message deleted]")
		} else if isSystemMsg {
			// Render as a centered announcement with fixed-length bars: ─── message text ───
			// Simplified approach: fixed 5 bars on each side

			textStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Green)).
				Bold(true)
			barStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment))

			// Fixed bars: 5 on each side
			leftBar := "───── "
			rightBar := " ─────"

			// Truncate message if too long (leave room for bars + padding)
			msgContent := msg.Content
			maxMsgLen := viewportWidth - 20 // Reserve ~10 chars per side for bars and padding
			if maxMsgLen < 30 {
				maxMsgLen = 30
			}

			// Simple rune-based truncation
			msgRunes := []rune(msgContent)
			if len(msgRunes) > maxMsgLen {
				msgContent = string(msgRunes[:maxMsgLen-1]) + "…"
			}

			// Build the line: bars + message + bars (with space padding)
			line := barStyle.Render(leftBar) + textStyle.Render(msgContent) + barStyle.Render(rightBar)

			// Center the line in the viewport
			contentLine = lipgloss.PlaceHorizontal(viewportWidth, lipgloss.Center, line)
		} else if msg.IsWhisper {
			// Whisper: render with alignment based on ownership
			contentLine = a.renderMessageContent(messageContentWithCursor, viewportWidth, msg.IsOwn)
			// Apply whisper styling (orange/italic) to the rendered content
			whisperStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Orange)).
				Italic(true)
			contentLine = whisperStyle.Render(contentLine)
		} else {
			// Regular messages — highlight @mentions of the current user
			// Pass alignment based on message ownership
			contentLine = a.renderMessageContent(messageContentWithCursor, viewportWidth, msg.IsOwn)
		}

		// Apply width and highlighting
		if isSelected || isInLevel2 {
			// Apply highlight with alignment
			if msg.IsOwn {
				// Right-align with highlight and symmetric right padding
				contentStyle := lipgloss.NewStyle().
					Background(lipgloss.Color(a.theme.Colors.Selection)).
					Width(viewportWidth).
					Align(lipgloss.Right).
					PaddingRight(2)
				contentLine = contentStyle.Render(contentLine)
			} else {
				// Left-align with highlight and left padding
				contentStyle := lipgloss.NewStyle().
					Background(lipgloss.Color(a.theme.Colors.Selection)).
					Width(viewportWidth).
					PaddingLeft(2)
				contentLine = contentStyle.Render(contentLine)
			}
		} else {
			// No highlight - apply width for proper formatting
			if msg.IsOwn {
				contentStyle := lipgloss.NewStyle().
					Width(viewportWidth).
					Align(lipgloss.Right).
					PaddingRight(2)
				contentLine = contentStyle.Render(contentLine)
			} else {
				// Left-align with left padding to match right side visual spacing
				contentStyle := lipgloss.NewStyle().Width(viewportWidth).PaddingLeft(2)
				contentLine = contentStyle.Render(contentLine)
			}
		}
		content.WriteString(contentLine)

		// Peer-to-peer attachment manifest (if any). The server never has the
		// file's bytes -- this just tells the reader who to request it from.
		for _, att := range msg.Attachments {
			attLine := formatAttachmentLine(att, viewportWidth, msg.IsOwn, a.theme)
			content.WriteString(attLine)
			content.WriteString("\n")
		}

		// Spacing between messages based on density setting
		density := ""
		if a.uiConfig != nil {
			density = a.uiConfig.Display.MessageDensity
		}
		switch density {
		case "compact":
			content.WriteString("\n") // no blank line
		case "spacious":
			if showHeader {
				content.WriteString("\n\n\n") // extra gap before new sender groups
			} else {
				content.WriteString("\n\n")
			}
		default: // "normal" or unset
			content.WriteString("\n\n")
		}

		lineOffsets[i] = runningLines
		runningLines += strings.Count(content.String()[msgStartLen:], "\n")
	}

	a.messageLineOffsets = lineOffsets
	a.chatViewport.SetContent(content.String())
}

// renderMessageContent renders message text, highlighting @mentions of the current user.
// urlRegex matches http and https URLs.
var urlRegex = regexp.MustCompile(`https?://[^\s<>"{}|\\^` + "`" + `\[\]]+`)

// formatAttachmentLine renders a peer-to-peer attachment manifest line under a
// message: filename, size, and the command to fetch it. No inline preview or
// automatic download -- the receiver decides whether/when to pull the bytes.
func formatAttachmentLine(att models.Attachment, width int, rightAlign bool, theme *themes.Theme) string {
	label := fmt.Sprintf("[file] %s (%s) — /download %s", att.Filename, humanFileSize(att.Size), att.ID.String())
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.Colors.Comment)).
		Italic(true).
		Width(width)
	if rightAlign {
		style = style.Align(lipgloss.Right).PaddingRight(2)
	} else {
		style = style.PaddingLeft(2)
	}
	return style.Render(label)
}

// humanFileSize formats a byte count as a short human-readable string (KB/MB/GB).
func humanFileSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// osc8Link wraps text in an OSC 8 terminal hyperlink.
// Uses ESC\ (ST, String Terminator, 0x1B 0x5C) which Windows Terminal requires
// for reliable Ctrl+Click support. Hold Ctrl and left-click the link to open it.
func osc8Link(url, styledText string) string {
	const st = "\033\\"
	return "\033]8;;" + url + st + styledText + "\033]8;;" + st
}

func (a *App) renderMessageContent(text string, width int, rightAlign bool) string {
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
		// Check if this is a reply quote line (starts with "↩")
		if strings.HasPrefix(line, "↩ ") {
			// Apply gray italic styling to the entire quote line
			replyStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).  // Dim gray (same as input box)
				Italic(true)
			return replyStyle.Render(line)
		}

		var out strings.Builder
		seg := line
		for len(seg) > 0 {
			urlLoc := urlRegex.FindStringIndex(seg)
			mentionLoc := []int{-1, -1}
			everyoneLoc := []int{-1, -1}

			// Check for @everyone or @here
			if idx := strings.Index(strings.ToLower(seg), "@everyone"); idx != -1 {
				everyoneLoc = []int{idx, idx + len("@everyone")}
			} else if idx := strings.Index(strings.ToLower(seg), "@here"); idx != -1 {
				everyoneLoc = []int{idx, idx + len("@here")}
			}

			// Check for personal mention
			if alias != "" {
				token := "@" + alias
				if idx := strings.Index(strings.ToLower(seg), strings.ToLower(token)); idx != -1 {
					mentionLoc = []int{idx, idx + len(token)}
				}
			}

			useURL := urlLoc != nil
			useMention := mentionLoc[0] != -1
			useEveryone := everyoneLoc[0] != -1

			// Determine which match comes first
			if useURL && useMention {
				if mentionLoc[0] < urlLoc[0] {
					useURL = false
				} else {
					useMention = false
				}
			}
			if useURL && useEveryone {
				if everyoneLoc[0] < urlLoc[0] {
					useURL = false
				} else {
					useEveryone = false
				}
			}
			if useMention && useEveryone {
				if everyoneLoc[0] < mentionLoc[0] {
					useMention = false
				} else {
					useEveryone = false
				}
			}

			switch {
			case useEveryone:
				if everyoneLoc[0] > 0 {
					out.WriteString(msgStyle.Render(seg[:everyoneLoc[0]]))
				}
				out.WriteString(mentionStyle.Render(seg[everyoneLoc[0]:everyoneLoc[1]]))
				seg = seg[everyoneLoc[1]:]
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

	// Just render each line with styling (mentions, links) - no width/alignment here
	// Width and alignment will be handled by the caller
	for i, line := range lines {
		renderedLines[i] = renderLine(line)
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

// typingDisplayUser is one resolved entry in a.typingUsers — a display name
// plus whether it's a plugin's own service account, so the status line can
// render "X is thinking" for any Mynah-style persona instead of "X is
// typing", without hardcoding any particular plugin's identity.
type typingDisplayUser struct {
	Name  string
	IsBot bool
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
	users := make([]typingDisplayUser, 0, len(a.typingExpiry))
	for uid := range a.typingExpiry {
		var name string
		if n, ok := nameMap[uid]; ok {
			name = n
		} else if n, ok := a.typingUsernames[uid]; ok && n != "" {
			name = n
		} else {
			name = uid.String()[:8]
		}
		users = append(users, typingDisplayUser{Name: name, IsBot: a.typingIsBot[uid]})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })
	a.typingUsers = users
}

// clearTypingState resets all typing indicators for the current channel.
// Called on channel switch so stale indicators don't bleed across channels.
func (a *App) clearTypingState() {
	a.typingExpiry = nil
	a.typingUsernames = nil
	a.typingIsBot = nil
	a.typingUsers = nil
}

// updateViewportSize updates viewport and textarea dimensions based on window size.
// Must use the same column widths and height math as renderMainView / renderChatPanel
// so that chatViewport.Width is correct before updateChatContent() is called.
func (a *App) updateViewportSize() {
	// Must match renderMainView exactly — use animated widths, not hardcoded defaults
	availableWidth := a.width - 1
	serverIconsWidth := a.serverListAnimWidth
	if serverIconsWidth < 10 {
		serverIconsWidth = 10
	}
	channelsWidth := 26
	showMembers := a.uiConfig == nil || a.uiConfig.ShowMembersList
	membersWidth := 0
	if showMembers {
		membersWidth = a.membersAnimWidth
		if membersWidth < 10 {
			membersWidth = 10
		}
	}
	chatWidth := availableWidth - serverIconsWidth - channelsWidth - membersWidth
	if chatWidth < 60 && showMembers && membersWidth > 10 {
		membersWidth = 10
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

// isCursorOverChatViewport checks if mouse coordinates are within chat viewport bounds
func (a *App) isCursorOverChatViewport(x, y int) bool {
	// Match layout calculation from views.go renderMainView exactly
	availableWidth := a.width - 1
	serverIconsWidth := a.serverListAnimWidth
	if serverIconsWidth < 10 {
		serverIconsWidth = 10
	}
	channelsWidth := 26
	showMembers := a.uiConfig == nil || a.uiConfig.ShowMembersList
	membersWidth := 0
	if showMembers {
		membersWidth = a.membersAnimWidth
		if membersWidth < 10 {
			membersWidth = 10
		}
	}
	chatWidth := availableWidth - serverIconsWidth - channelsWidth - membersWidth
	if chatWidth < 60 && showMembers && membersWidth > 10 {
		membersWidth = 10
		chatWidth = availableWidth - serverIconsWidth - channelsWidth - membersWidth
	}

	// Chat panel X boundaries
	chatLeftX := serverIconsWidth + channelsWidth // 48
	chatRightX := chatLeftX + chatWidth

	// Y boundaries (entire panel height minus status bar)
	chatTopY := 0
	chatBottomY := a.height - 2

	return x >= chatLeftX && x < chatRightX && y >= chatTopY && y < chatBottomY
}

// calculateMessageLinePosition returns the starting line number (0-based) of
// a message in the last content updateChatContent() rendered. Reads
// a.messageLineOffsets — recorded as a side effect of that actual render —
// rather than re-deriving line counts here: a from-scratch reimplementation
// previously assumed no word-wrapping ("matches renderMessageContent()"),
// but updateChatContent() wraps content to viewportWidth via lipgloss's
// Width() before writing it, so that assumption undercounted every message
// with any wrapped line. Found 2026-08-19 via message navigation's
// scroll-into-view landing short for exactly that reason — long messages
// (e.g. Alice's recipe replies) wrap to several visual lines each, and the
// error compounded with every one of them before the target message.
func (a *App) calculateMessageLinePosition(messageIndex int) int {
	if messageIndex < 0 || messageIndex >= len(a.messageLineOffsets) {
		return -1
	}
	return a.messageLineOffsets[messageIndex]
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

	// Check if we're editing a message
	if a.editingMessageID != nil && a.editingChannelID != nil && a.activeConn != nil && a.currentClientServer != nil {
		messageID := *a.editingMessageID
		channelID := *a.editingChannelID
		serverID := a.currentClientServer.ID

		// Clear editing state
		a.editingMessageID = nil
		a.editingChannelID = nil

		// Send edit message request
		payload := &protocol.EditMessagePayload{
			MessageID: messageID,
			ChannelID: channelID,
			Content:   content,
		}

		data, err := json.Marshal(payload)
		if err != nil {
			return func() tea.Msg {
				return ErrorMsg{Error: fmt.Sprintf("Failed to marshal edit payload: %v", err)}
			}
		}

		msg := &protocol.Message{
			Op:   protocol.OpEditMessage,
			Data: data,
		}

		return func() tea.Msg {
			if err := a.connMgr.SendRaw(serverID, msg); err != nil {
				return ErrorMsg{Error: fmt.Sprintf("Failed to edit message: %v", err)}
			}
			return nil
		}
	}

	// Create and send message via connection manager
	if a.activeConn != nil && a.currentChannel != nil && a.currentClientServer != nil {
		serverID := a.currentClientServer.ID
		channelID := a.currentChannel.ID

		// If replying, embed quote inline in message content
		var replyToID *uuid.UUID
		if a.replyTarget != nil {
			// Prepend quoted message to content (inline, will be styled on display)
			quotedContent := fmt.Sprintf("↩ %s: %s\n%s", a.replyTarget.AuthorName, a.replyQuote, content)
			content = quotedContent
			replyToID = &a.replyTarget.ID
			// Clear reply state after embedding
			a.replyTarget = nil
			a.replyQuote = ""
		}

		return func() tea.Msg {
			if err := a.connMgr.SendMessage(serverID, channelID, content, replyToID); err != nil {
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

	a.pendingFileTransferCmd = nil
	result, err := a.commandHandler.Execute(cmd)
	pumpCmd := a.pendingFileTransferCmd
	a.pendingFileTransferCmd = nil
	if err != nil {
		a.statusMessage = fmt.Sprintf("Command failed: %v", err)
		a.statusError = true
		return pumpCmd
	}

	// Special handling for help command - display in modal overlay
	if cmd.Name == "help" {
		a.openHelpModal(result)
	} else {
		a.statusMessage = result
		a.statusError = false
	}
	return pumpCmd
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
		"attach",
		"ban",
		"create-channel",
		"create-category",
		"create-group",
		"create-role",
		"delete-channel",
		"delete-category",
		"delete-group",
		"download",
		"help",
		"kick",
		"links",
		"move-channel",
		"mute",
		"pin",
		"rename-channel",
		"role",
		"roles",
		"status",
		"theme",
		"timeout",
		"title",
		"unban",
		"unmute",
		"unpin",
		"whisper",
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

// exitNavModeMsg signals to exit message navigation mode after key handling
type exitNavModeMsg struct {
	setFocus       bool
	enableEditMode bool
}

// typingTickMsg drives the typing indicator animation and expiry pruning
type typingTickMsg time.Time

// typingTickDuration returns the tick interval for the current typing animation style.
func (a *App) typingTickDuration() time.Duration {
	if a.uiConfig != nil {
		switch a.uiConfig.Display.TypingAnimation {
		case "pulse", "meter":
			return 150 * time.Millisecond
		case "points", "ellipsis":
			return 300 * time.Millisecond
		case "hamburger":
			return 333 * time.Millisecond
		}
	}
	return 100 * time.Millisecond // "braille", "dot", "line", or default
}

// serverListAnimTickMsg drives the server list collapse/expand animation
type serverListAnimTickMsg struct{}

// membersAnimTickMsg drives the members panel collapse/expand animation
type membersAnimTickMsg struct{}

// contextMenuAnimTickMsg drives the member context menu pop-in animation
type contextMenuAnimTickMsg struct{}

const contextMenuMaxFrames = 8

func contextMenuAnimTick() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg {
		return contextMenuAnimTickMsg{}
	})
}

// panelAnimMaxFrames is shared by the Settings and Server Management slide animations.
// 60 frames × 16ms = 960ms total.
const panelAnimMaxFrames = 60

type settingsPanelAnimTickMsg struct{}
type srvMgmtPanelAnimTickMsg struct{}

func settingsPanelAnimTick() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg {
		return settingsPanelAnimTickMsg{}
	})
}

func srvMgmtPanelAnimTick() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg {
		return srvMgmtPanelAnimTickMsg{}
	})
}

// chatReflowMsg triggers a chat content reflow after panel animation completes.
// Fired as the final step of an animation so View() has one frame to update
// chatViewport.Width before updateChatContent() reads it.
type chatReflowMsg struct{}

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
	// Refresh chat viewport with new theme colors
	a.updateChatContent()
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
		sc.mu.Lock()
		hasToken := sc.Token != ""
		sc.RetryCount = 0 // reset so each fresh disconnect starts a new backoff cycle
		sc.mu.Unlock()
		if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
			if hasToken {
				a.statusMessage = fmt.Sprintf("Disconnected from %s — reconnecting...", a.currentClientServer.Name)
			} else {
				a.statusMessage = fmt.Sprintf("Disconnected from %s", a.currentClientServer.Name)
			}
			a.statusError = true
			a.stopVoiceEngine()
		}
		if hasToken {
			return a.scheduleReconnect(serverID)
		}

	case ErrorMsg:
		sc.SetState(StateError)
		sc.LastError = fmt.Errorf("%s", msg.Error)
		if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
			a.statusMessage = fmt.Sprintf("Error: %s", msg.Error)
			a.statusError = true
			a.stopVoiceEngine()
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

	// Cache plugin-provided channel kinds so the client can render/create
	// plugin channels generically without any plugin-specific code compiled in.
	if a.pluginChannelKinds == nil {
		a.pluginChannelKinds = make(map[string]protocol.PluginChannelKindInfo)
	}
	for _, kind := range payload.PluginChannelKinds {
		a.pluginChannelKinds[kind.PluginID+":"+kind.Kind] = kind
	}

	// Mark as ready and clear any reconnect backoff state
	sc.SetState(StateReady)
	sc.mu.Lock()
	sc.RetryCount = 0
	sc.mu.Unlock()

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

	// Check if this is an error message (OpDispatch with no Type field or empty Type)
	if msg.Type == "" {
		var errorPayload protocol.ErrorPayload
		if err := json.Unmarshal(msg.Data, &errorPayload); err == nil {
			// Successfully parsed error payload - display it to user
			a.statusMessage = fmt.Sprintf("Error: %s", errorPayload.Message)
			log.Printf("Server error [code %d]: %s", errorPayload.Code, errorPayload.Message)
			return nil
		}
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
		// Seed voice states received in SERVER_CREATE
		for _, vs := range payload.VoiceStates {
			sc.VoiceStates[vs.UserID] = vs
		}
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

		// FALLBACK: If this is the currently selected client server but conditions above failed,
		// ensure channels are loaded anyway (fixes race condition on first login)
		if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
			if a.channelTree == nil || len(a.channelTree.FlatList) == 0 {
				// Set activeConn and currentServer if not already set
				if a.activeConn == nil {
					a.activeConn = sc
				}
				if a.currentServer == nil && len(sc.Servers) > 0 {
					a.currentServer = sc.Servers[0]
				}
				// Force load channels
				a.loadChannelTree()
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
			IsBotAuthor: payload.Author != nil && payload.Author.IsServiceAccount,
		}

		// Add message to connection's message history
		sc.AddMessage(payload.Message.ChannelID, display)

		// Check for @mention (personal mention or @everyone/@here) — computed before
		// the isCurrentChannel guard so sounds can fire regardless of active channel.
		isCurrentChannel := a.currentChannel != nil && a.currentChannel.ID == payload.Message.ChannelID
		isMutedChannel := a.mutedChannels[payload.Message.ChannelID]
		isMutedServer := a.mutedServers[serverID]

		hasMention := false
		if sc.User != nil && containsMention(payload.Message.Content, sc.User.Username) {
			hasMention = true
		}
		if payload.Message.MentionEveryone {
			hasMention = true
		}

		// Unread tracking: only for messages not in the currently viewed channel
		if !isCurrentChannel && !isMutedChannel && !isMutedServer {
			if a.unreadCounts[serverID] == nil {
				a.unreadCounts[serverID] = make(map[uuid.UUID]int)
			}
			a.unreadCounts[serverID][payload.Message.ChannelID]++
			if hasMention {
				if a.mentionCounts[serverID] == nil {
					a.mentionCounts[serverID] = make(map[uuid.UUID]int)
				}
				a.mentionCounts[serverID][payload.Message.ChannelID]++
			}
		}

		// Sound and desktop notification: fire for all non-own, non-muted messages.
		// Sound plays even in the current channel; desktop popup only when away from it.
		if !display.IsOwn && !isMutedChannel && !isMutedServer {
			channelName := ""
			for _, channels := range sc.Channels {
				for _, ch := range channels {
					if ch.ID == payload.Message.ChannelID {
						channelName = ch.Name
						break
					}
				}
			}
			srvName := ""
			if sc.ServerInfo != nil {
				srvName = sc.ServerInfo.Name
			}
			a.triggerMessageNotification(payload.Author.Username, srvName, channelName, payload.Message.Content, hasMention)
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
			isWhisper := msgDisplay.IsWhisper

			// System messages have no author
			authorName := ""
			recipientName := ""
			authorColor := a.theme.Colors.Purple
			if isWhisper {
				authorColor = a.theme.Colors.Orange
			}
			isOwn := false
			if msgDisplay.Author != nil {
				authorName = msgDisplay.Author.Username
				isOwn = msgDisplay.Author.ID == currentUserID
			}
			if msgDisplay.Recipient != nil {
				recipientName = msgDisplay.Recipient.Username
			}

			display := &MessageDisplay{
				Message:       msgDisplay.Message,
				AuthorName:    authorName,
				RecipientName: recipientName,
				AuthorColor:   authorColor,
				IsOwn:         isOwn,
				ShowHeader:    !isSystem,
				IsWhisper:     isWhisper,
				IsSystem:      isSystem,
				IsBotAuthor:   msgDisplay.Author != nil && msgDisplay.Author.IsServiceAccount,
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

	case protocol.EventMessageUpdate:
		// Parse message update payload
		var payload protocol.MessageUpdatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse MESSAGE_UPDATE payload: %v", err)
			return nil
		}

		// Update message in connection's message history
		sc.mu.Lock()
		if messages, ok := sc.Messages[payload.ChannelID]; ok {
			for _, msg := range messages {
				if msg.ID == payload.ID {
					msg.Content = payload.Content
					msg.EditedAt = payload.EditedAt
					break
				}
			}
		}
		sc.mu.Unlock()

		// Update UI if this is for the active connection and current channel
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			if a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
				a.updateChatContent()
			}
		}

	case protocol.EventMessageDelete:
		// Parse message delete payload
		var payload protocol.MessageDeletePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse MESSAGE_DELETE payload: %v", err)
			return nil
		}

		// Mark the message as deleted in-place so the placeholder renders
		sc.mu.Lock()
		for _, m := range sc.Messages[payload.ChannelID] {
			if m.ID == payload.ID {
				m.IsDeleted = true
				break
			}
		}
		sc.mu.Unlock()

		// Update UI if this is for the active connection and current channel
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			if a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
				a.updateChatContent()
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
				m.User.StatusText = payload.StatusText
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

		// If Server Management view is open and showing Members, refresh the member list
		if a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.SelectedCategory == 2 { // Members category
				serverID := a.getActiveServerID()
				if serverID != uuid.Nil && sc != nil && serverID == payload.ServerID {
					a.loadMemberListForManagement()
				}
			}
		}

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
		found := false
		for i, m := range sc.Members {
			if m.User != nil && m.User.ID == payload.User.ID {
				sc.Members[i] = buildMemberDisplay(payload.Member, payload.User, roleMap)
				found = true
				break
			}
		}
		// If member not found (e.g., was previously banned/removed), add them back
		if !found && payload.User != nil {
			sc.Members = append(sc.Members, buildMemberDisplay(payload.Member, payload.User, roleMap))
		}
		sc.mu.Unlock()

		// If Server Management view is open and showing Members, refresh the member list
		if a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.SelectedCategory == 2 { // Members category
				serverID := a.getActiveServerID()
				if serverID != uuid.Nil && sc != nil && serverID == payload.ServerID {
					a.loadMemberListForManagement()
				}
			}
		}

	case protocol.EventTitleUpdate:
		var payload protocol.AssignTitlePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse TITLE_UPDATE payload: %v", err)
			return nil
		}

		// Update member title in local state
		sc.mu.Lock()
		for _, member := range sc.Members {
			if member.User != nil && member.User.ID == payload.UserID {
				if member.Member != nil {
					member.Member.CustomTitle = payload.Title
				}
				break
			}
		}
		sc.mu.Unlock()

		log.Printf("Title updated for user %s: %s", payload.UserID, payload.Title)

	case protocol.EventWhisperCreate:
		var whisperPayload protocol.WhisperCreatePayload
		if err := json.Unmarshal(msg.Data, &whisperPayload); err != nil {
			log.Printf("Failed to parse WHISPER_CREATE payload: %v", err)
			return nil
		}
		isOwn := sc.User != nil && whisperPayload.FromUser.ID == sc.User.ID
		recipientName := ""
		if whisperPayload.ToUser != nil {
			recipientName = whisperPayload.ToUser.Username
		}
		display := &MessageDisplay{
			Message: &models.Message{
				ID:        uuid.New(),
				ChannelID: whisperPayload.ChannelID,
				AuthorID:  whisperPayload.FromUser.ID,
				Content:   whisperPayload.Content,
				CreatedAt: whisperPayload.Timestamp,
			},
			AuthorName:    whisperPayload.FromUser.Username,
			RecipientName: recipientName,
			AuthorColor:   a.theme.Colors.Orange,
			IsOwn:         isOwn,
			ShowHeader:    true,
			IsWhisper:     true,
			IsBotAuthor:   whisperPayload.FromUser != nil && whisperPayload.FromUser.IsServiceAccount,
		}
		if a.activeConn != nil {
			a.activeConn.AddMessage(whisperPayload.ChannelID, display)

			// Unread tracking: whispers always count as unread/mention if not in current channel
			isCurrentChannel := a.currentChannel != nil && a.currentChannel.ID == whisperPayload.ChannelID
			if !isCurrentChannel && !a.mutedChannels[whisperPayload.ChannelID] && !isOwn {
				// Increment unread count
				if a.unreadCounts[serverID] == nil {
					a.unreadCounts[serverID] = make(map[uuid.UUID]int)
				}
				a.unreadCounts[serverID][whisperPayload.ChannelID]++

				// Whispers always count as mentions (they're direct messages to you)
				if a.mentionCounts[serverID] == nil {
					a.mentionCounts[serverID] = make(map[uuid.UUID]int)
				}
				a.mentionCounts[serverID][whisperPayload.ChannelID]++
			}

			// Only update chat content and scroll if the whisper is in the currently viewed channel
			if isCurrentChannel {
				a.updateChatContent()
				a.scrollToBottom()
			}
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
		// Display system message in the specified channel
		display := &MessageDisplay{
			Message: &models.Message{
				ID:        uuid.New(),
				ChannelID: payload.ChannelID,
				Content:   payload.Content,
				CreatedAt: payload.Timestamp,
			},
			IsSystem:   true,
			ShowHeader: false,
		}
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			sc.AddMessage(payload.ChannelID, display)
			if a.currentChannel != nil && a.currentChannel.ID == payload.ChannelID {
				a.updateChatContent()
				a.scrollToBottom()
			}
		}

	case protocol.EventPluginPaneFrame:
		var payload protocol.PluginPaneFramePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse PLUGIN_PANE_FRAME payload: %v", err)
			return nil
		}
		if a.activeConn != nil && a.activeConn.ServerID == serverID {
			a.applyPluginPaneFrame(payload)
		}

	case protocol.EventPluginEvent:
		var payload protocol.PluginEventPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse PLUGIN_EVENT payload: %v", err)
			return nil
		}
		if payload.Kind == "leave_pane" {
			var closePayload protocol.PluginPaneClosePayload
			if err := json.Unmarshal(payload.Payload, &closePayload); err != nil {
				return nil
			}
			// Only act if this is still the pane currently showing — the
			// plugin's signal could in principle arrive after the viewer
			// already navigated elsewhere on their own.
			if a.pluginPane != nil && a.pluginPane.ChannelID == closePayload.ChannelID {
				a.leavePluginPane()
				a.focus = FocusChannelList
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
		a.typingExpiry[typingPayload.UserID] = time.Now().Add(5 * time.Second)
		if a.typingUsernames == nil {
			a.typingUsernames = make(map[uuid.UUID]string)
		}
		a.typingUsernames[typingPayload.UserID] = typingPayload.Username
		if a.typingIsBot == nil {
			a.typingIsBot = make(map[uuid.UUID]bool)
		}
		a.typingIsBot[typingPayload.UserID] = typingPayload.IsBot
		a.rebuildTypingUsers()

	case protocol.EventTypingStop:
		var stopPayload protocol.TypingStopEventPayload
		if err := json.Unmarshal(msg.Data, &stopPayload); err != nil {
			return nil
		}
		if a.typingExpiry != nil {
			delete(a.typingExpiry, stopPayload.UserID)
			delete(a.typingUsernames, stopPayload.UserID)
			delete(a.typingIsBot, stopPayload.UserID)
			a.rebuildTypingUsers()
		}

	case protocol.EventChannelCreate:
		// Parse channel payload
		var payload protocol.ChannelCreatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse CHANNEL_CREATE payload: %v", err)
			return nil
		}
		// ChannelCreatePayload.PluginConfig is its own top-level JSON field
		// (not read off the embedded Channel), so payload.Channel.PluginConfig
		// is still nil after unmarshal here — copy it over so the cached
		// channel this client holds is immediately edit-ready without a
		// reconnect round trip.
		if payload.Channel != nil && len(payload.PluginConfig) > 0 {
			payload.Channel.PluginConfig = payload.PluginConfig
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

				// Refresh Server Management view if open
				if a.view == ViewServerManagement && a.serverManagementState != nil {
					if a.serverManagementState.SelectedCategory == 0 {
						a.loadChannelListForManagement(a.currentServer.ID)
					}
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

				// Refresh Server Management view if open
				if a.view == ViewServerManagement && a.serverManagementState != nil {
					if a.serverManagementState.SelectedCategory == 0 {
						a.loadChannelListForManagement(a.currentServer.ID)
					}
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

				// Refresh Server Management view if open
				if a.view == ViewServerManagement && a.serverManagementState != nil {
					if a.serverManagementState.SelectedCategory == 0 {
						a.loadChannelListForManagement(a.currentServer.ID)
					}
				}
			}
		}

	case protocol.EventRoleCreate:
		// Parse role create payload
		var roleData models.Role
		if err := json.Unmarshal(msg.Data, &roleData); err != nil {
			log.Printf("Failed to parse ROLE_CREATE payload: %v", err)
			return nil
		}

		sc.mu.Lock()
		// Add role to server's role list
		if sc.Roles == nil {
			sc.Roles = make(map[uuid.UUID][]*models.Role)
		}
		if _, ok := sc.Roles[roleData.ServerID]; !ok {
			sc.Roles[roleData.ServerID] = []*models.Role{}
		}

		// Check if role already exists (shouldn't happen, but be safe)
		roleExists := false
		for _, r := range sc.Roles[roleData.ServerID] {
			if r.ID == roleData.ID {
				roleExists = true
				break
			}
		}

		if !roleExists {
			// CRITICAL FIX: Create heap-allocated copy to avoid dangling pointer
			// (roleData is stack-allocated and becomes invalid when function returns)
			roleCopy := roleData
			sc.Roles[roleData.ServerID] = append(sc.Roles[roleData.ServerID], &roleCopy)
		}
		sc.mu.Unlock()

		// If Server Management view is open and showing Roles, refresh the role list
		if a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.SelectedCategory == 1 { // Roles category (index 1: Categories = ["Channels", "Roles", "Members"])
				// Reload roles for current server
				serverID := a.getActiveServerID()
				if serverID != uuid.Nil && sc != nil && serverID == roleData.ServerID {
					sc.mu.RLock()
					if roles, ok := sc.Roles[serverID]; ok {
						roleList := make([]*models.Role, len(roles))
						copy(roleList, roles)
						// Sort by position descending
						for i := 0; i < len(roleList)-1; i++ {
							for j := i + 1; j < len(roleList); j++ {
								if roleList[j].Position > roleList[i].Position {
									roleList[i], roleList[j] = roleList[j], roleList[i]
								}
							}
						}
						a.serverManagementState.RoleList = roleList
						a.statusMessage = fmt.Sprintf("Role '%s' created", roleData.Name)
					}
					sc.mu.RUnlock()
				}

				// Close role form if it was open (role creation successful)
				if a.serverManagementState.RoleFormOpen {
					a.serverManagementState.RoleFormOpen = false
					a.serverManagementState.RoleFormState = nil
					a.statusMessage = fmt.Sprintf("Created role: %s", roleData.Name)
				}
			}
		}


	case protocol.EventRoleUpdate:
		// Parse role update payload
		var roleData models.Role
		if err := json.Unmarshal(msg.Data, &roleData); err != nil {
			log.Printf("Failed to parse ROLE_UPDATE payload: %v", err)
			return nil
		}

		sc.mu.Lock()
		// Update role in server's role list
		if sc.Roles != nil {
			if roles, ok := sc.Roles[roleData.ServerID]; ok {
				for i, r := range roles {
					if r.ID == roleData.ID {
						// CRITICAL FIX: Create heap-allocated copy to avoid dangling pointer
						roleCopy := roleData
						sc.Roles[roleData.ServerID][i] = &roleCopy
						break
					}
				}
			}
		}
		sc.mu.Unlock()

		// If Server Management view is open and showing Roles, refresh the role list
		if a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.SelectedCategory == 1 { // Roles category (index 1: Categories = ["Channels", "Roles", "Members"])
				// Reload roles for current server
				serverID := a.getActiveServerID()
				if serverID != uuid.Nil && sc != nil && serverID == roleData.ServerID {
					sc.mu.RLock()
					if roles, ok := sc.Roles[serverID]; ok {
						roleList := make([]*models.Role, len(roles))
						copy(roleList, roles)
						// Sort by position descending
						for i := 0; i < len(roleList)-1; i++ {
							for j := i + 1; j < len(roleList); j++ {
								if roleList[j].Position > roleList[i].Position {
									roleList[i], roleList[j] = roleList[j], roleList[i]
								}
							}
						}
						a.serverManagementState.RoleList = roleList
						a.statusMessage = fmt.Sprintf("Role '%s' updated", roleData.Name)
					}
					sc.mu.RUnlock()
				}

				// Close permissions editor if it was open for this role
				if a.serverManagementState.PermissionsEditorOpen &&
					a.serverManagementState.PermissionsEditorRole != nil &&
					a.serverManagementState.PermissionsEditorRole.ID == roleData.ID {
					a.serverManagementState.PermissionsEditorOpen = false
					a.serverManagementState.PermissionsEditorRole = nil
					a.serverManagementState.PermModifiedBits = 0
					a.statusMessage = fmt.Sprintf("Updated permissions for role: %s", roleData.Name)
				}
			}
		}

	case protocol.EventRoleDelete:
		// Parse role delete payload
		var data struct {
			ServerID uuid.UUID `json:"server_id"`
			RoleID   uuid.UUID `json:"role_id"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			log.Printf("Failed to parse ROLE_DELETE payload: %v", err)
			return nil
		}

		sc.mu.Lock()
		// Remove role from server's role list
		if sc.Roles != nil {
			if roles, ok := sc.Roles[data.ServerID]; ok {
				newRoles := make([]*models.Role, 0, len(roles))
				for _, r := range roles {
					if r.ID != data.RoleID {
						newRoles = append(newRoles, r)
					}
				}
				sc.Roles[data.ServerID] = newRoles
			}
		}
		sc.mu.Unlock()

		// If Server Management view is open and showing Roles, refresh the role list
		if a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.SelectedCategory == 1 { // Roles category (index 1: Categories = ["Channels", "Roles", "Members"])
				// Reload roles for current server
				serverID := a.getActiveServerID()
				if serverID != uuid.Nil && sc != nil && serverID == data.ServerID {
					sc.mu.RLock()
					if roles, ok := sc.Roles[serverID]; ok {
						roleList := make([]*models.Role, len(roles))
						copy(roleList, roles)
						// Sort by position descending
						for i := 0; i < len(roleList)-1; i++ {
							for j := i + 1; j < len(roleList); j++ {
								if roleList[j].Position > roleList[i].Position {
									roleList[i], roleList[j] = roleList[j], roleList[i]
								}
							}
						}
						a.serverManagementState.RoleList = roleList
					a.statusMessage = "Role deleted successfully"

						// Adjust selection if needed
						if a.serverManagementState.SelectedRole >= len(roleList) {
							a.serverManagementState.SelectedRole = len(roleList) - 1
							if a.serverManagementState.SelectedRole < 0 {
								a.serverManagementState.SelectedRole = 0
							}
						}
					}
					sc.mu.RUnlock()
				}
			}
		}

	case protocol.EventRetentionPolicyUpdate:
		// Parse retention policy update payload
		var payload protocol.RetentionPolicyUpdatePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse RETENTION_POLICY_UPDATE payload: %v", err)
			return nil
		}

		// If Server Management view is open and showing Messages, update the policy
		if a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.Categories[a.serverManagementState.SelectedCategory] == "Messages" {
				// Server default has nil ChannelID
				if payload.Policy == nil || payload.Policy.ChannelID == nil {
					a.serverManagementState.RetentionPolicy = payload.Policy
				}
				// Always apply the full override list when provided
				if payload.ChannelOverrides != nil {
					a.serverManagementState.ChannelOverrides = payload.ChannelOverrides
					if a.serverManagementState.SelectedOverride >= len(payload.ChannelOverrides) {
						a.serverManagementState.SelectedOverride = len(payload.ChannelOverrides) - 1
					}
					if a.serverManagementState.SelectedOverride < 0 {
						a.serverManagementState.SelectedOverride = 0
					}
				}
				a.statusMessage = "Retention policy updated"
			}
		}

	case protocol.EventPluginConfigUpdate:
		// Parse installed plugin list + config payload (response to
		// OpPluginConfigGet, or a broadcast after OpPluginConfigSet)
		var payload protocol.PluginConfigListPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse PLUGIN_CONFIG_UPDATE payload: %v", err)
			return nil
		}

		if a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.Categories[a.serverManagementState.SelectedCategory] == "Plugins" {
				a.serverManagementState.PluginList = payload.Plugins
				if a.serverManagementState.SelectedPlugin >= len(payload.Plugins) {
					a.serverManagementState.SelectedPlugin = len(payload.Plugins) - 1
				}
				if a.serverManagementState.SelectedPlugin < 0 {
					a.serverManagementState.SelectedPlugin = 0
				}
			}
		}

	case protocol.EventMessagesPruned:
		// Parse messages pruned payload
		var payload protocol.MessagesPrunedPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse MESSAGES_PRUNED payload: %v", err)
			return nil
		}

		// If this was a manual prune and Server Management is open, show results
		if payload.TriggerType == "manual" && a.view == ViewServerManagement && a.serverManagementState != nil {
			if a.serverManagementState.Categories[a.serverManagementState.SelectedCategory] == "Messages" {
				a.serverManagementState.PruneResults = &payload
				a.serverManagementState.PruneResultsOpen = true
			}
		}

		// Update status message
		a.statusMessage = fmt.Sprintf("Pruned %d messages", payload.TotalDeleted)

	case protocol.EventVoiceStateUpdate:
		var payload protocol.VoiceStateEventPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse VOICE_STATE_UPDATE payload: %v", err)
			return nil
		}

		sc.mu.Lock()
		if payload.ChannelID == nil {
			// User left voice
			delete(sc.VoiceStates, payload.UserID)
			delete(sc.VoiceSpeaking, payload.UserID)
			// Clear our own voice channel tracking
			if sc.User != nil && payload.UserID == sc.User.ID {
				sc.CurrentVoiceChannelID = uuid.Nil
			}
		} else {
			vs := &models.VoiceState{
				UserID:           payload.UserID,
				ServerID:         payload.ServerID,
				ChannelID:        *payload.ChannelID,
				IsSelfMuted:      payload.IsSelfMuted,
				IsSelfDeafened:   payload.IsSelfDeafened,
				IsServerMuted:    payload.IsServerMuted,
				IsServerDeafened: payload.IsServerDeafened,
			}
			sc.VoiceStates[payload.UserID] = vs
			if sc.User != nil && payload.UserID == sc.User.ID {
				sc.CurrentVoiceChannelID = *payload.ChannelID
			}
		}
		sc.mu.Unlock()

		// Keep the running engine's peer list in sync with the channel roster.
		if a.voiceEngine != nil && sc.User != nil {
			if payload.UserID == sc.User.ID {
				// We ourselves left (or were moved off) this channel — stop the engine.
				if payload.ChannelID == nil {
					a.stopVoiceEngine()
				}
				// If we were moved to a new channel, startVoiceEngine fires via EventVoiceServerUpdate.
			} else {
				sc.mu.RLock()
				myChannelID := sc.CurrentVoiceChannelID
				sc.mu.RUnlock()
				if payload.ChannelID == nil || *payload.ChannelID != myChannelID {
					a.voiceEngine.RemovePeer(payload.UserID)
				} else {
					a.voiceEngine.AddPeer(payload.UserID)
				}
			}
		}

	case protocol.EventVoiceSpeaking:
		var payload protocol.VoiceSpeakingEventPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to parse VOICE_SPEAKING payload: %v", err)
			return nil
		}

		sc.mu.Lock()
		sc.VoiceSpeaking[payload.UserID] = payload.IsSpeaking
		// Mirror speaking state into VoiceState if present
		if vs, ok := sc.VoiceStates[payload.UserID]; ok {
			vs.IsSpeaking = payload.IsSpeaking
		}
		sc.mu.Unlock()

	case protocol.EventVoiceServerUpdate:
		var vsPayload protocol.VoiceServerUpdatePayload
		if err := json.Unmarshal(msg.Data, &vsPayload); err != nil {
			log.Printf("voice: VOICE_SERVER_UPDATE parse error: %v", err)
			return nil
		}
		return a.startVoiceEngine(sc, &vsPayload)

	case protocol.EventVoiceSignal:
		var sigPayload protocol.VoiceSignalRelayPayload
		if err := json.Unmarshal(msg.Data, &sigPayload); err != nil {
			log.Printf("voice: VOICE_SIGNAL parse error: %v", err)
			return nil
		}
		if a.voiceEngine != nil {
			a.voiceEngine.HandleSignal(sigPayload.SourceUserID, sigPayload.Type, sigPayload.SDP, sigPayload.Candidate)
		}

	case protocol.EventFileTransferSignal:
		var sigPayload protocol.FileTransferSignalRelayPayload
		if err := json.Unmarshal(msg.Data, &sigPayload); err != nil {
			log.Printf("filetransfer: FILE_TRANSFER_SIGNAL parse error: %v", err)
			return nil
		}
		if engine, cmd := a.ensureFileTransferEngine(); engine != nil {
			engine.HandleSignal(sigPayload.SourceUserID, sigPayload.AttachmentID, sigPayload.Type, sigPayload.SDP, sigPayload.Candidate)
			return cmd
		}
	}

	return nil
}

// ── Voice engine lifecycle ────────────────────────────────────────────────────

// startVoiceEngine creates and starts the VoiceEngine in response to a
// VOICE_SERVER_UPDATE event. It also seeds the peer list with any users
// already present in the voice channel.
//
// In stub builds (no -tags voice) the audio engine is unavailable. Presence
// still works — VoiceStates is populated via EventVoiceStateUpdate — so we
// skip the engine entirely and show a quiet status rather than a red error.
func (a *App) startVoiceEngine(sc *ServerConnection, payload *protocol.VoiceServerUpdatePayload) tea.Cmd {
	if !isVoiceSupported() {
		// Presence-only mode: user appears in the voice channel member list
		// but no audio engine runs (stub build has no CGO audio).
		a.statusMessage = "Joined voice channel (presence only — no mic/speaker in this build)"
		a.statusError = false
		return nil
	}

	a.stopVoiceEngine() // tear down any existing engine first

	if sc.User == nil {
		return nil
	}

	a.voiceSigOut   = make(chan VoiceSignalOut, 64)
	a.voiceEventOut = make(chan interface{}, 64)
	a.voiceQuit     = make(chan struct{})

	engine := NewVoiceEngine(a.audioConfig, sc.User.ID, a.voiceSigOut, a.voiceEventOut)
	a.voiceEngine = engine

	// Collect peers already in the channel before starting.
	sc.mu.RLock()
	var existingPeers []uuid.UUID
	for uid, vs := range sc.VoiceStates {
		if vs.ChannelID == payload.ChannelID && uid != sc.User.ID {
			existingPeers = append(existingPeers, uid)
		}
	}
	sc.mu.RUnlock()

	serverID  := payload.ServerID
	channelID := payload.ChannelID
	stunURLs  := payload.STUNUrls

	startCmd := func() (result tea.Msg) {
		// Recover from any CGO/malgo panics (e.g. nil audio backend on some
		// Windows 10 driver configurations) and return them as clean errors.
		defer func() {
			if r := recover(); r != nil {
				result = VoiceEngineErrorMsg{Err: fmt.Errorf("voice engine panic: %v", r)}
			}
		}()
		if err := engine.Start(serverID, channelID, stunURLs); err != nil {
			return VoiceEngineErrorMsg{Err: err}
		}
		for _, uid := range existingPeers {
			engine.AddPeer(uid)
		}
		return nil // VoiceEngineReadyMsg arrives via waitForVoiceEvent
	}

	return tea.Batch(startCmd, a.waitForVoiceEvent(), a.waitForVoiceSignal())
}

// stopVoiceEngine tears down the running VoiceEngine and unblocks any pending
// waitForVoiceEvent / waitForVoiceSignal commands via the quit channel.
func (a *App) stopVoiceEngine() {
	if a.voiceEngine == nil {
		return
	}
	engine := a.voiceEngine
	a.voiceEngine = nil
	a.voiceQuality = nil // stale data is useless after engine stops
	a.voiceLevels  = nil
	if a.voiceQuit != nil {
		close(a.voiceQuit)
		a.voiceQuit = nil
	}
	go engine.Stop() // non-blocking: malgo device teardown can take a moment
}

// waitForVoiceEvent returns a Cmd that blocks until the engine pushes an event
// (speaking state, peer connect/disconnect, ready, etc.) or the engine stops.
func (a *App) waitForVoiceEvent() tea.Cmd {
	ch   := a.voiceEventOut
	quit := a.voiceQuit
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case evt, ok := <-ch:
			if !ok {
				return nil
			}
			return evt
		case <-quit:
			return nil
		}
	}
}

// waitForVoiceSignal returns a Cmd that blocks until the engine queues an
// outbound WebRTC signal (offer / answer / ICE candidate) to forward to the
// server, or the engine stops.
func (a *App) waitForVoiceSignal() tea.Cmd {
	ch   := a.voiceSigOut
	quit := a.voiceQuit
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case sig, ok := <-ch:
			if !ok {
				return nil
			}
			return sig
		case <-quit:
			return nil
		}
	}
}

// ── File transfer engine lifecycle ───────────────────────────────────────────

// ensureFileTransferEngine lazily creates the (single, app-lifetime) file
// transfer engine on first use, loading any previously-shared attachments
// from ~/.concord/shared_files.json so this client keeps serving files it
// shared before a restart. Returns nil if there's no authenticated user yet.
func (a *App) ensureFileTransferEngine() (*FileTransferEngine, tea.Cmd) {
	if a.fileTransferEngine != nil {
		return a.fileTransferEngine, nil
	}
	if a.activeConn == nil || a.activeConn.User == nil {
		return nil, nil
	}

	sharedFiles := make(map[uuid.UUID]string)
	if a.configMgr != nil {
		if cfg, err := a.configMgr.LoadSharedFiles(); err == nil {
			for idStr, entry := range cfg.Files {
				if id, err := uuid.Parse(idStr); err == nil {
					sharedFiles[id] = entry.LocalPath
				}
			}
		}
	}

	downloadDir := filepath.Join(homeDirOrTemp(), "Downloads")

	a.fileTransferSigOut = make(chan FileTransferSignalOut, 32)
	a.fileTransferEventOut = make(chan interface{}, 32)

	engine := NewFileTransferEngine(a.activeConn.User.ID, downloadDir, sharedFiles, a.fileTransferSigOut, a.fileTransferEventOut)
	a.fileTransferEngine = engine

	cmd := tea.Batch(a.waitForFileTransferEvent(), a.waitForFileTransferSignal())
	a.pendingFileTransferCmd = cmd
	return engine, cmd
}

func homeDirOrTemp() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return os.TempDir()
}

// waitForFileTransferEvent blocks until the engine emits a progress/done/
// offline/rejected event.
func (a *App) waitForFileTransferEvent() tea.Cmd {
	ch := a.fileTransferEventOut
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return nil
		}
		return evt
	}
}

// waitForFileTransferSignal blocks until the engine queues an outbound
// signaling message to forward to the server.
func (a *App) waitForFileTransferSignal() tea.Cmd {
	ch := a.fileTransferSigOut
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		sig, ok := <-ch
		if !ok {
			return nil
		}
		return sig
	}
}
