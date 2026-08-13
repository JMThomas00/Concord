package client

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/models"
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
	// Edit mode locks a plugin channel's TypeIndex at 0 (see OriginalType's
	// doc comment) — so isPlugin can't rely on TypeIndex alone there, or an
	// existing plugin channel's create_fields would never get laid out for
	// editing.
	isPlugin := state.TypeIndex >= 3 || (state.Mode == "edit" && state.OriginalType == models.ChannelTypePlugin)
	l := channelFormFieldLayout{isVoice: state.TypeIndex == 1, isPlugin: isPlugin}
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

// buildFieldEditors constructs the parallel textInputs/values slices for a
// list of manifest-declared fields, seeded from currentValues (server-known
// values, e.g. an existing plugin's saved config) falling back to each
// field's declared Default. Shared by the channel-creation plugin fields and
// the Settings > Plugins config sub-page — the only two places that render
// manifest-declared fields generically.
func buildFieldEditors(fields []protocol.PluginField, currentValues map[string]string) ([]textinput.Model, []string) {
	textInputs := make([]textinput.Model, len(fields))
	values := make([]string, len(fields))

	for i, f := range fields {
		val, ok := currentValues[f.Key]
		if !ok {
			val = f.Default
		}
		switch f.Type {
		case "text", "number":
			ti := textinput.New()
			ti.SetValue(val)
			ti.CharLimit = 100
			ti.Width = 40
			textInputs[i] = ti
		default: // boolean, select, channel_select
			if f.Type == "boolean" && val == "" {
				val = "false"
			}
			if f.Type == "select" && val == "" && len(f.Options) > 0 {
				val = f.Options[0]
			}
			values[i] = val
		}
	}
	return textInputs, values
}

// collectFieldValues reads the current values out of a set of field editors
// into the map shape both ChannelCreateRequest.PluginConfig and
// PluginConfigSetRequest.Config expect.
func collectFieldValues(fields []protocol.PluginField, textInputs []textinput.Model, values []string) map[string]string {
	result := make(map[string]string, len(fields))
	for i, f := range fields {
		switch f.Type {
		case "text", "number":
			result[f.Key] = textInputs[i].Value()
		default:
			result[f.Key] = values[i]
		}
	}
	return result
}

