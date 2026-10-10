package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// A plugin's page (Server Settings → Plugins → Enter): everything about one
// installed plugin in one place. Its settings — or, for a plugin with
// several instances (Mynah's personas), each instance's settings — and every
// channel that uses it.

// PluginPageState is the open plugin page.
type PluginPageState struct {
	BaseID string // the installed plugin this page is for
	Cursor int
	// Confirm holds the ID of an instance awaiting a second X to remove it.
	Confirm string
	// Naming is set while typing a name: "add", or the ID being renamed.
	Naming    string
	NameInput textinput.Model
}

// pluginPageRow is one selectable row of the page.
type pluginPageRow struct {
	kind    string // "settings", "instance", "add", "channel", "newchannel"
	plugin  *protocol.PluginInfo
	channel *models.Channel
}

// pluginWithInstances returns the installed plugin baseID and its
// instances (the plugin itself first).
func (s *ServerManagementState) pluginWithInstances(baseID string) []*protocol.PluginInfo {
	var base *protocol.PluginInfo
	var out []*protocol.PluginInfo
	for i := range s.AllPlugins {
		p := &s.AllPlugins[i]
		switch {
		case p.ID == baseID:
			base = p
		case p.BaseID == baseID:
			out = append(out, p)
		}
	}
	if base == nil {
		return nil
	}
	return append([]*protocol.PluginInfo{base}, out...)
}

// pluginPageRows lists the page's selectable rows in order.
func (a *App) pluginPageRows(s *ServerManagementState) []pluginPageRow {
	page := s.PluginPage
	plugins := s.pluginWithInstances(page.BaseID)
	if len(plugins) == 0 {
		return nil
	}
	var rows []pluginPageRow
	if plugins[0].AllowsInstances {
		for _, p := range plugins {
			rows = append(rows, pluginPageRow{kind: "instance", plugin: p})
		}
		rows = append(rows, pluginPageRow{kind: "add"})
	} else {
		rows = append(rows, pluginPageRow{kind: "settings", plugin: plugins[0]})
	}
	ids := map[string]bool{}
	for _, p := range plugins {
		ids[p.ID] = true
	}
	for _, ch := range a.getCurrentChannels() {
		if ch.Type == models.ChannelTypePlugin && ids[ch.PluginID] {
			rows = append(rows, pluginPageRow{kind: "channel", channel: ch})
		}
	}
	if len(a.baseKindsFor(page.BaseID)) > 0 {
		rows = append(rows, pluginPageRow{kind: "newchannel"})
	}
	return rows
}

// baseKindsFor is the channel kinds plugin baseID offers on this server.
func (a *App) baseKindsFor(baseID string) []protocol.PluginChannelKindInfo {
	var out []protocol.PluginChannelKindInfo
	for _, k := range a.pluginKindOptions() {
		if k.PluginID == baseID {
			out = append(out, k)
		}
	}
	return out
}

