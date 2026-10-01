package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/plugins"
	"github.com/concord-chat/concord/internal/protocol"
)

// HandlePluginManage runs one live plugin lifecycle action (install, update,
// uninstall, restart, rescan) for a server admin. The work -- downloads,
// stopping processes, waiting for an updated plugin to come up -- runs off
// the admin's read loop, so their client stays responsive; the outcome
// arrives as EventPluginManageResult followed by the refreshed plugin list.
func (h *Handlers) HandlePluginManage(c *Client, msg *protocol.Message) {
	var req protocol.PluginManageRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManagePlugins); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	adminID := c.UserID
	go func() {
		msg, err := h.runPluginAction(req)
		result := protocol.PluginManageResult{Action: req.Action, PluginID: req.PluginID, OK: err == nil, Message: msg}
		if err != nil {
			result.Message = err.Error()
			MsgLog.Warn("Plugin action failed", "action", req.Action, "plugin_id", req.PluginID, "admin", adminID, "error", err)
		} else {
			MsgLog.Info("Plugin action done", "action", req.Action, "plugin_id", req.PluginID, "admin", adminID)
		}
		_ = h.dispatchTo(c, protocol.EventPluginManageResult, result)
		h.sendPluginList(c)
	}()
}

// HandlePluginInstall is the older install-only opcode; it's OpPluginManage
// with Action "install".
func (h *Handlers) HandlePluginInstall(c *Client, msg *protocol.Message) {
	var req protocol.PluginInstallRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	manage, _ := protocol.NewMessage(protocol.OpPluginManage, protocol.PluginManageRequest{
		ServerID: req.ServerID, Action: protocol.PluginActionInstall,
		PluginID: req.PluginID, SourceURL: req.SourceURL, SHA256: req.SHA256,
	})
	h.HandlePluginManage(c, manage)
}

