package client

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"
)

// How voice levels show in the members panel (Settings > Display > Voice
// Level), and adjusting each person's volume from there: ←/→ on a selected
// member, or click, drag or scroll the slider.
//
//	bar     a full-width level bar on its own row (the original)
//	slider  one row in the style of Waveform: the line lights up with the
//	        voice, the ● knob is that person's volume, and its percentage
//	wave    a tiny scrolling waveform in place of the presence dot: no extra row
//	ring    the presence dot lights up (◉) while they talk: no extra row
//	off     nothing
var voiceLevelStyles = []string{"bar", "slider", "wave", "ring", "off"}

const (
	maxMemberVolume  = 2.0  // per-user volume runs 0–200%
	memberVolumeStep = 0.05 // ←/→ and the mouse wheel
	waveSamples      = 6    // history kept per person for the wave style (3 braille cells)
	speakingLevel    = 0.15 // scaled level that counts as talking (ring)
)

// voiceLevelStyle is the style in force, honoring the older on/off setting.
func (a *App) voiceLevelStyle() string {
	if a.uiConfig == nil {
		return "bar"
	}
	d := a.uiConfig.Display
	for _, s := range voiceLevelStyles {
		if d.VoiceLevelStyle == s {
			return s
		}
	}
	if d.MembersHideVUMeter {
		return "off"
	}
	return "bar"
}

// nextVoiceLevelStyle is the style after s.
func nextVoiceLevelStyle(s string) string {
	for i, v := range voiceLevelStyles {
		if v == s {
			return voiceLevelStyles[(i+1)%len(voiceLevelStyles)]
		}
	}
	return voiceLevelStyles[0]
}

// scaledLevel maps a raw voice level (RMS) onto 0–1 for display.
func scaledLevel(level float32) float64 {
	return math.Min(float64(level)*4, 1)
}

// levelColor is green-to-red by loudness, in theme colors.
func (a *App) levelColor(scaled float64) string {
	switch {
	case scaled > 0.85:
		return a.theme.Colors.Red
	case scaled > 0.6:
		return a.theme.Colors.Yellow
	}
	return a.theme.Colors.Cyan
}

// recordVoiceLevel keeps a short history per person for the wave style.
func (a *App) recordVoiceLevel(id uuid.UUID, level float32) {
	if a.voiceHistory == nil {
		a.voiceHistory = map[uuid.UUID][]float32{}
	}
	h := append(a.voiceHistory[id], level)
	if len(h) > waveSamples {
		h = h[len(h)-waveSamples:]
	}
	a.voiceHistory[id] = h
}

// memberVolume is the playback volume set for a person (1 = 100%).
func (a *App) memberVolume(id uuid.UUID) float64 {
	if v, ok := a.audioConfig.PerUserVolumes[id.String()]; ok {
		return v
	}
	return 1
}

// setMemberVolume sets, applies and saves a person's playback volume.
func (a *App) setMemberVolume(id uuid.UUID, vol float64) {
	vol = math.Round(math.Min(math.Max(vol, 0), maxMemberVolume)*100) / 100
	if a.voiceEngine != nil {
		a.voiceEngine.SetUserVolume(id, vol)
	}
	if a.audioConfig.PerUserVolumes == nil {
		a.audioConfig.PerUserVolumes = make(map[string]float64)
	}
	a.audioConfig.PerUserVolumes[id.String()] = vol
	a.saveAudioConfig()
}

// localUserID is who this client is signed in as on the active server.
func (a *App) localUserID() uuid.UUID {
	if a.activeConn != nil && a.activeConn.User != nil {
		return a.activeConn.User.ID
	}
	return uuid.Nil
}

// memberInVoice reports whether someone is in a voice channel on screen.
func (a *App) memberInVoice(id uuid.UUID) bool {
	groups, _, _ := a.memberSections()
	for _, g := range groups {
		for _, m := range g.members {
			if m.User != nil && m.User.ID == id {
				return true
			}
		}
	}
	return false
}

// adjustSelectedMemberVolume changes the volume of the member selected in
// the members panel by delta, if they're someone else in voice. It reports
// whether it did, so ←/→ fall through otherwise.
func (a *App) adjustSelectedMemberVolume(delta float64) bool {
	flat := a.buildFlatMemberList()
	if a.selectedMemberIndex < 0 || a.selectedMemberIndex >= len(flat) {
		return false
	}
	m := flat[a.selectedMemberIndex]
	if m.User == nil || m.User.ID == a.localUserID() || !a.memberInVoice(m.User.ID) {
		return false
	}
	a.setMemberVolume(m.User.ID, a.memberVolume(m.User.ID)+delta)
	return true
}

// volumeZone names the clickable volume control for a person.
func volumeZone(id uuid.UUID) string { return "member-vol:" + id.String() }

