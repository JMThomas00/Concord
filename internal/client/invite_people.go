package client

// Invite People (/invite, I in the members panel; servers v0.1.9+): make an
// invite code, email one, see your invites (moderators: everyone's) and
// answer join requests. The same view sits under the join settings on
// Server Settings › Invites (server_invites.go).

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/concord-chat/concord/internal/protocol"
)

// Expiry and use-limit choices.
var (
	inviteAges = []struct {
		label string
		secs  int
	}{{"1 hour", 3600}, {"1 day", 86400}, {"7 days", 7 * 86400}, {"Never", 0}}
	inviteUses = []struct {
		label string
		n     int
	}{{"Unlimited", 0}, {"1 use", 1}, {"5 uses", 5}, {"10 uses", 10}, {"25 uses", 25}}
)

const defaultInviteAge = 2 // 7 days

// Tabs of the view.
const (
	inviteTabMake = iota
	inviteTabEmail
	inviteTabList
	inviteTabRequests
)

type invitesView struct {
	sc       *ServerConnection
	p        *protocol.InvitesPayload // nil until the server answers
	tab      int
	field    int // make/email: the row with focus
	cursor   int // list/requests
	age, use int
	email    textinput.Model
	note     textinput.Model
	lastCode string // the invite just made
	confirm  string // a code or user ID waiting for a second press
	notice   string
	isErr    bool
	busy     bool
}

func newInvitesView(sc *ServerConnection) *invitesView {
	v := &invitesView{sc: sc, age: defaultInviteAge}
	v.email = newInput("friend@example.com", 254, false)
	v.note = newInput("A line for them (optional)", 200, false)
	return v
}

// tabs is the tabs this person gets: email only where the server can send
// it, requests only for moderators.
func (v *invitesView) tabs() []int {
	t := []int{}
	if v.p == nil || v.p.CanInvite {
		t = append(t, inviteTabMake)
		if v.p != nil && v.p.MailEnabled {
			t = append(t, inviteTabEmail)
		}
	}
	t = append(t, inviteTabList)
	if v.p != nil && v.p.CanManage {
		t = append(t, inviteTabRequests)
	}
	return t
}

func inviteTabName(t int, v *invitesView) string {
	switch t {
	case inviteTabMake:
		return "Make a code"
	case inviteTabEmail:
		return "Email an invite"
	case inviteTabList:
		if v.p != nil && v.p.CanManage {
			return "All invites"
		}
		return "Your invites"
	case inviteTabRequests:
		n := 0
		if v.p != nil {
			n = len(v.p.Requests)
		}
		return fmt.Sprintf("Requests (%d)", n)
	}
	return ""
}

// sendInvites sends one OpInvites request for v's server.
func (a *App) sendInvites(v *invitesView, req protocol.InvitesRequest) {
	if v == nil || v.sc == nil {
		return
	}
	req.ServerID = wireServerID(v.sc)
	msg, err := protocol.NewMessage(protocol.OpInvites, req)
	if err != nil {
		return
	}
	v.sc.mu.RLock()
	conn := v.sc.Connection
	v.sc.mu.RUnlock()
	if conn == nil {
		v.notice, v.isErr = "Not connected", true
		return
	}
	if err := conn.Send(msg); err != nil {
		v.notice, v.isErr = "Couldn't send: "+err.Error(), true
		return
	}
	if req.Action != protocol.InvitesList {
		v.busy = true
	}
}

// openInvitePeople opens the overlay for the server on screen.
func (a *App) openInvitePeople() {
	sc := a.activeConn
	if sc == nil {
		a.statusMessage, a.statusError = "Not connected to a server", true
		return
	}
	if !keepsReads(sc) {
		a.statusMessage, a.statusError = serverName(sc)+" is older than v0.1.9, so it has no invites", true
		return
	}
	a.invitePeople = newInvitesView(sc)
	a.sendInvites(a.invitePeople, protocol.InvitesRequest{Action: protocol.InvitesList})
}

