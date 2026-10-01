package client

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	runewidth "github.com/mattn/go-runewidth"
	zone "github.com/lrstanley/bubblezone"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/themes"
)

// typingAnimNames is the ordered list of available typing indicator animation styles.
var typingAnimNames = []string{"braille", "dot", "line", "pulse", "points", "meter", "hamburger", "ellipsis"}

// typingAnimFrames returns the spinner frames for the named typing animation style.
func typingAnimFrames(name string) []string {
	switch name {
	case "dot":
		return []string{"⣾ ", "⣽ ", "⣻ ", "⢿ ", "⡿ ", "⣟ ", "⣯ ", "⣷ "}
	case "line":
		return []string{"|", "/", "-", "\\"}
	case "pulse":
		return []string{"█", "▓", "▒", "░"}
	case "points":
		return []string{"∙∙∙", "●∙∙", "∙●∙", "∙∙●"}
	case "meter":
		return []string{"▱▱▱", "▰▱▱", "▰▰▱", "▰▰▰", "▰▰▱", "▰▱▱", "▱▱▱"}
	case "hamburger":
		// Was ☱☲☴ (U+2631/2632/2634, Yijing trigrams) — Unicode classifies those
		// as East-Asian-Width "Wide" (2 terminal columns), but go-runewidth v0.0.16
		// measures them as 1 column. That 1-cell-per-frame undercount, repeated
		// every animation tick, drifts Bubbletea's cursor-up redraw math by a row
		// each time — the previous "X is typing..." line never gets fully
		// overwritten, so a fresh copy prints below it and stacks indefinitely
		// (confirmed live: reproduces only with this style, not braille/dot/etc,
		// and only while someone else is actively typing since your own typing
		// indicator is suppressed). ▬▭ (U+25AC/25AD, BLACK/WHITE RECTANGLE) are
		// East-Asian-Width "Neutral" — unambiguously 1 column everywhere — so
		// go-runewidth and every real terminal agree.
		return []string{"▬▬▬", "▭▬▬", "▬▭▬", "▬▬▭"}
	case "ellipsis":
		return []string{"   ", ".  ", ".. ", "..."}
	default: // "braille" or ""
		return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
}

type keyHint struct{ key, desc string }

// renderKeyHints draws a row of shortcut hints as key chips plus a readable
// description. Stays on one row unless that row is wider than maxWidth, then
// splits whole hints into the fewest evenly-filled centered rows that fit.
func (a *App) renderKeyHints(hints []keyHint, maxWidth int) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Padding(0, 1)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))

	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = keyStyle.Render(h.key) + " " + descStyle.Render(h.desc)
	}
	const gap = "   "
	row := strings.Join(parts, gap)
	if maxWidth <= 0 || lipgloss.Width(row) <= maxWidth {
		return row
	}

	var rows []string
	for n := 2; n <= len(parts); n++ {
		per := (len(parts) + n - 1) / n
		rows = rows[:0]
		fits := true
		for i := 0; i < len(parts); i += per {
			r := strings.Join(parts[i:min(i+per, len(parts))], gap)
			if lipgloss.Width(r) > maxWidth {
				fits = false
			}
			rows = append(rows, r)
		}
		if fits {
			break
		}
	}
	return lipgloss.JoinVertical(lipgloss.Center, rows...)
}

// maxBannerHeight is the tallest banner's line count. The login/register
// logo slot is always this tall so the form below it never moves when a
// different logo (1 to 21 lines) is shown.
var maxBannerHeight = func() int {
	h := 1
	for _, b := range banners {
		if n := strings.Count(trimBannerArt(b.Art), "\n") + 1; n > h {
			h = n
		}
	}
	return h
}()

const bannerFormGap = 2 // blank rows between the logo slot and the form

// formTextIndent is where login/register form text starts: the form box's
// left padding. The banner and the shortcut hints are indented the same
// amount so all three share one left edge.
const formTextIndent = 2

var formHintsIndent = lipgloss.NewStyle().PaddingLeft(formTextIndent)

// layoutBannerScreen places the logo bottom-aligned in a fixed-height slot
// with the form block below it. stableBelow is the form block's height
// without transient lines (an error message), so an error appearing doesn't
// move the form either; it only grows downward. The slot shrinks, clipping
// the logo's top rows, only when the terminal is too short for it.
const (
	bannerBoxMaxWidth = 100 // fits 318 of the 327 banners; wider ones are skipped
	bannerBoxMinWidth = 60  // below this, the grapes give way so more banners fit
	grapeLockupGap    = 4
)

// bannerDims holds each banner's trimmed width and height, for fit checks.
var bannerDims = func() [][2]int {
	dims := make([][2]int, len(banners))
	for i, b := range banners {
		art := trimBannerArt(b.Art)
		dims[i] = [2]int{lipgloss.Width(art), strings.Count(art, "\n") + 1}
	}
	return dims
}()

// logoLockup is the login/register logo area: the grapes (when they fit)
// then a fixed-size banner box. It depends only on the terminal size and
// the form below it, never on which banner is showing, so the grapes and
// the box's left edge stay put across shuffles.
type logoLockup struct {
	slot   int // rows
	boxW   int // banner box columns
	grapes bool
}

func (a *App) logoLockupFor(stableBelow int) logoLockup {
	slot := maxBannerHeight
	if room := a.height - bannerFormGap - stableBelow; slot > room {
		slot = room
	}
	if slot < 1 {
		slot = 1
	}
	avail := a.width - 4
	grapes := slot >= grapeLogos[grapeLogoSize].rows && avail >= grapeLogoSize+grapeLockupGap+bannerBoxMinWidth
	boxW := avail
	if grapes {
		boxW -= grapeLogoSize + grapeLockupGap
	}
	boxW = max(1, min(boxW, bannerBoxMaxWidth))
	return logoLockup{slot: slot, boxW: boxW, grapes: grapes}
}

func (g logoLockup) fits(i int) bool {
	return i >= 0 && i < len(bannerDims) && bannerDims[i][0] <= g.boxW && bannerDims[i][1] <= g.slot
}

// currentLockup is the lockup geometry for the screen being shown, if it
// has one (login or register).
func (a *App) currentLockup() (logoLockup, bool) {
	var stable int
	switch a.view {
	case ViewLogin:
		_, stable = a.loginFormBlock()
	case ViewRegister:
		_, stable = a.registerFormBlock()
	default:
		return logoLockup{}, false
	}
	return a.logoLockupFor(stable), true
}

func (a *App) layoutBannerScreen(banner, below string, stableBelow int) string {
	g := a.logoLockupFor(stableBelow)
	slot := g.slot

	// Banners anchor to the box's left edge and bottom, so each one starts
	// on the same column and baseline. One too big for the box (only
	// possible until a fitting banner is picked, e.g. mid-resize) is clipped.
	bannerLines := strings.Split(banner, "\n")
	if len(bannerLines) > slot {
		bannerLines = bannerLines[len(bannerLines)-slot:]
	}
	for i, l := range bannerLines {
		bannerLines[i] = ansi.Truncate(l, g.boxW, "")
	}
	box := lipgloss.Place(g.boxW, slot, lipgloss.Left, lipgloss.Bottom, strings.Join(bannerLines, "\n"))

	// Right column: the banner box, then the form and hints beneath it, all
	// sharing one left edge (formTextIndent in, where the form's text
	// starts). Its width depends only on the terminal size.
	box = lipgloss.NewStyle().PaddingLeft(formTextIndent).Render(box)
	colW := max(lipgloss.Width(box), lipgloss.Width(below))
	column := strings.Join([]string{
		lipgloss.PlaceHorizontal(colW, lipgloss.Left, box),
		strings.Repeat("\n", bannerFormGap-1),
		lipgloss.PlaceHorizontal(colW, lipgloss.Left, below),
	}, "\n")

	group := column
	// visibleTop is the first row of what normally shows: most banners are
	// short, so the top of the 21-row slot is usually empty.
	visibleTop := max(0, slot-10)
	visibleW := lipgloss.Width(below)
	if g.grapes {
		// The grapes sit left of the column with their bottom row level with
		// the form's last row (the password box on the login screen). That
		// row is 2 + the hint rows up from the bottom of the stable form
		// block (form bottom padding, then the hints), and error messages
		// appear below it, so the grapes stay put along with the form.
		grapeRows := grapeLogos[grapeLogoSize].rows
		formLastRow := slot + bannerFormGap + stableBelow - 2 - max(1, a.formHintRows)
		grapeTop := max(0, formLastRow-(grapeRows-1))
		grapes := strings.Repeat("\n", grapeTop) + a.renderGrapeLogo()
		group = lipgloss.JoinHorizontal(lipgloss.Top, grapes, strings.Repeat(" ", grapeLockupGap), column)
		visibleTop = grapeTop
		visibleW += grapeLogoSize + grapeLockupGap
	}

	// Center what normally shows (grapes top to hints; grapes left edge to
	// the end of the hints row) rather than the whole reserved area, so the
	// empty room kept for rare tall or wide banners doesn't push everything
	// down and to the left. Both depend only on the terminal size and the
	// stable form height, never on the banner, so nothing shifts on a
	// shuffle. Clamped so the full group always stays on screen.
	fullH := slot + bannerFormGap + stableBelow
	topPad := (a.height-(fullH-visibleTop))/2 - visibleTop
	if overflow := topPad + slot + bannerFormGap + lipgloss.Height(below) - a.height; overflow > 0 {
		topPad -= overflow
	}
	topPad = max(0, topPad)

	groupW := lipgloss.Width(group)
	leftPad := max(0, min((a.width-visibleW)/2, a.width-groupW))

	content := strings.Repeat("\n", topPad) + lipgloss.NewStyle().PaddingLeft(leftPad).Render(group)
	return lipgloss.Place(a.width, a.height, lipgloss.Left, lipgloss.Top, content, lipgloss.WithWhitespaceChars(" "))
}

// formErrorLines is how many rows an error message adds inside a login or
// register form (its wrapped text plus the blank line after it).
func formErrorLines(err string, formWidth int) int {
	if err == "" {
		return 0
	}
	return lipgloss.Height(lipgloss.NewStyle().Width(formWidth-4).Render("⚠ "+err)) + 1
}

// renderLoginView renders the login screen: the logo lockup above the form.
func (a *App) renderLoginView() string {
	below, stable := a.loginFormBlock()
	return a.layoutBannerScreen(a.renderBanner(), below, stable)
}

