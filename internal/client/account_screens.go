package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// Account screens (2026-09-30):
//
//   - Profiles (Ctrl+P on the login screen): the identities saved on this
//     computer. Switch, add, edit (fix a typo in the alias or email, which
//     also updates the accounts on the servers you're signed in to) and
//     forget.
//   - The code screen: enter an emailed code. It verifies a new account,
//     resets a forgotten password (Ctrl+F on the login screen), or confirms
//     a changed email.

// --- shared helpers ---

func newInput(placeholder string, limit int, secret bool) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	in.Width = 34
	if secret {
		in.EchoMode = textinput.EchoPassword
	}
	return in
}

func (a *App) dim(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render(s)
}

// serverByID is the saved server with this ID.
func (a *App) serverByID(id uuid.UUID) *ClientServerInfo {
	for _, s := range a.configMgr.GetClientServers() {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// emailOn is the email the active profile uses on a server.
func (a *App) emailOn(server *ClientServerInfo) string {
	if server != nil && server.SavedCredentials.belongsTo(a.localIdentity) && server.SavedCredentials.Email != "" {
		return server.SavedCredentials.Email
	}
	if a.localIdentity != nil {
		return a.localIdentity.Email
	}
	return ""
}

// signOutAll signs every server out (no automatic reconnect) and clears
// the selection, before another profile signs in.
func (a *App) signOutAll() {
	for _, sc := range a.connMgr.GetAllConnections() {
		sc.mu.Lock()
		sc.Token, sc.User = "", nil
		sc.mu.Unlock()
		_ = a.connMgr.DisconnectServer(sc.ServerID)
	}
	a.stopVoiceEngine()
	a.activeConn = nil
	a.currentServer = nil
	a.currentChannel = nil
	a.pendingVerify = map[uuid.UUID]string{}
}

// --- Profiles ---

// ProfilesState is the open Profiles screen.
type ProfilesState struct {
	Cursor    int
	Confirm   string // ID awaiting a second F (forget)
	Notice    string
	NoticeErr bool
	Busy      bool
	Back      View

	// Editing a profile (nil: the list).
	EditID    string
	EditFocus int
	EditAlias textinput.Model
	EditEmail textinput.Model
	EditPass  textinput.Model // its current password, to confirm

	// Changing the active profile's password (nil: not).
	Password *passwordChangeState
}

func (a *App) openProfiles() {
	a.profilesState = &ProfilesState{Back: a.view}
	if a.localIdentity != nil {
		list, _ := a.configMgr.Profiles()
		for i, p := range list {
			if p.ID == a.localIdentity.ID {
				a.profilesState.Cursor = i
			}
		}
	}
	a.view = ViewProfiles
}

func (a *App) handleProfilesKey(msg tea.KeyMsg) tea.Cmd {
	s := a.profilesState
	if s.Busy {
		return nil
	}
	if s.Password != nil {
		return a.handlePasswordChangeKey(msg)
	}
	if s.EditID != "" {
		return a.handleProfileEditKey(msg)
	}
	list, active := a.configMgr.Profiles()
	key := msg.String()
	if key != "f" && key != "F" {
		s.Confirm = ""
	}
	var cur *LocalIdentity
	if s.Cursor < len(list) {
		cur = list[s.Cursor]
	}
	switch key {
	case "ctrl+q":
		return tea.Quit
	case "esc":
		a.profilesState = nil
		a.view = ViewLogin
		a.initLoginView()
	case "up", "k", "left", "h", "shift+tab":
		if s.Cursor > 0 {
			s.Cursor--
		}
	case "down", "j", "right", "l", "tab":
		// One past the last profile is the + card.
		if s.Cursor < len(list) {
			s.Cursor++
		}
	case "enter":
		if cur == nil {
			a.profilesState = nil
			a.startAddProfile()
			return nil
		}
		if cur.ID != active || a.localIdentity == nil || a.localIdentity.ID != cur.ID {
			a.signOutAll()
			if err := a.configMgr.SetActiveProfile(cur.ID); err != nil {
				s.Notice, s.NoticeErr = err.Error(), true
				return nil
			}
			a.localIdentity = cur
		}
		a.profilesState = nil
		a.view = ViewLogin
		a.initLoginView()
	case "a", "A":
		a.profilesState = nil
		a.startAddProfile()
	case "p", "P":
		if cur != nil && a.localIdentity != nil && cur.ID == a.localIdentity.ID {
			a.startPasswordChange()
		} else {
			s.Notice, s.NoticeErr = "Switch to this profile first (Enter), then change its password.", true
		}
	case "e", "E":
		if cur != nil {
			s.EditID, s.EditFocus = cur.ID, 0
			s.EditAlias = newInput("Alias", 32, false)
			s.EditAlias.SetValue(cur.Alias)
			s.EditEmail = newInput("Email", 255, false)
			s.EditEmail.SetValue(cur.Email)
			s.EditPass = newInput("Its password, to confirm", 128, true)
			s.EditAlias.Focus()
			s.Notice = ""
		}
	case "f", "F":
		if cur == nil {
			return nil
		}
		if s.Confirm != cur.ID {
			s.Confirm = cur.ID
			s.Notice, s.NoticeErr = "Press F again to forget "+cur.Alias+" on this computer. Its accounts on servers stay; you can add the profile back later.", true
			return nil
		}
		wasActive := a.localIdentity != nil && a.localIdentity.ID == cur.ID
		next, err := a.configMgr.ForgetProfile(cur.ID)
		if err != nil {
			s.Notice, s.NoticeErr = err.Error(), true
			return nil
		}
		s.Confirm, s.Notice, s.NoticeErr = "", "Forgot "+cur.Alias+".", false
		if s.Cursor > 0 {
			s.Cursor--
		}
		if wasActive {
			a.signOutAll()
			a.localIdentity = a.configMgr.GetIdentity()
			if next == "" {
				a.profilesState = nil
				a.startAddProfile()
			}
		}
	}
	return nil
}

// startAddProfile opens the identity setup form to add a profile.
func (a *App) startAddProfile() {
	a.addingProfile = true
	a.identityAlias.Reset()
	a.identityEmail.Reset()
	a.identityPassword.Reset()
	a.identityPasswordConfirm.Reset()
	a.identityFocus = 0
	a.identityError = ""
	a.identityAlias.Focus()
	a.view = ViewIdentitySetup
}

func (a *App) handleProfileEditKey(msg tea.KeyMsg) tea.Cmd {
	s := a.profilesState
	inputs := []*textinput.Model{&s.EditAlias, &s.EditEmail, &s.EditPass}
	move := func(d int) {
		inputs[s.EditFocus].Blur()
		s.EditFocus = (s.EditFocus + d + len(inputs)) % len(inputs)
		inputs[s.EditFocus].Focus()
	}
	switch msg.String() {
	case "esc":
		s.EditID, s.Notice = "", ""
		return nil
	case "tab", "down":
		move(1)
		return nil
	case "shift+tab", "up":
		move(-1)
		return nil
	case "enter":
		if s.EditFocus < len(inputs)-1 {
			move(1)
			return nil
		}
		return a.saveProfileEdit()
	}
	var cmd tea.Cmd
	*inputs[s.EditFocus], cmd = inputs[s.EditFocus].Update(msg)
	return cmd
}

// profileEditResultMsg reports how a profile edit went on each server.
type profileEditResultMsg struct {
	profile *LocalIdentity
	lines   []string
	pending []uuid.UUID // servers waiting for an email confirmation code
	failed  bool
}

func (a *App) saveProfileEdit() tea.Cmd {
	s := a.profilesState
	list, _ := a.configMgr.Profiles()
	var p *LocalIdentity
	for _, q := range list {
		if q.ID == s.EditID {
			p = q
		}
	}
	if p == nil {
		s.EditID = ""
		return nil
	}
	alias := strings.TrimSpace(s.EditAlias.Value())
	email := strings.ToLower(strings.TrimSpace(s.EditEmail.Value()))
	switch {
	case len(alias) < 2 || len(alias) > 32:
		s.Notice, s.NoticeErr = "Alias must be 2-32 characters", true
		return nil
	case !strings.Contains(email, "@"):
		s.Notice, s.NoticeErr = "Enter a valid email address", true
		return nil
	case s.EditPass.Value() != p.Password:
		s.Notice, s.NoticeErr = "That isn't this profile's password", true
		return nil
	}
	if alias == p.Alias && strings.EqualFold(email, p.Email) {
		s.EditID = ""
		return nil
	}
	oldAlias, oldEmail := p.Alias, p.Email
	updated := *p
	updated.Alias, updated.Email = alias, email
	isActive := a.localIdentity != nil && a.localIdentity.ID == p.ID
	if err := a.saveProfileKeepingActive(&updated); err != nil {
		s.Notice, s.NoticeErr = err.Error(), true
		return nil
	}
	if isActive {
		a.localIdentity = &updated
	}
	s.EditID = ""
	if !isActive {
		s.Notice, s.NoticeErr = "Saved on this computer. Its server accounts change the next time you edit it while signed in as it.", false
		return nil
	}
	s.Busy, s.Notice, s.NoticeErr = true, "Updating your servers…", false

	// Update every server this profile is signed in to right now.
	type target struct {
		id    uuid.UUID
		name  string
		token string
		api   AccountAPI
	}
	var targets []target
	for _, sc := range a.connMgr.GetAllConnections() {
		sc.mu.RLock()
		tok := sc.Token
		sc.mu.RUnlock()
		if tok == "" {
			continue
		}
		api, _ := a.connMgr.accountAPIFor(sc.ServerID)
		targets = append(targets, target{sc.ServerID, sc.ServerInfo.Name, tok, api})
	}
	newAlias, newEmail := "", ""
	if alias != oldAlias {
		newAlias = alias
	}
	if !strings.EqualFold(email, oldEmail) {
		newEmail = email
	}
	password := p.Password
	profile := &updated
	return func() tea.Msg {
		res := profileEditResultMsg{profile: profile}
		if len(targets) == 0 {
			res.lines = append(res.lines, "Saved. You aren't signed in to any server right now, so their accounts keep the old details.")
			return res
		}
		for _, t := range targets {
			r, err := t.api.UpdateAccount(t.token, password, newAlias, newEmail)
			switch {
			case err != nil:
				res.failed = true
				res.lines = append(res.lines, t.name+": "+err.Error())
			case r.VerificationRequired:
				res.pending = append(res.pending, t.id)
				res.lines = append(res.lines, t.name+": check "+r.PendingEmail+" for a code to confirm it")
			default:
				res.lines = append(res.lines, t.name+": updated")
				if newEmail != "" {
					_ = a.configMgr.SaveServerSignIn(t.id, profile.ID, newEmail, t.token, r.User.ID)
				}
			}
		}
		return res
	}
}

// saveProfileKeepingActive saves p without changing which profile is active.
func (a *App) saveProfileKeepingActive(p *LocalIdentity) error {
	_, active := a.configMgr.Profiles()
	if err := a.configMgr.SaveIdentity(p); err != nil {
		return err
	}
	if active != "" && active != p.ID {
		return a.configMgr.SetActiveProfile(active)
	}
	return nil
}

func (a *App) applyProfileEditResult(m profileEditResultMsg) tea.Cmd {
	if s := a.profilesState; s != nil {
		s.Busy = false
		s.Notice, s.NoticeErr = strings.Join(m.lines, "\n"), m.failed
	}
	if len(m.pending) > 0 {
		a.openCodeScreen(codeModeConfirmEmail, m.pending[0], m.profile.Email)
		a.codeState.Queue = m.pending[1:]
		a.codeState.Notice = strings.Join(m.lines, "\n")
	}
	return nil
}

func (a *App) renderProfilesView() string {
	s := a.profilesState
	if s.Password != nil {
		return a.renderPasswordChange()
	}
	list, active := a.configMgr.Profiles()
	var b strings.Builder
	if s.EditID != "" {
		b.WriteString(a.dim("Fix a typo or change how you appear. Servers you're signed in to\nnow are updated too.") + "\n\n")
		b.WriteString(a.stageField("Alias", s.EditFocus == 0, s.EditAlias.View()) + "\n")
		b.WriteString(a.stageField("Email", s.EditFocus == 1, s.EditEmail.View()) + "\n")
		b.WriteString(a.stageField("Password", s.EditFocus == 2, s.EditPass.View()) + "\n")
		if s.Notice != "" {
			b.WriteString("\n" + a.stageNotice(s.Notice, s.NoticeErr))
		}
		return a.stagePage("Profiles", "Edit your profile", "profile", b.String(),[]keyHint{{"Tab", "Next"}, {"Enter", "Save"}, {"Esc", "Cancel"}})
	}
	b.WriteString(a.profileCards(list, active, s.Cursor) + "\n\n")
	if s.Busy {
		b.WriteString(a.dim("Working…") + "\n")
	}
	b.WriteString(a.stageNotice(s.Notice, s.NoticeErr))
	return a.stagePage("Profiles", "Who's using Concord?", "Concord", b.String(),
		[]keyHint{{"←→", "Choose"}, {"Enter", "Use"}, {"E", "Edit"}, {"P", "Password"}, {"F", "Forget"}, {"Esc", "Back"}})
}

// profileCardW is a profile card's width, border included.
const profileCardW = 18

// profileCards draws the profiles as a row of cards, the + card last,
// scrolled to keep the selected one in view.
func (a *App) profileCards(list []*LocalIdentity, active string, cursor int) string {
	fit := max(1, (stageWidth+1)/(profileCardW+1))
	n := len(list) + 1
	first := max(0, min(cursor-fit+1, n-fit))
	first = min(first, cursor)
	var cards []string
	for i := first; i < n && i < first+fit; i++ {
		if i == len(list) {
			cards = append(cards, a.profileCard("", "", "", false, i == cursor))
			continue
		}
		p := list[i]
		cards = append(cards, a.profileCard(p.Alias, p.Email, p.ID, p.ID == active, i == cursor))
		cards = append(cards, " ")
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	more := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	left, right := " ", " "
	if first > 0 {
		left = "‹"
	}
	if first+fit < n {
		right = "›"
	}
	pad := strings.Repeat("\n", 2)
	return lipgloss.JoinHorizontal(lipgloss.Top, more.Render(pad+left)+" ", row, " "+more.Render(pad+right))
}

// avatarColours are the colours a profile's initial can sit on.
func (a *App) avatarColour(id string) string {
	c := a.theme.Colors
	choices := []string{c.Purple, c.Pink, c.Cyan, c.Green, c.Orange, c.Yellow}
	h := 0
	for _, r := range id {
		h = h*31 + int(r)
	}
	return choices[(h%len(choices)+len(choices))%len(choices)]
}

// profileCard is one card: the initial on a coloured chip, alias, email,
// and whether it's the profile in use. An empty alias is the + card.
func (a *App) profileCard(alias, email, id string, current, selected bool) string {
	c := a.theme.Colors
	inner := profileCardW - 2
	border := c.Comment
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground))
	if selected {
		border = c.Purple
		nameStyle = nameStyle.Foreground(lipgloss.Color(c.Purple)).Bold(true)
	}
	center := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center)
	var rows []string
	if alias == "" {
		plus := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Bold(true)
		if selected {
			plus = plus.Foreground(lipgloss.Color(c.Purple))
		}
		rows = []string{center.Render(plus.Render(" + ")), center.Render(nameStyle.Render("Add profile")), center.Render(a.dim("someone new")), ""}
	} else {
		initial := strings.ToUpper(string([]rune(alias)[0]))
		chip := lipgloss.NewStyle().Background(lipgloss.Color(a.avatarColour(id))).
			Foreground(lipgloss.Color(c.Background)).Bold(true).Render(" " + initial + " ")
		tag := ""
		if current {
			tag = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green)).Render("● in use")
		}
		rows = []string{
			center.Render(chip),
			center.Render(nameStyle.Render(truncateWidth(alias, inner-2))),
			center.Render(a.dim(truncateWidth(email, inner-2))),
			center.Render(tag),
		}
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(border)).
		Width(inner).Render(strings.Join(rows, "\n"))
}