// runPluginAction performs one action and returns a short success message.
func (h *Handlers) runPluginAction(req protocol.PluginManageRequest) (string, error) {
	// An instance runs its base plugin's files: updating "it" updates the
	// base (and with it every instance).
	if req.Action == protocol.PluginActionUpdate {
		if m, ok := h.plugins.Registry().Manifest(req.PluginID); ok && m.IsInstance() {
			req.PluginID = m.BaseID
		}
	}
	fetch := plugins.InstallRequest{PluginID: req.PluginID, SourceURL: req.SourceURL, SHA256: req.SHA256}
	switch req.Action {
	case protocol.PluginActionAddInstance:
		id, err := h.plugins.AddInstance(req.PluginID, req.Name)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Added %s (%s). Configure it under its name in this list.", strings.TrimSpace(req.Name), id), nil

	case protocol.PluginActionRenameInstance:
		if err := h.plugins.RenameInstance(req.PluginID, req.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("Renamed to %s.", strings.TrimSpace(req.Name)), nil

	case protocol.PluginActionRemoveInstance:
		name := req.PluginID
		if m, ok := h.plugins.Registry().Manifest(req.PluginID); ok {
			name = m.Plugin.Name
		}
		if err := h.plugins.RemoveInstance(req.PluginID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed %s. Its channels and settings are kept, in case you add it back.", name), nil

	case protocol.PluginActionAdoptInstance:
		if err := h.plugins.AdoptInstance(req.PluginID, req.TargetID); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s is now an instance of %s, with its channels and settings kept. Re-enter any API key it used from its old plugin.toml.", req.PluginID, req.TargetID), nil

	case protocol.PluginActionInstall:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		f, err := plugins.InstallFromURL(ctx, h.plugins.PluginsDir(), fetch)
		if err != nil {
			return "", err
		}
		if err := h.plugins.Load(f.ID); err != nil {
			return "", fmt.Errorf("installed %s, but it failed to start: %w", f.ID, err)
		}
		return fmt.Sprintf("Installed %s %s%s.", f.ID, f.Version, verifiedNote(f.Verified)), nil

	case protocol.PluginActionUpdate:
		if fetch.SourceURL == "" {
			// "Update" with nothing typed: check the plugin's own declared source.
			if m, ok := h.plugins.Registry().Manifest(req.PluginID); ok {
				fetch.SourceURL = m.Plugin.SourceURL
			}
			if fetch.SourceURL == "" {
				return "", fmt.Errorf("%s doesn't say where its updates come from; enter its GitHub repo or a link", req.PluginID)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		f, err := plugins.FetchToStaging(ctx, h.plugins.PluginsDir(), fetch)
		if err != nil {
			return "", err
		}
		defer plugins.DiscardStaged(f)
		if err := h.plugins.Update(req.PluginID, f.Dir); err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated %s to %s%s.", req.PluginID, f.Version, verifiedNote(f.Verified)), nil

	case protocol.PluginActionUninstall:
		if err := h.plugins.Uninstall(req.PluginID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Uninstalled %s. Its channels and saved data are kept for a reinstall.", req.PluginID), nil

	case protocol.PluginActionRestart:
		if err := h.plugins.Restart(req.PluginID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Restarted %s.", req.PluginID), nil

	case protocol.PluginActionRescan:
		res := h.plugins.Rescan()
		parts := []string{}
		if len(res.Added) > 0 {
			parts = append(parts, "added "+strings.Join(res.Added, ", "))
		}
		if len(res.Removed) > 0 {
			parts = append(parts, "removed "+strings.Join(res.Removed, ", "))
		}
		if len(res.Reloaded) > 0 {
			parts = append(parts, "reloaded "+strings.Join(res.Reloaded, ", "))
		}
		for folder, err := range res.Invalid {
			parts = append(parts, fmt.Sprintf("skipped %s (%v)", folder, err))
		}
		if len(parts) == 0 {
			return "Rescanned: no changes.", nil
		}
		return "Rescanned: " + strings.Join(parts, "; ") + ".", nil
	}
	return "", fmt.Errorf("unknown plugin action %q", req.Action)
}

// sendPluginList pushes the current Settings > Plugins list to the admin
// connection that asked (not every connection of that account).
func (h *Handlers) sendPluginList(c *Client) {
	installedList, err := h.db.ListInstalledPlugins()
	if err != nil {
		return
	}
	infos := make([]protocol.PluginInfo, 0, len(installedList))
	for _, installed := range installedList {
		infos = append(infos, h.buildPluginInfo(installed, false))
	}
	_ = h.dispatchTo(c, protocol.EventPluginConfigUpdate, protocol.PluginConfigListPayload{Plugins: infos})
}

// onPluginStopped drops a stopped plugin's connection, so nothing that
// authenticated with its old token keeps acting.
func (h *Handlers) onPluginStopped(pluginID string) {
	if serviceUserID, err := h.plugins.ServiceUserIDFor(pluginID); err == nil {
		h.hub.DisconnectUser(serviceUserID)
	}
}

// onPluginRegistryChanged tells every connected client the current set of
// plugin channel kinds, so an installed plugin's channel types can be
// created (and its channels render as panes) without reconnecting.
func (h *Handlers) onPluginRegistryChanged() {
	payload := protocol.PluginRegistryPayload{PluginChannelKinds: h.PluginChannelKindInfos(), PluginClients: h.PluginClientInfos()}
	servers, err := h.db.GetAllServers()
	if err != nil {
		return
	}
	for _, s := range servers {
		_ = h.hub.BroadcastToServer(s.ID, protocol.EventPluginRegistryUpdate, payload, nil)
	}
}

// verifiedNote says, for the admin's result message, what a download was
// checked against.
func verifiedNote(from string) string {
	switch from {
	case "":
		return " (no checksum published; downloaded over https)"
	case "you":
		return " (checksum matched)"
	default:
		return " (checksum verified via " + from + ")"
	}
}