// loginFormBlock renders the login form plus shortcut hints, and the block's
// height without transient lines (an error message).
func (a *App) loginFormBlock() (string, int) {
	// Render login form with fixed width
	formWidth := 50
	var b strings.Builder

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true)

	// Form field styles
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Width(10)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(0, 1).
		Width(36)

	focusedInputStyle := inputStyle.
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple))

	var hints []keyHint

	if a.localIdentity != nil {
		// Local identity mode: show welcome + password only
		b.WriteString(subtitleStyle.Render(fmt.Sprintf("Welcome back, %s!", a.localIdentity.Alias)))
		b.WriteString("\n\n")

		// Email shown as read-only label (not editable)
		emailStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		b.WriteString(labelStyle.Render("Email:"))
		b.WriteString(emailStyle.Render(a.localIdentity.Email))
		b.WriteString("\n\n")

		// Password field
		b.WriteString(labelStyle.Render("Password:"))
		b.WriteString(focusedInputStyle.Render(a.loginPassword.View()))
		b.WriteString("\n\n")

		// Error message
		if a.loginError != "" {
			errorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Red)).
				Bold(true)
			b.WriteString(errorStyle.Render("⚠ " + a.loginError))
			b.WriteString("\n\n")
		}

		hints = []keyHint{{"Enter", "Unlock"}, {"Ctrl+P", "Not you?"}, {"Ctrl+F", "Forgot password"}, {"Ctrl+S", "Settings"}, {"Ctrl+T", "Themes"}, {"Ctrl+Q", "Quit"}}
	} else {
		// Standard login mode: email + password + register link
		b.WriteString(subtitleStyle.Render("Terminal Chat - Login to continue"))
		b.WriteString("\n\n")

		// Email field
		b.WriteString(labelStyle.Render("Email:"))
		if a.loginFocus == 0 {
			b.WriteString(focusedInputStyle.Render(a.loginEmail.View()))
		} else {
			b.WriteString(inputStyle.Render(a.loginEmail.View()))
		}
		b.WriteString("\n\n")

		// Password field
		b.WriteString(labelStyle.Render("Password:"))
		if a.loginFocus == 1 {
			b.WriteString(focusedInputStyle.Render(a.loginPassword.View()))
		} else {
			b.WriteString(inputStyle.Render(a.loginPassword.View()))
		}
		b.WriteString("\n\n")

		// Error message
		if a.loginError != "" {
			errorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Red)).
				Bold(true)
			b.WriteString(errorStyle.Render("⚠ " + a.loginError))
			b.WriteString("\n\n")
		}

		// Register link
		linkStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		focusedLinkStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Purple)).
			Bold(true).
			Underline(true)
		if a.registerLinkFocused {
			b.WriteString(focusedLinkStyle.Render("→ Register New Account"))
		} else {
			b.WriteString(linkStyle.Render("  Register New Account"))
		}
		b.WriteString("\n\n")

		hints = []keyHint{{"Tab", "Switch fields"}, {"Enter", "Login/Register"}, {"Ctrl+G", "Discover servers"}, {"Ctrl+S", "Settings"}, {"Ctrl+T", "Themes"}, {"Ctrl+Q", "Quit"}}
	}

	// Create the form box with padding and fixed width
	formStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Width(formWidth)

	loginForm := formStyle.Render(strings.TrimRight(b.String(), "\n"))

	// Hints sit below the form, not inside its fixed 50-column box. Six of
	// them (the profile unlock) read best as two aligned rows of three.
	hintBlock := a.renderKeyHints(hints, a.width-4)
	if len(hints) == 6 {
		grid := a.renderKeyHintGrid([][]keyHint{hints[:3], hints[3:]})
		if lipgloss.Width(grid) <= a.width-4 {
			hintBlock = grid
		}
	}
	a.formHintRows = lipgloss.Height(hintBlock)
	below := lipgloss.JoinVertical(lipgloss.Left, loginForm, formHintsIndent.Render(hintBlock))
	return below, lipgloss.Height(below) - formErrorLines(a.loginError, formWidth)
}

// renderRegisterView renders the registration screen
func (a *App) renderRegisterView() string {
	below, stable := a.registerFormBlock()
	return a.layoutBannerScreen(a.renderBanner(), below, stable)
}

// registerFormBlock is loginFormBlock's counterpart for the register screen.
func (a *App) registerFormBlock() (string, int) {

	// Render registration form with fixed width
	formWidth := 50
	var b strings.Builder

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true)

	b.WriteString(subtitleStyle.Render("Create a New Account"))
	b.WriteString("\n\n")

	// Form field styles
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Width(18)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(0, 1).
		Width(36)

	focusedInputStyle := inputStyle.
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple))

	// Email field
	b.WriteString(labelStyle.Render("Email:"))
	if a.loginFocus == 0 {
		b.WriteString(focusedInputStyle.Render(a.loginEmail.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginEmail.View()))
	}
	b.WriteString("\n\n")

	// Alias (Username) field
	b.WriteString(labelStyle.Render("Alias:"))
	if a.loginFocus == 1 {
		b.WriteString(focusedInputStyle.Render(a.loginUsername.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginUsername.View()))
	}
	b.WriteString("\n\n")

	// Password field
	b.WriteString(labelStyle.Render("Password:"))
	if a.loginFocus == 2 {
		b.WriteString(focusedInputStyle.Render(a.loginPassword.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginPassword.View()))
	}
	b.WriteString("\n\n")

	// Confirm Password field
	b.WriteString(labelStyle.Render("Confirm Password:"))
	if a.loginFocus == 3 {
		b.WriteString(focusedInputStyle.Render(a.loginPasswordConfirm.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginPasswordConfirm.View()))
	}
	b.WriteString("\n\n")

	// Error message
	if a.loginError != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Bold(true)
		b.WriteString(errorStyle.Render("⚠ " + a.loginError))
		b.WriteString("\n\n")
	}

	// Back link
	linkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))

	b.WriteString(linkStyle.Render("  Esc: Back to Login"))

	// Create the form box with padding and fixed width
	formStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Width(formWidth)

	registerForm := formStyle.Render(b.String())

	hints := []keyHint{{"Tab", "Switch fields"}, {"Enter", "Create account"}, {"Esc", "Back"}, {"Ctrl+Q", "Quit"}}
	hintBlock := a.renderKeyHints(hints, a.width-4)
	a.formHintRows = lipgloss.Height(hintBlock)
	below := lipgloss.JoinVertical(lipgloss.Left, registerForm, formHintsIndent.Render(hintBlock))
	return below, lipgloss.Height(below) - formErrorLines(a.loginError, formWidth)
}

// updateLoginForm handles login form input
func (a *App) updateLoginForm(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	if a.view == ViewRegister {
		// Register view has 4 fields: email, username, password, confirm password
		switch a.loginFocus {
		case 0:
			a.loginEmail, cmd = a.loginEmail.Update(msg)
		case 1:
			a.loginUsername, cmd = a.loginUsername.Update(msg)
		case 2:
			a.loginPassword, cmd = a.loginPassword.Update(msg)
		case 3:
			a.loginPasswordConfirm, cmd = a.loginPasswordConfirm.Update(msg)
		}
	} else {
		// Login view has 2 fields: email, password
		// (Note: registerLinkFocused is handled by keyboard navigation, not textinput)
		switch a.loginFocus {
		case 0:
			a.loginEmail, cmd = a.loginEmail.Update(msg)
		case 1:
			a.loginPassword, cmd = a.loginPassword.Update(msg)
		}
	}

	return cmd
}

// handleLoginSubmit attempts to log in
func (a *App) handleLoginSubmit() tea.Cmd {
	password := a.loginPassword.Value()

	// Local identity mode: verify password locally then auto-connect all servers
	if a.localIdentity != nil {
		if password == "" {
			a.loginError = "Please enter your password"
			return nil
		}
		if password != a.localIdentity.Password {
			a.loginError = "Incorrect password (Ctrl+F if you've forgotten it)"
			return nil
		}
		a.loginError = ""
		a.view = ViewMain
		a.focus = FocusServerIcons // Start on server icons (consistent with auto-login)
		for serverID, email := range a.pendingVerify {
			a.openCodeScreen(codeModeVerify, serverID, email)
			a.codeState.Back = ViewMain
			break
		}

		// Only auto-connect servers that aren't already connected
		// (servers auto-connect in background during Init, so they may already be ready)
		servers := a.configMgr.GetClientServers()
		cmds := make([]tea.Cmd, 0, len(servers))
		allConnected := true
		for _, server := range servers {
			conn := a.connMgr.GetConnection(server.ID)
			if conn == nil || conn.GetState() != StateReady {
				cmds = append(cmds, a.autoConnectServer(server.ID))
				allConnected = false
			}
		}

		if allConnected {
			a.statusMessage = "Ready"
		} else {
			a.statusMessage = "Connecting to servers..."
		}

		return tea.Batch(cmds...)
	}

	// Standard server login (no local identity configured)
	email := strings.TrimSpace(a.loginEmail.Value())
	if email == "" || password == "" {
		a.loginError = "Please enter email and password"
		return nil
	}

	if a.currentClientServer == nil {
		a.loginError = "No server selected"
		return nil
	}

	a.loginError = ""
	a.statusMessage = "Logging in..."

	// Attempt login via ConnectionManager
	return func() tea.Msg {
		serverID := a.currentClientServer.ID

		// Login via HTTP API
		user, token, err := a.connMgr.Login(serverID, email, password)
		if err != nil {
			return LoginErrorMsg{Error: err.Error()}
		}

		// Return success - WebSocket connection will be established asynchronously
		return LoginSuccessMsg{
			User:     user,
			Token:    token,
			ServerID: serverID,
			Servers:  []*models.Server{},
		}
	}
}

// handleRegisterSubmit attempts to register a new account
func (a *App) handleRegisterSubmit() tea.Cmd {
	email := strings.TrimSpace(a.loginEmail.Value())
	username := strings.TrimSpace(a.loginUsername.Value())
	password := a.loginPassword.Value()
	confirmPassword := a.loginPasswordConfirm.Value()

	// Validation
	if email == "" || username == "" || password == "" || confirmPassword == "" {
		a.loginError = "All fields are required"
		return nil
	}

	if len(username) < 2 || len(username) > 32 {
		a.loginError = "Alias must be 2-32 characters"
		return nil
	}

	if len(password) < 8 {
		a.loginError = "Password must be at least 8 characters"
		return nil
	}

	if password != confirmPassword {
		a.loginError = "Passwords do not match"
		return nil
	}

	if a.currentClientServer == nil {
		a.loginError = "No server selected"
		return nil
	}

	a.loginError = ""
	a.statusMessage = "Creating account..."

	// Attempt registration via ConnectionManager
	return func() tea.Msg {
		serverID := a.currentClientServer.ID

		// Register via HTTP API
		user, token, err := a.connMgr.Register(serverID, username, email, password)
		if err != nil {
			return LoginErrorMsg{Error: err.Error()}
		}

		// Connect WebSocket
		if err := a.connMgr.ConnectServer(serverID); err != nil {
			return LoginErrorMsg{Error: fmt.Sprintf("Failed to connect: %v", err)}
		}

		// Authenticate with token
		if err := a.connMgr.Identify(serverID, token); err != nil {
			return LoginErrorMsg{Error: fmt.Sprintf("Failed to authenticate: %v", err)}
		}

		// Set active connection
		a.activeConn = a.connMgr.GetConnection(serverID)

		return LoginSuccessMsg{
			User:    user,
			Token:   token,
			Servers: []*models.Server{},
		}
	}
}