func truncateWidth(s string, w int) string {
	return ansi.Truncate(s, w, "…")
}

// --- the code screen ---

const (
	codeModeVerify       = "verify"        // a new account's email
	codeModeForgot       = "forgot"        // reset a forgotten password
	codeModeConfirmEmail = "confirm_email" // a changed email
)

// AccountCodeState is the open code screen.
type AccountCodeState struct {
	Mode     string
	ServerID uuid.UUID
	Email    string
	Back     View
	Queue    []uuid.UUID // more servers to confirm an email on

	// forgot: step 0 picks the server, step 1 takes the code
	Step    int
	Servers []*ClientServerInfo
	Pick    int

	// TempPassword: the server can't email, so Code holds a temporary
	// password its admin set (concord-server --reset-password).
	TempPassword bool

	Code    textinput.Model
	NewPass textinput.Model
	Confirm textinput.Model
	Focus   int

	// verify: Ctrl+E corrects the email or alias
	Fixing   bool
	FixEmail textinput.Model
	FixAlias textinput.Model

	Notice string
	IsErr  bool
	Busy   bool

	// The boxes (code_boxes.go): when each letter was typed, when the
	// last code was sent (for the resend wait), and when the server
	// accepted the code (the ripple before the screen closes).
	typedAt  []time.Time
	SentAt   time.Time
	Accepted time.Time
}

