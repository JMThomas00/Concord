package client

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"
)

func TestVoiceLevelStyleSetting(t *testing.T) {
	a := settingsTestApp()
	if a.voiceLevelStyle() != "bar" {
		t.Fatalf("default %q", a.voiceLevelStyle())
	}
	a.uiConfig.Display.MembersHideVUMeter = true // set by an older client
	if a.voiceLevelStyle() != "off" {
		t.Fatalf("hidden meter reads as %q", a.voiceLevelStyle())
	}
	seen := map[string]bool{}
	s := "bar"
	for range voiceLevelStyles {
		s = nextVoiceLevelStyle(s)
		seen[s] = true
	}
	if len(seen) != len(voiceLevelStyles) {
		t.Fatalf("cycle reached %v", seen)
	}

	// Enter on Settings > Display > Voice Level cycles it, keeping the old
	// flag in step for older clients.
	a.uiConfig.Display = DisplayConfig{}
	a.settingsState.DisplayFocusField = 9
	a.handleDisplayFieldActivate(a.settingsState)
	if a.voiceLevelStyle() != "slider" || a.uiConfig.Display.MembersHideVUMeter {
		t.Fatalf("after Enter: %q hidden=%v", a.voiceLevelStyle(), a.uiConfig.Display.MembersHideVUMeter)
	}
	for a.voiceLevelStyle() != "off" {
		a.handleDisplayFieldActivate(a.settingsState)
	}
	if !a.uiConfig.Display.MembersHideVUMeter {
		t.Fatal("off didn't set the old flag")
	}
}

func TestBrailleBars(t *testing.T) {
	for _, c := range []struct {
		l, r int
		want rune
	}{{0, 0, '⠀'}, {1, 1, '⣀'}, {4, 4, '⣿'}, {4, 1, '⣇'}, {2, 3, '⣤' | 0x10}} {
		if got := brailleBars(c.l, c.r); got != c.want {
			t.Errorf("brailleBars(%d,%d) = %c, want %c", c.l, c.r, got, c.want)
		}
	}
}

// The slider's knob sits at the volume, and the voice never lights the
// line past it (it's the loudness after the volume).
func TestVolumeTrack(t *testing.T) {
	a := settingsTestApp()
	for _, c := range []struct {
		vol, level float64
		knobAt     int
	}{{1, 1, 10}, {0, 0.5, 0}, {2, 1, 20}, {0.5, 1, 5}} {
		track := []rune(ansi.Strip(a.volumeTrack(21, c.vol, c.level, true, false)))
		if len(track) != 21 {
			t.Fatalf("width %d", len(track))
		}
		knob := strings.IndexRune(string(track), '●')
		if knob < 0 || len([]rune(string(track)[:knob])) != c.knobAt {
			t.Errorf("vol %.1f: track %q, want the knob at %d", c.vol, string(track), c.knobAt)
		}
		lit := strings.Count(string(track), "━")
		if lit > c.knobAt+1 {
			t.Errorf("vol %.1f level %.1f: %d lit past the knob: %q", c.vol, c.level, lit, string(track))
		}
	}
}

// Each style renders in the members panel without overflowing it.
func TestMembersPanelVoiceLevelStyles(t *testing.T) {
	want := map[string]string{"bar": "[█", "slider": "100%", "wave": "⣿⣿⣿ talker", "ring": "◉", "off": ""}
	for _, style := range voiceLevelStyles {
		a, m := newMembersTestApp(t)
		if a.uiConfig == nil {
			a.uiConfig = &UIConfig{}
		}
		if a.uiConfig == nil {
			a.uiConfig = &UIConfig{}
		}
		a.uiConfig.Display.VoiceLevelStyle = style
		talker := m["talker"].User.ID
		for i := 0; i < waveSamples; i++ {
			a.voiceLevels = map[uuid.UUID]float32{}
			a.recordVoiceLevel(talker, 0.3)
		}
		a.voiceLevels = nil
		a.voiceLevels = map[uuid.UUID]float32{talker: 0.3}
		raw := zone.Scan(a.renderUserList(30, 40))
		view := ansi.Strip(raw)
		if want[style] != "" && !strings.Contains(view, want[style]) {
			t.Errorf("%s: no %q in\n%s", style, want[style], view)
		}
		for _, line := range strings.Split(raw, "\n") {
			if lipgloss.Width(line) > 30 {
				t.Errorf("%s: line wider than the panel: %q", style, ansi.Strip(line))
			}
		}
	}
}

// ←/→ on a selected voice member turns them down and up; not yourself,
// not someone outside voice.
func TestMemberVolumeFromTheKeyboard(t *testing.T) {
	a, m := newMembersTestApp(t)
	if a.uiConfig == nil {
		a.uiConfig = &UIConfig{}
	}
	a.view = ViewMain
	a.focus = FocusUserList
	index := func(name string) int {
		for i, d := range a.buildFlatMemberList() {
			if d.User.Username == name {
				return i
			}
		}
		t.Fatalf("no %s", name)
		return -1
	}
	a.selectedMemberIndex = index("talker")
	a.handleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	a.handleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	a.handleKeyPress(tea.KeyMsg{Type: tea.KeyLeft})
	if v := a.memberVolume(m["talker"].User.ID); v != 1.05 {
		t.Fatalf("talker at %v after → → ←", v)
	}
	if !strings.Contains(ansi.Strip(a.renderUserList(30, 40)), "◂105%▸") {
		t.Fatal("the selected member's volume isn't shown")
	}
	for i := 0; i < 50; i++ {
		a.handleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	}
	if v := a.memberVolume(m["talker"].User.ID); v != maxMemberVolume {
		t.Fatalf("volume went to %v, past the 200%% cap", v)
	}
	for _, name := range []string{"ash", "pixel"} { // yourself; not in voice
		a.selectedMemberIndex = index(name)
		if a.adjustSelectedMemberVolume(0.05) {
			t.Fatalf("adjusted %s", name)
		}
	}
}

// Clicking, dragging or scrolling the slider sets the volume.
func TestMemberVolumeWithTheMouse(t *testing.T) {
	a, m := newMembersTestApp(t)
	if a.uiConfig == nil {
		a.uiConfig = &UIConfig{}
	}
	a.view = ViewMain
	a.uiConfig.Display.VoiceLevelStyle = "slider"
	talker := m["talker"].User.ID
	zone.Scan(a.renderUserList(30, 40))
	time.Sleep(20 * time.Millisecond) // zone bounds are recorded asynchronously
	z := zone.Get(volumeZone(talker))
	if z == nil || z.IsZero() {
		t.Fatal("no volume zone for the voice member")
	}
	a.handleMainViewMouse(tea.MouseMsg{X: z.EndX, Y: z.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if v := a.memberVolume(talker); v != maxMemberVolume || a.focus != FocusUserList {
		t.Fatalf("click at the end: %v, focus %v", v, a.focus)
	}
	a.handleMainViewMouse(tea.MouseMsg{X: z.StartX, Y: z.StartY, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if v := a.memberVolume(talker); v != 0 {
		t.Fatalf("dragged to the start: %v", v)
	}
	a.Update(tea.MouseMsg{X: z.StartX + 2, Y: z.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, Type: tea.MouseWheelUp})
	if v := a.memberVolume(talker); v != memberVolumeStep {
		t.Fatalf("scrolled up from 0: %v", v)
	}
}
