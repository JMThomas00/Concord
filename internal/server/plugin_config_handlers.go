package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/plugins"
	"github.com/concord-chat/concord/internal/protocol"
)

// SetPluginSecrets gives the handlers the key used to seal "secret"
// plugin config values at rest. Without one (some tests), secrets are
// stored as given.
func (h *Handlers) SetPluginSecrets(box *plugins.SecretBox) {
	h.secrets = box
}

// buildPluginInfo assembles one plugin's Settings > Plugins row: its
// manifest-declared server_config_field definitions plus its currently
// stored values. For an admin's client, secret values are left out (only
// SecretsSet says which have one); forPlugin gives the plugin itself every
// value in plaintext.
func (h *Handlers) buildPluginInfo(installed *models.InstalledPlugin, forPlugin bool) protocol.PluginInfo {
	manifest, _ := h.plugins.Registry().Manifest(installed.ID)
	info := protocol.PluginInfo{
		ID:        installed.ID,
		Name:      installed.Name,
		Version:   installed.Version,
		Enabled:   installed.Enabled,
		Status:    installed.Status,
		LastError: installed.LastError,
	}
	secret := map[string]bool{}
	if manifest != nil {
		info.Product = manifest.Plugin.Product
		info.SourceURL = manifest.Plugin.SourceURL
		for _, f := range manifest.ServerConfigFields {
			info.ConfigFields = append(info.ConfigFields, protocol.PluginField{
				Key: f.Key, Label: f.Label, Type: f.Type, Options: f.Options,
				Default: f.Default, Required: f.Required, Help: f.Help,
			})
			secret[f.Key] = f.Type == "secret"
		}
	}
	values, err := h.db.GetPluginServerConfig(installed.ID)
	if err != nil {
		return info
	}
	info.ConfigValues = make(map[string]string, len(values))
	for key, v := range values {
		if !secret[key] {
			info.ConfigValues[key] = v
			continue
		}
		if !forPlugin {
			if v != "" {
				info.SecretsSet = append(info.SecretsSet, key)
			}
			continue
		}
		if h.secrets != nil {
			plain, err := h.secrets.Open(v)
			if err != nil {
				PluginLog.Warn("Couldn't decrypt a plugin secret; re-enter it in Settings > Plugins", "plugin_id", installed.ID, "field", key, "error", err)
				continue
			}
			v = plain
		}
		info.ConfigValues[key] = v
	}
	return info
}

// HandleSetPluginConfig toggles a plugin's enabled flag and/or updates its
// server-wide config values from Settings > Plugins. Values are validated
// against the manifest before anything is saved; problems come back per
// field in an EventPluginManageResult (Action "configure"). Enabling or
// disabling waits for the process to stop, so the work runs off the
// admin's read loop.
func (h *Handlers) HandleSetPluginConfig(c *Client, msg *protocol.Message) {
	var req protocol.PluginConfigSetRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManagePlugins); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	reply := func(err error, fieldErrors map[string]string) {
		result := protocol.PluginManageResult{Action: "configure", PluginID: req.PluginID, OK: err == nil && len(fieldErrors) == 0, FieldErrors: fieldErrors}
		switch {
		case err != nil:
			result.Message = err.Error()
		case len(fieldErrors) > 0:
			result.Message = "Some settings need fixing."
		default:
			result.Message = "Saved."
		}
		_ = c.SendDispatch(protocol.EventPluginManageResult, result)
	}

	manifest, ok := h.plugins.Registry().Manifest(req.PluginID)
	if !ok {
		reply(fmt.Errorf("plugin %q isn't installed", req.PluginID), nil)
		return
	}
	if len(req.Config) > 0 {
		stored, _ := h.db.GetPluginServerConfig(req.PluginID)
		isTextChannel := func(id uuid.UUID) bool {
			ch, err := h.db.GetChannelByID(id)
			return err == nil && ch.Type == models.ChannelTypeText && ch.ServerID == req.ServerID
		}
		if problems := plugins.ValidateValues(manifest.ServerConfigFields, req.Config, stored, isTextChannel); len(problems) > 0 {
			reply(nil, problems)
			return
		}
	}

	go func() {
		if req.Enabled != nil {
			if err := h.plugins.SetEnabled(req.PluginID, *req.Enabled); err != nil {
				reply(fmt.Errorf("failed to update plugin: %w", err), nil)
				h.sendPluginList(c)
				return
			}
		}
		secret := map[string]bool{}
		for _, f := range manifest.ServerConfigFields {
			secret[f.Key] = f.Type == "secret"
		}
		for key, value := range req.Config {
			if secret[key] && h.secrets != nil {
				sealed, err := h.secrets.Seal(value)
				if err != nil {
					reply(fmt.Errorf("failed to encrypt %s: %w", key, err), nil)
					return
				}
				value = sealed
			}
			if err := h.db.SetPluginServerConfig(req.PluginID, key, value); err != nil {
				MsgLog.Error("Failed to persist plugin server config", "plugin_id", req.PluginID, "key", key, "error", err)
				reply(fmt.Errorf("failed to save %s", key), nil)
				return
			}
		}
		reply(nil, nil)
		h.sendPluginList(c)

		// A plugin has no other way to learn its own server_config_field
		// values changed, so push them (plaintext, secrets included) the
		// same targeted way HandleCreateChannel notifies a plugin about its
		// new channel -- e.g. so an AI plugin reacts live when an admin
		// flips mention_enabled, rather than on its next reconnect.
		if installed, err := h.db.GetInstalledPlugin(req.PluginID); err == nil && installed != nil {
			info := h.buildPluginInfo(installed, true)
			h.sendToPlugin(req.PluginID, protocol.EventPluginConfigUpdate, protocol.PluginConfigListPayload{Plugins: []protocol.PluginInfo{info}})
		}
	}()
}

// validatePluginChannelConfig checks a plugin channel's create_field values
// (on create, or on edit against what's stored) and describes any problems
// in one message, e.g. `Seating: must be one of: seats, challenge, private`.
func (h *Handlers) validatePluginChannelConfig(serverID uuid.UUID, fields []plugins.ConfigField, submitted, stored map[string]string) error {
	isTextChannel := func(id uuid.UUID) bool {
		ch, err := h.db.GetChannelByID(id)
		return err == nil && ch.Type == models.ChannelTypeText && ch.ServerID == serverID
	}
	problems := plugins.ValidateValues(fields, submitted, stored, isTextChannel)
	if len(problems) == 0 {
		return nil
	}
	var parts []string
	for _, f := range fields {
		if p, ok := problems[f.Key]; ok {
			parts = append(parts, f.Label+": "+p)
			delete(problems, f.Key)
		}
	}
	for key, p := range problems {
		parts = append(parts, key+": "+p)
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}