func (a *App) openCodeScreen(mode string, serverID uuid.UUID, email string) {
	st := &AccountCodeState{Mode: mode, ServerID: serverID, Email: email, Back: a.view}
	if a.view == ViewAccountCode && a.codeState != nil {
		st.Back = a.codeState.Back
	}
	st.Code = newInput("ABC 234", 9, false)
	if mode != codeModeForgot {
		st.SentAt = time.Now() // opened because a code was just sent
	}
	st.NewPass = newInput("New password (min 8 chars)", 128, true)
	st.Confirm = newInput("Confirm new password", 128, true)
	st.Code.Focus()
	if mode == codeModeForgot {
		st.Servers = a.configMgr.GetClientServers()
		if a.currentClientServer != nil {
			for i, s := range st.Servers {
				if s.ID == a.currentClientServer.ID {
					st.Pick = i
				}
			}
		}
		if len(st.Servers) == 1 {
			st.ServerID = st.Servers[0].ID
		}
	}
	a.codeState = st
	a.view = ViewAccountCode
}

// accountCodeMsg is the outcome of a code screen request.
type accountCodeMsg struct {
	action   string // "verify", "resend", "fix", "forgot", "reset", "confirm"
	serverID uuid.UUID
	email    string
	alias    string
	userID   uuid.UUID
	token    string
	err      error
	synced   []string // reset: how updating the other servers went
}

