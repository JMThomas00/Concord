package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The hub browser opens over the login screen (Ctrl+G). The stage's
// background (drifting grapes, stars) used to keep painting into its empty
// cells (Jordan, 2026-10-06).
func TestHubBrowserCoversTheStage(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	a.view = ViewLogin
	if !a.onStage() {
		t.Fatal("login screen should be on stage")
	}
	a.showHubBrowser = true
	a.hubBrowser = newHubBrowserState([]string{defaultHubURL}, a.width, a.height)
	if a.onStage() {
		t.Error("stage effects still on while the hub browser covers the login screen")
	}
	if a.photoGrapes() {
		t.Error("photo grapes would be painted over the hub browser")
	}
}

// While a hub loads, the browser shows which hub and a bar with a grape at
// its tip, and keeps redrawing so the bar moves.
func TestHubBrowserLoadingBar(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	a.view = ViewLogin
	a.showHubBrowser = true
	a.hubBrowser = newHubBrowserState([]string{"https://hub.example.test"}, a.width, a.height)
	a.hubBrowser.loading = true
	a.hubBrowser.loadStart = time.Now().Add(-time.Second)

	view := ansi.Strip(a.renderHubBrowserView())
	if !strings.Contains(view, "Connecting to hub.example.test") || !strings.Contains(view, hubLoadingGrape) {
		t.Errorf("no loading bar for the selected hub in:\n%s", view)
	}
	if !a.fxMoving() {
		t.Error("loading bar wouldn't animate: fxMoving is false while a hub loads")
	}
	early := ansi.Strip(strings.Join(a.renderHubLoading(120, 0), "\n"))
	later := ansi.Strip(strings.Join(a.renderHubLoading(120, 5*time.Second), "\n"))
	if strings.Count(later, "━") <= strings.Count(early, "━") {
		t.Error("the bar doesn't fill as time passes")
	}

	a.hubBrowser.loading = false
	if strings.Contains(ansi.Strip(a.renderHubBrowserView()), "Connecting to") {
		t.Error("loading bar still shown after the listing arrived")
	}
}

// The login screen points at the hub browser on its own line, for both the
// profile unlock and the email login.
func TestLoginShowsGrapevineTip(t *testing.T) {
	for _, withIdentity := range []bool{true, false} {
		a := newLoginTestApp(t, 160, 45, withIdentity)
		view := ansi.Strip(a.renderLoginView())
		if !strings.Contains(view, "Ctrl+G") || !strings.Contains(view, "Grapevine") {
			t.Errorf("identity=%v: no Ctrl+G tip in:\n%s", withIdentity, view)
		}
		if strings.Count(view, "Ctrl+G") != 1 {
			t.Errorf("identity=%v: Ctrl+G shown %d times", withIdentity, strings.Count(view, "Ctrl+G"))
		}
	}
	a := newLoginTestApp(t, 120, 28, true)
	if strings.Contains(ansi.Strip(a.renderLoginView()), "Tip:") {
		t.Error("tip shown on a screen too short to spare the rows")
	}
}