// renderServerIconsCollapsed renders the server list in compact badge mode (width ≤ 10).
// Each server is shown as a (S) circle badge + connection indicator + unread dot.
func (a *App) renderServerIconsCollapsed(width, height int) string {
	var b strings.Builder
	servers := a.configMgr.GetClientServers()
	for i, server := range servers {
		var state ConnectionState
		if conn := a.connMgr.GetConnection(server.ID); conn != nil {
			state = conn.GetState()
		}
		indicator := "○"
		indicatorColor := a.theme.Colors.Comment
		if state == StateReady {
			indicator = "●"
			indicatorColor = a.theme.Colors.Green
		} else if state == StateConnecting || state == StateAuthenticating {
			indicator = "◐"
			indicatorColor = a.theme.Colors.Yellow
		} else if state == StateError {
			indicator = "○"
			indicatorColor = a.theme.Colors.Red
		}
		initial := "?"
		if len(server.Name) > 0 {
			initial = strings.ToUpper(string([]rune(server.Name)[0]))
		}
		badge := fmt.Sprintf("(%s)", initial)
		indicatorStr := lipgloss.NewStyle().Foreground(lipgloss.Color(indicatorColor)).Render(indicator)
		unreadDot := ""
		if counts := a.unreadCounts[server.ID]; len(counts) > 0 {
			hasMention := false
			for chID, n := range counts {
				if n > 0 && a.mentionCounts[server.ID] != nil && a.mentionCounts[server.ID][chID] > 0 {
					hasMention = true
					break
				}
			}
			dotColor := a.theme.Colors.Foreground
			if hasMention {
				dotColor = a.theme.Colors.Red
			}
			unreadDot = lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render("·")
		}
		var line string
		if i == a.serverIndex {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true).Render("▶"+badge) + indicatorStr + unreadDot
		} else {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render(" "+badge) + indicatorStr + unreadDot
		}
		b.WriteString(zone.Mark(fmt.Sprintf("server-row:%d", i), line) + "\n")
	}
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		// lipgloss.Height(N) sets the content height; Border() then adds 2
		// more lines (top+bottom), so the rendered block is N+2 lines total,
		// not N -- subtract 2 here so the box the caller gets back is
		// actually `height` lines tall, matching what renderMainView budgets
		// for every panel. Found 2026-09-06: this exact mismatch was making
		// the whole screen render 2 lines taller than the real terminal,
		// which bubbletea then silently truncated from the TOP to fit --
		// shifting every panel's visual position up by 2 rows relative to
		// what mouse.go's zone-based hit-testing computes, so clicking a
		// channel always resolved to the row 2 above the one actually
		// clicked. See "Concord - Mouse Support Plan" in the Obsidian vault.
		Width(width - 2).Height(height - 2).Padding(0, 0)
	if a.focus == FocusServerIcons {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}
	return a.labelPanelBorder(boxStyle.Render(b.String()), "SERVERS", a.focus == FocusServerIcons)
}

// renderServerIcons renders the server icons column (leftmost column)
func (a *App) renderServerIcons(width, height int) string {
	if width <= 12 {
		return a.renderServerIconsCollapsed(width, height)
	}

	var b strings.Builder

	// Available inner width (subtract border)
	innerWidth := width - 2

	// Get servers from disk (source of truth)
	servers := a.configMgr.GetClientServers()

	// Render server list
	for i, server := range servers {
		// Get connection state
		var state ConnectionState
		if conn := a.connMgr.GetConnection(server.ID); conn != nil {
			state = conn.GetState()
		}

		// Connection indicator
		indicator := "○"
		indicatorColor := a.theme.Colors.Comment
		if state == StateReady {
			indicator = "●"
			indicatorColor = a.theme.Colors.Green
		} else if state == StateConnecting || state == StateAuthenticating {
			indicator = "◐"
			indicatorColor = a.theme.Colors.Yellow
		} else if state == StateError {
			indicator = "○"
			indicatorColor = a.theme.Colors.Red
		}

		// Truncate server name to fit: innerWidth - 3 (prefix) - 2 (indicator+space)
		maxNameLen := innerWidth - 5
		if maxNameLen < 4 {
			maxNameLen = 4
		}
		name := server.Name
		if len([]rune(name)) > maxNameLen {
			name = string([]rune(name)[:maxNameLen-1]) + "…"
		}

		indicatorStr := lipgloss.NewStyle().Foreground(lipgloss.Color(indicatorColor)).Render(indicator)

		// Unread dot for this server (any channel has unreads?)
		serverUnreadDot := ""
		if counts := a.unreadCounts[server.ID]; len(counts) > 0 {
			hasMention := false
			for chID, n := range counts {
				if n > 0 {
					if a.mentionCounts[server.ID] != nil && a.mentionCounts[server.ID][chID] > 0 {
						hasMention = true
						break
					}
				}
			}
			dotColor := a.theme.Colors.Foreground
			if hasMention {
				dotColor = a.theme.Colors.Red
			}
			serverUnreadDot = lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render("·")
		}

		var line string
		if i == a.serverIndex {
			// Selected: bold purple with ▶ prefix
			nameStr := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(maxNameLen).
				Render(name)
			line = fmt.Sprintf("▶ %s %s%s", nameStr, indicatorStr, serverUnreadDot)
		} else {
			// Unselected: dimmed
			nameStr := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Width(maxNameLen).
				Render(name)
			line = fmt.Sprintf("  %s %s%s", nameStr, indicatorStr, serverUnreadDot)
		}

		b.WriteString(zone.Mark(fmt.Sprintf("server-row:%d", i), line))
		b.WriteString("\n")
	}

	// Removed "Manage Servers" button - now accessible via 's' key or Ctrl+S

	// Wrap in bordered box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Width(width - 2).
		// Border() adds 2 lines on top of Height(N) -- see the matching
		// comment in renderServerIconsCollapsed for the full explanation.
		Height(height - 2).
		Padding(0, 0)

	// Highlight border if focused
	if a.focus == FocusServerIcons {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return a.labelPanelBorder(boxStyle.Render(b.String()), "SERVERS", a.focus == FocusServerIcons)
}

// renderMainView renders the main chat interface with 4-column layout
func (a *App) renderMainView() string {
	// Use width-1 to account for potential terminal scrollbar or edge
	availableWidth := a.width - 1

	// Server list width animates between 22 (expanded) and 10 (collapsed)
	serverIconsWidth := a.serverListAnimWidth
	if serverIconsWidth < 10 {
		serverIconsWidth = 10
	}
	channelsWidth := 26 // Channels list column

	// Members width animates between 30 (expanded) and 10 (collapsed badge strip).
	// ShowMembersList=false is the legacy "completely hidden" (3-column) mode.
	showMembers := a.uiConfig == nil || a.uiConfig.ShowMembersList
	membersWidth := 0
	if showMembers {
		membersWidth = a.membersAnimWidth
		if membersWidth < 10 {
			membersWidth = 10
		}
	}

	chatWidth := availableWidth - serverIconsWidth - channelsWidth - membersWidth
	// All panels use Width(w-2) so outer rendered width = w. Total = availableWidth exactly.

	// Ensure chat has minimum width (only squash members if terminal is very narrow)
	if chatWidth < 60 && showMembers && membersWidth > 10 {
		membersWidth = 10
		chatWidth = availableWidth - serverIconsWidth - channelsWidth - membersWidth
	}

	// Height for panels (reserve 1 line for status bar, 1 line for top border visibility)
	panelHeight := a.height - 2

	// Render each panel with exact dimensions (borders included in width/height).
	// Each is wrapped in zone.Mark so a mouse click can be resolved to "which
	// panel" without re-deriving these widths a second time -- see mouse.go.
	serverIcons := zone.Mark("server-icons", a.renderServerIcons(serverIconsWidth, panelHeight))
	channels := zone.Mark("channel-list", a.renderChannelList(channelsWidth, panelHeight))
	chat := zone.Mark("chat-panel", a.renderChatPanel(chatWidth, panelHeight))

	// Combine panels horizontally (3 or 4 columns depending on members visibility)
	var mainContent string
	if showMembers {
		members := zone.Mark("user-list", a.renderUserList(membersWidth, panelHeight))
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, serverIcons, channels, chat, members)
	} else {
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, serverIcons, channels, chat)
	}

	// Add status bar
	statusBar := a.renderStatusBar()

	// Add top margin line for border visibility
	topMargin := ""

	return lipgloss.JoinVertical(lipgloss.Left, topMargin, mainContent, statusBar)
}

// renderSidebar renders the server/channel sidebar
// renderServerList renders the server list pane (top 40% of sidebar)
func (a *App) renderServerList(width, height int) string {
	var b strings.Builder

	// Header: "SERVERS"
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		PaddingLeft(1)
	b.WriteString(headerStyle.Render("SERVERS"))
	b.WriteString("\n")

	// Server list styles
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		PaddingLeft(1)

	unselectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		PaddingLeft(1)

	// Get protocol servers from active connection
	var servers []*models.Server
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		servers = a.activeConn.Servers
		a.activeConn.mu.RUnlock()
	}

	// Render server list
	for i, srv := range servers {
		name := srv.Name
		if len(name) > width-4 {
			name = name[:width-7] + "..."
		}

		if i == a.protocolServerIndex && a.focus == FocusServerIcons {
			b.WriteString(selectedStyle.Render(name))
		} else if a.currentServer != nil && srv.ID == a.currentServer.ID {
			b.WriteString(unselectedStyle.Foreground(
				lipgloss.Color(a.theme.Colors.Foreground)).Render(name))
		} else {
			b.WriteString(unselectedStyle.Render(name))
		}
		b.WriteString("\n")
	}

	// Placeholder if no servers
	if len(servers) == 0 {
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			PaddingLeft(1)
		b.WriteString(placeholderStyle.Render("No servers"))
		b.WriteString("\n")
	}

	// Apply border
	boxStyle := lipgloss.NewStyle().
		Width(width - 2).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	if a.focus == FocusServerIcons {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return boxStyle.Render(b.String())
}

// renderChannelList renders the channel list pane (bottom 60% of sidebar)
// renderCategoryRow renders a category row in the channel list
func (a *App) renderCategoryRow(node *ChannelTreeNode, width int) string {
	// Check if this category is currently selected
	isSelected := a.currentChannel != nil && a.currentChannel.ID == node.Channel.ID

	// Collapse indicator
	indicator := "▼"
	if a.collapsedCategories[node.Channel.ID] {
		indicator = "▶"
	}

	// Selection prefix
	selectionPrefix := " "
	if isSelected {
		selectionPrefix = ">"
	}

	// Category name (uppercase, bold, comment color)
	name := strings.ToUpper(node.Channel.Name)

	// Truncate if needed
	maxLen := width - 6 // Leave room for indicator and padding
	if len(name) > maxLen {
		name = name[:maxLen-3] + "..."
	}

	fullText := fmt.Sprintf("%s %s", indicator, name)

	// Apply style based on selection
	categoryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		PaddingLeft(1)

	selectedCategoryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		PaddingLeft(1)

	if isSelected {
		return selectedCategoryStyle.Render(selectionPrefix + fullText)
	}
	return categoryStyle.Render(selectionPrefix + fullText)
}