func (a *App) codeServerName(id uuid.UUID) string {
	if s := a.serverByID(id); s != nil {
		return s.Name
	}
	return "the server"
}

func (a *App) handleAccountCodeKey(msg tea.KeyMsg) tea.Cmd {
	st := a.codeState
	if !st.Accepted.IsZero() {
		return nil // the ripple is playing; the screen closes itself
	}
	if st.Busy {
		if msg.String() == "ctrl+q" {
			return tea.Quit
		}
		return nil
	}
	if st.Fixing {
		return a.handleCodeFixKey(msg)
	}
	key := msg.String()
	if key == "ctrl+q" {
		return tea.Quit
	}
	if key == "esc" {
		return a.closeCodeScreen(false)
	}

	// forgot, step 0: choose the server to reset with
	if st.Mode == codeModeForgot && st.Step == 0 {
		switch key {
		case "up", "k", "left", "shift+tab":
			if st.Pick > 0 {
				st.Pick--
			}
		case "down", "j", "right", "tab":
			if st.Pick < len(st.Servers)-1 {
				st.Pick++
			}
		case "enter":
			if len(st.Servers) == 0 {
				return nil
			}
			server := st.Servers[st.Pick]
			st.ServerID, st.Email = server.ID, a.emailOn(server)
			return a.codeRequest("forgot", func(api AccountAPI) accountCodeMsg {
				return accountCodeMsg{err: api.ForgotPassword(st.Email)}
			})
		}
		return nil
	}

	inputs := []*textinput.Model{&st.Code}
	if st.Mode == codeModeForgot {
		inputs = append(inputs, &st.NewPass, &st.Confirm)
	}
	move := func(d int) {
		inputs[st.Focus].Blur()
		st.Focus = (st.Focus + d + len(inputs)) % len(inputs)
		inputs[st.Focus].Focus()
	}
	switch key {
	case "tab", "down":
		move(1)
		return nil
	case "shift+tab", "up":
		move(-1)
		return nil
	case "ctrl+r":
		return a.resendCode()
	case "ctrl+e":
		if st.Mode == codeModeVerify && a.localIdentity != nil {
			st.Fixing = true
			st.FixEmail = newInput("Email", 255, false)
			st.FixEmail.SetValue(st.Email)
			st.FixAlias = newInput("Alias", 32, false)
			st.FixAlias.SetValue(a.localIdentity.Alias)
			st.FixEmail.Focus()
			st.Focus = 0
			st.Notice = ""
		}
		return nil
	case "enter":
		if st.Focus < len(inputs)-1 {
			move(1)
			return nil
		}
		return a.submitCode()
	}
	var cmd tea.Cmd
	*inputs[st.Focus], cmd = inputs[st.Focus].Update(msg)
	return cmd
}

