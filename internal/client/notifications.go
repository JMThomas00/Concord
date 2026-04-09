package client

import (
	"fmt"
	"time"

	"github.com/gen2brain/beeep"
)

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

// triggerMessageNotification fires the appropriate sound for an incoming message,
// respecting global config and any per-server overrides.
// serverID is used to look up per-server sound overrides.
func (a *App) triggerMessageNotification(authorName, serverName, channelName, content string, isMention bool) {
	cfg := a.notifConfig

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
