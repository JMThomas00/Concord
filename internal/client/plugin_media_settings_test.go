package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/concord-chat/concord/internal/themes"
)

func TestImagesSettingCycles(t *testing.T) {
	seen := map[string]bool{}
	s := ""
	for i := 0; i < len(imagesSettings); i++ {
		s = nextImagesSetting(s)
		seen[s] = true
	}
	if len(seen) != len(imagesSettings) {
		t.Fatalf("cycle visited %v", seen)
	}
	if nextImagesSetting("off") != "auto" {
		t.Fatal("off should wrap to auto")
	}
}

// macOS Terminal only gets blocks; the page says so and names terminals
// that do better. A terminal that supports pictures gets no hint.
func TestImagesSettingHints(t *testing.T) {
	defer SetTerminalGraphics(termGraphics)

	SetTerminalGraphics(TerminalGraphics{Term: "Apple_Terminal"})
	if got := imagesSettingLabel(""); got != "Auto: Blocks in macOS Terminal" {
		t.Fatalf("label %q", got)
	}
	if h := strings.Join(imagesSettingHints("auto"), " "); !strings.Contains(h, "iTerm2") {
		t.Fatalf("hints %q", h)
	}

	SetTerminalGraphics(TerminalGraphics{Sixel: true, Term: "Windows Terminal"})
	if got := imagesSettingLabel("auto"); got != "Auto: Sixel in Windows Terminal" {
		t.Fatalf("label %q", got)
	}
	if h := imagesSettingHints(""); h != nil {
		t.Fatalf("hints %q", h)
	}
	if h := imagesSettingHints("kitty"); len(h) != 1 {
		t.Fatalf("forcing an unsupported method should warn: %q", h)
	}
}

func settingsTestApp() *App {
	theme := themes.GetDefaultTheme()
	return &App{
		theme: theme, styles: theme.BuildStyles(), width: 120, height: 40,
		uiConfig:      &UIConfig{},
		audioConfig:   AudioConfig{PluginSoundVolume: 0.8},
		settingsState: &SettingsState{FocusOnForm: true},
	}
}

func TestPluginPicturesOnTheDisplayPage(t *testing.T) {
	a := settingsTestApp()
	s := a.settingsState
	s.DisplayFocusField = 13
	a.updateDisplayScroll(s)
	view := ansi.Strip(a.renderDisplayContent(100, 40))
	if !strings.Contains(view, "Plugin Pictures") {
		t.Fatalf("the focused last field isn't visible:\n%s", view)
	}
	a.handleDisplayFieldActivate(s)
	if a.uiConfig.Display.Images != "kitty" {
		t.Fatalf("Images = %q after Enter", a.uiConfig.Display.Images)
	}
}

func TestPluginSoundsOnTheAudioPage(t *testing.T) {
	a := settingsTestApp()
	s := a.settingsState
	s.AudioFocusField = 13
	view := ansi.Strip(a.renderAudioContent(100, 30))
	if !strings.Contains(view, "Plugin Sound Volume") {
		t.Fatalf("the focused last field isn't visible on a short page:\n%s", view)
	}

	s.AudioFocusField = 12
	a.handleAudioFieldActivate(s)
	if !a.audioConfig.PluginSoundsMuted {
		t.Fatal("Enter on Play Plugin Sounds should mute them")
	}

	s.AudioFocusField = 13
	a.handleAudioFieldActivate(s) // slider mode
	for i := 0; i < 100; i++ {
		a.adjustAudioSlider(s, -1)
	}
	if v := a.audioConfig.PluginSoundVolume; v <= 0 || v > 0.02 {
		t.Fatalf("volume turned all the way down = %v; 0 would read back as the default", v)
	}
}