// countVoiceUsers returns how many users are currently in the given voice channel.
func (a *App) countVoiceUsers(channelID uuid.UUID) int {
	if a.activeConn == nil {
		return 0
	}
	a.activeConn.mu.RLock()
	defer a.activeConn.mu.RUnlock()
	count := 0
	for _, vs := range a.activeConn.VoiceStates {
		if vs.ChannelID == channelID {
			count++
		}
	}
	return count
}

// voiceQualityBar returns a compact 4-diamond quality indicator based on ICE RTT.
// latencyMs == -1 means no data yet (all hollow diamonds).
func voiceQualityBar(latencyMs int) string {
	switch {
	case latencyMs < 0:
		return "◇◇◇◇"
	case latencyMs < 50:
		return fmt.Sprintf("◆◆◆◆ %dms", latencyMs)
	case latencyMs < 100:
		return fmt.Sprintf("◆◆◆◇ %dms", latencyMs)
	case latencyMs < 200:
		return fmt.Sprintf("◆◆◇◇ %dms", latencyMs)
	default:
		return fmt.Sprintf("◆◇◇◇ %dms", latencyMs)
	}
}

// voiceIndicator returns a short string showing a member's voice state (speaking, muted, etc.).
// Returns "" when the user is not in any voice channel.
func (a *App) voiceIndicator(userID uuid.UUID) string {
	if a.activeConn == nil {
		return ""
	}
	a.activeConn.mu.RLock()
	vs, ok := a.activeConn.VoiceStates[userID]
	speaking := a.activeConn.VoiceSpeaking[userID]
	a.activeConn.mu.RUnlock()
	if !ok {
		return ""
	}
	switch {
	case vs.IsServerMuted || vs.IsServerDeafened:
		return "✕" // server-muted/deafened
	case vs.IsSelfDeafened:
		return "≈" // deafened
	case vs.IsSelfMuted:
		return "✕" // self-muted
	case speaking:
		return "▶" // speaking
	default:
		return "♪" // in voice, not muted
	}
}

// renderChannelRow renders a channel row in the channel list
func (a *App) renderChannelRow(node *ChannelTreeNode, width int) string {
	// Indent if has parent category
	indent := ""
	if node.Parent != nil && node.Parent.IsCategory {
		indent = "  " // 2-space indent
	}

	// Channel prefix
	prefix := a.channelIcon(node.Channel)

	// Add lock icon for locked channels
	lockIcon := ""
	if node.Channel.IsLocked {
		lockIcon = "⊗ "
	}

	// Build badge (right-aligned suffix)
	var badge string
	isSelected := a.currentChannel != nil && a.currentChannel.ID == node.Channel.ID
	if node.Channel.Type == models.ChannelTypeVoice {
		// Voice channels show active user count instead of unread dots
		if count := a.countVoiceUsers(node.Channel.ID); count > 0 {
			badge = fmt.Sprintf(" [%d]", count)
		}
	} else if !isSelected && a.currentClientServer != nil {
		serverID := a.currentClientServer.ID
		mentions := 0
		unreads := 0
		if a.mentionCounts[serverID] != nil {
			mentions = a.mentionCounts[serverID][node.Channel.ID]
		}
		if a.unreadCounts[serverID] != nil {
			unreads = a.unreadCounts[serverID][node.Channel.ID]
		}
		if mentions > 0 {
			badge = fmt.Sprintf(" @%d", mentions)
		} else if unreads > 0 {
			badge = " ●"
		}
	}

	// Channel name
	channelName := prefix + lockIcon + node.Channel.Name

	// Truncate if needed (leave room for badge)
	maxLen := width - len(indent) - 4 - len(badge)
	if maxLen < 4 {
		maxLen = 4
	}
	if len(channelName) > maxLen {
		channelName = channelName[:maxLen-3] + "..."
	}

	// Selection indicator
	selectionPrefix := " "
	if isSelected {
		selectionPrefix = ">"
	}

	fullText := indent + selectionPrefix + channelName + badge

	// Apply style
	channelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		PaddingLeft(1)

	selectedChannelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		PaddingLeft(1)

	// Check if this channel is currently selected
	if isSelected {
		if a.focus == FocusChannelList {
			return selectedChannelStyle.Render(fullText)
		}
		return channelStyle.Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Render(fullText)
	}

	// Unread channels render brighter
	if badge != "" {
		if a.currentClientServer != nil {
			serverID := a.currentClientServer.ID
			if a.mentionCounts[serverID] != nil && a.mentionCounts[serverID][node.Channel.ID] > 0 {
				return channelStyle.Foreground(lipgloss.Color(a.theme.Colors.Red)).Bold(true).Render(fullText)
			}
		}
		return channelStyle.Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Bold(true).Render(fullText)
	}

	return channelStyle.Render(fullText)
}

