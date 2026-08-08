package client

import (
	"sort"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// pluginKindOptions returns every plugin-provided channel kind advertised at
// READY, in a stable order — used by the channel-creation type selector so
// the same list appears in the same order across renders.
func (a *App) pluginKindOptions() []protocol.PluginChannelKindInfo {
	if len(a.pluginChannelKinds) == 0 {
		return nil
	}
	keys := make([]string, 0, len(a.pluginChannelKinds))
	for k := range a.pluginChannelKinds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]protocol.PluginChannelKindInfo, len(keys))
	for i, k := range keys {
		out[i] = a.pluginChannelKinds[k]
	}
	return out
}

// pluginChannelKind looks up the READY-advertised metadata for a
// plugin-provided channel, if any. The client never has plugin-specific code
// compiled in — everything it knows about a plugin channel's display comes
// from this cache, populated once at connect time (see app.go's OpReady handling).
func (a *App) pluginChannelKind(ch *models.Channel) (protocol.PluginChannelKindInfo, bool) {
	if ch == nil || ch.Type != models.ChannelTypePlugin || a.pluginChannelKinds == nil {
		return protocol.PluginChannelKindInfo{}, false
	}
	info, ok := a.pluginChannelKinds[ch.PluginID+":"+ch.PluginChannelKind]
	return info, ok
}

// channelIcon returns the prefix icon+space for a channel row, e.g. "# ",
// "♪ ", or a plugin's declared icon — falling back to a generic marker for a
// plugin channel whose kind metadata hasn't arrived yet (e.g. the owning
// plugin is disabled).
func (a *App) channelIcon(ch *models.Channel) string {
	switch ch.Type {
	case models.ChannelTypeVoice:
		return "♪ "
	case models.ChannelTypeCategory:
		return "▼ "
	case models.ChannelTypePlugin:
		if info, ok := a.pluginChannelKind(ch); ok && info.Icon != "" {
			return info.Icon + " "
		}
		return "▤ "
	default:
		return "# "
	}
}

// channelTypeLabel returns a human-readable type name for a channel, used in
// Server Settings' Channels list and the channel-creation type selector.
func (a *App) channelTypeLabel(ch *models.Channel) string {
	switch ch.Type {
	case models.ChannelTypeVoice:
		return "Voice"
	case models.ChannelTypeCategory:
		return "Category"
	case models.ChannelTypePlugin:
		if info, ok := a.pluginChannelKind(ch); ok && info.DisplayName != "" {
			return info.DisplayName
		}
		return "Plugin"
	default:
		return "Text"
	}
}

// isRemotePaneChannel reports whether a channel should render via the
// generic remote pane instead of the normal chat viewport+input.
func (a *App) isRemotePaneChannel(ch *models.Channel) bool {
	info, ok := a.pluginChannelKind(ch)
	return ok && info.RemotePane
}

// textChannelNames returns the current server's text channel names, used to
// populate "channel_select" plugin config fields (e.g. an activity
// notification channel picker) without hardcoding any plugin's field semantics.
func (a *App) textChannelNames() []string {
	var names []string
	for _, ch := range a.getCurrentChannels() {
		if ch.Type == models.ChannelTypeText {
			names = append(names, ch.Name)
		}
	}
	return names
}
