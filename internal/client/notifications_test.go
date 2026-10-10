package client

import "testing"

// TestShouldSendDesktopNotification table-tests the pure decision function
// behind the new Settings > Messages desktop-popup feature, without ever
// invoking the real OS notifier (beeep.Notify pops a genuine native toast,
// so it must stay out of unit tests).
func TestShouldSendDesktopNotification(t *testing.T) {
	tests := []struct {
		name             string
		mode             string
		scope            string
		isMention        bool
		isCurrentChannel bool
		isCurrentServer  bool
		want             bool
	}{
		{
			name: "zero-value config (upgrading an existing config.json) never notifies",
			mode: "", scope: "", isMention: true, isCurrentChannel: false, isCurrentServer: true,
			want: false,
		},
		{
			name: "mode off never notifies even for a mention",
			mode: DesktopNotifyModeOff, scope: DesktopNotifyScopeAllServers, isMention: true,
			want: false,
		},
		{
			name: "mode mentions suppresses a plain message",
			mode: DesktopNotifyModeMentions, scope: DesktopNotifyScopeAllServers, isMention: false,
			want: false,
		},
		{
			name: "mode mentions fires for an actual mention",
			mode: DesktopNotifyModeMentions, scope: DesktopNotifyScopeAllServers, isMention: true,
			want: true,
		},
		{
			name: "mode all fires for a plain message",
			mode: DesktopNotifyModeAll, scope: DesktopNotifyScopeAllServers, isMention: false,
			want: true,
		},
		{
			name:             "the exact channel currently on screen never pops, regardless of mode",
			mode:             DesktopNotifyModeAll,
			scope:            DesktopNotifyScopeAllServers,
			isMention:        true,
			isCurrentChannel: true,
			isCurrentServer:  true,
			want:             false,
		},
		{
			name: "scope current_server suppresses a message from a background server",
			mode: DesktopNotifyModeAll, scope: DesktopNotifyScopeCurrentServer,
			isCurrentServer: false,
			want:            false,
		},
		{
			name: "scope current_server allows a message from the active server",
			mode: DesktopNotifyModeAll, scope: DesktopNotifyScopeCurrentServer,
			isCurrentServer: true,
			want:            true,
		},
		{
			name: "scope all_servers allows a message from a background server",
			mode: DesktopNotifyModeAll, scope: DesktopNotifyScopeAllServers,
			isCurrentServer: false,
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSendDesktopNotification(tt.mode, tt.scope, tt.isMention, tt.isCurrentChannel, tt.isCurrentServer)
			if got != tt.want {
				t.Errorf("shouldSendDesktopNotification(mode=%q, scope=%q, isMention=%v, isCurrentChannel=%v, isCurrentServer=%v) = %v, want %v",
					tt.mode, tt.scope, tt.isMention, tt.isCurrentChannel, tt.isCurrentServer, got, tt.want)
			}
		})
	}
}

// TestHandleNotifFieldActivateCyclesDesktopMode confirms Enter/Space cycles
// the Notifications category's "Desktop Notifications" field (field 0)
// through off → mentions → all → off. This field originally lived on its
// own "Messages" settings category; moved back into Notifications (as a
// distinct "Desktop Notifications" section, alongside "Audio Notifications")
// per a follow-up request, since both sections govern the same incoming-
// message event and belong together for the user.
func TestHandleNotifFieldActivateCyclesDesktopMode(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	s := &SettingsState{NotifFocusField: 0}

	if a.notifConfig.DesktopNotifyMode != "" {
		t.Fatalf("test setup bug: expected zero-value DesktopNotifyMode, got %q", a.notifConfig.DesktopNotifyMode)
	}

	a.handleNotifFieldActivate(s)
	if a.notifConfig.DesktopNotifyMode != DesktopNotifyModeMentions {
		t.Errorf("1st activate: got %q, want %q", a.notifConfig.DesktopNotifyMode, DesktopNotifyModeMentions)
	}

	a.handleNotifFieldActivate(s)
	if a.notifConfig.DesktopNotifyMode != DesktopNotifyModeAll {
		t.Errorf("2nd activate: got %q, want %q", a.notifConfig.DesktopNotifyMode, DesktopNotifyModeAll)
	}

	a.handleNotifFieldActivate(s)
	if a.notifConfig.DesktopNotifyMode != DesktopNotifyModeOff {
		t.Errorf("3rd activate: got %q, want %q", a.notifConfig.DesktopNotifyMode, DesktopNotifyModeOff)
	}
}

// TestHandleNotifFieldActivateTogglesDesktopScope confirms the "Notify From"
// field (field 1) toggles between all_servers and current_server.
func TestHandleNotifFieldActivateTogglesDesktopScope(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	s := &SettingsState{NotifFocusField: 1}

	a.handleNotifFieldActivate(s)
	if a.notifConfig.DesktopNotifyScope != DesktopNotifyScopeCurrentServer {
		t.Errorf("1st activate: got %q, want %q", a.notifConfig.DesktopNotifyScope, DesktopNotifyScopeCurrentServer)
	}

	a.handleNotifFieldActivate(s)
	if a.notifConfig.DesktopNotifyScope != DesktopNotifyScopeAllServers {
		t.Errorf("2nd activate: got %q, want %q", a.notifConfig.DesktopNotifyScope, DesktopNotifyScopeAllServers)
	}
}

// TestHandleNotifFieldActivateStillTogglesSoundFields is a regression guard
// for the field renumbering that came with folding the desktop-notification
// fields into this category: fields 2-4 (previously 0-2) must still be the
// audio toggles, not accidentally shifted or duplicated.
func TestHandleNotifFieldActivateStillTogglesSoundFields(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)

	s := &SettingsState{NotifFocusField: 2}
	a.handleNotifFieldActivate(s)
	if !a.notifConfig.SoundsMuted {
		t.Errorf("field 2 (Notification Sounds) did not toggle SoundsMuted")
	}

	s = &SettingsState{NotifFocusField: 3}
	a.handleNotifFieldActivate(s)
	if !a.notifConfig.MentionsOnly {
		t.Errorf("field 3 (Mentions Only) did not toggle MentionsOnly")
	}

	s = &SettingsState{NotifFocusField: 4}
	a.handleNotifFieldActivate(s)
	if !a.notifConfig.BellOnMention {
		t.Errorf("field 4 (Terminal Bell on Mention) did not toggle BellOnMention")
	}
}

// TestSettingsCategoriesHasNoStandaloneMessagesCategory guards against the
// standalone "Messages" category (added, then folded back into
// Notifications per a follow-up request) silently reappearing.
func TestSettingsCategoriesHasNoStandaloneMessagesCategory(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.openSettings(ViewMain)
	cats := a.settingsState.Categories

	for _, c := range cats {
		if c == "Messages" {
			t.Errorf("expected no standalone Messages category, got %v", cats)
		}
	}
	if len(cats) == 0 || cats[len(cats)-1] != "Help & Guide" {
		t.Fatalf("expected Help & Guide to remain the last category, got %v", cats)
	}
}