// codeRequest runs fn against the code screen's server off the UI thread.
func (a *App) codeRequest(action string, fn func(api AccountAPI) accountCodeMsg) tea.Cmd {
	st := a.codeState
	api, ok := a.connMgr.accountAPIFor(st.ServerID)
	if !ok {
		st.Notice, st.IsErr = "That server isn't in your list any more.", true
		return nil
	}
	st.Busy, st.Notice = true, ""
	serverID := st.ServerID
	return func() tea.Msg {
		m := fn(api)
		m.action, m.serverID = action, serverID
		return m
	}
}

func (a *App) resendCode() tea.Cmd {
	st := a.codeState
	switch st.Mode {
	case codeModeVerify:
		email, pw := st.Email, a.localIdentity.Password
		return a.codeRequest("resend", func(api AccountAPI) accountCodeMsg {
			return accountCodeMsg{err: api.ResendCode(email, pw)}
		})
	case codeModeForgot:
		email := st.Email
		return a.codeRequest("forgot", func(api AccountAPI) accountCodeMsg {
			return accountCodeMsg{err: api.ForgotPassword(email)}
		})
	}
	return nil
}

func (a *App) submitCode() tea.Cmd {
	st := a.codeState
	code := strings.TrimSpace(st.Code.Value())
	if !st.TempPassword && len(cleanTypedCode(code)) != 6 {
		st.Notice, st.IsErr = "The code is 6 characters, like ABC 234.", true
		return nil
	}
	email := st.Email
	switch st.Mode {
	case codeModeVerify:
		return a.codeRequest("verify", func(api AccountAPI) accountCodeMsg {
			user, token, err := api.VerifyAccount(email, code)
			m := accountCodeMsg{token: token, err: err, email: email}
			if user != nil {
				m.userID = user.ID
			}
			return m
		})
	case codeModeConfirmEmail:
		token := a.tokenFor(st.ServerID)
		return a.codeRequest("confirm", func(api AccountAPI) accountCodeMsg {
			return accountCodeMsg{err: api.ConfirmEmail(token, code), email: email, token: token}
		})
	case codeModeForgot:
		pw := st.NewPass.Value()
		if len(pw) < 8 {
			st.Notice, st.IsErr = "Password must be at least 8 characters", true
			return nil
		}
		if pw != st.Confirm.Value() {
			st.Notice, st.IsErr = "Passwords do not match", true
			return nil
		}
		return a.resetAndSync(code, pw)
	}
	return nil
}

func cleanTypedCode(s string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(s)
}

// tokenFor is the live session token for a server ("" if not signed in).
func (a *App) tokenFor(serverID uuid.UUID) string {
	if sc := a.connMgr.GetConnection(serverID); sc != nil {
		sc.mu.RLock()
		defer sc.mu.RUnlock()
		return sc.Token
	}
	return ""
}
// syncTarget is a server whose password follows the profile's.
type syncTarget struct {
	id    uuid.UUID
	name  string
	email string
	token string // a live or saved session, "" if none
	api   AccountAPI
}

// passwordSyncTargets lists the servers the active profile uses, except skip.
func (a *App) passwordSyncTargets(skip uuid.UUID) []syncTarget {
	profile := a.localIdentity
	var out []syncTarget
	for _, s := range a.configMgr.GetClientServers() {
		if s.ID == skip {
			continue
		}
		api, ok := a.connMgr.accountAPIFor(s.ID)
		if !ok {
			continue
		}
		token := a.tokenFor(s.ID)
		if token == "" && s.SavedCredentials.belongsTo(profile) {
			token = s.SavedCredentials.Token
		}
		out = append(out, syncTarget{s.ID, s.Name, a.emailOn(s), token, api})
	}
	return out
}

// changeOn changes one server's password from oldPassword, signing in with
// it first when there's no usable session.
func changeOn(t syncTarget, oldPassword, newPassword string) error {
	var err error
	if t.token != "" {
		err = t.api.ChangePassword(t.token, oldPassword, newPassword)
		if apiErr, ok := err.(*APIError); !ok || (apiErr.Code != "bad_token" && apiErr.Code != "no_token") {
			return err
		}
	}
	conn := NewConnection("")
	conn.serverAddr = t.api.Addr
	_, token, err := conn.Login(t.email, oldPassword)
	if err != nil {
		return err
	}
	return t.api.ChangePassword(token, oldPassword, newPassword)
}

// syncPassword changes the password on every target, returning a line per
// server and how many succeeded.
func syncPassword(targets []syncTarget, oldPassword, newPassword string) ([]string, int) {
	var lines []string
	ok := 0
	for _, t := range targets {
		if err := changeOn(t, oldPassword, newPassword); err != nil {
			lines = append(lines, t.name+": not changed ("+err.Error()+")")
		} else {
			ok++
			lines = append(lines, t.name+": changed")
		}
	}
	return lines, ok
}

