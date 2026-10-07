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

// A listing that arrives before the bar has filled waits for it, then
// shows; one held for a load that's been replaced (a refresh) is dropped.
func TestHubListingWaitsForTheBar(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	a.view = ViewLogin
	a.showHubBrowser = true
	a.hubBrowser = newHubBrowserState([]string{"https://hub.example.test"}, a.width, a.height)
	s := &a.hubBrowser
	s.loading, s.loadStart = true, time.Now()

	msg := hubServersLoadedMsg{hubURL: "https://hub.example.test", servers: []HubServerEntry{{ID: "1", Name: "Grape Hall"}}}
	if _, cmd := a.handleHubMsg(msg); cmd == nil || !s.loading || !s.arrived {
		t.Fatalf("early listing shown at once (loading=%v arrived=%v)", s.loading, s.arrived)
	}
	held := msg
	held.held, held.loadStart = true, s.loadStart
	s.loadStart = s.loadStart.Add(-hubLoadMin) // the bar has run its course
	held.loadStart = s.loadStart
	a.handleHubMsg(held)
	if s.loading || len(s.allServers) != 1 {
		t.Fatalf("held listing not shown after the bar (loading=%v servers=%d)", s.loading, len(s.allServers))
	}

	s.loading, s.loadStart, s.allServers = true, time.Now(), nil // a refresh
	stale := msg
	stale.held, stale.loadStart = true, time.Now().Add(-time.Minute)
	a.handleHubMsg(stale)
	if !s.loading || len(s.allServers) != 0 {
		t.Error("a listing held for an earlier load replaced the new one")
	}
}
