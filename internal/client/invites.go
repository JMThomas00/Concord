package client

// Invites (servers v0.1.9+): joining an invite only or approval server, the
// Invite People overlay, and Server Settings › Invites.

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// JoinDetails go with sign-ins to a server that isn't letting this profile
// in yet: an invite code, and a note for a join request.
type JoinDetails struct {
	Invite string
	Note   string
}

// joinDetails is what to send with a sign-in: the code typed on the join
// screen, else one saved from Add Server.
func (sc *ServerConnection) joinDetails() JoinDetails {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	j := sc.join
	if j.Invite == "" && sc.ServerInfo != nil {
		j.Invite = sc.ServerInfo.Invite
	}
	return j
}

// Answers that mean the server didn't make this profile a member
// (internal/server/invites.go).
const (
	joinInviteRequired  = "invite_required"
	joinInviteInvalid   = "invite_invalid"
	joinApprovalPending = "approval_pending"
	joinDenied          = "join_denied"
	joinBanned          = "banned"
)

func isJoinCode(code string) bool {
	switch code {
	case joinInviteRequired, joinInviteInvalid, joinApprovalPending, joinDenied, joinBanned:
		return true
	}
	return false
}

// splitInvite reads what was pasted into Add Server's address: an invite
// string host:port/CODE gives the address and the code; anything else is
// just an address.
func splitInvite(s string) (address, code string) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "concord://"), "https://")
	i := strings.LastIndex(s, "/")
	if i < 0 {
		return s, ""
	}
	addr, rest := s[:i], cleanInviteCode(s[i+1:])
	if len(rest) != 8 {
		return strings.TrimSuffix(s, "/"), ""
	}
	if j := strings.Index(addr, "/"); j >= 0 { // host:port/invite/CODE
		addr = addr[:j]
	}
	return addr, rest
}

// cleanInviteCode is a code as the server reads it: letters and digits,
// upper case.
func cleanInviteCode(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, s)
}

// inviteString is the one thing to paste: address:port/CODE.
func inviteString(info *ClientServerInfo, code string) string {
	if info == nil {
		return code
	}
	return info.Address + ":" + strconv.Itoa(info.Port) + "/" + code
}

// ── Joining: the join screen ─────────────────────────────────────────────────

// joinRetryEvery is how often a request waiting for a moderator is checked.
const joinRetryEvery = 30 * time.Second

// joinState is a server that didn't let this profile in yet.
type joinState struct {
	code      string // invite_required, invite_invalid, approval_pending, join_denied, banned
	message   string // the server's words
	dismissed bool   // the join screen was put away: don't open it by itself again
	retrying  bool   // a check is scheduled (approval_pending)
}

type joinRetryMsg struct{ serverID uuid.UUID }

// joinBlocked takes a sign-in the server answered with a join refusal.
func (a *App) joinBlocked(msg AutoConnectMsg) tea.Cmd {
	if a.pendingJoin == nil {
		a.pendingJoin = map[uuid.UUID]*joinState{}
	}
	js := a.pendingJoin[msg.ServerID]
	if js == nil {
		js = &joinState{}
		a.pendingJoin[msg.ServerID] = js
	}
	js.code = msg.Join
	if msg.Err != nil {
		js.message = msg.Err.Error()
	}
	name := a.codeServerName(msg.ServerID)
	a.statusMessage, a.statusError = name+": "+js.message, msg.Join != joinApprovalPending

	if st := a.codeState; st != nil && st.Mode == codeModeJoin && st.ServerID == msg.ServerID {
		st.Busy = false
		st.Notice, st.IsErr = js.message, msg.Join == joinInviteInvalid || msg.Join == joinBanned
		if msg.Join == joinApprovalPending {
			st.Notice, st.IsErr = "", false
		}
	} else if a.view == ViewMain && a.codeState == nil && !js.dismissed {
		a.openJoinScreen(msg.ServerID)
	}
	if msg.Join == joinApprovalPending && !js.retrying {
		js.retrying = true
		id := msg.ServerID
		return tea.Tick(joinRetryEvery, func(time.Time) tea.Msg { return joinRetryMsg{id} })
	}
	return nil
}