func (a *App) renderChannelList(width, height int) string {
	var b strings.Builder

	// Server name header
	serverNameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Bold(true).
		Width(width - 2).
		Padding(0, 1)

	serverName := "No Server"
	serverOffline := false
	if a.currentServer != nil {
		serverName = a.currentServer.Name
	} else if a.currentClientServer != nil {
		serverName = a.currentClientServer.Name
		serverOffline = true
	}
	b.WriteString(serverNameStyle.Render(serverName))
	b.WriteString("\n\n")

	// Render hierarchical channel tree. Each row is marked with a zone keyed
	// by the channel/category's own ID so a click can call selectChannelByID
	// directly -- see handleMainViewMouse in mouse.go. Both branches use the
	// same zone ID scheme; clicking a category currently selects/highlights
	// it exactly like arrow-navigating onto it (collapse/expand still needs
	// left/right or 'h', unchanged).
	if a.channelTree != nil && len(a.channelTree.FlatList) > 0 {
		// Only the tree scrolls; the server name above stays put. When it
		// overflows, rows are laid out one column narrower to leave room for
		// the scrollbar (so right-aligned badges aren't clipped).
		visible := height - 2 - 2 // borders, server name + blank line
		rowWidth := width
		if len(a.channelTree.FlatList) > visible {
			rowWidth--
		}
		rows := make([]string, 0, len(a.channelTree.FlatList))
		sel, selKey := -1, ""
		for i, node := range a.channelTree.FlatList {
			zoneID := "channel-row:" + node.Channel.ID.String()
			if node.IsCategory {
				rows = append(rows, zone.Mark(zoneID, a.renderCategoryRow(node, rowWidth)))
			} else {
				rows = append(rows, zone.Mark(zoneID, a.renderChannelRow(node, rowWidth)))
			}
			if a.currentChannel != nil && node.Channel.ID == a.currentChannel.ID {
				sel, selKey = i, node.Channel.ID.String()
			}
		}
		// No trailing newline: when the list fills the panel, one would make
		// the box a line taller than its height.
		b.WriteString(a.scrollPanel(&a.channelScroll, rows, width-2, visible, sel, sel+1, selKey))
	} else {
		// Placeholder if no channels
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			PaddingLeft(1)

		var placeholder string
		switch {
		case serverOffline:
			placeholder = "Server offline"
		case a.currentServer == nil:
			placeholder = "Select a server"
		default:
			placeholder = "No channels"
		}
		b.WriteString(placeholderStyle.Render(placeholder))
		b.WriteString("\n")
	}

	// Apply border
	boxStyle := lipgloss.NewStyle().
		Width(width - 2).
		// Border() adds 2 lines on top of Height(N) -- see the matching
		// comment in renderServerIconsCollapsed for the full explanation.
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	if a.focus == FocusChannelList {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return a.labelPanelBorder(boxStyle.Render(b.String()), "CHANNELS", a.focus == FocusChannelList)
}

// renderSidebar renders the sidebar with split panes (server list + channel list)
func (a *App) renderSidebar(width, height int) string {
	// Split: 40% server list, 60% channel list
	serverListHeight := int(float64(height) * 0.4)
	channelListHeight := height - serverListHeight

	// Ensure minimum heights
	if serverListHeight < 10 {
		serverListHeight = 10
		channelListHeight = height - 10
	}
	if channelListHeight < 10 {
		channelListHeight = 10
		serverListHeight = height - 10
	}

	// Render both panes
	serverList := a.renderServerList(width, serverListHeight)
	channelList := a.renderChannelList(width, channelListHeight)

	// Join vertically (server list on top, channel list on bottom)
	return lipgloss.JoinVertical(lipgloss.Left, serverList, channelList)
}

// typingVerbPhrase renders one typist's status clause — "X is thinking" for
// a plugin's own service account, "X is typing" for a real person.
func typingVerbPhrase(u typingDisplayUser) string {
	if u.IsBot {
		return u.Name + " is thinking"
	}
	return u.Name + " is typing"
}

// renderChatPanel renders the main chat area
func (a *App) renderChatPanel(width, height int) string {
	// Interior width (account for borders)
	interiorWidth := width - 2

	// The channel name sits in the chat box's own top border (see
	// embedBorderTitle), so the box starts on the same row as the other
	// panels' borders -- no separate header/spacer rows.
	inputHeight := 6 // 4 textarea lines + top and bottom border
	chatHeight := height - inputHeight
	chatHeight -= 1 // Reserve space for typing indicator (always present, blank when inactive)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Bold(true)
	topicStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	channelTitle := titleStyle.Render("Select a channel")
	if a.currentChannel != nil {
		channelTitle = titleStyle.Render("# " + a.currentChannel.Name)
		if a.pluginPane != nil && a.pluginPane.ChannelID == a.currentChannel.ID && a.pluginPane.Title != "" {
			channelTitle = titleStyle.Render(a.pluginPane.Title) // set by the plugin (pane_title)
		} else if a.currentChannel.Topic != "" {
			channelTitle += topicStyle.Render(" — " + a.currentChannel.Topic)
		}
	}

	// Pinned messages header — shown above the chat viewport when pins exist
	pinnedHeader := ""
	pinnedHeaderLines := 0
	if a.activeConn != nil && a.currentChannel != nil {
		a.activeConn.mu.RLock()
		pinnedMsgs := a.activeConn.PinnedMessages[a.currentChannel.ID]
		a.activeConn.mu.RUnlock()

		if len(pinnedMsgs) > 0 {
			// Header style (grey/comment color)
			pinHeaderStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Width(interiorWidth).
				PaddingLeft(1)
			// Message content style (yellow - more noticeable)
			pinContentStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Yellow)).
				Width(interiorWidth).
				PaddingLeft(1)

			var pinBuf strings.Builder
			// Cap display to 3 pinned messages to limit height consumption
			displayCount := len(pinnedMsgs)
			if displayCount > 3 {
				displayCount = 3
			}
			pinBuf.WriteString(pinHeaderStyle.Render(fmt.Sprintf("★ %d pinned message(s)  (/unpin N to remove)", len(pinnedMsgs))))
			pinBuf.WriteString("\n")
			for i := 0; i < displayCount; i++ {
				pm := pinnedMsgs[i]
				// Word-wrap the message content to fit the width
				// Account for "[i] " prefix (4 chars max) and padding
				maxWidth := interiorWidth - 6
				if maxWidth < 20 {
					maxWidth = 20
				}
				wrappedLines := a.splitMessageIntoLines(pm.Content, maxWidth)

				// Render each wrapped line with proper indentation
				for lineIdx, line := range wrappedLines {
					var prefix string
					if lineIdx == 0 {
						// First line: show message number
						prefix = fmt.Sprintf("[%d] ", i+1)
					} else {
						// Continuation lines: indent to align with first line content
						prefix = "    "
					}
					pinBuf.WriteString(pinContentStyle.Render(prefix + line))
					pinBuf.WriteString("\n")
				}
			}
			pinBuf.WriteString(pinHeaderStyle.Render(strings.Repeat("─", interiorWidth-2)))
			pinnedHeader = strings.TrimRight(pinBuf.String(), "\n")
			// Count actual lines by counting newlines in rendered output (+1 for final line)
			pinnedHeaderLines = strings.Count(pinnedHeader, "\n") + 1
		}
	}

	// Chat viewport - always show border for consistent sizing.
	// Border() adds 2 lines on top of Height(N), so pass chatHeight-2 here
	// to get a chatHeight-tall block overall -- see the matching comment in
	// renderServerIconsCollapsed. viewportHeight below already correctly
	// assumed a chatHeight-2 content area; only this box's own Height() call
	// was still off by 2.
	chatBorderColor := lipgloss.Color(a.theme.Colors.Selection)
	if a.focus == FocusChat {
		chatBorderColor = lipgloss.Color(a.theme.Colors.Purple)
	}
	chatStyle := lipgloss.NewStyle().
		Width(width - 2).
		Height(chatHeight - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(chatBorderColor)

	// Keep textarea width in sync with panel interior
	a.input.SetWidth(interiorWidth - 2)

	// Update viewport size to match interior, reducing height for pinned messages
	viewportHeight := chatHeight - 2 - pinnedHeaderLines
	if a.chatViewport.Width != interiorWidth || a.chatViewport.Height != viewportHeight {
		a.chatViewport.Width = interiorWidth
		a.chatViewport.Height = viewportHeight
	}

	// Marked precisely around just the viewport's own rendered content (not
	// the whole panel, which also includes the header/pinned-messages block
	// above it) so a click's position relative to this zone lines up exactly
	// with a row in a.messageLineOffsets once YOffset is added -- see
	// resolveMessageAtLine in mouse.go. Using the scanned zone bounds here
	// avoids hand-deriving the header/pinned-message height offset, which
	// varies with pin count and would otherwise be a fourth copy of the
	// layout math this sprint's zone-registry approach is meant to retire.
	chatContent := zone.Mark("chat-viewport-content", a.chatViewport.View())

	// Check if there are messages in active connection
	var hasMessages bool
	if a.activeConn != nil && a.currentChannel != nil {
		messages := a.activeConn.GetMessages(a.currentChannel.ID)
		hasMessages = len(messages) > 0
	}

	// Plugin channels replace the normal chat viewport with whatever frame
	// the owning plugin process last rendered — Concord just displays it.
	if a.pluginPane != nil && a.currentChannel != nil && a.pluginPane.ChannelID == a.currentChannel.ID {
		chatContent = a.renderPluginPaneFrame(interiorWidth, chatHeight-2-pinnedHeaderLines)
		hasMessages = true // Suppress the "No messages yet" empty state
	}

	if !hasMessages {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			Width(interiorWidth).
			Align(lipgloss.Center).
			MarginTop((chatHeight - 2) / 3)
		chatContent = emptyStyle.Render("No messages yet. Say hello!")
	}

	// Prepend pinned messages INSIDE the chat border (above the viewport content)
	if pinnedHeader != "" {
		chatContent = pinnedHeader + "\n" + chatContent
	}

	chat := embedBorderTitle(chatStyle.Render(chatContent), channelTitle, lipgloss.NewStyle().Foreground(chatBorderColor))

	// Typing indicator — always reserve space (render blank when inactive to prevent layout shift).
	// The animation style and tick rate are set via Settings > Display > Typing Animation.
	typing := ""
	if len(a.typingUsers) > 0 {
		animName := ""
		if a.uiConfig != nil {
			animName = a.uiConfig.Display.TypingAnimation
		}
		frames := typingAnimFrames(animName)
		frame := frames[a.typingFrame%len(frames)]
		var who string
		switch len(a.typingUsers) {
		case 1:
			who = typingVerbPhrase(a.typingUsers[0])
		case 2:
			u0, u1 := a.typingUsers[0], a.typingUsers[1]
			if u0.IsBot == u1.IsBot {
				verb := "are typing"
				if u0.IsBot {
					verb = "are thinking"
				}
				who = u0.Name + " and " + u1.Name + " " + verb
			} else {
				who = typingVerbPhrase(u0) + " and " + typingVerbPhrase(u1)
			}
		default:
			who = fmt.Sprintf("%d people are typing", len(a.typingUsers))
		}
		spinnerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Bold(true)
		textStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true)
		typingLine := "  " + spinnerStyle.Render(frame) + " " + textStyle.Render(who+"...")
		typing = lipgloss.NewStyle().Width(width).Render(typingLine)
	} else {
		// Render blank line to maintain spacing (prevents border shift when typing starts/stops)
		typing = lipgloss.NewStyle().Width(width).Height(1).Render("")
	}

	// Input area — full rounded border; textarea is 4 content lines so that
	// 1 top border + 4 content + 1 bottom border = 6 rows total (same slot, no gap).
	inputBorderColor := lipgloss.Color(a.theme.Colors.Comment)
	if a.focus == FocusInput {
		inputBorderColor = lipgloss.Color(a.theme.Colors.Purple)
	}
	inputStyle := lipgloss.NewStyle().
		Width(width - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(inputBorderColor)

	// Prepare input content with optional reply quote
	inputContent := a.injectMentionGhost(a.input.View())
	if a.pluginPane != nil && a.currentChannel != nil && a.pluginPane.ChannelID == a.currentChannel.ID {
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true)
		hint := "Tab into the pane to use " + a.channelTypeLabel(a.currentChannel) + " — Ctrl+] hands keys back to Concord"
		if a.paneFocused() {
			hint = "Keys are going to " + a.channelTypeLabel(a.currentChannel) + " — Ctrl+] to leave"
		}
		inputContent = hintStyle.Render(hint)
	}
	if a.replyTarget != nil {
		// Show reply quote above input (styled, dimmed, italic)
		replyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).  // Dim gray
			Italic(true)
		replyLine := fmt.Sprintf("↩ Replying to %s: %s", a.replyTarget.AuthorName, a.replyQuote)
		inputContent = replyStyle.Render(replyLine) + "\n" + inputContent
	}
	input := zone.Mark("chat-input", inputStyle.Render(inputContent))

	// Combine vertically — always include typing row (blank when inactive) to prevent border shift
	// Note: pinnedHeader is now rendered INSIDE the chat border, not as a separate element
	parts := []string{chat, typing, input}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// labelPanelBorder puts a panel's name (SERVERS, CHANNELS, MEMBERS) in its
// top border, styled like the chat panel's channel title. Panels too narrow
// to show the whole label (the collapsed columns) keep a plain border.
func (a *App) labelPanelBorder(box, label string, focused bool) string {
	borderColor := lipgloss.Color(a.theme.Colors.Selection)
	if focused {
		borderColor = lipgloss.Color(a.theme.Colors.Purple)
	}
	top := strings.SplitN(box, "\n", 2)[0]
	if lipgloss.Width(label)+5 > lipgloss.Width(top) {
		return box
	}
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Bold(true).Render(label)
	return embedBorderTitle(box, title, lipgloss.NewStyle().Foreground(borderColor))
}

// embedBorderTitle replaces a rounded-border box's top edge with
// "╭─ <title> ───╮", truncating the title with an ellipsis when it doesn't
// fit. The title may carry its own styling; the border runs are drawn in
// borderStyle so they match the rest of the box.
func embedBorderTitle(box, title string, borderStyle lipgloss.Style) string {
	lines := strings.SplitN(box, "\n", 2)
	width := lipgloss.Width(lines[0])
	const chrome = 5 // "╭─ " + " " + "╮"
	if width < chrome+2 {
		return box
	}
	if maxTitle := width - chrome - 1; lipgloss.Width(title) > maxTitle {
		title = ansi.Truncate(title, maxTitle, "…")
	}
	fill := width - chrome - lipgloss.Width(title)
	top := borderStyle.Render("╭─ ") + title + borderStyle.Render(" "+strings.Repeat("─", fill)+"╮")
	if len(lines) == 1 {
		return top
	}
	return top + "\n" + lines[1]
}

// injectMentionGhost post-processes the textarea View() output to show the
// top @mention suggestion as inline ghost text (dim italic) immediately after
// the cursor, replacing an equal number of trailing spaces so line width stays
// constant and borders are not pushed around.
func (a *App) injectMentionGhost(view string) string {
	if !a.showMentionPopup || len(a.mentionSuggestions) == 0 {
		return view
	}
	suggestion := a.mentionSuggestions[0]
	if len(suggestion) <= len(a.mentionQuery) {
		return view
	}
	ghost := suggestion[len(a.mentionQuery):]

	ghostRendered := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Render(ghost)

	lines := strings.Split(view, "\n")
	cursorLine := a.input.Line()
	if cursorLine >= len(lines) {
		return view
	}

	line := lines[cursorLine]

	// The textarea renders the cursor character with reverse-video (\x1b[7m).
	// Find that sequence and skip past it and its reset to locate the injection point.
	cursorSeq := "\x1b[7m"
	cursorStart := strings.Index(line, cursorSeq)
	if cursorStart < 0 {
		return view
	}
	// Skip \x1b[7m + one cursor rune + any trailing CSI reset sequences.
	i := cursorStart + len(cursorSeq)
	if i < len(line) {
		_, size := utf8.DecodeRuneInString(line[i:])
		i += size
	}
	for i < len(line) && line[i] == '\x1b' {
		j := i + 1
		if j < len(line) && line[j] == '[' {
			j++
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) {
				j++
			}
			i = j
		} else {
			break
		}
	}
	insertOffset := i

	// Remove len(ghost) trailing visible characters from the suffix so the
	// overall visual width of the line does not change.
	suffix := line[insertOffset:]
	suffix = ansiTrimTrailingVisChars(suffix, len(ghost))

	lines[cursorLine] = line[:insertOffset] + ghostRendered + suffix
	return strings.Join(lines, "\n")
}

