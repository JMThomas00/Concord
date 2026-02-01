package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/themes"
)

// View represents different views/screens in the application
type View int

const (
	ViewLogin View = iota
	ViewMain
	ViewServerList
	ViewChannelList
	ViewSettings
)

// FocusArea represents which area of the UI has focus
type FocusArea int

const (
	FocusSidebar FocusArea = iota
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

	// Connection state
	conn      *Connection
	connected bool
	user      *models.User
	token     string

	// Server state
	servers       []*models.Server
	currentServer *models.Server
	serverIndex   int

	// Channel state
	channels       []*models.Channel
	currentChannel *models.Channel
	channelIndex   int

	// Messages
	messages     []*MessageDisplay
	messageIndex int

	// Members in current server
	members []*MemberDisplay

	// UI components
	input         textinput.Model
	chatViewport  viewport.Model
	sidebarScroll int

	// Login form
	loginEmail    textinput.Model
	loginPassword textinput.Model
	loginFocus    int
	loginError    string

	// Status message
	statusMessage string
	statusError   bool

	// Typing indicator
	typingUsers []string
}

// MessageDisplay wraps a message with display information
type MessageDisplay struct {
	*models.Message
	AuthorName  string
	AuthorColor string
	IsOwn       bool
	ShowHeader  bool // Show author/timestamp (false for consecutive messages)
}

// MemberDisplay wraps a member with display information
type MemberDisplay struct {
	User   *models.User
	Member *models.ServerMember
	Role   *models.Role
}

// NewApp creates a new application instance
func NewApp(serverAddr string) *App {
	// Initialize text input for chat
	input := textinput.New()
	input.Placeholder = "Type a message..."
	input.CharLimit = 2000
	input.Width = 50

	// Initialize login inputs
	loginEmail := textinput.New()
	loginEmail.Placeholder = "Email"
	loginEmail.Focus()

	loginPassword := textinput.New()
	loginPassword.Placeholder = "Password"
	loginPassword.EchoMode = textinput.EchoPassword

	// Load default theme
	theme := themes.GetDefaultTheme()
	styles := theme.BuildStyles()

	return &App{
		view:          ViewLogin,
		focus:         FocusInput,
		theme:         theme,
		styles:        styles,
		input:         input,
		loginEmail:    loginEmail,
		loginPassword: loginPassword,
		messages:      make([]*MessageDisplay, 0),
		members:       make([]*MemberDisplay, 0),
		servers:       make([]*models.Server, 0),
		channels:      make([]*models.Channel, 0),
	}
}

// Init implements tea.Model
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
	)
}

// Update implements tea.Model
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := a.handleKeyPress(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateViewportSize()

	case ConnectedMsg:
		a.connected = true
		a.statusMessage = "Connected to server"
		a.statusError = false

	case DisconnectedMsg:
		a.connected = false
		a.statusMessage = "Disconnected from server"
		a.statusError = true

	case LoginSuccessMsg:
		a.user = msg.User
		a.token = msg.Token
		a.servers = msg.Servers
		a.view = ViewMain
		a.focus = FocusInput
		a.input.Focus()
		if len(a.servers) > 0 {
			a.selectServer(0)
		}

	case LoginErrorMsg:
		a.loginError = msg.Error
		a.statusError = true

	case MessageReceivedMsg:
		a.addMessage(msg.Message, msg.Author)
		a.scrollToBottom()

	case TypingMsg:
		a.updateTypingIndicator(msg.Users)

	case ServerDataMsg:
		a.channels = msg.Channels
		if len(a.channels) > 0 {
			a.selectChannel(0)
		}

	case ErrorMsg:
		a.statusMessage = msg.Error
		a.statusError = true
	}

	// Update focused component
	switch a.view {
	case ViewLogin:
		cmd := a.updateLoginForm(msg)
		cmds = append(cmds, cmd)
	case ViewMain:
		if a.focus == FocusInput {
			var cmd tea.Cmd
			a.input, cmd = a.input.Update(msg)
			cmds = append(cmds, cmd)
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
	case ViewLogin:
		return a.renderLoginView()
	case ViewMain:
		return a.renderMainView()
	default:
		return "Unknown view"
	}
}

// handleKeyPress handles keyboard input
func (a *App) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "ctrl+q":
		return tea.Quit

	case "tab":
		a.cycleFocus()
		return nil

	case "shift+tab":
		a.cycleFocusReverse()
		return nil

	case "enter":
		if a.view == ViewLogin {
			return a.handleLoginSubmit()
		}
		if a.view == ViewMain && a.focus == FocusInput {
			return a.handleSendMessage()
		}

	case "esc":
		if a.view == ViewMain {
			a.focus = FocusSidebar
			a.input.Blur()
		}

	case "up", "k":
		if a.focus == FocusSidebar {
			a.navigateSidebar(-1)
		} else if a.focus == FocusChat {
			a.chatViewport.LineUp(1)
		}

	case "down", "j":
		if a.focus == FocusSidebar {
			a.navigateSidebar(1)
		} else if a.focus == FocusChat {
			a.chatViewport.LineDown(1)
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
		// Cycle through servers
		if len(a.servers) > 0 {
			a.serverIndex = (a.serverIndex + 1) % len(a.servers)
			a.selectServer(a.serverIndex)
		}
	}

	return nil
}