// joinRetry checks again whether a moderator let this profile in.
func (a *App) joinRetry(m joinRetryMsg) tea.Cmd {
	js := a.pendingJoin[m.serverID]
	if js == nil {
		return nil
	}
	js.retrying = false
	if js.code != joinApprovalPending || a.localIdentity == nil {
		return nil
	}
	return a.autoConnectServer(m.serverID)
}

// joinedServer: a sign-in worked, so whatever was waiting is done.
func (a *App) joinedServer(id uuid.UUID) {
	if a.connMgr != nil {
		a.connMgr.SetJoin(id, JoinDetails{})
		if sc := a.connMgr.GetConnection(id); sc != nil && sc.ServerInfo != nil {
			sc.mu.Lock()
			sc.ServerInfo.Invite = ""
			sc.mu.Unlock()
		}
	}
	if s := a.serverByID(id); s != nil {
		s.Invite = ""
	}
	if _, ok := a.pendingJoin[id]; !ok {
		return
	}
	delete(a.pendingJoin, id)
	a.statusMessage, a.statusError = "Welcome to "+a.codeServerName(id)+"!", false
	if st := a.codeState; st != nil && st.Mode == codeModeJoin && st.ServerID == id {
		a.codeState = nil
		a.view = ViewMain
		a.celebrate()
	}
}

// openJoinScreen asks for an invite code (or shows the wait for a
// moderator) for a server that hasn't let this profile in.
func (a *App) openJoinScreen(id uuid.UUID) {
	a.openCodeScreen(codeModeJoin, id, "")
	st := a.codeState
	st.SentAt = time.Time{}
	st.Code = newInput("ABCD-EFGH, or the whole invite (address/CODE)", 160, false)
	st.Note = newInput("A note for the moderators (optional)", 200, false)
	st.Code.Focus()
	if js := a.pendingJoin[id]; js != nil {
		js.dismissed = false
		if js.code == joinInviteInvalid || js.code == joinBanned {
			st.Notice, st.IsErr = js.message, true
		}
	}
}

func (a *App) joinScreenClosed(id uuid.UUID) {
	js := a.pendingJoin[id]
	if js == nil {
		return
	}
	js.dismissed = true
	name := a.codeServerName(id)
	if js.code == joinApprovalPending {
		a.statusMessage, a.statusError = name+" is waiting for a moderator; Concord checks every 30 seconds. Select it for the invite box.", false
	} else {
		a.statusMessage, a.statusError = name+" needs an invite: select it and press Enter.", true
	}
}

func (a *App) handleJoinKey(msg tea.KeyMsg) tea.Cmd {
	st := a.codeState
	js := a.pendingJoin[st.ServerID]
	waiting := js != nil && js.code == joinApprovalPending
	inputs := []*textinput.Model{&st.Code}
	if waiting {
		inputs = append(inputs, &st.Note)
	}
	if st.Focus >= len(inputs) {
		st.Focus = 0
	}
	move := func(d int) {
		inputs[st.Focus].Blur()
		st.Focus = (st.Focus + d + len(inputs)) % len(inputs)
		inputs[st.Focus].Focus()
	}
	switch msg.String() {
	case "tab", "down":
		move(1)
		return nil
	case "shift+tab", "up":
		move(-1)
		return nil
	case "ctrl+r":
		return a.submitJoin("", st.Note.Value())
	case "enter":
		code := strings.TrimSpace(st.Code.Value())
		if code != "" {
			if c := cleanInviteCode(code); len(c) != 8 {
				st.Notice, st.IsErr = "An invite code is 8 characters, like ABCD-EFGH.", true
				return nil
			}
		}
		if code == "" && !waiting {
			st.Notice, st.IsErr = "Type or paste the invite code you were sent.", true
			return nil
		}
		return a.submitJoin(code, st.Note.Value())
	}
	var cmd tea.Cmd
	*inputs[st.Focus], cmd = inputs[st.Focus].Update(msg)
	return cmd
}

// submitJoin signs in again with a code (and note).
func (a *App) submitJoin(code, note string) tea.Cmd {
	st := a.codeState
	if a.connMgr == nil {
		return nil
	}
	j := JoinDetails{Invite: cleanInviteCode(code), Note: strings.TrimSpace(note)}
	a.connMgr.SetJoin(st.ServerID, j)
	st.Busy, st.Notice = true, ""
	return a.autoConnectServer(st.ServerID)
}