// renderVoiceSlider is the slider style's row: the track lights up with
// the voice (loudness after this person's volume), the ● knob sits at
// their volume, then its percentage. Your own row shows your microphone's
// level, without a knob.
func (a *App) renderVoiceSlider(id uuid.UUID, level float32, width int, local, selected bool) string {
	dir := "↓"
	if local {
		dir = "↑"
	}
	label := "  mic"
	vol := 1.0
	if !local {
		vol = a.memberVolume(id)
		label = fmt.Sprintf("%4d%%", int(math.Round(vol*100)))
	}
	w := max(width-4-len(label)-1, 6) // "  ↓ " + track + " " + label
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	labelStyle := dim
	if selected || (!local && vol != 1) {
		labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	}
	t := a.volumeTrack(w, vol, scaledLevel(level), !local, selected)
	if !local {
		t = zone.Mark(volumeZone(id), t)
	}
	return "  " + dim.Render(dir) + " " + t + " " + labelStyle.Render(label)
}

// volumeTrack draws a w-cell slider: lit (━, colored by loudness) as far as
// the voice reaches after the volume, the ● knob at the volume (0–200%,
// when knob is set), and a dim line (─) for the rest.
func (a *App) volumeTrack(w int, vol, scaled float64, knob, selected bool) string {
	at := -1
	fill := int(math.Round(scaled * float64(w)))
	if knob {
		at = int(math.Round(vol / maxMemberVolume * float64(w-1)))
		fill = int(math.Round(scaled * vol / maxMemberVolume * float64(w)))
	}
	lit := lipgloss.NewStyle().Foreground(lipgloss.Color(a.levelColor(scaled)))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	knobColor := a.theme.Colors.Foreground
	if selected {
		knobColor = a.theme.Colors.Purple
	}
	knobStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(knobColor)).Bold(true)
	var b strings.Builder
	for i := 0; i < w; i++ {
		switch {
		case i == at:
			b.WriteString(knobStyle.Render("●"))
		case i < fill:
			b.WriteString(lit.Render("━"))
		default:
			b.WriteString(dim.Render("─"))
		}
	}
	return b.String()
}

// renderVoiceWave is the wave style: the last few levels as braille bars,
// two to a cell, in place of the presence dot.
func (a *App) renderVoiceWave(id uuid.UUID) string {
	const cells = waveSamples / 2
	h := a.voiceHistory[id]
	samples := make([]float32, waveSamples)
	copy(samples[waveSamples-len(h):], h)
	var b strings.Builder
	for c := 0; c < cells; c++ {
		b.WriteRune(brailleBars(barHeight(samples[2*c]), barHeight(samples[2*c+1])))
	}
	color := a.theme.Colors.Comment
	if s := scaledLevel(samples[waveSamples-1]); s >= speakingLevel {
		color = a.levelColor(s)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(b.String())
}

// barHeight is a level as 1–4 braille dots (1 is the quiet baseline).
func barHeight(level float32) int {
	return 1 + int(math.Round(scaledLevel(level)*3))
}

// brailleBars draws two bars (0–4 dots high, from the bottom) in one
// braille cell.
func brailleBars(left, right int) rune {
	leftDots := []rune{0x40, 0x04, 0x02, 0x01}  // dots 7, 3, 2, 1
	rightDots := []rune{0x80, 0x20, 0x10, 0x08} // dots 8, 6, 5, 4
	r := rune(0x2800)
	for i := 0; i < min(left, 4); i++ {
		r |= leftDots[i]
	}
	for i := 0; i < min(right, 4); i++ {
		r |= rightDots[i]
	}
	return r
}

// volumeBadge is a person's volume beside their name in the styles without
// a slider: shown when it isn't 100%, or while they're selected.
func (a *App) volumeBadge(id uuid.UUID, selected bool) string {
	vol := a.memberVolume(id)
	if vol == 1 && !selected {
		return ""
	}
	color := a.theme.Colors.Comment
	if vol != 1 {
		color = a.theme.Colors.Yellow
	}
	text := fmt.Sprintf("%d%%", int(math.Round(vol*100)))
	if selected {
		text = "◂" + text + "▸"
	}
	return " " + zone.Mark(volumeZone(id), lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text))
}

// resolveVolumeZone finds the volume control under the mouse, and where
// along it (0–1) the pointer is.
func (a *App) resolveVolumeZone(x, y int) (uuid.UUID, float64, bool) {
	for _, m := range a.buildFlatMemberList() {
		if m.User == nil {
			continue
		}
		z := zone.Get(volumeZone(m.User.ID))
		if z == nil || z.IsZero() || y < z.StartY || y > z.EndY || x < z.StartX || x > z.EndX {
			continue
		}
		frac := 0.0
		if span := z.EndX - z.StartX; span > 0 {
			frac = float64(x-z.StartX) / float64(span)
		}
		return m.User.ID, frac, true
	}
	return uuid.Nil, 0, false
}