// cycleFocus moves focus to the next area
func (a *App) cycleFocus() {
	if a.view == ViewLogin {
		a.loginFocus = (a.loginFocus + 1) % 2
		if a.loginFocus == 0 {
			a.loginEmail.Focus()
			a.loginPassword.Blur()
		} else {
			a.loginEmail.Blur()
			a.loginPassword.Focus()
		}
		return
	}

	switch a.focus {
	case FocusSidebar:
		a.focus = FocusChat
	case FocusChat:
		a.focus = FocusInput
		a.input.Focus()
	case FocusInput:
		a.focus = FocusSidebar
		a.input.Blur()
	}
}

// cycleFocusReverse moves focus to the previous area
func (a *App) cycleFocusReverse() {
	switch a.focus {
	case FocusSidebar:
		a.focus = FocusInput
		a.input.Focus()
	case FocusChat:
		a.focus = FocusSidebar
	case FocusInput:
		a.focus = FocusChat
		a.input.Blur()
	}
}

// navigateSidebar navigates the sidebar list
func (a *App) navigateSidebar(delta int) {
	if len(a.channels) == 0 {
		return
	}
	a.channelIndex += delta
	if a.channelIndex < 0 {
		a.channelIndex = len(a.channels) - 1
	} else if a.channelIndex >= len(a.channels) {
		a.channelIndex = 0
	}
	a.selectChannel(a.channelIndex)
}

// selectServer selects a server and loads its data
func (a *App) selectServer(index int) {
	if index < 0 || index >= len(a.servers) {
		return
	}
	a.serverIndex = index
	a.currentServer = a.servers[index]
	// Request server data from connection
	// a.conn.RequestServerData(a.currentServer.ID)
}

// selectChannel selects a channel
func (a *App) selectChannel(index int) {
	if index < 0 || index >= len(a.channels) {
		return
	}
	a.channelIndex = index
	a.currentChannel = a.channels[index]
	a.messages = make([]*MessageDisplay, 0)
	// Request channel messages
	// a.conn.RequestMessages(a.currentChannel.ID)
}

// addMessage adds a message to the display
func (a *App) addMessage(msg *models.Message, author *models.User) {
	isOwn := a.user != nil && msg.AuthorID == a.user.ID

	// Check if we should show header (different author or time gap)
	showHeader := true
	if len(a.messages) > 0 {
		lastMsg := a.messages[len(a.messages)-1]
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

	a.messages = append(a.messages, display)
	a.updateChatContent()
}

// updateChatContent rebuilds the chat viewport content
func (a *App) updateChatContent() {
	var content strings.Builder

	for _, msg := range a.messages {
		if msg.ShowHeader {
			// Render author line
			authorStyle := a.styles.UsernameOther
			if msg.IsOwn {
				authorStyle = a.styles.UsernameSelf
			}
			timestamp := msg.CreatedAt.Format("15:04")
			header := fmt.Sprintf("%s  %s",
				authorStyle.Render(msg.AuthorName),
				a.styles.Timestamp.Render(timestamp))
			content.WriteString(header)
			content.WriteString("\n")
		}

		// Render message content
		content.WriteString(a.styles.MessageContent.Render(msg.Content))
		content.WriteString("\n")
	}

	a.chatViewport.SetContent(content.String())
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
	// Reserve space for sidebar (20%), input (3 lines), status (1 line)
	sidebarWidth := a.width / 5
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	chatWidth := a.width - sidebarWidth - 2 // -2 for borders
	chatHeight := a.height - 5              // -5 for input and status

	a.chatViewport = viewport.New(chatWidth, chatHeight)
	a.chatViewport.Style = a.styles.ChatContainer
	a.input.Width = chatWidth - 4
}

// handleSendMessage sends the current input as a message
func (a *App) handleSendMessage() tea.Cmd {
	content := strings.TrimSpace(a.input.Value())
	if content == "" {
		return nil
	}

	a.input.Reset()

	// Create and send message
	if a.conn != nil && a.currentChannel != nil {
		return func() tea.Msg {
			// a.conn.SendMessage(a.currentChannel.ID, content)
			return nil
		}
	}

	return nil
}

// --- Message types for tea.Cmd ---

// ConnectedMsg indicates successful connection
type ConnectedMsg struct{}

// DisconnectedMsg indicates disconnection
type DisconnectedMsg struct{}

// LoginSuccessMsg indicates successful login
type LoginSuccessMsg struct {
	User    *models.User
	Token   string
	Servers []*models.Server
}

// LoginErrorMsg indicates login failure
type LoginErrorMsg struct {
	Error string
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

// SetTheme sets the application theme
func (a *App) SetTheme(theme *themes.Theme) {
	a.theme = theme
	a.styles = theme.BuildStyles()
}

// SetToken sets the authentication token
func (a *App) SetToken(token string) {
	a.token = token
}