// resetAndSync resets the password on the chosen server (with its emailed
// code, or a temporary password its admin set when it can't email), then
// changes it on every other server this profile uses, so one new password
// works everywhere.
func (a *App) resetAndSync(code, newPassword string) tea.Cmd {
	st := a.codeState
	oldPassword := a.localIdentity.Password
	email := st.Email
	temp := st.TempPassword
	others := a.passwordSyncTargets(st.ServerID)
	return a.codeRequest("reset", func(api AccountAPI) accountCodeMsg {
		var m accountCodeMsg
		if temp {
			conn := NewConnection("")
			conn.serverAddr = api.Addr
			user, token, err := conn.Login(email, code)
			if err == nil {
				err = api.ChangePassword(token, code, newPassword)
			}
			if err != nil {
				return accountCodeMsg{err: fmt.Errorf("that temporary password didn't work: %w", err), email: email}
			}
			m = accountCodeMsg{token: token, email: email, userID: user.ID}
		} else {
			user, token, err := api.ResetPassword(email, code, newPassword)
			if err != nil {
				return accountCodeMsg{err: err, email: email}
			}
			m = accountCodeMsg{token: token, email: email, userID: user.ID}
		}
		m.synced, _ = syncPassword(others, oldPassword, newPassword)
		return m
	})
}

func (a *App) handleCodeFixKey(msg tea.KeyMsg) tea.Cmd {
	st := a.codeState
	inputs := []*textinput.Model{&st.FixEmail, &st.FixAlias}
	move := func(d int) {
		inputs[st.Focus].Blur()
		st.Focus = (st.Focus + d + len(inputs)) % len(inputs)
		inputs[st.Focus].Focus()
	}
	switch msg.String() {
	case "esc":
		st.Fixing, st.Focus = false, 0
		st.Code.Focus()
		return nil
	case "tab", "down":
		move(1)
		return nil
	case "shift+tab", "up":
		move(-1)
		return nil
	case "enter":
		if st.Focus < len(inputs)-1 {
			move(1)
			return nil
		}
		email := strings.ToLower(strings.TrimSpace(st.FixEmail.Value()))
		alias := strings.TrimSpace(st.FixAlias.Value())
		if !strings.Contains(email, "@") || len(alias) < 2 || len(alias) > 32 {
			st.Notice, st.IsErr = "Enter a valid email and a 2-32 character alias.", true
			return nil
		}
		oldEmail, pw := st.Email, a.localIdentity.Password
		return a.codeRequest("fix", func(api AccountAPI) accountCodeMsg {
			sentTo, err := api.FixRegistration(oldEmail, pw, email, alias)
			if sentTo == "" {
				sentTo = email
			}
			return accountCodeMsg{err: err, email: sentTo, alias: alias}
		})
	}
	var cmd tea.Cmd
	*inputs[st.Focus], cmd = inputs[st.Focus].Update(msg)
	return cmd
}

// applyAccountCodeResult handles an accountCodeMsg.
func (a *App) applyAccountCodeResult(m accountCodeMsg) tea.Cmd {
	st := a.codeState
	if st == nil {
		return nil
	}
	st.Busy = false
	if m.err != nil {
		st.Notice, st.IsErr = m.err.Error(), true
		if e, ok := m.err.(*APIError); ok && e.Code == apiMailUnavailable && m.action == "forgot" {
			// No email from this server: its admin can set a temporary
			// password instead, entered where the code would go.
			st.Step, st.TempPassword, st.Focus = 1, true, 0
			st.Code = newInput("Temporary password", 64, true)
			st.Code.Focus()
			st.Notice = e.Message + " If they gave you a temporary password (concord-server --reset-password), enter it below; or Esc and pick another server."
		}
		return nil
	}
	st.IsErr = false
	name := a.codeServerName(m.serverID)
	switch m.action {
	case "resend":
		st.Notice = "A new code is on its way to " + st.Email + "."
		st.SentAt = time.Now()
	case "forgot":
		st.Step = 1
		st.SentAt = time.Now()
		st.Notice = "If " + st.Email + " has an account on " + name + ", a code is on its way. Enter it with your new password."
		st.Code.Focus()
	case "fix":
		p := *a.localIdentity
		if strings.EqualFold(p.Email, st.Email) {
			p.Email = m.email
		}
		p.Alias = m.alias
		if err := a.configMgr.SaveIdentity(&p); err == nil {
			a.localIdentity = &p
		}
		delete(a.pendingVerify, m.serverID)
		a.pendingVerify[m.serverID] = m.email
		st.Email, st.Fixing, st.Focus = m.email, false, 0
		st.Code.Reset()
		st.Code.Focus()
		st.Notice = "Fixed (your profile too). A new code was sent to " + m.email + "."
		st.SentAt = time.Now()
	case "verify":
		delete(a.pendingVerify, m.serverID)
		_ = a.configMgr.SaveServerSignIn(m.serverID, a.localIdentity.ID, m.email, m.token, m.userID)
		a.statusMessage, a.statusError = "Email verified on "+name+". Welcome!", false
		cmd := a.connectServerAsync(m.serverID, m.token)
		return tea.Batch(a.acceptCode(st), cmd)
	case "confirm":
		_ = a.configMgr.SaveServerSignIn(m.serverID, a.localIdentity.ID, m.email, m.token, uuid.Nil)
		if len(st.Queue) > 0 {
			next := st.Queue[0]
			queue := st.Queue[1:]
			a.openCodeScreen(codeModeConfirmEmail, next, m.email)
			a.codeState.Queue = queue
			a.codeState.Notice = "Confirmed on " + name + ". Now the code from " + a.codeServerName(next) + "."
			return nil
		}
		a.statusMessage, a.statusError = "Email changed on "+name+".", false
		return a.acceptCode(st)
	case "reset":
		p := *a.localIdentity
		p.Password = st.NewPass.Value()
		if err := a.configMgr.SaveIdentity(&p); err != nil {
			st.Notice, st.IsErr = "Reset worked, but saving the new password here failed: "+err.Error(), true
			return nil
		}
		a.localIdentity = &p
		_ = a.configMgr.SaveServerSignIn(m.serverID, p.ID, m.email, m.token, m.userID)
		summary := "Password reset on " + name + "."
		if len(m.synced) > 0 {
			summary += " Other servers: " + strings.Join(m.synced, "; ") + "."
		}
		a.statusMessage, a.statusError = summary, strings.Contains(summary, "not changed")
		// Signed in: unlock and connect, the same as entering the password.
		a.codeState = nil
		a.loginPassword.SetValue(p.Password)
		a.view = ViewLogin
		return a.handleLoginSubmit()
	}
	return nil
}