// ansiVisColToByteOffset returns the byte offset in s at which visible column
// visCol (0-indexed) begins, skipping ANSI/CSI/OSC escape sequences entirely.
// Returns len(s) when visCol exceeds the visible length.
func ansiVisColToByteOffset(s string, visCol int) int {
	col := 0
	i := 0
	for i < len(s) {
		if col >= visCol {
			return i
		}
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				// CSI: \x1b[ params finalByte (0x40–0x7e)
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
			} else if j < len(s) && s[j] == ']' {
				// OSC: ends with BEL or ST (\x1b\\)
				j++
				for j < len(s) {
					if s[j] == '\a' {
						j++
						break
					}
					if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
			}
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		col++
	}
	return i
}

// ansiTrimTrailingVisChars removes up to n trailing visible characters from s,
// preserving ANSI escape sequences. Returns the truncated string.
func ansiTrimTrailingVisChars(s string, n int) string {
	if n <= 0 {
		return s
	}
	// Count total visible characters.
	visLen := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
			}
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		visLen++
	}
	target := visLen - n
	if target <= 0 {
		return ""
	}
	return s[:ansiVisColToByteOffset(s, target)]
}

// renderMemberAvatar renders a colored circle with the member's initial, e.g. "(A)"
func (a *App) renderMemberAvatar(name, colorHex string) string {
	initial := "?"
	if len([]rune(name)) > 0 {
		initial = strings.ToUpper(string([]rune(name)[:1]))
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorHex)).
		Bold(true).
		Render("(" + initial + ")")
}

// presenceDot returns the dot character and color for a member's status
func presenceDot(status models.UserStatus, theme *themes.Theme) (string, string) {
	switch status {
	case models.StatusOnline:
		return "●", theme.Colors.Green
	case models.StatusIdle:
		return "◑", theme.Colors.Yellow
	case models.StatusDND:
		return "●", theme.Colors.Red
	default:
		return "○", theme.Colors.Comment
	}
}

// renderUserListCollapsed renders the members panel as a compact badge strip (width ≤ 12).
// Each member is shown as an optional ♪ (in voice) + (U) avatar badge + presence dot.
func (a *App) renderUserListCollapsed(width, height int) string {
	var b strings.Builder

	flatMembers := a.buildFlatMemberList()

	// Snapshot voice states
	var voiceStates map[uuid.UUID]*models.VoiceState
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		voiceStates = a.activeConn.VoiceStates
		a.activeConn.mu.RUnlock()
	}

	var rows []string
	sel, selKey := -1, ""
	for i, m := range flatMembers {
		if m.User == nil {
			continue
		}
		dot, dotColor := presenceDot(m.User.Status, a.theme)
		dotStr := lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render(dot)

		inVoice := voiceStates != nil && voiceStates[m.User.ID] != nil
		voicePrefix := "  "
		if inVoice {
			voicePrefix = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Render("♪ ")
		}

		isSelected := a.focus == FocusUserList && i == a.selectedMemberIndex
		var line string
		if isSelected {
			initial := "?"
			if r := []rune(m.GetDisplayName()); len(r) > 0 {
				initial = strings.ToUpper(string(r[:1]))
			}
			avatar := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true).
				Render("(" + initial + ")")
			line = voicePrefix + "▶" + avatar + dotStr
		} else {
			line = voicePrefix + a.renderMemberAvatar(m.GetDisplayName(), m.AvatarColor) + dotStr
		}
		if isSelected {
			sel, selKey = len(rows), m.User.ID.String()
		}
		rows = append(rows, zone.Mark("member-row:"+m.User.ID.String(), line))
	}
	b.WriteString(a.scrollPanel(&a.memberScroll, rows, width-2, height-2, sel, sel+1, selKey))

	boxStyle := lipgloss.NewStyle().
		// Border() adds 2 lines on top of Height(N) -- see the matching
		// comment in renderServerIconsCollapsed for the full explanation.
		Width(width - 2).Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))
	if a.focus == FocusUserList {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}
	return a.labelPanelBorder(boxStyle.Render(b.String()), "MEMBERS", a.focus == FocusUserList)
}

func (a *App) renderUserList(width, height int) string {
	if width <= 12 {
		return a.renderUserListCollapsed(width, height)
	}

	visible := height - 2 // inside the borders
	lines, selStart, selEnd, selKey := a.memberListLines(width - 2)
	if len(lines) > visible {
		// Overflows: lay out one column narrower to leave room for the
		// scrollbar, so names and role labels aren't clipped by it.
		lines, selStart, selEnd, selKey = a.memberListLines(width - 3)
	}
	content := a.scrollPanel(&a.memberScroll, lines, width-2, visible, selStart, selEnd, selKey)

	userListStyle := lipgloss.NewStyle().
		Width(width - 2).
		// Border() adds 2 lines on top of Height(N) -- see the matching
		// comment in renderServerIconsCollapsed for the full explanation.
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))
	if a.focus == FocusUserList {
		userListStyle = userListStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}
	return a.labelPanelBorder(userListStyle.Render(content), "MEMBERS", a.focus == FocusUserList)
}

// memberListLines builds the members panel's content at a given interior
// width, as lines, plus the selected member's line range and a key that
// changes when the selection does (see scrollPanel).
func (a *App) memberListLines(innerWidth int) ([]string, int, int, string) {
	var b strings.Builder
	selStart, selEnd, selKey := -1, -1, ""

	// Collect members
	var members []*MemberDisplay
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		members = a.activeConn.Members
		a.activeConn.mu.RUnlock()
	}

	if len(members) == 0 {
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true)
		b.WriteString(placeholderStyle.Render("No members"))
		b.WriteString("\n")
	} else {
		// Selection highlighting walks the same order memberSections yields.
		var flatMembers []*MemberDisplay
		if a.focus == FocusUserList {
			flatMembers = a.buildFlatMemberList()
		}

		voiceGroups, onlineMembers, offlineMembers := a.memberSections()
		voiceUserSet := make(map[uuid.UUID]struct{})
		for _, g := range voiceGroups {
			for _, m := range g.members {
				voiceUserSet[m.User.ID] = struct{}{}
			}
		}

		sectionHeaderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Bold(true).
			Width(innerWidth)

		voiceHeaderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Bold(true).
			Width(innerWidth)

		// Local user ID for ↑ vs ↓ VU direction label.
		var localUID uuid.UUID
		if a.activeConn != nil && a.activeConn.User != nil {
			localUID = a.activeConn.User.ID
		}

		// Track position in flat list for selection highlighting.
		flatIndex := 0

		renderMember := func(m *MemberDisplay) {
			// Rendered into a local builder (not b directly) so the whole
			// multi-line block for this member can be wrapped in a single
			// zone.Mark before being appended -- a click anywhere within a
			// member's block (VU row, name row, title row, status row) then
			// resolves to this exact member. See mouse.go's
			// resolveClickedMemberRow.
			var mb strings.Builder
			prefix := "  "
			baseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
			isSelected := a.focus == FocusUserList && len(flatMembers) > 0 && flatIndex == a.selectedMemberIndex
			if isSelected {
				prefix = "> "
			}

			dot, dotColor := presenceDot(m.User.Status, a.theme)
			dotStr := lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render(dot)
			_, inVoice := voiceUserSet[m.User.ID]

			// Offline members are dimmed throughout.
			offline := !inVoice && !isOnlineStatus(m.User.Status)
			avatarColor := m.AvatarColor
			if offline {
				avatarColor = a.theme.Colors.Comment
				baseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
			}
			avatar := a.renderMemberAvatar(m.GetDisplayName(), avatarColor)

			hideVU := a.uiConfig != nil && a.uiConfig.Display.MembersHideVUMeter
			hideQuality := a.uiConfig != nil && a.uiConfig.Display.MembersHideQuality

			// ── Row 1 (voice members only): full-width level bar ──────────────
			if inVoice && !hideVU {
				var level float32
				if a.voiceLevels != nil {
					level = a.voiceLevels[m.User.ID]
				}
				scaled := level * 4
				if scaled > 1.0 {
					scaled = 1.0
				}

				dirStr := "↓"
				if m.User.ID == localUID {
					dirStr = "↑"
				}

				vuSegs := innerWidth - 5
				if vuSegs < 4 {
					vuSegs = 4
				}
				filled := int(scaled * float32(vuSegs))
				bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", vuSegs-filled) + "]"

				vuColor := a.theme.Colors.Cyan
				if scaled > 0.6 {
					vuColor = a.theme.Colors.Yellow
				}
				if scaled > 0.85 {
					vuColor = a.theme.Colors.Red
				}

				barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(vuColor))
				mb.WriteString("  " + dirStr + barStyle.Render(bar) + "\n")
			}

			// ── Row 2: avatar · dot · name · quality  ────────────────────────
			// Format: (G) • gh0st ◆◆◆◇
			qualStr := ""
			nameMaxLen := innerWidth - 9 // prefix(2)+avatar(3)+sp(1)+dot(1)+sp(1)+pad(1)

			if inVoice && !hideQuality && m.User.ID != localUID {
				latencyMs := -1
				if a.voiceQuality != nil {
					if ms, ok := a.voiceQuality[m.User.ID]; ok {
						latencyMs = ms
					}
				}
				qText := voiceQualityBar(latencyMs)
				var qColor string
				switch {
				case latencyMs < 0:
					qColor = a.theme.Colors.Comment
				case latencyMs < 50:
					qColor = a.theme.Colors.Green
				case latencyMs < 100:
					qColor = a.theme.Colors.Cyan
				case latencyMs < 200:
					qColor = a.theme.Colors.Yellow
				default:
					qColor = a.theme.Colors.Red
				}
				qualStr = " " + lipgloss.NewStyle().Foreground(lipgloss.Color(qColor)).Render(qText)
				nameMaxLen -= len([]rune(qText)) + 1
			}
			// Role label after the name ("ash  admin"), in the role's own
			// color, now that members are grouped by presence instead of role.
			// Dropped when it would squeeze the name below 6 characters.
			roleLabel := ""
			if r := m.HighestRole; r != nil {
				roleName := r.Name
				if len([]rune(roleName)) > 10 {
					roleName = string([]rune(roleName)[:9]) + "…"
				}
				if nameMaxLen-len([]rune(roleName))-2 >= 6 {
					roleColor := a.theme.Colors.Comment
					if r.Color != 0 && !offline {
						roleColor = r.GetColorHex()
					}
					roleLabel = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(roleColor)).Render(roleName)
					nameMaxLen -= len([]rune(roleName)) + 2
				}
			}
			if nameMaxLen < 4 {
				nameMaxLen = 4
			}

			name := m.GetDisplayName()
			if len([]rune(name)) > nameMaxLen {
				name = string([]rune(name)[:nameMaxLen-1]) + "…"
			}
			nameStr := baseStyle.Render(name)

			mb.WriteString(prefix + avatar + " " + dotStr + " " + nameStr + roleLabel + qualStr + "\n")

			// ── Rows 3 & 4 (optional): title, status ──────────────────────────
			if m.Member != nil && m.Member.CustomTitle != "" {
				titleColor := a.theme.Colors.Yellow
				if offline {
					titleColor = a.theme.Colors.Comment
				}
				titleStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(titleColor)).
					Bold(true)
				titleText := m.Member.CustomTitle
				titleMaxLen := innerWidth - 10
				if titleMaxLen < 10 {
					titleMaxLen = 10
				}
				if len([]rune(titleText)) > titleMaxLen {
					runes := []rune(titleText)
					titleText = string(runes[:titleMaxLen-1]) + "…"
				}
				mb.WriteString("    " + titleStyle.Render(titleText) + "\n")
			}

			if m.User.StatusText != "" {
				statusStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Comment)).
					Italic(true)
				statusText := m.User.StatusText
				statusMaxLen := innerWidth - 10
				if statusMaxLen < 10 {
					statusMaxLen = 10
				}
				if len([]rune(statusText)) > statusMaxLen {
					runes := []rune(statusText)
					statusText = string(runes[:statusMaxLen-1]) + "…"
				}
				mb.WriteString("    " + statusStyle.Render(statusText) + "\n")
			}

			if isSelected {
				selStart = strings.Count(b.String(), "\n")
				selEnd = selStart + strings.Count(mb.String(), "\n")
				selKey = m.User.ID.String()
			}
			b.WriteString(zone.Mark("member-row:"+m.User.ID.String(), mb.String()))
			flatIndex++
		}

		// A blank line closes each group, so voice/online/offline read as
		// separate blocks.
		sections := 0
		startSection := func(header string) {
			if sections > 0 {
				b.WriteString("\n")
			}
			sections++
			b.WriteString(header)
			b.WriteString("\n")
		}

		// ── 1. Voice channel groups (top of panel) ────────────────────────────
		for _, g := range voiceGroups {
			startSection(voiceHeaderStyle.Render(fmt.Sprintf("── ♪ %s (%d) ──", strings.ToUpper(g.name), len(g.members))))
			for _, m := range g.members {
				renderMember(m)
			}
		}

		// ── 2. Online, then offline (role shown beside each name) ─────────────
		for _, sec := range []struct {
			label   string
			members []*MemberDisplay
		}{{"ONLINE", onlineMembers}, {"OFFLINE", offlineMembers}} {
			if len(sec.members) == 0 {
				continue
			}
			startSection(sectionHeaderStyle.Render(fmt.Sprintf("── %s (%d) ──", sec.label, len(sec.members))))
			for _, m := range sec.members {
				renderMember(m)
			}
		}
	}

	// The last member's zone end marker lands after the final newline; fold
	// it back into the previous line so it doesn't count as an extra row.
	lines := strings.Split(b.String(), "\n")
	if n := len(lines); n > 1 && lipgloss.Width(lines[n-1]) == 0 {
		lines[n-2] += lines[n-1]
		lines = lines[:n-1]
	}
	return lines, selStart, selEnd, selKey
}

