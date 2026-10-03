package client

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// initIdentitySetupForm resets the identity setup form to its initial state
func (a *App) initIdentitySetupForm() {
	a.identityAlias.Reset()
	a.identityEmail.Reset()
	a.identityPassword.Reset()
	a.identityPasswordConfirm.Reset()
	a.identityError = ""
	a.identityFocus = 0
	a.identityAlias.Focus()
	a.identityEmail.Blur()
	a.identityPassword.Blur()
	a.identityPasswordConfirm.Blur()
}

// updateIdentitySetupForm routes tea messages to the focused identity form field
func (a *App) updateIdentitySetupForm(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch a.identityFocus {
	case 0:
		a.identityAlias, cmd = a.identityAlias.Update(msg)
	case 1:
		a.identityEmail, cmd = a.identityEmail.Update(msg)
	case 2:
		a.identityPassword, cmd = a.identityPassword.Update(msg)
	case 3:
		a.identityPasswordConfirm, cmd = a.identityPasswordConfirm.Update(msg)
	}
	return cmd
}

// cycleIdentityFocus advances or reverses focus among the identity form fields
func (a *App) cycleIdentityFocus(reverse bool) {
	a.identityAlias.Blur()
	a.identityEmail.Blur()
	a.identityPassword.Blur()
	a.identityPasswordConfirm.Blur()

	if reverse {
		a.identityFocus--
		if a.identityFocus < 0 {
			a.identityFocus = 3
		}
	} else {
		a.identityFocus = (a.identityFocus + 1) % 4
	}

	switch a.identityFocus {
	case 0:
		a.identityAlias.Focus()
	case 1:
		a.identityEmail.Focus()
	case 2:
		a.identityPassword.Focus()
	case 3:
		a.identityPasswordConfirm.Focus()
	}
}

// handleIdentitySetupKey handles key events on the identity setup view
func (a *App) handleIdentitySetupKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	// One question at a time: Enter or Tab checks this answer before the
	// next question, Shift+Tab goes back.
	case "tab", "enter":
		if a.identityFocus == 3 {
			if msg.String() == "enter" {
				return a.handleIdentitySetupSubmit()
			}
			return nil
		}
		if err := a.identityStepError(a.identityFocus); err != "" {
			a.identityError = err
			return nil
		}
		a.identityError = ""
		a.cycleIdentityFocus(false)
		a.celebrate()
		return nil
	case "shift+tab":
		if a.identityFocus > 0 {
			a.identityError = ""
			a.cycleIdentityFocus(true)
		}
		return nil
	case "esc":
		// Adding a profile goes back to the list; on first run there's
		// nothing to go back to.
		if a.addingProfile {
			a.addingProfile = false
			a.openProfiles()
			a.profilesState.Back = ViewLogin
		}
		return nil
	}
	// Regular typing is handled by the component update section in Update()
	return nil
}

// handleIdentitySetupSubmit validates and saves the local identity
func (a *App) handleIdentitySetupSubmit() tea.Cmd {
	alias := strings.TrimSpace(a.identityAlias.Value())
	email := strings.TrimSpace(a.identityEmail.Value())
	password := a.identityPassword.Value()

	for step := 0; step < 4; step++ {
		if err := a.identityStepError(step); err != "" {
			// Back to the question with the problem.
			for a.identityFocus != step {
				a.cycleIdentityFocus(a.identityFocus > step)
			}
			a.identityError = err
			return nil
		}
	}

	identity := &LocalIdentity{
		ID:       uuid.NewString(),
		Alias:    alias,
		Email:    strings.ToLower(email),
		Password: password,
	}

	if a.addingProfile {
		// Another person on this computer: sign the current one out, save
		// the new profile as active, and sign in as it everywhere.
		for _, p := range a.profileList() {
			if strings.EqualFold(p.Email, identity.Email) {
				a.identityError = "A profile with that email is already on this computer (Esc, then pick it)"
				return nil
			}
		}
		a.addingProfile = false
		a.signOutAll()
		a.localIdentity = identity
		a.identityError = ""
		if err := a.configMgr.SaveIdentity(identity); err != nil {
			a.identityError = "Failed to save profile: " + err.Error()
			return nil
		}
		if len(a.clientServers) == 0 {
			a.view = ViewAddServer
			a.initAddServerForm()
			return nil
		}
		a.view = ViewMain
		a.focus = FocusServerIcons
		a.statusMessage, a.statusError = "Signing in as "+alias+"…", false
		var cmds []tea.Cmd
		for _, s := range a.clientServers {
			cmds = append(cmds, a.autoConnectServer(s.ID))
		}
		return tea.Batch(cmds...)
	}

	a.localIdentity = identity
	a.identityError = ""

	// Decide next view
	if len(a.clientServers) == 0 {
		a.view = ViewAddServer
		a.initAddServerForm()
	} else {
		a.view = ViewMain
	}

	// Save identity to disk; auto-connect cmds are issued by Init() on next startup
	// or will be triggered by autoConnectServer calls from the caller
	return func() tea.Msg {
		if err := a.configMgr.SaveIdentity(identity); err != nil {
			return ErrorMsg{Error: "Failed to save identity: " + err.Error()}
		}
		return nil
	}
}

