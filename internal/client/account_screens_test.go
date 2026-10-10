package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

func keyOf(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// accountApp is the login screen with two profiles saved (amy active).
func accountApp(t *testing.T) *App {
	t.Helper()
	a := newLayoutTestApp(t, 140, 45)
	a.configMgr = tempConfig(t)
	a.pendingVerify = map[uuid.UUID]string{}
	a.loginEmail, a.loginPassword, a.loginUsername, a.loginPasswordConfirm = textinput.New(), textinput.New(), textinput.New(), textinput.New()
	a.identityAlias, a.identityEmail, a.identityPassword, a.identityPasswordConfirm = textinput.New(), textinput.New(), textinput.New(), textinput.New()
	for _, p := range []*LocalIdentity{
		{Alias: "ben", Email: "ben@example.com", Password: "password456"},
		{Alias: "amy", Email: "amy@example.com", Password: "password123"},
	} {
		if err := a.configMgr.SaveIdentity(p); err != nil {
			t.Fatal(err)
		}
	}
	a.localIdentity = a.configMgr.GetIdentity()
	a.view = ViewLogin
	return a
}

func TestLoginScreenOpensProfilesAndSwitches(t *testing.T) {
	a := accountApp(t)
	a.handleKeyPress(keyOf("ctrl+p"))
	if a.view != ViewProfiles {
		t.Fatalf("Ctrl+P on the login screen: view %v", a.view)
	}
	out := ansi.Strip(a.renderProfilesView())
	if !strings.Contains(out, "amy") || !strings.Contains(out, "ben") || !strings.Contains(out, "in use") {
		t.Fatalf("profiles:\n%s", out)
	}
	// The cursor starts on the active profile (amy, second); go to ben.
	a.handleKeyPress(keyOf("k"))
	a.handleKeyPress(keyOf("enter"))
	if a.view != ViewLogin || a.localIdentity.Alias != "ben" || a.configMgr.GetIdentity().Alias != "ben" {
		t.Fatalf("switching: view %v identity %+v", a.view, a.localIdentity)
	}
	if form, _ := a.loginFormBlock(); !strings.Contains(ansi.Strip(form), "Welcome back, ben!") {
		t.Fatal("login screen doesn't greet the new profile")
	}
}

func TestProfileEditChecksThePasswordAndFixesTypos(t *testing.T) {
	a := accountApp(t)
	a.openProfiles()
	a.handleKeyPress(keyOf("e"))
	s := a.profilesState
	if s.EditID == "" {
		t.Fatal("E didn't open the editor")
	}
	s.EditEmail.SetValue("amy@example.org")
	s.EditPass.SetValue("wrong-password")
	s.EditFocus = 2
	a.handleKeyPress(keyOf("enter"))
	if !s.NoticeErr || a.configMgr.GetIdentity().Email != "amy@example.com" {
		t.Fatal("saved with the wrong password")
	}
	s.EditPass.SetValue("password123")
	cmd := a.handleKeyPress(keyOf("enter"))
	if a.configMgr.GetIdentity().Email != "amy@example.org" || a.localIdentity.Email != "amy@example.org" {
		t.Fatalf("email not fixed: %+v", a.configMgr.GetIdentity())
	}
	if cmd == nil {
		t.Fatal("no server update was started")
	}
	if m, ok := cmd().(profileEditResultMsg); !ok || !strings.Contains(strings.Join(m.lines, " "), "aren't signed in") {
		t.Fatalf("result %+v", m)
	}
}

func TestForgotPasswordAndPendingVerificationScreens(t *testing.T) {
	a := accountApp(t)
	a.clientServers = []*ClientServerInfo{NewClientServerInfo("Home", "127.0.0.1", 1, false)}
	if err := a.configMgr.SaveServers(&ServersConfig{Version: 1, Servers: a.clientServers}); err != nil {
		t.Fatal(err)
	}
	_, _ = a.connMgr.AddServer(a.clientServers[0])

	a.handleKeyPress(keyOf("ctrl+f"))
	if a.view != ViewAccountCode || a.codeState.Mode != codeModeForgot {
		t.Fatalf("Ctrl+F: view %v", a.view)
	}
	if out := ansi.Strip(a.renderAccountCodeView()); !strings.Contains(out, "Home") || !strings.Contains(out, "reset code") {
		t.Fatalf("forgot screen:\n%s", out)
	}
	// A server that can't email switches to a temporary password.
	a.codeState.Busy = true
	a.applyAccountCodeResult(accountCodeMsg{action: "forgot", serverID: a.clientServers[0].ID, err: &APIError{Code: apiMailUnavailable, Message: "Home can't send email."}})
	if !a.codeState.TempPassword || a.codeState.Step != 1 || !strings.Contains(ansi.Strip(a.renderAccountCodeView()), "Temporary password") {
		t.Fatal("no temporary-password fallback")
	}
	a.handleKeyPress(keyOf("esc"))
	if a.view != ViewLogin {
		t.Fatalf("Esc from forgot: view %v", a.view)
	}

	// Unlocking with a server waiting for its code opens the code screen.
	a.pendingVerify[a.clientServers[0].ID] = "amy@example.com"
	a.loginPassword.SetValue("password123")
	a.handleLoginSubmit()
	if a.view != ViewAccountCode || a.codeState.Mode != codeModeVerify {
		t.Fatalf("after unlock: view %v", a.view)
	}
	out := ansi.Strip(a.renderAccountCodeView())
	if !strings.Contains(out, "amy@example.com") || !strings.Contains(out, "Wrong email?") {
		t.Fatalf("verify screen:\n%s", out)
	}
	a.handleKeyPress(keyOf("esc"))
	if a.view != ViewMain || !strings.Contains(a.statusMessage, "waiting for your email code") {
		t.Fatalf("Esc from verify: view %v status %q", a.view, a.statusMessage)
	}
}
