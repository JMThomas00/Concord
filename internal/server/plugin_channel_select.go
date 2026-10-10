package server

import (
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/plugins"
)

// migrateChannelSelectNames rewrites plugin "channel_select" values saved
// as channel names into channel IDs.
//
// The client used to store the picked channel's name, while the server
// (e.g. HandlePluginEvent's activity_notify_channel) and plugins resolve the
// value as a UUID -- so an activity channel picked in Settings > Plugins
// silently never received anything. The client now stores IDs; this fixes
// up values saved before that. A name matching no text channel, or more
// than one, is left alone and logged for the admin to re-pick.
func migrateChannelSelectNames(db *database.DB, reg *plugins.Registry) {
	servers, err := db.GetAllServers()
	if err != nil {
		DBLog.Warn("channel_select migration: could not list servers", "error", err)
		return
	}
	textChannels := func(serverID uuid.UUID) []*models.Channel {
		chs, err := db.GetServerChannels(serverID)
		if err != nil {
			return nil
		}
		var out []*models.Channel
		for _, ch := range chs {
			if ch.Type == models.ChannelTypeText {
				out = append(out, ch)
			}
		}
		return out
	}
	resolve := func(name string, candidates []*models.Channel) (uuid.UUID, bool) {
		var found []uuid.UUID
		for _, ch := range candidates {
			if ch.Name == name {
				found = append(found, ch.ID)
			}
		}
		if len(found) != 1 {
			return uuid.Nil, false
		}
		return found[0], true
	}
	isLegacy := func(v string) bool {
		if v == "" {
			return false
		}
		_, err := uuid.Parse(v)
		return err != nil
	}

	var allText []*models.Channel
	for _, srv := range servers {
		allText = append(allText, textChannels(srv.ID)...)
	}

	for _, m := range reg.All() {
		pluginID := m.Plugin.ID

		// Server-wide config isn't scoped to one Concord server, so a name
		// is matched across all of them and must be unique.
		values, err := db.GetPluginServerConfig(pluginID)
		if err == nil {
			for _, f := range m.ServerConfigFields {
				if f.Type != "channel_select" || !isLegacy(values[f.Key]) {
					continue
				}
				if id, ok := resolve(values[f.Key], allText); ok {
					if err := db.SetPluginServerConfig(pluginID, f.Key, id.String()); err == nil {
						DBLog.Info("Converted plugin channel setting from name to ID", "plugin_id", pluginID, "field", f.Key, "channel", values[f.Key])
					}
				} else {
					DBLog.Warn("Plugin channel setting names no single channel; re-pick it in Settings > Plugins", "plugin_id", pluginID, "field", f.Key, "value", values[f.Key])
				}
			}
		}

		// Per-channel create_field values resolve within that channel's server.
		channels, err := db.GetChannelsByPlugin(pluginID)
		if err != nil {
			continue
		}
		for _, ch := range channels {
			kind, ok := m.ChannelKind(ch.PluginChannelKind)
			if !ok {
				continue
			}
			fixed := map[string]string{}
			for _, f := range kind.CreateFields {
				v := ch.PluginConfig[f.Key]
				if f.Type != "channel_select" || !isLegacy(v) {
					continue
				}
				if id, ok := resolve(v, textChannels(ch.ServerID)); ok {
					fixed[f.Key] = id.String()
				} else {
					DBLog.Warn("Plugin channel setting names no single channel; re-pick it in the channel's settings", "channel_id", ch.ID, "field", f.Key, "value", v)
				}
			}
			if len(fixed) > 0 {
				if err := db.SetPluginChannelConfig(ch.ID, fixed); err == nil {
					DBLog.Info("Converted plugin channel settings from names to IDs", "channel_id", ch.ID, "fields", len(fixed))
				}
			}
		}
	}
}