// setPluginKind switches the channel-creation form to a plugin channel kind,
// rebuilding the field editors for that kind's declared create_fields.
func setPluginKind(state *ChannelFormState, info protocol.PluginChannelKindInfo) {
	state.PluginID = info.PluginID
	state.PluginKind = info.Kind
	state.PluginFields = info.CreateFields
	state.PluginTextInputs, state.PluginValues = buildFieldEditors(info.CreateFields, nil)
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

// pluginConfigValues collects the channel-creation form's current field
// values into the map shape ChannelCreateRequest.PluginConfig expects.
func pluginConfigValues(state *ChannelFormState) map[string]string {
	return collectFieldValues(state.PluginFields, state.PluginTextInputs, state.PluginValues)
}

// pluginDisplayLabel formats a plugin's list-row/header label as
// "Product (Name)" when the manifest declares [plugin].product as a family
// distinct from this install's own name — e.g. multiple Mynah personas
// ("Burt", "Alice") installed side by side would otherwise show up as
// unrelated plugins named after the persona, with no indication they're the
// same underlying product. Falls back to plain name for a plugin like Tukan
// that doesn't declare one.
func pluginDisplayLabel(name, product string) string {
	if product == "" || product == name {
		return name
	}
	return fmt.Sprintf("%s (%s)", product, name)
}

// newPluginConfigFormState builds the Settings > Plugins config sub-page
// state for one plugin, seeded from its currently-saved server config values.
func newPluginConfigFormState(info protocol.PluginInfo) *PluginConfigFormState {
	textInputs, values := buildFieldEditors(info.ConfigFields, info.ConfigValues)
	return &PluginConfigFormState{
		PluginID:   info.ID,
		Product:    info.Product,
		Fields:     info.ConfigFields,
		TextInputs: textInputs,
		Values:     values,
	}
}

// pluginConfigFormValues collects the config sub-page's current field values
// into the map shape PluginConfigSetRequest.Config expects.
func pluginConfigFormValues(state *PluginConfigFormState) map[string]string {
	return collectFieldValues(state.Fields, state.TextInputs, state.Values)
}

// handleOpenPluginConfigAction opens the Settings > Plugins > <name> config
// sub-page for the currently selected plugin (Enter on the plugin list).
func (a *App) handleOpenPluginConfigAction() {
	s := a.serverManagementState
	if s.SelectedPlugin < 0 || s.SelectedPlugin >= len(s.PluginList) {
		return
	}
	s.PluginConfigState = newPluginConfigFormState(s.PluginList[s.SelectedPlugin])
}

// handleTogglePluginAction flips the selected plugin's enabled flag ("t" on
// the plugin list) — the Manager starts/stops its Supervisor live, no
// restart of Concord required.
func (a *App) handleTogglePluginAction() {
	s := a.serverManagementState
	if s.SelectedPlugin < 0 || s.SelectedPlugin >= len(s.PluginList) {
		return
	}
	newEnabled := !s.PluginList[s.SelectedPlugin].Enabled
	a.sendPluginConfigSet(s.PluginList[s.SelectedPlugin].ID, &newEnabled, nil)
}

// sendPluginConfigSet sends OpPluginConfigSet with whichever of
// enabled/config the caller wants to change (either may be nil/empty).
func (a *App) sendPluginConfigSet(pluginID string, enabled *bool, config map[string]string) {
	if a.activeConn == nil || a.currentServer == nil {
		return
	}
	req := &protocol.PluginConfigSetRequest{
		ServerID: a.currentServer.ID,
		PluginID: pluginID,
		Enabled:  enabled,
		Config:   config,
	}
	msg, err := protocol.NewMessage(protocol.OpPluginConfigSet, req)
	if err == nil {
		_ = a.activeConn.Connection.Send(msg)
	}
}

// handlePluginConfigKey drives the Settings > Plugins > <name> config
// sub-page: tab/shift+tab cycle fields (then Save, then Back), up/down/
// left/right cycle boolean/select/channel_select values, and text/number
// fields forward keystrokes to their textinput.Model — mirroring
// handleChannelFormKey's pattern for the same generic field types.
func (a *App) handlePluginConfigKey(msg tea.KeyMsg) tea.Cmd {
	state := a.serverManagementState.PluginConfigState
	saveField := len(state.Fields)
	backField := len(state.Fields) + 1

	blurAll := func() {
		for i := range state.TextInputs {
			state.TextInputs[i].Blur()
		}
	}
	focusField := func(f int) {
		if f < 0 || f >= len(state.Fields) {
			return
		}
		ft := state.Fields[f].Type
		if ft == "text" || ft == "number" {
			state.TextInputs[f].Focus()
		}
	}

	switch msg.String() {
	case "esc":
		a.serverManagementState.PluginConfigState = nil
		return nil

	case "tab":
		state.FocusField = (state.FocusField + 1) % (backField + 1)
		blurAll()
		focusField(state.FocusField)
		return nil

	case "shift+tab":
		state.FocusField--
		if state.FocusField < 0 {
			state.FocusField = backField
		}
		blurAll()
		focusField(state.FocusField)
		return nil

	case "up", "down", "left", "right":
		if state.FocusField >= 0 && state.FocusField < len(state.Fields) {
			f := state.Fields[state.FocusField]
			if f.Type != "text" && f.Type != "number" {
				dir := 1
				if msg.String() == "up" || msg.String() == "left" {
					dir = -1
				}
				state.Values[state.FocusField] = cyclePluginFieldValue(f, state.Values[state.FocusField], dir, a.textChannelNames())
			}
		}
		return nil

	case "enter":
		if state.FocusField == saveField {
			return a.handleSavePluginConfig()
		} else if state.FocusField == backField {
			a.serverManagementState.PluginConfigState = nil
		}
		return nil
	}

	if state.FocusField >= 0 && state.FocusField < len(state.Fields) {
		f := state.Fields[state.FocusField]
		if f.Type == "text" || f.Type == "number" {
			var cmd tea.Cmd
			state.TextInputs[state.FocusField], cmd = state.TextInputs[state.FocusField].Update(msg)
			return cmd
		}
	}
	return nil
}

// handleSavePluginConfig sends the edited server_config_field values and
// closes the sub-page; the refreshed plugin list arrives via
// EventPluginConfigUpdate like any other config change.
func (a *App) handleSavePluginConfig() tea.Cmd {
	state := a.serverManagementState.PluginConfigState
	a.sendPluginConfigSet(state.PluginID, nil, pluginConfigFormValues(state))
	a.serverManagementState.PluginConfigState = nil
	a.statusMessage = "Saving plugin configuration..."
	return nil
}