// renderStatusBar renders the bottom status bar
func (a *App) renderStatusBar() string {
	// Create styles with background for each text segment
	connectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Green))

	disconnectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Red))

	textStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))

	// Left side: connection status and user
	leftContent := ""
	isConnected := false
	var currentUser *models.User

	if a.activeConn != nil {
		isConnected = a.activeConn.GetState() == StateReady
		a.activeConn.mu.RLock()
		currentUser = a.activeConn.User
		a.activeConn.mu.RUnlock()
	}

	if isConnected {
		leftContent = connectedStyle.Render(" ● Connected")
	} else {
		leftContent = disconnectedStyle.Render(" ○ Disconnected")
	}

	if currentUser != nil {
		leftContent += textStyle.Render("  |  " + currentUser.FullUsername())
	}

	// Voice channel pill — shown whenever the local user is in a voice channel,
	// even while viewing a text channel.
	inVoice := false
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		vcID := a.activeConn.CurrentVoiceChannelID
		a.activeConn.mu.RUnlock()
		if vcID != uuid.Nil {
			inVoice = true
			// Resolve channel name by scanning all channels on this connection.
			vcName := "voice"
			a.activeConn.mu.RLock()
			for _, chList := range a.activeConn.Channels {
				for _, ch := range chList {
					if ch.ID == vcID {
						vcName = ch.Name
						break
					}
				}
			}
			a.activeConn.mu.RUnlock()
			voicePillStyle := lipgloss.NewStyle().
				Background(lipgloss.Color(a.theme.Colors.Selection)).
				Foreground(lipgloss.Color(a.theme.Colors.Green)).
				Bold(true)
			leftContent += voicePillStyle.Render("  |  ♪ " + vcName)
		}
	}

	// Right side: help text (include Server Settings for admins). Each
	// clickable hint is zone.Mark-ed independently so a click can resolve
	// which segment was pressed -- "Tab: Navigate" and "Enter: Join/Leave
	// voice" are deliberately left unmarked, see Area 5 in "Concord - Mouse
	// Support Plan".
	helpText := "Tab: Navigate  |  " + zone.Mark("statusbar-settings", "Ctrl+S: Settings") + "  |  "
	if a.currentUserRoleLevel() >= roleLevelAdmin {
		helpText += zone.Mark("statusbar-server-settings", "Ctrl+B: Server Settings") + "  |  "
	}
	if inVoice {
		helpText += "Enter: Join/Leave voice  |  "
	}
	helpText += zone.Mark("statusbar-help", "Type /help") + "  |  " + zone.Mark("statusbar-quit", "Ctrl+Q: Quit") + " "
	rightContent := textStyle.Render(helpText)

	// Calculate spacing (must know left/right widths before truncating center)
	leftLen := lipgloss.Width(leftContent)
	rightLen := lipgloss.Width(rightContent)

	// Center: status message (truncated to fit available width)
	centerContent := ""
	if a.statusMessage != "" {
		centerStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(a.theme.Colors.Selection))
		if a.statusError {
			centerStyle = centerStyle.Foreground(lipgloss.Color(a.theme.Colors.Red))
		} else {
			centerStyle = centerStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan))
		}
		maxCenter := a.width - leftLen - rightLen - 4
		msg := a.statusMessage
		if maxCenter > 6 && len([]rune(msg)) > maxCenter {
			msg = string([]rune(msg)[:maxCenter-3]) + "..."
		}
		centerContent = centerStyle.Render(msg)
	}

	centerLen := lipgloss.Width(centerContent)
	totalSpace := a.width - leftLen - rightLen - centerLen

	var bar string
	spacerStyle := textStyle
	if totalSpace > 0 {
		leftPad := totalSpace / 2
		rightPad := totalSpace - leftPad
		bar = leftContent + spacerStyle.Render(strings.Repeat(" ", leftPad)) + centerContent + spacerStyle.Render(strings.Repeat(" ", rightPad)) + rightContent
	} else {
		bar = leftContent + spacerStyle.Render("  ") + centerContent + rightContent
	}

	return bar
}

// renderLinkBrowserOverlay renders the link browser modal overlay on top of the base view
func (a *App) renderLinkBrowserOverlay(baseView string) string {
	if a.linkBrowserState == nil {
		return baseView
	}

	// Calculate overlay dimensions (centered modal)
	overlayWidth := 80
	if overlayWidth > a.width-4 {
		overlayWidth = a.width - 4
	}

	s := a.linkBrowserState
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Background(lipgloss.Color(a.theme.Semantic.InputBg))

	// Header — includes a sort-order indicator once one's been applied, since
	// there's no other visible cue that the list isn't in its original order.
	sortLabel := ""
	switch s.SortOrder {
	case linkSortAscending:
		sortLabel = "  (A→Z)"
	case linkSortDescending:
		sortLabel = "  (Z→A)"
	}
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Background(lipgloss.Color(a.theme.Semantic.InputBg)).
		Bold(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	header := headerStyle.Render(fmt.Sprintf("Links (%d)%s", len(s.Links), sortLabel))

	// Search bar — always shown once a search has ever been started (active
	// typing or an applied-but-since-unfocused filter), so it's clear why the
	// list might be shorter than the total link count.
	var searchLine string
	if s.Searching || s.Query != "" {
		searchStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(a.theme.Semantic.InputBg)).
			Width(overlayWidth - 2)
		prefix := "Search: "
		if s.Searching {
			searchLine = searchStyle.Render(prefix + s.Query + "█")
		} else {
			searchLine = searchStyle.Render(dimStyle.Render(prefix+s.Query) + dimStyle.Render("  (esc while searching to clear)"))
		}
	}

	// Link list — only the current scroll window, numbered by visible
	// position (row 1-9 within the window, not an absolute index into the
	// full list) so number-key selection stays meaningful once it scrolls.
	visibleStart := s.ScrollOffset
	visibleEnd := visibleStart + linkBrowserVisibleRows
	if visibleEnd > len(s.Links) {
		visibleEnd = len(s.Links)
	}

	var linkLines []string
	if len(s.Links) == 0 {
		linkLines = append(linkLines, dimStyle.Width(overlayWidth-2).Render("No links match"))
	}
	for i := visibleStart; i < visibleEnd; i++ {
		link := s.Links[i]
		row := i - visibleStart // 0-based position within the visible window

		numberStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Background(lipgloss.Color(a.theme.Semantic.InputBg)).
			Bold(true)
		linkStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Background(lipgloss.Color(a.theme.Semantic.InputBg))

		// Highlight selected link — a leading cursor plus a background swap,
		// so selection stays visible even on themes with subtle contrast
		// between the two colors.
		cursor := "  "
		if i == s.SelectedIndex {
			cursor = "▶ "
			numberStyle = numberStyle.Background(lipgloss.Color(a.theme.Colors.Selection))
			linkStyle = linkStyle.Background(lipgloss.Color(a.theme.Colors.Selection))
		}

		// Truncate link if too long
		maxLinkLen := overlayWidth - 14
		displayLink := link
		if len(displayLink) > maxLinkLen {
			displayLink = displayLink[:maxLinkLen-1] + "…"
		}

		numberLabel := "   "
		if row < 9 {
			numberLabel = fmt.Sprintf("[%d]", row+1)
		}
		line := cursor + numberStyle.Render(numberLabel) + linkStyle.Render(" "+displayLink)

		// Wrap line in full-width style to ensure background fills
		lineStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(a.theme.Semantic.InputBg)).
			Width(overlayWidth - 2)
		linkLines = append(linkLines, zone.Mark(fmt.Sprintf("link-row:%d", i), lineStyle.Render(line)))
	}

	// Scroll indicators, same convention as the channel/member overwrite
	// target picker.
	if visibleStart > 0 {
		linkLines = append([]string{dimStyle.Width(overlayWidth - 2).Render(fmt.Sprintf("↑ %d more", visibleStart))}, linkLines...)
	}
	if visibleEnd < len(s.Links) {
		linkLines = append(linkLines, dimStyle.Width(overlayWidth-2).Render(fmt.Sprintf("↓ %d more", len(s.Links)-visibleEnd)))
	}

	// Footer with keybind hints
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Background(lipgloss.Color(a.theme.Semantic.InputBg)).
		Italic(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	var hints string
	if s.Searching {
		hints = hintStyle.Render("Enter: Apply  •  Esc: Cancel")
	} else {
		hints = hintStyle.Render("1-9/↑↓: Select  •  Enter: Open  •  C: Copy  •  /: Search  •  A/D: Sort  •  Esc: Close")
	}

	// Build modal content
	var modalContent strings.Builder
	modalContent.WriteString(header + "\n")
	if searchLine != "" {
		modalContent.WriteString(searchLine + "\n")
	}
	modalContent.WriteString("\n")
	for _, line := range linkLines {
		modalContent.WriteString(line + "\n")
	}
	modalContent.WriteString("\n" + hints)

	// Calculate modal height
	modalHeight := len(linkLines) + 5 // header + links + footer + spacing
	if searchLine != "" {
		modalHeight++
	}

	// Wrap in box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Width(overlayWidth).
		Height(modalHeight).
		Padding(1).
		Background(lipgloss.Color(a.theme.Semantic.InputBg))

	modal := boxStyle.Render(modalContent.String())

	// Place modal centered on screen
	yOffset := (a.height - modalHeight) / 2
	if yOffset < 0 {
		yOffset = 0
	}
	xOffset := (a.width - overlayWidth) / 2
	if xOffset < 0 {
		xOffset = 0
	}

	// Use lipgloss.Place to overlay the modal on the base view
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(""),
		lipgloss.WithWhitespaceForeground(lipgloss.Color(a.theme.Colors.Background)))
}