// invitesViews is every open invites view for sc (the overlay, Server
// Settings › Invites).
func (a *App) invitesViews(sc *ServerConnection) []*invitesView {
	var out []*invitesView
	if v := a.invitePeople; v != nil && v.sc == sc {
		out = append(out, v)
	}
	if s := a.serverManagementState; s != nil && s.Invites != nil && s.Invites.view.sc == sc {
		out = append(out, s.Invites.view)
	}
	return out
}

// handleInvitesEvent takes INVITES, INVITES_RESULT and JOIN_REQUEST.
func (a *App) handleInvitesEvent(sc *ServerConnection, msg *protocol.Message) {
	switch msg.Type {
	case protocol.EventInvites:
		var p protocol.InvitesPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			return
		}
		for _, v := range a.invitesViews(sc) {
			v.p = &p
			tabs := v.tabs()
			if !containsInt(tabs, v.tab) {
				v.tab = tabs[0]
			}
			v.cursor = min(v.cursor, max(0, v.listLen()-1))
		}
		if s := a.serverManagementState; s != nil && s.Invites != nil && s.Invites.view.sc == sc {
			s.Invites.loaded(&p)
		}
	case protocol.EventInvitesResult:
		var r protocol.InvitesResult
		if err := json.Unmarshal(msg.Data, &r); err != nil {
			return
		}
		for _, v := range a.invitesViews(sc) {
			v.busy = false
			v.notice, v.isErr = r.Message, !r.OK
			if r.OK && (r.Action == protocol.InvitesCreate || r.Action == protocol.InvitesEmail) {
				v.lastCode = r.Code
				if r.Action == protocol.InvitesCreate {
					v.notice = "Invite made: " + v.invite(r.Code) + "  (C copies it)"
				} else {
					v.email.SetValue("")
					v.note.SetValue("")
				}
			}
		}
	case protocol.EventJoinRequest:
		var p protocol.JoinRequestPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			return
		}
		a.toasts = append(a.toasts, &toast{label: "JOIN REQUEST", title: p.Username + " asked to join " + serverName(sc), brief: false})
		a.statusMessage, a.statusError = fmt.Sprintf("%s asked to join %s (%d waiting): /invite to answer", p.Username, serverName(sc), p.Pending), false
		for _, v := range a.invitesViews(sc) {
			a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesList})
		}
	}
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

func (v *invitesView) listLen() int {
	if v.p == nil {
		return 0
	}
	if v.tab == inviteTabRequests {
		return len(v.p.Requests)
	}
	return len(v.p.Invites)
}

