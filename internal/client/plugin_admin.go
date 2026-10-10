package client

import (
	"github.com/concord-chat/concord/internal/protocol"
)

// isTextField reports whether a plugin form field is typed in (a text
// input) rather than picked by cycling through values.
func isTextField(fieldType string) bool {
	return fieldType == "text" || fieldType == "number" || fieldType == "secret"
}

// handlePluginListKey handles the Settings > Plugins list's own actions and
// reports whether it used the key:
//
//	i install from a URL · u update the selected plugin · r restart it
//	x uninstall it (press twice) · s rescan the plugins folder
func (a *App) handlePluginListKey(key string) bool {
	s := a.serverManagementState
	var selected *protocol.PluginInfo
	if s.SelectedPlugin >= 0 && s.SelectedPlugin < len(s.PluginList) {
		selected = &s.PluginList[s.SelectedPlugin]
	}
	if key != "x" && key != "X" {
		s.UninstallConfirm = ""
	}

	switch key {
	case "i", "I":
		a.openPluginFetchForm(protocol.PluginActionInstall, "")
	case "u", "U":
		if selected != nil {
			a.openPluginFetchForm(protocol.PluginActionUpdate, selected.ID)
		}
	case "r", "R":
		if selected != nil {
			a.runPluginAction(protocol.PluginActionRestart, selected.ID, "Restarting "+selected.Name+"…")
		}
	case "x", "X":
		if selected == nil {
			return true
		}
		if s.UninstallConfirm != selected.ID {
			s.UninstallConfirm = selected.ID
			s.PluginNotice, s.PluginNoticeError = "Press X again to uninstall "+selected.Name+" (its channels and saved data are kept).", true
			return true
		}
		s.UninstallConfirm = ""
		a.runPluginAction(protocol.PluginActionUninstall, selected.ID, "Uninstalling "+selected.Name+"…")
	case "s", "S":
		a.runPluginAction(protocol.PluginActionRescan, "", "Rescanning the plugins folder…")
	case "m", "M":
		// An old separate install of a plugin that now takes instances (a
		// Mynah persona folder) becomes one of its instances.
		if selected == nil {
			return true
		}
		target := s.legacyInstanceTarget(*selected)
		if target == nil {
			return true
		}
		if !a.sendPluginManage(protocol.PluginManageRequest{Action: protocol.PluginActionAdoptInstance, PluginID: selected.ID, TargetID: target.ID}) {
			s.PluginNotice, s.PluginNoticeError = "Not connected", true
			return true
		}
		s.PluginNotice, s.PluginNoticeError = "Making "+selected.Name+" an instance of "+target.ID+"…", false
	default:
		return false
	}
	return true
}

// runPluginAction sends a one-shot OpPluginManage action and shows that
// it's under way; the outcome arrives via applyPluginManageResult.
func (a *App) runPluginAction(action, pluginID, pending string) {
	s := a.serverManagementState
	if !a.sendPluginManage(protocol.PluginManageRequest{Action: action, PluginID: pluginID}) {
		s.PluginNotice, s.PluginNoticeError = "Not connected", true
		return
	}
	s.PluginNotice, s.PluginNoticeError = pending, false
}

// sendPluginManage sends OpPluginManage for the current server.
func (a *App) sendPluginManage(req protocol.PluginManageRequest) bool {
	if a.activeConn == nil || a.activeConn.Connection == nil || a.currentServer == nil {
		return false
	}
	req.ServerID = a.currentServer.ID
	msg, err := protocol.NewMessage(protocol.OpPluginManage, req)
	if err != nil {
		return false
	}
	return a.activeConn.Connection.Send(msg) == nil
}

// openPluginFetchForm opens the generic plugin form set up to collect a
// release archive URL and its SHA-256 -- plus the plugin's ID for a fresh
// install. Reusing the manifest-field form keeps one editor for all of it.
func (a *App) openPluginFetchForm(mode, pluginID string) {
	field := protocol.PluginField{Key: "source", Label: "Where to get it", Type: "text", Required: true,
		Help: "A GitHub repo (owner/name) or a link to the plugin's .zip — the right download is picked and verified automatically"}
	current := map[string]string{}
	if mode == protocol.PluginActionUpdate {
		field.Required = false
		field.Help = "Leave as is to get the latest release from where this plugin came from"
		for _, p := range a.serverManagementState.PluginList {
			if p.ID == pluginID {
				current["source"] = p.SourceURL
			}
		}
	}
	fields := []protocol.PluginField{field}
	textInputs, values := buildFieldEditors(fields, current)
	if len(textInputs) > 0 {
		textInputs[0].Focus()
	}
	a.serverManagementState.PluginConfigState = &PluginConfigFormState{
		PluginID:   pluginID,
		Fields:     fields,
		TextInputs: textInputs,
		Values:     values,
		Mode:       mode,
	}
}

// applyPluginManageResult shows the outcome of a plugin action or settings
// save: on the Plugins page's notice line and the status bar, and -- for
// the open form it answers -- by closing it on success or showing what
// needs fixing.
func (a *App) applyPluginManageResult(r protocol.PluginManageResult) {
	a.statusMessage, a.statusError = r.Message, !r.OK
	s := a.serverManagementState
	if s == nil {
		return
	}
	s.PluginNotice, s.PluginNoticeError = r.Message, !r.OK

	form := s.PluginConfigState
	if form == nil || !form.Saving {
		return
	}
	answersForm := (r.Action == "configure" && form.Mode == "" && r.PluginID == form.PluginID) ||
		(r.Action == form.Mode && form.Mode != "")
	if !answersForm {
		return
	}
	form.Saving = false
	if r.OK {
		s.PluginConfigState = nil
		return
	}
	form.ErrorMsg = r.Message
	form.FieldErrors = r.FieldErrors
}