// ansiSeqEnd returns the index one past the end of the ANSI escape sequence starting at s[i].
// Handles CSI (\x1b[…), OSC (\x1b]…BEL/ST), and simple two-char escapes.
func ansiSeqEnd(s string, i int) int {
	j := i + 1 // skip \x1b
	if j >= len(s) {
		return j
	}
	switch s[j] {
	case '[': // CSI — skip '[' then scan to final byte (0x40–0x7E)
		j++
		for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
			j++
		}
		if j < len(s) {
			j++ // include final byte
		}
	case ']': // OSC — scan until BEL or ST (\x1b\)
		j++
		for j < len(s) {
			if s[j] == '\x07' {
				j++
				break
			}
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				j += 2
				break
			}
			j++
		}
	default: // simple two-char escape
		j++
	}
	return j
}

// visualTake returns the first n visual columns of s with ANSI escape codes preserved.
// If s is shorter than n columns, it pads with spaces. A reset is appended.
func visualTake(s string, n int) string {
	var out strings.Builder
	col := 0
	i := 0
	for i < len(s) && col < n {
		if s[i] == '\x1b' {
			j := ansiSeqEnd(s, i)
			out.WriteString(s[i:j])
			i = j
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		if col+w > n {
			break
		}
		out.WriteRune(r)
		col += w
		i += sz
	}
	if col < n {
		out.WriteString(strings.Repeat(" ", n-col))
	}
	out.WriteString("\x1b[0m")
	return out.String()
}

// visualSkip skips the first n visual columns of s and returns the rest.
// ANSI escape sequences encountered before the skip point are re-emitted as a
// preamble so that color state is correct at the start of the returned string.
func visualSkip(s string, n int) string {
	col := 0
	i := 0
	var preamble strings.Builder
	for i < len(s) {
		if s[i] == '\x1b' {
			j := ansiSeqEnd(s, i)
			if col >= n {
				return preamble.String() + s[i:]
			}
			preamble.WriteString(s[i:j])
			i = j
			continue
		}
		if col >= n {
			return preamble.String() + s[i:]
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		col += runewidth.RuneWidth(r)
		i += sz
	}
	return ""
}

// easeInOutCubic maps t∈[0,1] through a cubic ease-in-out curve: slow start,
// fast middle, slow finish — gives panel slides a natural, polished feel.
func easeInOutCubic(t float64) float64 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	return 1 - math.Pow(-2*t+2, 3)/2
}

// clipPanelLeft clips a rendered full-screen panel to its leftmost animWidth visual columns.
// The right portion of the screen is left blank. Used for slide-from-left animations.
func clipPanelLeft(view string, animWidth int) string {
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = visualTake(line, animWidth) + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

// clipPanelRight clips a rendered full-screen panel to its rightmost animWidth visual columns,
// placed at the right edge with blank space on the left. Used for slide-from-right animations.
func clipPanelRight(view string, animWidth, totalWidth int) string {
	skipCols := totalWidth - animWidth
	if skipCols < 0 {
		skipCols = 0
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = strings.Repeat(" ", skipCols) + "\x1b[0m" + visualSkip(line, skipCols)
	}
	return strings.Join(lines, "\n")
}

// overlayCenter places the fg string centered over bg at terminal dimensions termW×termH,
// showing the background content around the dialog rather than a solid fill.
func overlayCenter(bg, fg string, termW, termH int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	fgH := len(fgLines)
	fgW := 0
	for _, l := range fgLines {
		if w := lipgloss.Width(l); w > fgW {
			fgW = w
		}
	}

	startY := (termH - fgH) / 2
	startX := (termW - fgW) / 2
	if startX < 0 {
		startX = 0
	}
	if startY < 0 {
		startY = 0
	}

	// Ensure bgLines has enough rows
	for len(bgLines) < termH {
		bgLines = append(bgLines, strings.Repeat(" ", termW))
	}

	for y, fgLine := range fgLines {
		bgY := startY + y
		if bgY < 0 || bgY >= len(bgLines) {
			continue
		}
		bgLine := bgLines[bgY]
		bgW := lipgloss.Width(bgLine)
		needed := startX + fgW
		if bgW < needed {
			bgLine += strings.Repeat(" ", needed-bgW)
		}
		left := visualTake(bgLine, startX)
		right := visualSkip(bgLine, startX+fgW)
		bgLines[bgY] = left + fgLine + right
	}

	if len(bgLines) > termH {
		bgLines = bgLines[:termH]
	}
	return strings.Join(bgLines, "\n")
}

// renderMemberContextMenuOverlay renders the member action context menu overlay.
// When VolumeSlider is active it shows an inline ASCII slider instead.
// The dialog pops in from the center via an animation and the main view is visible behind it.
func (a *App) renderMemberContextMenuOverlay(baseView string) string {
	if a.memberContextMenu == nil {
		return baseView
	}

	// ── Compute target dimensions ─────────────────────────────────────────────
	targetW := 50
	if targetW > a.width-4 {
		targetW = a.width - 4
	}

	// Build full content to determine target height
	titleStyleFull := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center).
		Width(targetW - 2)
	hintStyleFull := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Align(lipgloss.Center).
		Width(targetW - 2)

	var fullLines []string
	var targetH int

	if vs := a.memberContextMenu.VolumeSlider; vs != nil {
		title := titleStyleFull.Render(fmt.Sprintf("Volume: @%s", a.memberContextMenu.TargetMember.User.Username))
		const barWidth = 20
		filled := int(vs.Volume / 2.0 * barWidth)
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		pct := int(vs.Volume*100 + 0.5)
		barStr := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Render(fmt.Sprintf("[%s] %d%%", bar, pct))
		barLine := lipgloss.NewStyle().Width(targetW - 2).Align(lipgloss.Center).Render(barStr)
		hints := hintStyleFull.Render("← −1%  Enter: Save  Esc: Back  +1% →")
		fullLines = []string{title, "", barLine, "", hints}
		targetH = 7
	} else {
		title := titleStyleFull.Render(fmt.Sprintf("Actions for @%s", a.memberContextMenu.TargetMember.User.Username))
		var actionLines []string
		for i, action := range a.memberContextMenu.Actions {
			keyStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).Bold(true)
			labelStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground))
			prefix := "  "
			if i == a.memberContextMenu.SelectedIndex {
				prefix = "> "
				keyStyle = keyStyle.Background(lipgloss.Color(a.theme.Semantic.SidebarSelected))
				labelStyle = labelStyle.Background(lipgloss.Color(a.theme.Semantic.SidebarSelected))
			}
			row := fmt.Sprintf("%s%s %s",
				prefix,
				keyStyle.Render(fmt.Sprintf("[%s]", action.Key)),
				labelStyle.Render(action.Label))
			actionLines = append(actionLines, zone.Mark(fmt.Sprintf("member-action-row:%d", i), row))
		}
		hints := hintStyleFull.Render("Enter: Execute  •  Esc: Close")
		fullLines = append(fullLines, title, "")
		fullLines = append(fullLines, actionLines...)
		fullLines = append(fullLines, "", hints)
		targetH = len(actionLines) + 5
	}

	// ── Apply pop-in animation ────────────────────────────────────────────────
	frame := a.memberContextMenu.AnimFrame
	var eased float64
	if frame >= contextMenuMaxFrames {
		eased = 1.0
	} else {
		t := float64(frame) / float64(contextMenuMaxFrames)
		eased = 1.0 - math.Pow(1.0-t, 3.0) // ease-out cubic
	}

	currentW := 8 + int(float64(targetW-8)*eased)
	if currentW > targetW {
		currentW = targetW
	}
	currentH := 3 + int(float64(targetH-3)*eased)
	if currentH > targetH {
		currentH = targetH
	}

	// Clip content to visible lines (subtract 4 for top/bottom border + top/bottom padding)
	visibleLines := currentH - 4
	if visibleLines < 0 {
		visibleLines = 0
	}
	var clippedContent string
	if visibleLines >= len(fullLines) {
		clippedContent = strings.Join(fullLines, "\n")
	} else {
		clippedContent = strings.Join(fullLines[:visibleLines], "\n")
	}

	// ── Render the modal box ──────────────────────────────────────────────────
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Width(currentW - 2).
		Height(currentH - 2).
		Padding(1).
		Background(lipgloss.Color(a.theme.Colors.Background))

	modal := boxStyle.Render(clippedContent)

	// ── Overlay dialog on the live main view ─────────────────────────────────
	return overlayCenter(baseView, modal, a.width, a.height)
}

// renderKeyHintGrid draws hints as rows with their columns lined up (the
// login screen's two rows of three).
func (a *App) renderKeyHintGrid(rows [][]keyHint) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Padding(0, 1)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	cells := make([][]string, len(rows))
	var widths []int
	for r, row := range rows {
		for c, h := range row {
			cell := keyStyle.Render(h.key) + " " + descStyle.Render(h.desc)
			cells[r] = append(cells[r], cell)
			if c >= len(widths) {
				widths = append(widths, 0)
			}
			widths[c] = max(widths[c], lipgloss.Width(cell))
		}
	}
	lines := make([]string, len(rows))
	for r, row := range cells {
		var b strings.Builder
		for c, cell := range row {
			b.WriteString(cell)
			if c < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[c]-lipgloss.Width(cell)+3))
			}
		}
		lines[r] = b.String()
	}
	return strings.Join(lines, "\n")
}
