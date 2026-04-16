//go:build novoice && !windows

// wasapi_devices_other.go enumerates audio devices on Linux and macOS using
// system commands — no CGO or build tags required.
//
// Linux:  uses pactl (PulseAudio/PipeWire) if available, otherwise arecord/aplay (ALSA)
// macOS:  uses system_profiler SPAudioDataType
//
// For fully-featured audio (capture + playback), build without -tags novoice (the default).

package client

import (
	"os/exec"
	"runtime"
	"strings"
)

// GetAudioDevices enumerates active audio input and output devices.
func GetAudioDevices() (inputs, outputs []AudioDevice, err error) {
	switch runtime.GOOS {
	case "linux":
		inputs = linuxPADevices("sources")
		outputs = linuxPADevices("sinks")
		if len(inputs)+len(outputs) == 0 {
			// PulseAudio not available — try ALSA
			inputs = linuxALSADevices("capture")
			outputs = linuxALSADevices("playback")
		}
	case "darwin":
		inputs, outputs = macOSAudioDevices()
	}
	return inputs, outputs, nil
}

// ── Linux: PulseAudio / PipeWire ─────────────────────────────────────────────

// linuxPADevices lists PulseAudio/PipeWire sources or sinks.
// `kind` is "sources" or "sinks".
func linuxPADevices(kind string) []AudioDevice {
	// `pactl list short sources` / `pactl list short sinks`
	// Output columns (tab-separated): index  name  module  format  state
	out, err := exec.Command("pactl", "list", "short", kind).Output()
	if err != nil {
		return nil
	}
	var devs []AudioDevice
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[1]
		// Skip monitor sources (loopback of output devices)
		if kind == "sources" && strings.HasSuffix(name, ".monitor") {
			continue
		}
		devs = append(devs, AudioDevice{ID: name, Name: paFriendlyName(name)})
	}
	return devs
}

// paFriendlyName converts a PulseAudio sink/source name to a human-readable label.
// e.g. "alsa_input.pci-0000_00_1b.0.analog-stereo" → "Analog Stereo Input"
func paFriendlyName(name string) string {
	// Ask pactl for the Description property of this specific source/sink.
	for _, kind := range []string{"sources", "sinks"} {
		out, err := exec.Command("pactl", "list", kind).Output()
		if err != nil {
			continue
		}
		lines := strings.Split(string(out), "\n")
		inBlock := false
		for _, l := range lines {
			if strings.Contains(l, "Name: "+name) {
				inBlock = true
			}
			if inBlock && strings.Contains(l, "device.description") {
				// device.description = "SteelSeries Sonar - Microphone"
				parts := strings.SplitN(l, "=", 2)
				if len(parts) == 2 {
					desc := strings.Trim(strings.TrimSpace(parts[1]), `"`)
					if desc != "" {
						return desc
					}
				}
			}
			if inBlock && strings.HasPrefix(strings.TrimSpace(l), "Name:") && !strings.Contains(l, name) {
				break // moved to next device block
			}
		}
	}
	// Fallback: prettify the raw name
	s := name
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.Title(s) //nolint:staticcheck // simple title-case for display
}

// linuxALSADevices lists ALSA devices via arecord/aplay as a PulseAudio fallback.
func linuxALSADevices(direction string) []AudioDevice {
	tool := "aplay"
	if direction == "capture" {
		tool = "arecord"
	}
	out, err := exec.Command(tool, "-l").Output()
	if err != nil {
		return nil
	}
	var devs []AudioDevice
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "card ") {
			continue
		}
		// card 0: PCH [HDA Intel PCH], device 0: ALC892 Analog [ALC892 Analog]
		//   → ID "hw:0,0", Name "HDA Intel PCH – ALC892 Analog"
		var cardIdx, devIdx int
		var cardName, devName string
		// Extract card index and name
		rest := strings.TrimPrefix(line, "card ")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) < 2 {
			continue
		}
		_, _ = cardIdx, devIdx
		cardIdx = int(rest[0] - '0')
		tail := parts[1]
		// "PCH [HDA Intel PCH], device 0: ALC892 Analog [ALC892 Analog]"
		bracketStart := strings.Index(tail, "[")
		bracketEnd := strings.Index(tail, "]")
		if bracketStart >= 0 && bracketEnd > bracketStart {
			cardName = tail[bracketStart+1 : bracketEnd]
		}
		devPart := strings.Index(tail, "device ")
		if devPart >= 0 {
			devStr := tail[devPart+7:]
			colonIdx := strings.Index(devStr, ":")
			if colonIdx > 0 {
				devIdx = int(devStr[0] - '0')
				devTail := devStr[colonIdx+1:]
				br1 := strings.Index(devTail, "[")
				br2 := strings.Index(devTail, "]")
				if br1 >= 0 && br2 > br1 {
					devName = devTail[br1+1 : br2]
				}
			}
		}
		id := "hw:" + string(rune('0'+cardIdx)) + "," + string(rune('0'+devIdx))
		name := cardName
		if devName != "" && devName != cardName {
			name += " – " + devName
		}
		if name == "" {
			name = id
		}
		devs = append(devs, AudioDevice{ID: id, Name: name})
	}
	return devs
}

// ── macOS: CoreAudio via system_profiler ──────────────────────────────────────

func macOSAudioDevices() (inputs, outputs []AudioDevice) {
	out, err := exec.Command("system_profiler", "SPAudioDataType").Output()
	if err != nil {
		return nil, nil
	}
	// Parse text output — each device block looks like:
	//   SteelSeries Sonar - Microphone:
	//     Manufacturer: SteelSeries
	//     ...
	//     Input Channels: 1
	// We identify device names by indentation (4 spaces) + colon suffix.
	lines := strings.Split(string(out), "\n")
	var currentName string
	var isInput, isOutput bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Device name: 4-space indent, ends with ':'
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") && strings.HasSuffix(trimmed, ":") {
			// Flush previous device
			if currentName != "" {
				dev := AudioDevice{ID: currentName, Name: currentName}
				if isInput {
					inputs = append(inputs, dev)
				}
				if isOutput {
					outputs = append(outputs, dev)
				}
			}
			currentName = strings.TrimSuffix(trimmed, ":")
			isInput, isOutput = false, false
		}
		if strings.Contains(trimmed, "Input Channels:") {
			isInput = true
		}
		if strings.Contains(trimmed, "Output Channels:") {
			isOutput = true
		}
	}
	// Flush last device
	if currentName != "" {
		dev := AudioDevice{ID: currentName, Name: currentName}
		if isInput {
			inputs = append(inputs, dev)
		}
		if isOutput {
			outputs = append(outputs, dev)
		}
	}
	return inputs, outputs
}