// handleInvitesKey: Tab changes tab; the rest depends on it. done: Esc
// was pressed (the caller closes).
func (a *App) handleInvitesKey(v *invitesView, msg tea.KeyMsg) (done bool, cmd tea.Cmd) {
	key := msg.String()
	confirm := v.confirm
	v.confirm = ""
	tabs := v.tabs()
	idx := 0
	for i, t := range tabs {
		if t == v.tab {
			idx = i
		}
	}
	switch key {
	case "esc":
		return true, nil
	case "tab":
		v.tab, v.cursor, v.field, v.notice = tabs[(idx+1)%len(tabs)], 0, 0, ""
		v.focusFields()
		return false, nil
	case "shift+tab":
		v.tab, v.cursor, v.field, v.notice = tabs[(idx+len(tabs)-1)%len(tabs)], 0, 0, ""
		v.focusFields()
		return false, nil
	}
	if v.busy {
		return false, nil
	}

	switch v.tab {
	case inviteTabMake:
		switch key {
		case "up":
			v.field = max(0, v.field-1)
		case "down":
			v.field = min(1, v.field+1)
		case "left", "right":
			d := 1
			if key == "left" {
				d = -1
			}
			if v.field == 0 {
				v.age = (v.age + d + len(inviteAges)) % len(inviteAges)
			} else {
				v.use = (v.use + d + len(inviteUses)) % len(inviteUses)
			}
		case "enter":
			v.notice = ""
			a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesCreate, MaxAge: inviteAges[v.age].secs, MaxUses: inviteUses[v.use].n})
		case "c", "C":
			a.copyInvite(v, v.lastCode, false)
		case "o", "O":
			a.copyInvite(v, v.lastCode, true)
		}
	case inviteTabEmail:
		switch key {
		case "up":
			v.field = max(0, v.field-1)
			v.focusFields()
			return false, nil
		case "down":
			v.field = min(2, v.field+1)
			v.focusFields()
			return false, nil
		case "enter":
			if strings.TrimSpace(v.email.Value()) == "" {
				v.notice, v.isErr = "Type their email address", true
				return false, nil
			}
			addr := ""
			if info := v.sc.ServerInfo; info != nil {
				addr = info.Address + ":" + fmt.Sprint(info.Port)
			}
			v.notice, v.isErr = "Sending…", false
			a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesEmail, Email: v.email.Value(), Note: v.note.Value(), MaxAge: inviteAges[v.age].secs, Address: addr})
			return false, nil
		}
		switch v.field {
		case 0:
			v.email, cmd = v.email.Update(msg)
		case 1:
			v.note, cmd = v.note.Update(msg)
		case 2:
			if key == "left" {
				v.age = (v.age + len(inviteAges) - 1) % len(inviteAges)
			} else if key == "right" {
				v.age = (v.age + 1) % len(inviteAges)
			}
		}
		return false, cmd
	case inviteTabList, inviteTabRequests:
		n := v.listLen()
		switch key {
		case "up":
			v.cursor = max(0, v.cursor-1)
		case "down":
			v.cursor = min(max(0, n-1), v.cursor+1)
		}
		if v.p == nil || v.cursor >= n {
			return false, nil
		}
		if v.tab == inviteTabList {
			in := v.p.Invites[v.cursor]
			switch key {
			case "c", "C":
				a.copyInvite(v, in.Code, false)
			case "o", "O":
				a.copyInvite(v, in.Code, true)
			case "x", "X", "delete":
				if confirm == in.Code {
					a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesRevoke, Code: in.Code})
				} else {
					v.confirm = in.Code
					v.notice, v.isErr = "Press X again to cancel "+codeShown(in.Code)+" · any other key keeps it", true
				}
			}
		} else {
			r := v.p.Requests[v.cursor]
			switch key {
			case "a", "A", "enter":
				a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesApprove, UserID: r.UserID})
			case "d", "D":
				if confirm == r.UserID.String() {
					a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesDeny, UserID: r.UserID})
				} else {
					v.confirm = r.UserID.String()
					v.notice, v.isErr = "Press D again to turn "+r.Username+" down · any other key cancels", true
				}
			}
		}
	}
	return false, nil
}

func (v *invitesView) focusFields() {
	v.email.Blur()
	v.note.Blur()
	if v.tab == inviteTabEmail {
		switch v.field {
		case 0:
			v.email.Focus()
		case 1:
			v.note.Focus()
		}
	}
}

func (a *App) copyInvite(v *invitesView, code string, overTor bool) {
	if code == "" {
		return
	}
	s := v.invite(code)
	if overTor {
		if s = v.torInvite(code); s == "" {
			return
		}
	}
	if err := clipboard.WriteAll(s); err != nil {
		v.notice, v.isErr = "Couldn't copy ("+err.Error()+"): "+s, true
		return
	}
	v.notice, v.isErr = "Copied "+s, false
}

func codeShown(code string) string {
	if len(code) == 8 {
		return code[:4] + "-" + code[4:]
	}
	return code
}

// handleInvitePeopleKey is the overlay's keys.
func (a *App) handleInvitePeopleKey(msg tea.KeyMsg) tea.Cmd {
	done, cmd := a.handleInvitesKey(a.invitePeople, msg)
	if done {
		a.invitePeople = nil
	}
	return cmd
}

