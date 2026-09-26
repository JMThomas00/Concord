package client

import (
	"fmt"
	"time"

	"github.com/gen2brain/beeep"
)

// Desktop notification mode/scope values -- stored in
// NotificationConfig.DesktopNotifyMode/DesktopNotifyScope. The zero value of
// each ("") is deliberately treated as the safe "off"/"all_servers" default
// by shouldSendDesktopNotification, so existing config.json files that
// predate this feature never start popping notifications unexpectedly.
const (
	DesktopNotifyModeOff      = "off"
	DesktopNotifyModeMentions = "mentions"
	DesktopNotifyModeAll      = "all"

	DesktopNotifyScopeAllServers    = "all_servers"
	DesktopNotifyScopeCurrentServer = "current_server"
)

// desktopNotifyBodyLimit truncates a notification body so a long message
// doesn't produce an oversized OS popup.
const desktopNotifyBodyLimit = 200

func init() {
	// beeep.AppName defaults to "DefaultAppName" -- that's what a Windows
	// toast's title bar shows unless this is set, regardless of the
	// title/message passed to Notify. Set once at package load so every
	// OS's native popup is correctly attributed to Concord.
	beeep.AppName = "Concord"
}

// shouldSendDesktopNotification decides whether an OS-native desktop popup
// should fire for an incoming message. Pure/side-effect-free on purpose --
// beeep.Notify actually pops a real native toast, so the decision logic is
// kept separate and unit-testable without ever invoking it.
//
// isCurrentChannel always suppresses the popup (mirrors the sound path's
// own "sound plays even in the current channel; desktop popup only when
// away from it" comment in app.go) -- a popup for a message you're already
// looking at is just noise. isCurrentServer only matters when scope is
// "current_server": it's the currently-active/connected server tab, not
// necessarily the message's own server.
func shouldSendDesktopNotification(mode, scope string, isMention, isCurrentChannel, isCurrentServer bool) bool {
	if isCurrentChannel {
		return false
	}
	switch mode {
	case DesktopNotifyModeAll:
		// proceed
	case DesktopNotifyModeMentions:
		if !isMention {
			return false
		}
	default: // "" (unset) or DesktopNotifyModeOff
		return false
	}
	if scope == DesktopNotifyScopeCurrentServer && !isCurrentServer {
		return false
	}
	return true
}

// sendDesktopNotification pops a native OS notification (Windows toast,
// macOS Notification Center, Linux D-Bus/notify-send via beeep's platform
// backends -- already pulled in transitively since beeep is a direct
// dependency used for sound alerts) asynchronously, matching playSound's
// own "never block the event loop" pattern.
func (a *App) sendDesktopNotification(title, message string) {
	if runes := []rune(message); len(runes) > desktopNotifyBodyLimit {
		message = string(runes[:desktopNotifyBodyLimit-1]) + "…"
	}
	go func() {
		_ = beeep.Notify(title, message, "") //nolint:errcheck
	}()
}

// soundTone is a single frequency+duration pair for a beep sound.
type soundTone struct {
	Freq float64 // Hz
	Dur  int     // milliseconds
}

// SoundOption defines a named alert sound.
type SoundOption struct {
	Name  string
	Tones []soundTone // empty = no sound; special handling for "Terminal Bell"
}

// SoundOptions is the ordered list of available alert sounds shown in the picker.
// Index 0 is always "None" (no sound).
var SoundOptions = []SoundOption{
	{Name: "None"},
	{Name: "Terminal Bell"}, // writes \a — uses the terminal's native bell
	{Name: "Ding",         Tones: []soundTone{{880, 100}}},
	{Name: "Chime",        Tones: []soundTone{{660, 220}}},
	{Name: "Bell",         Tones: []soundTone{{440, 320}}},
	{Name: "Blip",         Tones: []soundTone{{1200, 60}}},
	{Name: "Pip",          Tones: []soundTone{{440, 90}}},
	{Name: "Boop",         Tones: []soundTone{{280, 160}}},
	{Name: "Pop",          Tones: []soundTone{{200, 70}}},
	{Name: "Alert",        Tones: []soundTone{{880, 100}, {660, 180}}},
	{Name: "Notification", Tones: []soundTone{{660, 130}, {880, 130}}},
	{Name: "Knock",        Tones: []soundTone{{300, 90}, {200, 90}}},
	{Name: "Pulse",        Tones: []soundTone{{660, 70}, {660, 70}}},
}

// FindSoundIndex returns the index of name in SoundOptions, or 0 ("None") if not found.
func FindSoundIndex(name string) int {
	for i, s := range SoundOptions {
		if s.Name == name {
			return i
		}
	}
	return 0
}

// playSound plays the named sound asynchronously so it never blocks the event loop.
func (a *App) playSound(name string) {
	if name == "" || name == "None" {
		return
	}
	for _, opt := range SoundOptions {
		if opt.Name != name {
			continue
		}
		go func(o SoundOption) {
			if o.Name == "Terminal Bell" || len(o.Tones) == 0 {
				fmt.Print("\a")
				return
			}
			for i, t := range o.Tones {
				beeep.Beep(t.Freq, t.Dur) //nolint:errcheck
				if i < len(o.Tones)-1 {
					time.Sleep(40 * time.Millisecond)
				}
			}
		}(opt)
		return
	}
}

// triggerMessageNotification fires the appropriate sound (and, if enabled,
// OS desktop popup) for an incoming message, respecting global config and
// any per-server sound overrides. isCurrentChannel/isCurrentServer describe
// the message's context relative to what the user is currently looking at
// -- desktop popups (unlike sounds) are gated on these, since a popup for a
// channel already on screen is redundant. Desktop notifications deliberately
// don't participate in ServerSoundOverride -- scope/mode are global-only
// settings (Settings > Messages), not per-server.
func (a *App) triggerMessageNotification(authorName, serverName, channelName, content string, isMention, isCurrentChannel, isCurrentServer bool) {
	cfg := a.notifConfig

	if shouldSendDesktopNotification(cfg.DesktopNotifyMode, cfg.DesktopNotifyScope, isMention, isCurrentChannel, isCurrentServer) {
		title := fmt.Sprintf("%s (#%s)", serverName, channelName)
		if isMention {
			title = fmt.Sprintf("Mention in #%s (%s)", channelName, serverName)
		}
		a.sendDesktopNotification(title, fmt.Sprintf("%s: %s", authorName, content))
	}

	// Resolve effective settings: start with globals, apply server override if present.
	muted := cfg.SoundsMuted
	mentionsOnly := cfg.MentionsOnly
	mentionSound := cfg.MentionSound
	messageSound := cfg.MessageSound
	bellOnMention := cfg.BellOnMention

	if a.activeConn != nil {
		for _, srv := range a.clientServers {
			if srv.ID == a.activeConn.ServerID && srv.SoundOverride != nil {
				ov := srv.SoundOverride
				muted = ov.SoundsMuted
				mentionsOnly = ov.MentionsOnly
				if ov.MentionSound != "" {
					mentionSound = ov.MentionSound
				}
				if ov.MessageSound != "" {
					messageSound = ov.MessageSound
				}
				break
			}
		}
	}

	// Feature 2: terminal bell on mention (independent of sound mute).
	if isMention && bellOnMention {
		go fmt.Print("\a")
	}

	if muted {
		return
	}

	// Feature 1: mentions-only suppresses message sounds.
	if !isMention && mentionsOnly {
		return
	}

	if isMention {
		a.playSound(mentionSound)
	} else {
		a.playSound(messageSound)
	}
}
