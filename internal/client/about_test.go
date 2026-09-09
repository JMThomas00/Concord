package client

import (
	"strings"
	"testing"
)

// TestRenderAboutContentShowsClientBuildInfo confirms the client Settings
// About category (added for the "About tab" to-do item) renders the exact
// build vars SetBuildInfo was given -- fails before SetBuildInfo/the About
// category existed, since there'd be nothing to render or no vars to read.
func TestRenderAboutContentShowsClientBuildInfo(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)
	a.SetBuildInfo("1.2.3", "deadbeef", "2026-09-08T00:00:00Z")

	out := a.renderAboutContent(120, 40)

	for _, want := range []string{"1.2.3", "deadbeef", "2026-09-08T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderAboutContent() output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestRenderServerAboutContentShowsConnectedServerBuildInfo confirms Server
// Settings' About category renders the connected server's build info from
// the active connection's cached Ready-payload fields, and fails safely
// (no panic, a clear "no connection" message) with none active.
func TestRenderServerAboutContentShowsConnectedServerBuildInfo(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	noConn := a.renderServerAboutContent(120, 40)
	if !strings.Contains(noConn, "No active server connection") {
		t.Errorf("expected a no-connection message with no active connection, got:\n%s", noConn)
	}

	a.activeConn = &ServerConnection{
		ServerVersion:   "4.5.6",
		ServerGitCommit: "cafebabe",
		ServerBuildTime: "2026-09-08T12:00:00Z",
	}
	out := a.renderServerAboutContent(120, 40)
	for _, want := range []string{"4.5.6", "cafebabe", "2026-09-08T12:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderServerAboutContent() output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestSettingsCategoriesIncludesAboutBeforeHelpGuide confirms the new
// "About" category was inserted before "Help & Guide", not appended --
// this is what keeps every len(s.Categories)-1 "last category" special
// case (used throughout settings_view.go for Help & Guide) correct without
// any index-arithmetic changes.
func TestSettingsCategoriesIncludesAboutBeforeHelpGuide(t *testing.T) {
	s := &SettingsState{Categories: []string{"Theme", "Notifications", "Display", "Audio", "Manage Servers", "About", "Help & Guide"}}

	if s.Categories[settingsCatAbout] != "About" {
		t.Errorf("expected Categories[settingsCatAbout] to be %q, got %q", "About", s.Categories[settingsCatAbout])
	}
	if s.Categories[len(s.Categories)-1] != "Help & Guide" {
		t.Errorf("expected Help & Guide to remain the last category, got %q", s.Categories[len(s.Categories)-1])
	}
}
