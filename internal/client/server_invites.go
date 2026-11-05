package client

// Server Settings › Invites (administrators; servers v0.1.9+): how people
// join (open, invite only, approval), the email verification switch, a
// test email, and every invite and join request (invite_people.go).

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/concord-chat/concord/internal/protocol"
)

var joinModes = []struct{ mode, label, about string }{
	{protocol.JoinOpen, "Open", "anyone who can reach the server may sign up"},
	{protocol.JoinInvite, "Invite only", "signing up needs an invite code"},
	{protocol.JoinApproval, "Approval", "anyone may ask; a moderator lets them in (an invite skips the wait)"},
}

// Rows above the invites view.
const (
	srvInvRowMode = iota
	srvInvRowVerify
	srvInvRowTestMail
	srvInvRows
)

type serverInvitesState struct {
	view    *invitesView
	row     int  // srvInvRows: the invites view has the keys
	pick    int  // the join mode being chosen
	tooOld  bool // before v0.1.9
	confirm bool // turning verification off: press again
}

// openServerInvites starts the page for the server on screen.
func (a *App) openServerInvites() tea.Cmd {
	s := a.serverManagementState
	if s == nil || a.activeConn == nil {
		return nil
	}
	if s.Invites == nil || s.Invites.view.sc != a.activeConn {
		s.Invites = &serverInvitesState{view: newInvitesView(a.activeConn)}
		s.Invites.view.tab = inviteTabList
	}
	s.Invites.tooOld = !keepsReads(a.activeConn)
	if !s.Invites.tooOld {
		a.sendInvites(s.Invites.view, protocol.InvitesRequest{Action: protocol.InvitesList})
	}
	return nil
}

// loaded takes the server's answer: the join mode chooser starts at the
// server's mode.
func (st *serverInvitesState) loaded(p *protocol.InvitesPayload) {
	if st.view.tab == inviteTabMake || st.view.tab == inviteTabEmail {
		st.view.tab = inviteTabList
	}
	for i, m := range joinModes {
		if m.mode == p.JoinMode {
			st.pick = i
		}
	}
}

// handleServerInvitesKey: ↑↓ move through the settings into the list;
// ←→ choose a join mode, Enter applies it; Enter on the switch flips it.
func (a *App) handleServerInvitesKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	s := a.serverManagementState
	if s == nil || s.Invites == nil || !s.FocusOnForm {
		return false, nil
	}
	st := s.Invites
	v := st.view
	key := msg.String()
	if st.tooOld || v.p == nil || !v.p.CanSettings {
		return false, nil
	}
	confirm := st.confirm
	st.confirm = false
	if st.row >= srvInvRows {
		if key == "up" && v.cursor == 0 {
			st.row = srvInvRows - 1
			return true, nil
		}
		if key == "esc" {
			return false, nil
		}
		_, cmd := a.handleInvitesKey(v, msg)
		return true, cmd
	}
	switch key {
	case "up":
		st.row = max(0, st.row-1)
	case "down":
		st.row++
	case "left", "right":
		if st.row == srvInvRowMode {
			d := 1
			if key == "left" {
				d = -1
			}
			st.pick = (st.pick + d + len(joinModes)) % len(joinModes)
		}
	case "enter", " ":
		switch st.row {
		case srvInvRowMode:
			mode := joinModes[st.pick].mode
			if mode == v.p.JoinMode {
				return true, nil
			}
			a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesSettings, JoinMode: &mode})
		case srvInvRowVerify:
			if !v.p.MailEnabled {
				v.notice, v.isErr = "Email isn't set up on this server (the [mail] settings, or concord-server --reconfigure)", true
				return true, nil
			}
			on := !v.p.RequireVerification
			if !on && !confirm {
				st.confirm = true
				v.notice, v.isErr = "Press Enter again to stop checking new accounts' email addresses · any other key keeps it on", true
				return true, nil
			}
			a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesSettings, RequireVerification: &on})
		case srvInvRowTestMail:
			if !v.p.MailEnabled {
				v.notice, v.isErr = "Email isn't set up on this server", true
				return true, nil
			}
			v.notice, v.isErr = "Sending…", false
			a.sendInvites(v, protocol.InvitesRequest{Action: protocol.InvitesTestMail})
		}
	default:
		return false, nil
	}
	return true, nil
}

func (a *App) renderServerInvitesContent(width, height int) string {
	s := a.serverManagementState
	c := a.theme.Colors
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	head := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	hot := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	sel := lipgloss.NewStyle().Background(lipgloss.Color(c.Selection)).Foreground(lipgloss.Color(c.Foreground))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green))
	inner := max(20, width-4)
	var b strings.Builder
	b.WriteString(head.Render("Invites & Joining") + "\n\n")
	st := s.Invites
	switch {
	case st == nil:
	case st.tooOld:
		b.WriteString(dim.Render("This server is older than v0.1.9, so it has no invites.") + "\n")
	case st.view.p == nil:
		b.WriteString(dim.Render("Asking the server…") + "\n")
	case !st.view.p.CanSettings:
		b.WriteString(dim.Render("Only administrators change how people join. Use /invite to make invites.") + "\n")
	default:
		p := st.view.p
		focus := func(row int) bool { return s.FocusOnForm && st.row == row }
		mark := func(row int, label string) string {
			if focus(row) {
				return hot.Render("▸ " + label)
			}
			return "  " + label
		}
		// Join mode
		var opts []string
		for i, m := range joinModes {
			o := " " + m.label + " "
			if m.mode == p.JoinMode {
				o = "●" + m.label + " "
			}
			if i == st.pick && focus(srvInvRowMode) {
				o = sel.Render(o)
			}
			opts = append(opts, o)
		}
		b.WriteString(mark(srvInvRowMode, "How people join  ") + strings.Join(opts, " ") + "\n")
		b.WriteString(dim.Render("                   "+joinModes[st.pick].about) + "\n\n")
		// Verification
		state := "off"
		if p.RequireVerification {
			state = green.Render("on")
		}
		b.WriteString(mark(srvInvRowVerify, "Verify emails    ") + state + dim.Render("  new accounts confirm their address with a code") + "\n")
		mail := "not set up: add [mail] to concord-server.toml (or --reconfigure)"
		if p.MailEnabled {
			mail = "sending from " + p.MailFrom
		}
		b.WriteString(mark(srvInvRowTestMail, "Send test email  ") + dim.Render(mail) + "\n")
		if !p.CanSaveSettings {
			b.WriteString(dim.Render("  (this server has no settings file, so the switch lasts until it restarts)") + "\n")
		}
		b.WriteString("\n")
		lv := *st.view
		if st.row < srvInvRows {
			lv.notice = st.view.notice // shown below the list either way
		}
		b.WriteString(a.renderInvitesBody(&lv, inner, max(3, height-18)))
		if st.row < srvInvRows {
			b.WriteString("\n" + dim.Render("↑↓ move · ←→ choose · Enter apply · ↓ to the invites · Esc back"))
		}
	}
	return lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1).Render(b.String())
}