// closeCodeScreen leaves the code screen for where it was opened from (or
// the main view once signed in).
func (a *App) closeCodeScreen(done bool) tea.Cmd {
	st := a.codeState
	a.codeState = nil
	back := st.Back
	if back == ViewAccountCode || back == ViewProfiles {
		back = ViewMain
	}
	if !done && st.Mode == codeModeVerify {
		a.statusMessage = a.codeServerName(st.ServerID) + " is waiting for your email code: select it and press Enter."
		a.statusError = true
	}
	a.view = back
	if back == ViewLogin {
		a.initLoginView()
	}
	return nil
}

func (a *App) renderAccountCodeView() string {
	st := a.codeState
	now := time.Now()
	name := a.codeServerName(st.ServerID)
	bold := lipgloss.NewStyle().Bold(true)
	var b strings.Builder
	label, headline, accent := "", "", ""
	hints := []keyHint{{"Enter", "Submit"}, {"Esc", "Back"}}
	switch st.Mode {
	case codeModeVerify:
		label, headline, accent = "Verify", "Check your email", "email"
		if st.Fixing {
			headline, accent = "Fix your details", "Fix"
			b.WriteString(a.dim("Typo when you signed up? Fix it, and a new code goes to the\ncorrected address. Your profile is updated too.") + "\n\n")
			b.WriteString(a.stageField("Email", st.Focus == 0, st.FixEmail.View()) + "\n")
			b.WriteString(a.stageField("Alias", st.Focus == 1, st.FixAlias.View()) + "\n\n")
			hints = []keyHint{{"Tab", "Next"}, {"Enter", "Save and resend"}, {"Esc", "Cancel"}}
			break
		}
		b.WriteString(a.dim(name+" sent a code to ") + bold.Render(st.Email) + "\n\n")
		b.WriteString(a.renderCodeBoxes(st, now) + "\n\n")
		b.WriteString(a.renderResendBar(st, now) + "\n")
		hints = []keyHint{{"Enter", "Verify"}, {"Ctrl+E", "Wrong email?"}, {"Esc", "Later"}}
	case codeModeConfirmEmail:
		label, headline, accent = "New email", "Confirm your new email", "new email"
		b.WriteString(a.dim(name+" sent a code to ") + bold.Render(st.Email) + "\n")
		b.WriteString(a.dim("Your account there keeps the old address until you enter it.") + "\n\n")
		b.WriteString(a.renderCodeBoxes(st, now) + "\n\n")
		hints = []keyHint{{"Enter", "Confirm"}, {"Esc", "Later"}}
	case codeModeForgot:
		label, headline, accent = "Password reset", "Forgot your password?", "password"
		if st.Step == 0 {
			b.WriteString(a.dim("Which server should email you a reset code? Your new password\nis then set on every server this profile uses.") + "\n\n")
			sel := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
			for i, s := range st.Servers {
				if i == st.Pick {
					b.WriteString(sel.Render("▸ "+s.Name) + "  " + a.dim(a.emailOn(s)) + "\n")
				} else {
					b.WriteString("  " + s.Name + "\n")
				}
			}
			if len(st.Servers) == 0 {
				b.WriteString(a.dim("Add a server first.") + "\n")
			}
			hints = []keyHint{{"↑↓", "Choose"}, {"Enter", "Send code"}, {"Esc", "Back"}}
			break
		}
		if st.TempPassword {
			b.WriteString(a.dim("The temporary password from "+name+"'s admin:") + "\n")
			b.WriteString(a.stageField("Temporary", st.Focus == 0, st.Code.View()) + "\n\n")
		} else {
			b.WriteString(a.dim(name+" sent a code to ") + bold.Render(st.Email) + "\n\n")
			b.WriteString(a.renderCodeBoxes(st, now) + "\n\n")
		}
		b.WriteString(a.stageField("New", st.Focus == 1, st.NewPass.View()) + "\n")
		b.WriteString(a.stageField("Confirm", st.Focus == 2, st.Confirm.View()) + "\n")
		if st.Focus >= 1 {
			b.WriteString("\n" + a.passwordGrapes(st.NewPass.Value()) + "\n")
		}
		hints = []keyHint{{"Tab", "Next"}, {"Enter", "Reset"}, {"Ctrl+R", "Resend"}, {"Esc", "Back"}}
	}
	if st.Busy {
		b.WriteString("\n" + a.dim("Working…") + "\n")
	}
	if st.Notice != "" {
		b.WriteString("\n" + a.stageNotice(st.Notice, st.IsErr))
	}
	return a.stagePage(label, headline, accent, b.String(), hints)
}

