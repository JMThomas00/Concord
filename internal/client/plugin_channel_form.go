package client

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/concord-chat/concord/internal/protocol"
)

// channelFormFieldLayout describes the FocusField numbering for the current
// state of the channel create/edit form, which varies depending on whether
// the selected type is voice (adds a max-users field) or a plugin kind (adds
// its declared create_fields) — the two are mutually exclusive.
type channelFormFieldLayout struct {
	isVoice      bool
	isPlugin     bool
	pluginStart  int // FocusField of the first plugin field (only meaningful if isPlugin)
	maxUsersField int // FocusField of the max-users field (only meaningful if isVoice)
	submitField  int
	cancelField  int
}

func computeChannelFormLayout(state *ChannelFormState) channelFormFieldLayout {
	l := channelFormFieldLayout{isVoice: state.TypeIndex == 1, isPlugin: state.TypeIndex >= 3}
	next := 2 // 0=name, 1=type
	if l.isPlugin {
		l.pluginStart = next
		next += len(state.PluginFields)
	}
	if l.isVoice {
		l.maxUsersField = next
		next++
	}
	l.submitField = next
	l.cancelField = next + 1
	return l
}

// setPluginKind switches the form to a plugin channel kind, rebuilding the
// text inputs / default values for that kind's declared create_fields.
func setPluginKind(state *ChannelFormState, info protocol.PluginChannelKindInfo) {
	state.PluginID = info.PluginID
	state.PluginKind = info.Kind
	state.PluginFields = info.CreateFields
	state.PluginTextInputs = make([]textinput.Model, len(info.CreateFields))
	state.PluginValues = make([]string, len(info.CreateFields))

	for i, f := range info.CreateFields {
		switch f.Type {
		case "text", "number":
			ti := textinput.New()
			ti.SetValue(f.Default)
			ti.CharLimit = 100
			ti.Width = 40
			state.PluginTextInputs[i] = ti
		default: // boolean, select, channel_select
			state.PluginValues[i] = f.Default
			if f.Type == "boolean" && state.PluginValues[i] == "" {
				state.PluginValues[i] = "false"
			}
			if f.Type == "select" && state.PluginValues[i] == "" && len(f.Options) > 0 {
				state.PluginValues[i] = f.Options[0]
			}
		}
	}
}

// cyclePluginFieldValue advances a boolean/select field's value by one step
// in the given direction (+1 or -1). channelSelectOptions supplies the
// current server's text channel names for "channel_select" fields.
func cyclePluginFieldValue(field protocol.PluginField, current string, dir int, channelSelectOptions []string) string {
	switch field.Type {
	case "boolean":
		if current == "true" {
			return "false"
		}
		return "true"
	case "select":
		return cycleOption(field.Options, current, dir)
	case "channel_select":
		return cycleOption(channelSelectOptions, current, dir)
	}
	return current
}

func cycleOption(options []string, current string, dir int) string {
	if len(options) == 0 {
		return current
	}
	idx := 0
	for i, o := range options {
		if o == current {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(options)) % len(options)
	return options[idx]
}

// pluginConfigValues collects the current field values into the map shape
// ChannelCreateRequest.PluginConfig expects.
func pluginConfigValues(state *ChannelFormState) map[string]string {
	values := make(map[string]string, len(state.PluginFields))
	for i, f := range state.PluginFields {
		switch f.Type {
		case "text", "number":
			values[f.Key] = state.PluginTextInputs[i].Value()
		default:
			values[f.Key] = state.PluginValues[i]
		}
	}
	return values
}