func (a *App) renderJoinScreen(st *AccountCodeState, b *strings.Builder) (label, headline, accent string, hints []keyHint) {
	name := a.codeServerName(st.ServerID)
	js := a.pendingJoin[st.ServerID]
	code := joinInviteRequired
	if js != nil {
		code = js.code
	}
	bold := lipgloss.NewStyle().Bold(true)
	label, accent = "Join", "invite"
	hints = []keyHint{{"Enter", "Join"}, {"Esc", "Later"}}
	switch code {
	case joinApprovalPending:
		headline, accent = "Waiting to be let in", "moderator"
		b.WriteString(a.dim("Your request to join ") + bold.Render(name) + a.dim(" is waiting for a moderator.\nConcord checks every 30 seconds, so you can leave this open or come back later.") + "\n\n")
		b.WriteString(a.stageField("Note", st.Focus == 1, st.Note.View()) + "\n")
		b.WriteString(a.dim("Have an invite? It skips the wait:") + "\n")
		b.WriteString(a.stageField("Invite", st.Focus == 0, st.Code.View()) + "\n")
		hints = []keyHint{{"Tab", "Next"}, {"Enter", "Send"}, {"Ctrl+R", "Check now"}, {"Esc", "Later"}}
	case joinDenied:
		headline = "Not this time"
		b.WriteString(a.dim("Your request to join ") + bold.Render(name) + a.dim(" was turned down.\nIf a member sends you an invite, enter it here.") + "\n\n")
		b.WriteString(a.stageField("Invite", true, st.Code.View()) + "\n")
	case joinBanned:
		headline, accent = "You're banned", "banned"
		b.WriteString(a.dim("You're banned from ") + bold.Render(name) + a.dim(". An invite doesn't change that.") + "\n")
		hints = []keyHint{{"Esc", "Back"}}
	default:
		headline = name + " is invite only"
		b.WriteString(a.dim("Paste the invite you were sent (it looks like address:port/ABCDEFGH)\nor type its code. Ask a member if you don't have one.") + "\n\n")
		b.WriteString(a.stageField("Invite", true, st.Code.View()) + "\n")
	}
	return
}

// takePastedInvite turns an invite string pasted into Add Server's address
// (host:port/CODE) into the address, port and the code to sign up with.
// A name is filled in from the host if none was typed.
func (a *App) takePastedInvite() {
	v := a.addServerAddress.Value()
	if !strings.Contains(v, "/") {
		return
	}
	addr, code := splitInvite(v)
	if code == "" {
		return
	}
	host, port := addr, ""
	if i := strings.LastIndex(addr, ":"); i > 0 && !strings.Contains(addr[i+1:], "]") {
		host, port = addr[:i], addr[i+1:]
	}
	a.addServerAddress.SetValue(host)
	a.addServerAddress.CursorEnd()
	if port != "" {
		a.addServerPort.SetValue(port)
	}
	if strings.TrimSpace(a.addServerName.Value()) == "" {
		a.addServerName.SetValue(host)
	}
	a.addServerInvite = code
}

// Tags a Grapevine listing carries for its join mode
// (internal/server/grapevine.go).
const (
	tagInviteOnly = "invite-only"
	tagApproval   = "approval"
)

// joinBadge is the server browser's mark for a listing's join mode.
func joinBadge(tags []string) string {
	for _, t := range tags {
		switch t {
		case tagInviteOnly:
			return "(invite)"
		case tagApproval:
			return "(ask)"
		}
	}
	return ""
}

// joinHow says how a listed server lets people in ("" when it's open).
func joinHow(tags []string) string {
	for _, t := range tags {
		switch t {
		case tagInviteOnly:
			return "invite only: you'll need a code from a member"
		case tagApproval:
			return "by approval: you ask, a moderator lets you in"
		}
	}
	return ""
}

// plainTags is a listing's tags without the join mode's.
func plainTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if t != tagInviteOnly && t != tagApproval {
			out = append(out, t)
		}
	}
	return out
}
