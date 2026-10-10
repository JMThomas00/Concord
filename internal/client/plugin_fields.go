package client

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
)

// Shared pieces of the two forms that edit manifest-declared plugin fields:
// a plugin's settings (Server Settings → Plugins) and a plugin channel's
// Configure page (Server Settings → Channels).

// channelPicker edits a channel_multi_select field: a checklist of the
// server's text channels, opened with Enter on the field.
type channelPicker struct {
	Field    int      // index of the field being edited
	Label    string   // the field's label, for the page title
	Options  []string // text channel IDs, in server order
	Selected map[string]bool
	Cursor   int // 0..len(Options)-1 channels, len(Options) = Done
}

func newChannelPicker(field int, label, value string, options []string) *channelPicker {
	p := &channelPicker{Field: field, Label: label, Options: options, Selected: map[string]bool{}}
	for _, id := range splitChannelList(value) {
		p.Selected[id] = true
	}
	return p
}

// Value is the selection as stored: comma-separated IDs in channel order.
func (p *channelPicker) Value() string {
	var ids []string
	for _, id := range p.Options {
		if p.Selected[id] {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

// Key handles a key; done reports that the picker should close (Esc, or
// Enter on Done). Space or Enter on a channel toggles it; A selects all or
// none.
func (p *channelPicker) Key(key string) (done bool) {
	switch key {
	case "up", "k", "shift+tab":
		if p.Cursor > 0 {
			p.Cursor--
		}
	case "down", "j", "tab":
		if p.Cursor < len(p.Options) {
			p.Cursor++
		}
	case " ", "space", "enter":
		if p.Cursor == len(p.Options) {
			return key == "enter"
		}
		id := p.Options[p.Cursor]
		p.Selected[id] = !p.Selected[id]
	case "a", "A":
		all := len(p.Value()) > 0 && len(splitChannelList(p.Value())) == len(p.Options)
		for _, id := range p.Options {
			p.Selected[id] = !all
		}
	case "esc":
		return true
	}
	return false
}

func splitChannelList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// dimText renders the faint helper style used for "↑ N more" markers.
func (a *App) dimText(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render(s)
}

// writeChannelPicker renders an open channelPicker into sb.
func (a *App) writeChannelPicker(sb *settingsSectionBuilder, p *channelPicker) {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	sb.writeLine(labelStyle.Render("▸ " + p.Label + ":"))
	sb.writeLine(a.dimText("  Space or Enter checks a channel · A all/none · Esc done"))
	sb.writeBlank()
	names := map[string]string{}
	for _, ch := range a.getCurrentChannels() {
		names[ch.ID.String()] = ch.Name
	}
	cur := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	for i, id := range p.Options {
		box := "[ ]"
		if p.Selected[id] {
			box = "[x]"
		}
		line := "  " + box + " #" + names[id]
		if i == p.Cursor {
			sb.markFocus()
			line = cur.Render("▶ " + box + " #" + names[id])
		}
		sb.writeLine(line)
	}
	if len(p.Options) == 0 {
		sb.writeLine(a.dimText("  This server has no text channels yet."))
	}
	sb.writeBlank()
	done := "  [Done]"
	if p.Cursor == len(p.Options) {
		sb.markFocus()
		done = cur.Render("▶ [Done]")
	}
	sb.writeLine(done)
}

// writePluginField renders one manifest field (label, editor, then its
// help or what's wrong with it) into sb, marking it for scrolling when
// it has focus.
func (a *App) writePluginField(sb *settingsSectionBuilder, f protocol.PluginField, input *textinput.Model, value string, focused bool, problem string) {
	if focused {
		sb.markFocus()
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	required := ""
	if f.Required {
		required = " *"
	}
	sb.writeLine(labelStyle.Render("▸ " + f.Label + required + ":"))

	valStyle := lipgloss.NewStyle()
	if focused {
		valStyle = valStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan))
	}
	switch {
	case isTextField(f.Type):
		sb.writeLine(valStyle.Render("  " + input.View()))
	case f.Type == "channel_multi_select":
		hint := ""
		if focused {
			hint = a.dimText("  (Enter to choose)")
		}
		sb.writeLine(valStyle.Render("  "+a.pluginFieldValueLabel(f, value)) + hint)
	default:
		sb.writeLine(valStyle.Render("  ◂ " + a.pluginFieldValueLabel(f, value) + " ▸"))
	}
	if problem != "" {
		sb.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Render("  ⚠ " + problem))
	} else if f.Help != "" {
		sb.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Italic(true).Render("  " + f.Help))
	}
	sb.writeBlank()
}