// identityStepError checks one question's answer ("" when it's fine).
func (a *App) identityStepError(step int) string {
	switch step {
	case 0:
		if strings.TrimSpace(a.identityAlias.Value()) == "" {
			return "Pick a name to show people (you can change it later)"
		}
	case 1:
		email := strings.TrimSpace(a.identityEmail.Value())
		if email == "" {
			return "Enter your email address"
		}
		if !strings.Contains(email, "@") {
			return "That doesn't look like an email address"
		}
	case 2:
		if len(a.identityPassword.Value()) < 8 {
			return "Use at least 8 characters"
		}
	case 3:
		if a.identityPassword.Value() != a.identityPasswordConfirm.Value() {
			return "The two passwords don't match"
		}
	}
	return ""
}

// renderIdentitySetupView renders the first-run setup (or a new profile)
// on the login stage, one question at a time.
func (a *App) renderIdentitySetupView() string {
	label := "Welcome"
	if a.addingProfile {
		label = "New profile"
	}
	questions := []struct{ headline, accent, help, field string }{
		{"What should we call you?", "call you", "The name people see. Change it any time.", "Alias"},
		{"What's your email?", "email", "Servers send sign-in codes here. It stays on servers you join.", "Email"},
		{"Pick a password", "password", "It unlocks Concord here and signs you in to your servers.", "Password"},
		{"Once more, to be sure", "sure", "Type the same password again.", "Confirm"},
	}
	inputs := []string{a.identityAlias.View(), a.identityEmail.View(), a.identityPassword.View(), a.identityPasswordConfirm.View()}
	step := min(max(a.identityFocus, 0), 3)
	q := questions[step]

	var b strings.Builder
	b.WriteString(a.stageSteps(step, 4) + "\n\n")
	b.WriteString(a.dim(q.help) + "\n\n")
	b.WriteString(a.stageField(q.field, true, inputs[step]) + "\n")
	switch step {
	case 2:
		b.WriteString(a.passwordGrapes(a.identityPassword.Value()) + "\n")
	case 3:
		if v := a.identityPasswordConfirm.Value(); v != "" {
			if v == a.identityPassword.Value() {
				b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).Render("  ✓ They match") + "\n")
			} else if !strings.HasPrefix(a.identityPassword.Value(), v) {
				b.WriteString("\n" + a.dim("  ✗ Not the same yet") + "\n")
			}
		}
	}
	if a.identityError != "" {
		b.WriteString("\n" + a.stageNotice(a.identityError, true))
	}
	hints := []keyHint{{"Enter", "Next"}}
	if step == 3 {
		hints = []keyHint{{"Enter", "Done"}}
	}
	if step > 0 {
		hints = append(hints, keyHint{"Shift+Tab", "Back"})
	}
	if a.addingProfile {
		hints = append(hints, keyHint{"Esc", "Cancel"})
	}
	return a.stagePageWith(label, a.stageSteps(step, 4), q.headline, q.accent, b.String(), hints)
}
