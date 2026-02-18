package client

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	case "tab":
		a.cycleIdentityFocus(false)
		return nil
	case "shift+tab":
		a.cycleIdentityFocus(true)
		return nil
	case "enter":
		if a.identityFocus == 3 {
			return a.handleIdentitySetupSubmit()
		}
		a.cycleIdentityFocus(false)
		return nil
	case "esc":
		// Nothing to go back to on first run
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
	confirm := a.identityPasswordConfirm.Value()

	if alias == "" {
		a.identityError = "Alias is required"
		return nil
	}
	if email == "" {
		a.identityError = "Email is required"
		return nil
	}
	if !strings.Contains(email, "@") {
		a.identityError = "Enter a valid email address"
		return nil
	}
	if len(password) < 8 {
		a.identityError = "Password must be at least 8 characters"
		return nil
	}
	if password != confirm {
		a.identityError = "Passwords do not match"
		return nil
	}

	identity := &LocalIdentity{
		Alias:    alias,
		Email:    email,
		Password: password,
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

// renderIdentitySetupView renders the first-run identity configuration screen
func (a *App) renderIdentitySetupView() string {
	dialogWidth := 64
	dialogHeight := 22

	var content strings.Builder

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(titleStyle.Render("Welcome to Concord"))
	content.WriteString("\n\n")

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(subtitleStyle.Render("Set up your identity once. It will be used across all servers."))
	content.WriteString("\n\n")

	if a.identityError != "" {
		errStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Width(dialogWidth - 4).
			Align(lipgloss.Center)
		content.WriteString(errStyle.Render(a.identityError))
		content.WriteString("\n\n")
	}

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Width(dialogWidth - 4)

	fieldStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Width(dialogWidth - 6)

	content.WriteString(labelStyle.Render("Alias (Display Name):"))
	content.WriteString("\n")
	content.WriteString(fieldStyle.Render(a.identityAlias.View()))
	content.WriteString("\n\n")

	content.WriteString(labelStyle.Render("Email:"))
	content.WriteString("\n")
	content.WriteString(fieldStyle.Render(a.identityEmail.View()))
	content.WriteString("\n\n")

	content.WriteString(labelStyle.Render("Password:"))
	content.WriteString("\n")
	content.WriteString(fieldStyle.Render(a.identityPassword.View()))
	content.WriteString("\n\n")

	content.WriteString(labelStyle.Render("Confirm Password:"))
	content.WriteString("\n")
	content.WriteString(fieldStyle.Render(a.identityPasswordConfirm.View()))
	content.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	content.WriteString(helpStyle.Render("[Tab] Next  [Shift+Tab] Prev  [Enter] Confirm"))

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Padding(1, 2).
		Width(dialogWidth).
		Height(dialogHeight)

	dialog := dialogStyle.Render(content.String())

	return lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(dialog)
}