// whenLeft is how long until t, briefly ("" for never).
func whenLeft(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "never expires"
	}
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "expired"
	case d < time.Hour:
		return fmt.Sprintf("%dm left", int(d.Minutes())+1)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh left", int(d.Hours()))
	}
	return fmt.Sprintf("%dd left", int(d.Hours()/24))
}

// renderInvitesBody draws v's tabs and current tab in width.
func (a *App) renderInvitesBody(v *invitesView, width, rows int) string {
	c := a.theme.Colors
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Background)).Background(lipgloss.Color(c.Purple)).Bold(true)
	sel := lipgloss.NewStyle().Background(lipgloss.Color(c.Selection)).Foreground(lipgloss.Color(c.Foreground))
	hot := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Red))
	var b strings.Builder

	var tabs []string
	for _, t := range v.tabs() {
		if t == v.tab {
			tabs = append(tabs, on.Render(" "+inviteTabName(t, v)+" "))
		} else {
			tabs = append(tabs, dim.Render(" "+inviteTabName(t, v)+" "))
		}
	}
	b.WriteString(strings.Join(tabs, " ") + "\n\n")

	if v.p == nil {
		b.WriteString(dim.Render("  Asking the server…") + "\n")
		return b.String()
	}
	chooser := func(focused bool, label, value string) string {
		l := fmt.Sprintf("  %-9s", label)
		val := "◂ " + value + " ▸"
		if focused {
			return hot.Render("▸ "+l[2:]) + sel.Render(" "+val+" ")
		}
		return l + val
	}
	field := func(focused bool, label string, in textinput.Model) string {
		l := fmt.Sprintf("%-9s", label)
		if focused {
			return hot.Render("▸ "+l) + in.View()
		}
		return "  " + l + in.View()
	}
	hint := ""
	switch v.tab {
	case inviteTabMake:
		b.WriteString(dim.Render("  Anyone with the code can join until it runs out. Share the whole line,") + "\n")
		b.WriteString(dim.Render("  which Add Server reads as the address and the code.") + "\n\n")
		b.WriteString(chooser(v.field == 0, "Expires", inviteAges[v.age].label) + "\n")
		b.WriteString(chooser(v.field == 1, "Uses", inviteUses[v.use].label) + "\n")
		if v.lastCode != "" {
			b.WriteString("\n  " + green.Render(v.invite(v.lastCode)) + "\n")
			if t := v.torInvite(v.lastCode); t != "" {
				b.WriteString(dim.Render("  over Tor: ") + green.Render(t) + "\n")
			}
		}
		hint = "↑↓ choose · ←→ change · Enter make it · C copy · Tab next tab · Esc close"
		if v.p != nil && v.torInvite("X") != "" {
			hint = "↑↓ choose · ←→ change · Enter make it · C copy · O copy for Tor · Tab next tab · Esc close"
		}
	case inviteTabEmail:
		from := "the server's email address"
		if v.p.MailFrom != "" {
			from = v.p.MailFrom
		}
		b.WriteString(dim.Render("  Sent from "+from+". It works once, only for that address, and") + "\n")
		b.WriteString(dim.Render("  counts as confirming it, so they skip the verification code.") + "\n\n")
		b.WriteString(field(v.field == 0, "Email", v.email) + "\n")
		b.WriteString(field(v.field == 1, "Note", v.note) + "\n")
		b.WriteString(chooser(v.field == 2, "Expires", inviteAges[v.age].label) + "\n")
		hint = "↑↓ field · Enter send · Tab next tab · Esc close"
	case inviteTabList:
		if len(v.p.Invites) == 0 {
			b.WriteString(dim.Render("  No invites yet.") + "\n")
		}
		now := time.Now()
		start := min(max(0, v.cursor-rows/2), max(0, len(v.p.Invites)-rows))
		for i := start; i < len(v.p.Invites) && i < start+rows; i++ {
			in := v.p.Invites[i]
			uses := fmt.Sprintf("%d used", in.Uses)
			if in.MaxUses > 0 {
				uses = fmt.Sprintf("%d/%d used", in.Uses, in.MaxUses)
			}
			who := in.InviterName
			if in.Email != "" {
				who += " → " + in.Email
			}
			left := whenLeft(in.ExpiresAt, now)
			if !in.Usable {
				left = "done"
				if in.Uses < in.MaxUses || in.MaxUses == 0 {
					left = "expired"
				}
			}
			line := fmt.Sprintf("%-10s %-12s %-14s %s", codeShown(in.Code), uses, left, who)
			line = ansi.Truncate(line, width-4, "…")
			switch {
			case i == v.cursor:
				b.WriteString("▸ " + sel.Render(line) + "\n")
			case !in.Usable:
				b.WriteString("  " + dim.Render(line) + "\n")
			default:
				b.WriteString("  " + line + "\n")
			}
		}
		hint = "↑↓ choose · C copy · X cancel · Tab next tab · Esc close"
		if v.p != nil && v.torInvite("X") != "" {
			hint = "↑↓ choose · C copy · O copy for Tor · X cancel · Tab next tab · Esc close"
		}
	case inviteTabRequests:
		if len(v.p.Requests) == 0 {
			b.WriteString(dim.Render("  Nobody is waiting.") + "\n")
		}
		for i, r := range v.p.Requests {
			line := r.Username
			if r.Email != "" {
				line += " (" + r.Email + ")"
			}
			line += "  " + dim.Render(r.CreatedAt.In(viewerZone).Format("Jan 2 15:04"))
			if r.Note != "" {
				line += "\n      “" + r.Note + "”"
			}
			if i == v.cursor {
				b.WriteString(hot.Render("▸ ") + line + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		hint = "↑↓ choose · A let in · D turn down · Tab next tab · Esc close"
	}
	if v.busy {
		b.WriteString("\n" + dim.Render("  Working…") + "\n")
	} else if v.notice != "" {
		st := green
		if v.isErr {
			st = red
		}
		b.WriteString("\n  " + st.Render(ansi.Truncate(v.notice, width-4, "…")) + "\n")
	}
	b.WriteString("\n" + dim.Render(ansi.Truncate(hint, width, "…")))
	return b.String()
}

// renderInvitePeople is the overlay.
func (a *App) renderInvitePeople() string {
	v := a.invitePeople
	c := a.theme.Colors
	w := min(96, max(56, a.width-10))
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true).Render("Invite people to " + serverName(v.sc))
	body := a.renderInvitesBody(v, w-4, max(4, a.height-20))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(c.Purple)).
		Padding(0, 1).
		Width(w - 2).
		Background(lipgloss.Color(c.Background)).
		Render(title + "\n\n" + body)
}

// invite is the invite to paste: the normal one, or the Tor one when the
// server is reachable only through Tor (or this client reaches it that way).
func (v *invitesView) invite(code string) string {
	if v.p != nil && v.p.Onion != "" {
		onion := v.p.Onion + "/" + code
		if v.p.OnionKey != "" {
			onion += "#" + v.p.OnionKey
		}
		if v.p.TorOnly || (v.sc.ServerInfo != nil && strings.HasSuffix(strings.ToLower(v.sc.ServerInfo.Address), ".onion")) {
			return onion
		}
	}
	return inviteString(v.sc.ServerInfo, code)
}

// torInvite is the Tor invite (host.onion:port/CODE#KEY) when it isn't
// already the one invite shows; "" otherwise.
func (v *invitesView) torInvite(code string) string {
	if v.p == nil || v.p.Onion == "" {
		return ""
	}
	t := v.p.Onion + "/" + code
	if v.p.OnionKey != "" {
		t += "#" + v.p.OnionKey
	}
	if t == v.invite(code) {
		return ""
	}
	return t
}