// instanceName is the display name of the plugin or instance with this ID.
func (s *ServerManagementState) instanceName(id string) string {
	for _, p := range s.AllPlugins {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

// openPluginPage opens the page for the plugin selected in the list.
func (a *App) openPluginPage() {
	s := a.serverManagementState
	if s.SelectedPlugin < 0 || s.SelectedPlugin >= len(s.PluginList) {
		return
	}
	s.PluginPage = &PluginPageState{BaseID: s.PluginList[s.SelectedPlugin].ID}
	s.PluginNotice = ""
}

// handlePluginPageKey drives the plugin page.
func (a *App) handlePluginPageKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	page := s.PluginPage
	if page.Naming != "" {
		return a.handlePluginPageNaming(msg)
	}
	rows := a.pluginPageRows(s)
	if page.Cursor >= len(rows) {
		page.Cursor = max(0, len(rows)-1)
	}
	var row pluginPageRow
	if len(rows) > 0 {
		row = rows[page.Cursor]
	}
	k := msg.String()
	if k != "x" && k != "X" {
		page.Confirm = ""
	}
	switch k {
	case "esc":
		s.PluginPage = nil
	case "up", "k":
		if page.Cursor > 0 {
			page.Cursor--
		}
	case "down", "j":
		if page.Cursor < len(rows)-1 {
			page.Cursor++
		}
	case "enter":
		switch row.kind {
		case "settings", "instance":
			s.PluginConfigState = newPluginConfigFormState(*row.plugin)
		case "add":
			a.startPluginPageNaming("add", "")
		case "channel":
			a.editChannelFromPluginPage(row.channel)
		case "newchannel":
			a.newChannelFromPluginPage(page.BaseID)
		}
	case "n", "N":
		if row.kind == "instance" {
			a.startPluginPageNaming(row.plugin.ID, row.plugin.Name)
		}
	case "r", "R":
		if row.plugin != nil {
			a.runPluginAction(protocol.PluginActionRestart, row.plugin.ID, "Restarting "+row.plugin.Name+"…")
		}
	case "t", "T":
		if row.plugin != nil {
			enabled := !row.plugin.Enabled
			a.sendPluginConfigSet(row.plugin.ID, &enabled, nil)
		}
	case "x", "X":
		if row.kind != "instance" || row.plugin.BaseID == "" {
			if row.kind == "instance" {
				s.PluginNotice, s.PluginNoticeError = row.plugin.Name+" is the plugin itself: rename it, or uninstall the plugin from the list.", true
			}
			return nil
		}
		if page.Confirm != row.plugin.ID {
			page.Confirm = row.plugin.ID
			s.PluginNotice, s.PluginNoticeError = "Press X again to remove "+row.plugin.Name+" (its channels and settings are kept).", true
			return nil
		}
		page.Confirm = ""
		a.runPluginAction(protocol.PluginActionRemoveInstance, row.plugin.ID, "Removing "+row.plugin.Name+"…")
	}
	return nil
}

func (a *App) startPluginPageNaming(what, current string) {
	page := a.serverManagementState.PluginPage
	in := textinput.New()
	in.CharLimit = 32
	in.Width = 32
	in.Placeholder = "a name, e.g. Alice"
	in.SetValue(current)
	in.CursorEnd()
	in.Focus()
	page.Naming, page.NameInput = what, in
}

func (a *App) handlePluginPageNaming(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	page := s.PluginPage
	switch msg.String() {
	case "esc":
		page.Naming = ""
		return nil
	case "enter":
		name := strings.TrimSpace(page.NameInput.Value())
		if name == "" {
			return nil
		}
		if page.Naming == "add" {
			if !a.sendPluginManage(protocol.PluginManageRequest{Action: protocol.PluginActionAddInstance, PluginID: page.BaseID, Name: name}) {
				s.PluginNotice, s.PluginNoticeError = "Not connected", true
			} else {
				s.PluginNotice, s.PluginNoticeError = "Adding "+name+"…", false
			}
		} else {
			if !a.sendPluginManage(protocol.PluginManageRequest{Action: protocol.PluginActionRenameInstance, PluginID: page.Naming, Name: name}) {
				s.PluginNotice, s.PluginNoticeError = "Not connected", true
			} else {
				s.PluginNotice, s.PluginNoticeError = "Renaming to "+name+"…", false
			}
		}
		page.Naming = ""
		return nil
	}
	var cmd tea.Cmd
	page.NameInput, cmd = page.NameInput.Update(msg)
	return cmd
}

// editChannelFromPluginPage opens a plugin channel's edit form (Server
// Settings → Channels), the same as E on it there.
func (a *App) editChannelFromPluginPage(ch *models.Channel) {
	s := a.serverManagementState
	if a.currentServer == nil {
		return
	}
	a.switchToChannelsCategory()
	for i, c := range s.ChannelList {
		if c.ID == ch.ID {
			s.SelectedChannel = i
			a.handleEditAction()
			return
		}
	}
}

// newChannelFromPluginPage opens the create form with this plugin's channel
// type already chosen.
func (a *App) newChannelFromPluginPage(baseID string) {
	s := a.serverManagementState
	a.switchToChannelsCategory()
	s.SelectedChannel = -1
	a.handleCreateAction()
	state := s.ChannelFormState
	if state == nil {
		return
	}
	for i, k := range a.pluginKindOptions() {
		if k.PluginID == baseID {
			state.TypeIndex = 3 + i
			a.setPluginKind(state, k)
			return
		}
	}
}

// switchToChannelsCategory moves Server Settings to its Channels page.
func (a *App) switchToChannelsCategory() {
	s := a.serverManagementState
	s.PluginPage = nil
	s.SelectedCategory = 0
	s.FocusOnForm = true
	if a.currentServer != nil {
		a.loadChannelListForManagement(a.currentServer.ID)
	}
}

// legacyInstanceTarget reports, for a plugin installed on its own whose
// product is another installed plugin that takes instances (an old Mynah
// persona folder beside the new Mynah), that plugin — what M would make it
// an instance of.
func (s *ServerManagementState) legacyInstanceTarget(p protocol.PluginInfo) *protocol.PluginInfo {
	if p.BaseID != "" || p.AllowsInstances || p.Product == "" {
		return nil
	}
	for i := range s.AllPlugins {
		t := &s.AllPlugins[i]
		if t.ID != p.ID && t.BaseID == "" && t.AllowsInstances && (strings.EqualFold(t.Product, p.Product) || strings.EqualFold(t.Name, p.Product) || strings.EqualFold(t.ID, p.Product)) {
			return t
		}
	}
	return nil
}

// renderPluginPage draws the plugin page.
func (a *App) renderPluginPage(width, height int, s *ServerManagementState) string {
	page := s.PluginPage
	layout := calculateSettingsLayout(width, height, 3, 1)
	plugins := s.pluginWithInstances(page.BaseID)

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	if len(plugins) == 0 {
		top.writeLine(headerStyle.Render(page.BaseID))
		top.writeLine(a.dimText("This plugin isn't installed any more. Esc to go back."))
	} else {
		base := plugins[0]
		title := base.Product
		if title == "" {
			title = base.Name
		}
		top.writeLine(headerStyle.Render(fmt.Sprintf("%s v%s", title, base.Version)))
		sub := "Its settings and channels"
		if base.AllowsInstances {
			sub = fmt.Sprintf("%d instance(s): each has its own name, settings and channels", len(plugins))
		}
		top.writeLine(a.dimText(sub))
	}
	if s.PluginNotice != "" {
		color := a.theme.Colors.Green
		if s.PluginNoticeError {
			color = a.theme.Colors.Orange
		}
		top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s.PluginNotice))
	} else {
		top.writeBlank()
	}
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	middle := newScrollSection(layout.middleLines, layout.interiorWidth, a.dimText)
	rows := a.pluginPageRows(s)
	section := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	selStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
		Bold(true).Width(layout.interiorWidth)
	lastKind := ""
	for i, row := range rows {
		group := row.kind
		if group == "add" {
			group = "instance"
		}
		if group == "newchannel" {
			group = "channel"
		}
		if group != lastKind {
			if lastKind != "" {
				middle.writeBlank()
			}
			switch group {
			case "instance":
				middle.writeLine(section.Render("Instances"))
			case "settings":
				middle.writeLine(section.Render("Settings"))
			case "channel":
				middle.writeLine(section.Render("Channels"))
			}
			lastKind = group
		}
		var text string
		switch row.kind {
		case "settings":
			text = "Plugin settings — " + row.plugin.Status
		case "instance":
			text = fmt.Sprintf("%s — %s", row.plugin.Name, row.plugin.Status)
			if !row.plugin.Enabled {
				text += " (disabled)"
			}
		case "add":
			text = "+ Add instance"
		case "channel":
			text = "#" + row.channel.Name
			if len(plugins) > 1 {
				text += "  (" + s.instanceName(row.channel.PluginID) + ")"
			}
		case "newchannel":
			text = "+ New channel"
		}
		line := "  " + text
		if i == page.Cursor {
			middle.markFocus()
			line = selStyle.Render("▶ " + text)
		}
		middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-pluginpage-row:%d", i), line))
		if i == page.Cursor && row.plugin != nil && row.plugin.LastError != "" {
			middle.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Italic(true).Render("    ⚠ " + row.plugin.LastError))
		}
	}
	if page.Naming != "" {
		middle.writeBlank()
		label := "Name for the new instance:"
		if page.Naming != "add" {
			label = "New name for " + s.instanceName(page.Naming) + ":"
		}
		middle.markFocus()
		middle.writeLine(section.Render("▸ " + label))
		middle.writeLine("  " + page.NameInput.View())
		middle.writeLine(a.dimText("  Enter to save · Esc to cancel"))
	}
	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	bottom.writeLine(a.dimText("Navigation: ↑↓ select · Enter open · Esc back to the list"))
	bottom.writeLine(a.dimText("Instances: N rename · R restart · T enable/disable · X remove (twice)"))
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

// installedPlugins is the Plugins list: every plugin except instances,
// which are shown on their plugin's page.
func installedPlugins(all []protocol.PluginInfo) []protocol.PluginInfo {
	var out []protocol.PluginInfo
	for _, p := range all {
		if p.BaseID == "" {
			out = append(out, p)
		}
	}
	return out
}

// instanceCount is how many instances the installed plugin p has, itself
// included (0 for a plugin that doesn't take instances).
func (s *ServerManagementState) instanceCount(p protocol.PluginInfo) int {
	if !p.AllowsInstances {
		return 0
	}
	return len(s.pluginWithInstances(p.ID))
}
