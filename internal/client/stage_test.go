package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// The grapes stay put from page to page on the login stage, so moving
// between pages feels like one place.
func TestStagePagesKeepTheGrapesPinned(t *testing.T) {
	a := accountApp(t)
	grapeTop := strings.TrimSpace(ansi.Strip(strings.Split(a.renderGrapeLogo(), "\n")[0]))
	where := func(name string) (int, int) {
		t.Helper()
		row, col, lines := position(t, a.View(), grapeTop)
		if lines != a.height {
			t.Fatalf("%s: %d lines, want %d", name, lines, a.height)
		}
		return row, col
	}
	wantRow, wantCol := where("login")

	pages := []struct {
		name string
		open func()
	}{
		{"profiles", func() { a.openProfiles() }},
		{"profile edit", func() { a.openProfiles(); a.handleKeyPress(keyOf("e")) }},
		{"password change", func() { a.openProfiles(); a.startPasswordChange() }},
		{"code", func() { a.openCodeScreen(codeModeVerify, uuid.New(), "amy@example.com") }},
		{"add server", func() { a.view = ViewAddServer; a.initAddServerForm() }},
	}
	for step := 0; step < 4; step++ {
		step := step
		pages = append(pages, struct {
			name string
			open func()
		}{"setup step", func() {
			a.view = ViewIdentitySetup
			a.initIdentitySetupForm()
			a.identityAlias.SetValue("amy")
			a.identityEmail.SetValue("amy@example.com")
			a.identityPassword.SetValue("Password123!")
			for i := 0; i < step; i++ {
				a.cycleIdentityFocus(false)
			}
		}})
	}
	for _, p := range pages {
		a.view = ViewLogin
		p.open()
		if row, col := where(p.name); row != wantRow || col != wantCol {
			t.Errorf("%s: grapes at row %d col %d, login has them at row %d col %d", p.name, row, col, wantRow, wantCol)
		}
	}
}

func TestPasswordStrength(t *testing.T) {
	for _, c := range []struct {
		pw       string
		min, max int
	}{
		{"", 0, 0},
		{"abc", 1, 2},
		{"password", 2, 3},
		{"Tr0ub4dor&3", 6, 7},
		{"correct horse battery staple", 6, 8},
		{"Kx9!vQ2#mZ7@pL4$", 8, 8},
	} {
		if s := passwordStrength(c.pw); s < c.min || s > c.max {
			t.Errorf("%q scored %d, want %d..%d", c.pw, s, c.min, c.max)
		}
	}
}

// Each setup question is checked before the next one opens.
func TestSetupAsksOneQuestionAtATime(t *testing.T) {
	a := accountApp(t)
	a.view = ViewIdentitySetup
	a.initIdentitySetupForm()
	a.handleIdentitySetupKey(keyOf("enter"))
	if a.identityFocus != 0 || a.identityError == "" {
		t.Fatal("moved on without an alias")
	}
	a.identityAlias.SetValue("amy")
	a.handleIdentitySetupKey(keyOf("enter"))
	if a.identityFocus != 1 || a.identityError != "" {
		t.Fatalf("step %d, error %q", a.identityFocus, a.identityError)
	}
	a.identityEmail.SetValue("not-an-email")
	a.handleIdentitySetupKey(keyOf("enter"))
	if a.identityFocus != 1 {
		t.Fatal("accepted a bad email")
	}
	out := ansi.Strip(a.renderIdentitySetupView())
	if !strings.Contains(out, "What's your email?") || !strings.Contains(out, "2 of 4") {
		t.Fatalf("page:\n%s", out)
	}
}

func TestCodeBoxesFlipAndRipple(t *testing.T) {
	a := accountApp(t)
	a.openCodeScreen(codeModeVerify, uuid.New(), "amy@example.com")
	st := a.codeState
	st.Code.SetValue("k7q")
	now := time.Now()
	a.trackCodeTyping(now)
	if len(st.typedAt) != 3 || !a.codeAnimating(now) {
		t.Fatal("typing not tracked")
	}
	if out := ansi.Strip(a.renderCodeBoxes(st, now)); strings.Contains(out, "K") {
		t.Fatal("a letter showed before its flip")
	}
	if out := ansi.Strip(a.renderCodeBoxes(st, now.Add(time.Second))); !strings.Contains(out, "K") || !strings.Contains(out, "Q") {
		t.Fatalf("letters missing after the flip:\n%s", out)
	}
	cmd := a.acceptCode(st)
	if cmd == nil || a.handleAccountCodeKey(keyOf("x")) != nil || st.Code.Value() != "k7q" {
		t.Fatal("keys still reach the code while the ripple plays")
	}
	a.Update(codeAcceptedMsg{st})
	if a.codeState != nil {
		t.Fatal("screen didn't close after the ripple")
	}
}

func TestAnErrorShakesTheForm(t *testing.T) {
	a := accountApp(t)
	a.uiConfig = &UIConfig{}
	a.loginError = "Incorrect password"
	a.syncFx()
	now := time.Now()
	if !a.shaking(now) || a.grapeReact == nil || a.grapeReact.success {
		t.Fatal("no shake or flush")
	}
	a.fx.shakeAt = time.Time{}
	a.syncFx()
	if a.shaking(now) {
		t.Fatal("the same error shook again")
	}
}