// channelNames turns a channel_multi_select value into "#a, #b".
func (a *App) channelNames(value string) string {
	ids := splitChannelList(value)
	if len(ids) == 0 {
		return "(none picked)"
	}
	byID := map[uuid.UUID]string{}
	for _, ch := range a.getCurrentChannels() {
		byID[ch.ID] = ch.Name
	}
	var names []string
	for _, s := range ids {
		id, err := uuid.Parse(s)
		if name, ok := byID[id]; err == nil && ok {
			names = append(names, "#"+name)
		} else {
			names = append(names, "(deleted channel)")
		}
	}
	return strings.Join(names, ", ")
}

// truncateRunes shortens s to at most n characters, ending in "…".
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if n <= 0 {
		return ""
	}
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// channelConfigSummary is the one-line digest of a plugin channel's
// settings shown beside its Configure row: which instance, then each
// field's value.
func (a *App) channelConfigSummary(state *ChannelFormState) string {
	var parts []string
	if instanceRow, _ := configRows(state); instanceRow {
		parts = append(parts, state.InstanceOptions[state.InstanceIndex].InstanceName)
	}
	for i, f := range state.PluginFields {
		v := state.PluginValues[i]
		if isTextField(f.Type) {
			v = state.PluginTextInputs[i].Value()
			if f.Type == "secret" && v != "" {
				v = "••••"
			}
		} else if f.Type == "channel_multi_select" && v == "" {
			continue
		} else {
			v = a.pluginFieldValueLabel(f, v)
		}
		if v != "" {
			parts = append(parts, f.Label+": "+v)
		}
	}
	return strings.Join(parts, " · ")
}

// renderChannelConfigPage is a plugin channel's Configure page: which
// instance owns it (when the plugin has several), then the settings the
// plugin declares for its channels. It scrolls, so any number fits.
func (a *App) renderChannelConfigPage(width, height int, s *ServerManagementState) string {
	state := s.ChannelFormState
	layout := calculateSettingsLayout(width, height, 2, 0)
	kindName := a.channelFormKindName(state)

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	top.writeLine(headerStyle.Render("Configure " + kindName + " channel"))
	top.writeLine(a.dimText("Settings for this channel. Settings for the whole plugin are in Server Settings → Plugins."))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	middle := newScrollSection(layout.middleLines, layout.interiorWidth, a.dimText)
	instanceRow, rows := configRows(state)
	switch {
	case state.Picker != nil:
		a.writeChannelPicker(middle, state.Picker)
	default:
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
		if instanceRow {
			focused := state.ConfigFocus == 0
			if focused {
				middle.markFocus()
			}
			middle.writeLine(labelStyle.Render("▸ Which " + kindName + ":"))
			valStyle := lipgloss.NewStyle()
			if focused {
				valStyle = valStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan))
			}
			middle.writeLine(valStyle.Render("  ◂ " + state.InstanceOptions[state.InstanceIndex].InstanceName + " ▸"))
			middle.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Italic(true).
				Render("  The instance that answers in this channel"))
			middle.writeBlank()
		}
		if len(state.PluginFields) == 0 && !instanceRow {
			middle.writeLine(a.dimText("  This kind of channel has no settings of its own."))
			middle.writeBlank()
		}
		for i, f := range state.PluginFields {
			a.writePluginField(middle, f, &state.PluginTextInputs[i], state.PluginValues[i], channelConfigField(state) == i, "")
		}
		done := "  [Done]"
		if state.ConfigFocus == rows {
			middle.markFocus()
			done = "  " + lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Green)).
				Bold(true).Padding(0, 2).Render("Done")
		}
		middle.writeLine(done)
	}
	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeLine(a.dimText("↑↓/Tab: Move · ←→: Change · Enter: Choose channels / Done · Esc: Back"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// channelFormKindName is the plugin's name for the form's channel kind
// ("Mynah", "Chess Table").
func (a *App) channelFormKindName(state *ChannelFormState) string {
	if state.PluginDisplayLabel != "" {
		return state.PluginDisplayLabel
	}
	for _, k := range a.currentServerKinds() {
		if k.PluginID == state.PluginID && k.Kind == state.PluginKind {
			return k.DisplayName
		}
	}
	return "plugin"
}
