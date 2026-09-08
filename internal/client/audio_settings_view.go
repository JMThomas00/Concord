package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// renderAudioContent renders the Audio settings panel.
func (a *App) renderAudioContent(width, height int) string {
	s := a.settingsState
	layout := calculateSettingsLayout(width, height, 2, 0)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)

	focused := s != nil && s.FocusOnForm
	focusField := 0
	sliderActive := false
	if s != nil {
		focusField = s.AudioFocusField
		sliderActive = s.AudioSliderActive
	}

	cfg := a.audioConfig

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("Audio Settings"))
	top.writeLine(dimStyle.Render("Voice channel audio devices, volume, and codec options"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	writeField := func(fieldIdx int, label, value string) {
		isSelected := focused && focusField == fieldIdx
		lStyle := labelStyle
		vStyle := normalStyle
		marker := "  "
		if isSelected {
			lStyle = selectedStyle
			vStyle = selectedStyle
			marker = "▶ "
		}
		writeZoneMarkedLines(middle, fmt.Sprintf("audio-field:%d", fieldIdx),
			lStyle.Render(marker+label), vStyle.Render("    "+value))
		middle.writeBlank()
	}

	writeToggle := func(fieldIdx int, label string, enabled bool) {
		isSelected := focused && focusField == fieldIdx
		lStyle := labelStyle
		marker := "  "
		if isSelected {
			lStyle = selectedStyle
			marker = "▶ "
		}
		stateStr := dimStyle.Render("OFF")
		if enabled {
			stateStr = greenStyle.Render("ON")
		}
		if isSelected {
			stateStr = selectedStyle.Render(map[bool]string{true: "ON", false: "OFF"}[enabled])
		}
		middle.writeLine(zone.Mark(fmt.Sprintf("audio-field:%d", fieldIdx), lStyle.Render(marker+label)+"  "+stateStr))
		middle.writeBlank()
	}

	// progressBar renders an ASCII bar: [████████░░] 80%
	// When the field is in slider-active mode the bar is rendered in green and
	// ◄ / ► hints are appended so the user knows ←/→ adjusts the value.
	progressBar := func(fieldIdx int, val, max float64, barWidth int) string {
		if max == 0 {
			max = 1
		}
		pct := val / max
		if pct < 0 {
			pct = 0
		}
		if pct > 1 {
			pct = 1
		}
		filled := int(pct * float64(barWidth))
		bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"
		text := fmt.Sprintf("%s %d%%", bar, int(pct*100))

		isFieldSelected := focused && focusField == fieldIdx
		if isFieldSelected && sliderActive {
			// Green bar + ◄ ► hints — slider is live
			return greenStyle.Render("◄ "+text+" ►")
		}
		// Static hint when field is selected but slider not yet activated
		if isFieldSelected {
			return text + " ◀▶"
		}
		return text
	}

	// Prefer the friendly name for display; fall back to the device ID or placeholder.
	inputDev := cfg.InputDeviceName
	if inputDev == "" && cfg.InputDevice != "" {
		inputDev = cfg.InputDevice // ID shown only if name was never stored
	}
	if inputDev == "" {
		inputDev = "System Default"
	}
	outputDev := cfg.OutputDeviceName
	if outputDev == "" && cfg.OutputDevice != "" {
		outputDev = cfg.OutputDevice
	}
	if outputDev == "" {
		outputDev = "System Default"
	}

	// Field 0: Input Device
	writeField(0, "Input Device (Microphone)", inputDev+" ◀▶")

	// Inline device picker — shown directly below field 0 when input picker is open
	if s != nil && s.AudioPickerOpen && s.AudioPickerTarget == 0 {
		a.renderDevicePickerInline(middle, s, layout.interiorWidth)
	}

	// Field 1: Output Device (Speakers/Headphones)
	writeField(1, "Output Device (Speakers)", outputDev+" ◀▶")

	// Inline device picker — shown directly below field 1 when output picker is open
	if s != nil && s.AudioPickerOpen && s.AudioPickerTarget == 1 {
		a.renderDevicePickerInline(middle, s, layout.interiorWidth)
	}

	// Field 2: Input Gain
	writeField(2, "Input Gain", progressBar(2, cfg.InputGain, 2.0, 20))

	// Field 3: Output Volume
	writeField(3, "Output Volume", progressBar(3, cfg.OutputVolume, 1.0, 20))

	// Field 4: Voice Activity Detection
	writeToggle(4, "Voice Activity Detection (VAD)", cfg.VADEnabled)

	// Field 5: VAD Threshold
	writeField(5, "VAD Sensitivity", progressBar(5, cfg.VADThreshold, 1.0, 20))

	// Field 6: Push-to-Talk
	writeToggle(6, "Push-to-Talk (PTT)", cfg.PTTEnabled)

	// Field 7: PTT Key
	pttKey := cfg.PTTKey
	if pttKey == "" {
		pttKey = "ctrl+space"
	}
	writeField(7, "PTT Key", pttKey)

	// Field 8: Noise Suppression
	writeToggle(8, "Noise Suppression", cfg.NoiseSuppress)

	// Field 9: Echo Cancellation
	writeToggle(9, "Echo Cancellation", cfg.EchoCancellation)

	// Field 10: Codec Preset
	codec := cfg.CodecPreset
	if codec == "" {
		codec = "medium"
	}
	codecDesc := map[string]string{
		"low":    "Low    ( 8 kHz, telephone quality)",
		"medium": "Medium (16 kHz, standard voice)",
		"high":   "High   (24 kHz, HD voice)",
		"ultra":  "Ultra  (48 kHz, studio quality)",
	}[codec]
	if codecDesc == "" {
		codecDesc = codec
	}
	writeField(10, "Codec Quality", codecDesc+" ◀▶")

	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	helpText := "↑↓ navigate · Enter toggle or activate · Tab back to menu · Esc close"
	if sliderActive {
		helpText = "◄ ► (or hold) adjust value · Enter / Esc confirm"
	}
	bottom.writeLine(dimStyle.Render(helpText))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	// Border() adds 2 lines on top of Height(N) -- see the matching comment
	// in renderServerIconsCollapsed (views.go). Found again here 2026-09-07
	// wiring mouse support to the Audio category.
	return lipgloss.NewStyle().
		Width(width).Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// isAudioSliderField reports whether field idx is a continuous-value field
// that uses the ←/→ slider mode (as opposed to a toggle or device picker).
func isAudioSliderField(idx int) bool {
	return idx == 2 || idx == 3 || idx == 5 // Input Gain, Output Volume, VAD Threshold
}

// adjustAudioSlider nudges the value for the currently-active slider field.
// delta is +1 (right arrow) or -1 (left arrow); each step = 0.025.
func (a *App) adjustAudioSlider(s *SettingsState, delta int) {
	if !s.AudioSliderActive {
		return
	}
	const step = 0.025
	cfg := &a.audioConfig
	d := float64(delta) * step
	switch s.AudioFocusField {
	case 2: // Input Gain  0.0–2.0
		cfg.InputGain = clampF(cfg.InputGain+d, 0.0, 2.0)
	case 3: // Output Volume  0.0–1.0
		cfg.OutputVolume = clampF(cfg.OutputVolume+d, 0.0, 1.0)
	case 5: // VAD Threshold  0.0–1.0
		cfg.VADThreshold = clampF(cfg.VADThreshold+d, 0.0, 1.0)
	}
	a.saveAudioConfig()
	if a.voiceEngine != nil {
		a.voiceEngine.UpdateConfig(*cfg)
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// handleAudioFieldActivate is called on Enter/Space for the Audio category.
func (a *App) handleAudioFieldActivate(s *SettingsState) {
	cfg := &a.audioConfig

	// Slider fields: Enter activates/deactivates slider mode instead of cycling.
	if isAudioSliderField(s.AudioFocusField) {
		s.AudioSliderActive = !s.AudioSliderActive
		return
	}

	switch s.AudioFocusField {
	case 0: // Input Device — open device picker
		a.openAudioDevicePicker(s, 0)
		return
	case 1: // Output Device — open device picker
		a.openAudioDevicePicker(s, 1)
		return
	case 4: // VAD toggle
		cfg.VADEnabled = !cfg.VADEnabled
		if cfg.VADEnabled {
			cfg.PTTEnabled = false // VAD and PTT are mutually exclusive
		}
	case 5: // VAD Threshold: cycle 0.2 → 0.4 → 0.6 → 0.8 → 0.2
		switch {
		case cfg.VADThreshold < 0.2:
			cfg.VADThreshold = 0.2
		case cfg.VADThreshold < 0.4:
			cfg.VADThreshold = 0.4
		case cfg.VADThreshold < 0.6:
			cfg.VADThreshold = 0.6
		case cfg.VADThreshold < 0.8:
			cfg.VADThreshold = 0.8
		default:
			cfg.VADThreshold = 0.2
		}
	case 6: // PTT toggle
		cfg.PTTEnabled = !cfg.PTTEnabled
		if cfg.PTTEnabled {
			cfg.VADEnabled = false // VAD and PTT are mutually exclusive
		}
	case 7: // PTT Key — cycle through common options
		keys := []string{"ctrl+space", "ctrl+alt+m", "alt+v"}
		cur := cfg.PTTKey
		next := keys[0]
		for i, k := range keys {
			if k == cur && i+1 < len(keys) {
				next = keys[i+1]
				break
			}
		}
		cfg.PTTKey = next
	case 8: // Noise Suppression toggle
		cfg.NoiseSuppress = !cfg.NoiseSuppress
	case 9: // Echo Cancellation toggle
		cfg.EchoCancellation = !cfg.EchoCancellation
	case 10: // Codec Preset: cycle low → medium → high → ultra → low
		switch cfg.CodecPreset {
		case "low":
			cfg.CodecPreset = "medium"
		case "medium":
			cfg.CodecPreset = "high"
		case "high":
			cfg.CodecPreset = "ultra"
		default: // "ultra" or unknown
			cfg.CodecPreset = "low"
		}
	}

	a.saveAudioConfig()
}

// openAudioDevicePicker loads the device list and opens the inline picker for the given
// target (0=input, 1=output). On error (no audio hardware / stub build) an empty list
// with a single placeholder entry is shown so the UI doesn't panic.
func (a *App) openAudioDevicePicker(s *SettingsState, target int) {
	// Prefer the engine's existing context to avoid creating a second concurrent
	// WASAPI context, which silently returns no devices on some Windows configs.
	var inputs, outputs []AudioDevice
	if a.voiceEngine != nil {
		inputs, outputs, _ = a.voiceEngine.ListDevices()
	} else {
		inputs, outputs, _ = GetAudioDevices()
	}
	var devices []AudioDevice
	// Prepend a "System Default" sentinel so users can always go back to the default.
	devices = append(devices, AudioDevice{ID: "", Name: "System Default"})
	if target == 0 {
		devices = append(devices, inputs...)
	} else {
		devices = append(devices, outputs...)
	}
	if len(devices) == 1 && len(inputs)+len(outputs) == 0 {
		// No real devices (headless / stub build): show informational placeholder.
		devices = append(devices, AudioDevice{ID: "__none__", Name: "(no devices found)"})
	}

	// Position cursor on the currently selected device.
	cursor := 0
	currentID := a.audioConfig.InputDevice
	if target == 1 {
		currentID = a.audioConfig.OutputDevice
	}
	for i, d := range devices {
		if d.ID == currentID {
			cursor = i
			break
		}
	}

	s.AudioPickerOpen = true
	s.AudioPickerTarget = target
	s.AudioPickerDevices = devices
	s.AudioPickerCursor = cursor
}

// handleAudioPickerSelect confirms the highlighted device and closes the picker.
func (a *App) handleAudioPickerSelect(s *SettingsState) {
	if !s.AudioPickerOpen || s.AudioPickerCursor >= len(s.AudioPickerDevices) {
		s.AudioPickerOpen = false
		return
	}
	chosen := s.AudioPickerDevices[s.AudioPickerCursor]
	// "__none__" is the informational placeholder — ignore it.
	if chosen.ID == "__none__" {
		s.AudioPickerOpen = false
		s.AudioPickerDevices = nil
		return
	}
	if s.AudioPickerTarget == 0 {
		a.audioConfig.InputDevice = chosen.ID
		a.audioConfig.InputDeviceName = chosen.Name
	} else {
		a.audioConfig.OutputDevice = chosen.ID
		a.audioConfig.OutputDeviceName = chosen.Name
	}
	a.saveAudioConfig()
	// Apply new device to a running engine if present.
	if a.voiceEngine != nil {
		a.voiceEngine.UpdateConfig(a.audioConfig)
	}
	s.AudioPickerOpen = false
	s.AudioPickerDevices = nil
}

// renderDevicePickerInline writes an inline scrollable device list into the
// given sectionBuilder. It mimics the wiremix dropdown pattern: a border box
// with `>` prefix on the highlighted row and scroll hints when the list is tall.
func (a *App) renderDevicePickerInline(sb *settingsSectionBuilder, s *SettingsState, width int) {
	const maxVisible = 6
	devices := s.AudioPickerDevices
	if len(devices) == 0 {
		return
	}

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))

	// Determine scroll window.
	start := s.AudioPickerCursor - maxVisible/2
	if start < 0 {
		start = 0
	}
	if start+maxVisible > len(devices) {
		start = len(devices) - maxVisible
		if start < 0 {
			start = 0
		}
	}
	end := start + maxVisible
	if end > len(devices) {
		end = len(devices)
	}

	innerW := width - 6 // indent + borders
	if innerW < 10 {
		innerW = 10
	}

	sb.writeLine(borderStyle.Render("    ┌" + strings.Repeat("─", innerW) + "┐"))
	if start > 0 {
		sb.writeLine(borderStyle.Render("    │") + dimStyle.Render(fmt.Sprintf(" %-*s", innerW-1, "↑ more…")) + borderStyle.Render("│"))
	}
	for i := start; i < end; i++ {
		d := devices[i]
		label := d.Name
		maxLabel := innerW - 3 // 2 chars prefix + 1 char margin
		if len([]rune(label)) > maxLabel {
			label = string([]rune(label)[:maxLabel-1]) + "…"
		}
		if i == s.AudioPickerCursor {
			row := fmt.Sprintf("> %-*s", innerW-2, label)
			sb.writeLine(zone.Mark(fmt.Sprintf("audio-device-row:%d", i), borderStyle.Render("    │")+selectedStyle.Render(row)+borderStyle.Render("│")))
		} else {
			row := fmt.Sprintf("  %-*s", innerW-2, label)
			sb.writeLine(zone.Mark(fmt.Sprintf("audio-device-row:%d", i), borderStyle.Render("    │")+normalStyle.Render(row)+borderStyle.Render("│")))
		}
	}
	if end < len(devices) {
		sb.writeLine(borderStyle.Render("    │") + dimStyle.Render(fmt.Sprintf(" %-*s", innerW-1, "↓ more…")) + borderStyle.Render("│"))
	}
	sb.writeLine(borderStyle.Render("    └" + strings.Repeat("─", innerW) + "┘"))
	sb.writeLine(dimStyle.Render("    ↑↓ navigate · Enter select · Esc cancel"))
	sb.writeBlank()
}