// profileList is every profile saved on this computer.
func (a *App) profileList() []*LocalIdentity {
	list, _ := a.configMgr.Profiles()
	return list
}

// --- Profiles: change password (P) ---

// passwordChangeState is the open change-password form.
type passwordChangeState struct {
	Current, New, Confirm textinput.Model
	Focus                 int
}

// passwordChangedMsg reports a password change across the servers.
type passwordChangedMsg struct {
	newPassword string
	lines       []string
	changed     int
	total       int
}

func (a *App) startPasswordChange() {
	s := a.profilesState
	pc := &passwordChangeState{
		Current: newInput("Current password", 128, true),
		New:     newInput("New password (min 8 chars)", 128, true),
		Confirm: newInput("Confirm new password", 128, true),
	}
	pc.Current.Focus()
	s.Password, s.Notice = pc, ""
}

func (a *App) handlePasswordChangeKey(msg tea.KeyMsg) tea.Cmd {
	s := a.profilesState
	pc := s.Password
	inputs := []*textinput.Model{&pc.Current, &pc.New, &pc.Confirm}
	move := func(d int) {
		inputs[pc.Focus].Blur()
		pc.Focus = (pc.Focus + d + len(inputs)) % len(inputs)
		inputs[pc.Focus].Focus()
	}
	switch msg.String() {
	case "esc":
		s.Password, s.Notice = nil, ""
		return nil
	case "tab", "down":
		move(1)
		return nil
	case "shift+tab", "up":
		move(-1)
		return nil
	case "enter":
		if pc.Focus < len(inputs)-1 {
			move(1)
			return nil
		}
		switch {
		case pc.Current.Value() != a.localIdentity.Password:
			s.Notice, s.NoticeErr = "Your current password is wrong (Esc, then Ctrl+F on the login screen if you've forgotten it)", true
			return nil
		case len(pc.New.Value()) < 8:
			s.Notice, s.NoticeErr = "Password must be at least 8 characters", true
			return nil
		case pc.New.Value() != pc.Confirm.Value():
			s.Notice, s.NoticeErr = "Passwords do not match", true
			return nil
		}
		oldPassword, newPassword := pc.Current.Value(), pc.New.Value()
		targets := a.passwordSyncTargets(uuid.Nil)
		s.Busy, s.Notice, s.NoticeErr = true, "Changing it on your servers…", false
		return func() tea.Msg {
			lines, ok := syncPassword(targets, oldPassword, newPassword)
			return passwordChangedMsg{newPassword: newPassword, lines: lines, changed: ok, total: len(targets)}
		}
	}
	var cmd tea.Cmd
	*inputs[pc.Focus], cmd = inputs[pc.Focus].Update(msg)
	return cmd
}

// applyPasswordChanged saves the new password locally once at least one
// server took it (or there are none), and reports each server.
func (a *App) applyPasswordChanged(m passwordChangedMsg) tea.Cmd {
	s := a.profilesState
	if s == nil {
		return nil
	}
	s.Busy = false
	if m.total > 0 && m.changed == 0 {
		s.Notice, s.NoticeErr = "Nothing changed:\n"+strings.Join(m.lines, "\n"), true
		return nil
	}
	p := *a.localIdentity
	p.Password = m.newPassword
	if err := a.configMgr.SaveIdentity(&p); err != nil {
		s.Notice, s.NoticeErr = "Saving the new password failed: "+err.Error(), true
		return nil
	}
	a.localIdentity = &p
	s.Password = nil
	summary := "Password changed."
	if len(m.lines) > 0 {
		summary += "\n" + strings.Join(m.lines, "\n")
	}
	s.Notice, s.NoticeErr = summary, m.changed < m.total
	return nil
}

func (a *App) renderPasswordChange() string {
	s := a.profilesState
	pc := s.Password
	var b strings.Builder
	b.WriteString(a.dim("Changes it on this computer and on every server this profile uses.") + "\n\n")
	b.WriteString(a.stageField("Current", pc.Focus == 0, pc.Current.View()) + "\n")
	b.WriteString(a.stageField("New", pc.Focus == 1, pc.New.View()) + "\n")
	b.WriteString(a.stageField("Confirm", pc.Focus == 2, pc.Confirm.View()) + "\n")
	if pc.Focus >= 1 {
		b.WriteString("\n" + a.passwordGrapes(pc.New.Value()) + "\n")
	}
	if s.Busy {
		b.WriteString("\n" + a.dim("Working…") + "\n")
	}
	if s.Notice != "" {
		b.WriteString("\n" + a.stageNotice(s.Notice, s.NoticeErr))
	}
	return a.stagePage("Profiles", "Change your password", "password", b.String(),[]keyHint{{"Tab", "Next"}, {"Enter", "Change"}, {"Esc", "Cancel"}})
}
