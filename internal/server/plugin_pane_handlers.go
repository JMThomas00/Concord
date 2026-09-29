package server

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// ── Plugin Platform Handlers ────────────────────────────────────────────────
//
// The server relays plugin pane/event messages without parsing their
// contents: a human client's Enter/Input/Resize/Leave goes to the plugin
// that owns the channel, and a plugin's frames go to the viewers of its
// channels. Concord never understands what's inside a frame.
//
// PaneViewers (pane_viewers.go) is the routing table. Enter is the only
// step that touches the database (channel ownership + View Channels);
// after that, input is only relayed from registered viewers and frames are
// only delivered to them, so neither per-keystroke nor per-frame traffic
// costs a query, and a plugin can't reach anyone who isn't looking at one
// of its own channels.

// pluginConn returns pluginID's live service-account connection, or nil.
func (h *Handlers) pluginConn(pluginID string) *Client {
	serviceUserID, err := h.plugins.ServiceUserIDFor(pluginID)
	if err != nil {
		return nil
	}
	return h.hub.GetClient(serviceUserID)
}

// sendToPlugin delivers one event to pluginID's connection, if it's up.
// Returns false when the plugin isn't connected.
func (h *Handlers) sendToPlugin(pluginID string, event protocol.EventType, payload interface{}) bool {
	target := h.pluginConn(pluginID)
	if target == nil {
		return false
	}
	if err := h.dispatchTo(target, event, payload); err != nil {
		MsgLog.Error("Failed to relay to plugin", "plugin_id", pluginID, "event", event, "error", err)
	}
	return true
}

// sendPaneLeaveToPlugin tells a plugin a viewer is gone from one of its panes.
func (h *Handlers) sendPaneLeaveToPlugin(pluginID string, channelID, viewerID uuid.UUID) {
	h.sendToPlugin(pluginID, protocol.EventPluginPaneLeave, protocol.PluginPaneLeavePayload{
		ChannelID: channelID,
		ViewerID:  viewerID,
	})
}

// dispatchTo sends one event to one connection (not every connection of
// its user, as hub.SendToUser does).
func (h *Handlers) dispatchTo(conn *Client, event protocol.EventType, data interface{}) error {
	msg, err := protocol.NewDispatch(event, h.hub.NextSequence(), data)
	if err != nil {
		return err
	}
	conn.Send(msg)
	return nil
}

// viewersFor resolves who a plugin's frame or event is for: one viewer
// (viewerID set), or every viewer of the channel. Either way only viewers
// of pluginID's own panes, each with the connection that has it open.
func (h *Handlers) viewersFor(pluginID string, channelID, viewerID uuid.UUID) []channelViewer {
	if viewerID == uuid.Nil {
		return h.paneViewers.ChannelViewers(channelID, pluginID)
	}
	if _, owner, conn, ok := h.paneViewers.Viewer(viewerID, channelID); ok && owner == pluginID {
		return []channelViewer{{viewerID, conn}}
	}
	return nil
}

// sendLeavePane hands conn's keyboard back from channelID's pane (the
// same signal a plugin's leave_pane sends): used when the viewer opened
// the pane on another device and this one stops receiving it.
func (h *Handlers) sendLeavePane(conn *Client, channelID uuid.UUID) {
	payload, _ := json.Marshal(protocol.PluginPaneClosePayload{ChannelID: channelID})
	_ = h.dispatchTo(conn, protocol.EventPluginEvent, protocol.PluginEventPayload{
		Kind: protocol.PluginEventLeavePane, Payload: payload, ViewerID: conn.UserID,
	})
}

// memberDisplayName is how userID appears in serverID: their nickname
// there, else their display name, else their username.
func (h *Handlers) memberDisplayName(serverID uuid.UUID, user *models.User) string {
	if m, err := h.db.GetServerMember(serverID, user.ID); err == nil && m.Nickname != "" {
		return m.Nickname
	}
	return user.GetDisplayName()
}

// replayPaneEnters re-sends Enter for every current viewer of pluginID's
// channels to its (new) connection c, so a plugin that crashed and
// restarted, or connected after its viewers did, paints them without them
// having to do anything.
func (h *Handlers) replayPaneEnters(c *Client, pluginID string) {
	for _, enter := range h.paneViewers.PluginViewers(pluginID) {
		msg, err := protocol.NewMessage(protocol.OpDispatch, enter)
		if err != nil {
			continue
		}
		msg.Type = protocol.EventPluginPaneEnter
		c.send <- msg
	}
}

// HandlePluginPaneEnter registers the sender as a viewer of a plugin
// channel and relays the Enter (with server-stamped identity) to the plugin.
func (h *Handlers) HandlePluginPaneEnter(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneEnterPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	channel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil || channel.Type != models.ChannelTypePlugin {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Not a plugin channel")
		return
	}
	// A member the channel is hidden from can't open, watch or drive its
	// plugin just by knowing the channel ID.
	if err := h.hasChannelPermission(c.UserID, channel, models.PermissionViewChannels); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "You don't have access to this channel")
		return
	}

	req.ViewerID = c.UserID
	if c.User != nil {
		req.ViewerName = c.User.Username
		req.ViewerDisplayName = h.memberDisplayName(channel.ServerID, c.User)
	}
	_, _, oldConn, _ := h.paneViewers.Viewer(c.UserID, req.ChannelID)
	if prev := h.paneViewers.Enter(channel.PluginID, req, c); prev != nil {
		h.sendPaneLeaveToPlugin(prev.pluginID, prev.enter.ChannelID, c.UserID)
		oldConn = prev.conn
	}
	// Opened on another device: the earlier one stops getting frames, so
	// hand its keyboard back rather than leave it typing into a dead pane.
	if oldConn != nil && oldConn != c {
		h.sendLeavePane(oldConn, req.ChannelID)
	}
	// If the plugin is down the viewer stays registered, and it's replayed
	// to the plugin when it connects (see replayPaneEnters) -- the client
	// just shows "Waiting for plugin…" until then.
	if !h.sendToPlugin(channel.PluginID, protocol.EventPluginPaneEnter, req) {
		MsgLog.Debug("Plugin not connected; viewer registered for replay", "plugin_id", channel.PluginID, "channel_id", channel.ID)
	}
}

// HandlePluginPaneResize relays a viewport size (or theme) change from a
// registered viewer.
func (h *Handlers) HandlePluginPaneResize(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneResizePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	state, pluginID, ok := h.paneViewers.Update(c, req.ChannelID, func(e *protocol.PluginPaneEnterPayload) {
		e.Width, e.Height = req.Width, req.Height
		if req.Theme != nil {
			e.Theme = req.Theme
		}
	})
	if !ok {
		return // not viewing that pane (e.g. a resize racing a channel switch)
	}
	h.sendToPlugin(pluginID, protocol.EventPluginPaneResize, protocol.PluginPaneResizePayload{
		ChannelID:         state.ChannelID,
		ViewerID:          c.UserID,
		Width:             state.Width,
		Height:            state.Height,
		ViewerName:        state.ViewerName,
		ViewerDisplayName: state.ViewerDisplayName,
		Theme:             state.Theme,
	})
}

// HandlePluginPaneInput relays one keypress from a registered viewer.
func (h *Handlers) HandlePluginPaneInput(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneInputPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	state, pluginID, viewing, ok := h.paneViewers.Viewer(c.UserID, req.ChannelID)
	if !ok || viewing != c {
		return // keys only count from someone who Entered this pane
	}
	req.ViewerID = c.UserID
	req.ViewerName = state.ViewerName
	req.ViewerDisplayName = state.ViewerDisplayName
	if !h.sendToPlugin(pluginID, protocol.EventPluginPaneInput, req) {
		c.sendError(protocol.ErrorCodeServerError, "Plugin is not currently running")
	}
}

// HandlePluginPaneLeave unregisters a viewer and tells the plugin.
func (h *Handlers) HandlePluginPaneLeave(c *Client, msg *protocol.Message) {
	var req protocol.PluginPaneLeavePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if pluginID, ok := h.paneViewers.Leave(c, req.ChannelID); ok {
		h.sendPaneLeaveToPlugin(pluginID, req.ChannelID, c.UserID)
	}
}

// HandlePluginPaneFrame delivers a plugin's rendered frame to the viewer it
// names, or to every viewer of the channel when ViewerID is empty. Frames
// for anyone not currently viewing one of this plugin's channels are
// dropped: that's normal right after a viewer leaves, so it isn't an error.
func (h *Handlers) HandlePluginPaneFrame(c *Client, msg *protocol.Message) {
	if !c.IsPlugin {
		c.sendError(protocol.ErrorCodeForbidden, "Only plugin connections may push frames")
		return
	}
	var req protocol.PluginPaneFramePayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	// Stamped per plugin connection: a restarted plugin starts its Seq
	// counter over, and the client uses a changed Epoch to accept that new
	// stream instead of dropping every frame until Seq catches back up.
	req.Epoch = c.connEpoch

	viewers := h.viewersFor(c.PluginID, req.ChannelID, req.ViewerID)
	if len(viewers) == 0 && req.ViewerID != uuid.Nil {
		MsgLog.Debug("Dropped plugin frame for a non-viewer", "plugin_id", c.PluginID, "channel_id", req.ChannelID, "viewer_id", req.ViewerID)
		return
	}
	for _, v := range viewers {
		out := req
		out.ViewerID = v.userID
		if err := h.dispatchTo(v.conn, protocol.EventPluginPaneFrame, out); err != nil {
			MsgLog.Error("Failed to relay plugin pane frame", "channel_id", req.ChannelID, "viewer_id", v.userID, "error", err)
		}
	}
}

// HandlePluginEvent handles the generic {plugin_id, kind, payload} envelope
// from a plugin. The kinds Concord understands are listed with
// protocol.PluginEventNotify; any other kind sent with ViewerID set is
// relayed unchanged to that viewer's client -- as long as they're viewing
// one of this plugin's panes -- so plugins can add viewer-directed signals
// without a protocol change.
func (h *Handlers) HandlePluginEvent(c *Client, msg *protocol.Message) {
	if !c.IsPlugin {
		c.sendError(protocol.ErrorCodeForbidden, "Only plugin connections may send plugin events")
		return
	}
	var req protocol.PluginEventPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	req.PluginID = c.PluginID // never trust a client-claimed plugin id

	switch req.Kind {
	case protocol.PluginEventNotify:
		h.handlePluginNotify(c, req)
	case protocol.PluginEventNotifyUser:
		h.handlePluginNotifyUser(c, req)
	case protocol.PluginEventMembers:
		h.handlePluginMembers(c, req)
	case protocol.PluginEventPaneTitle:
		h.handlePluginPaneTitle(c, req)
	default:
		if req.ViewerID == uuid.Nil {
			MsgLog.Warn("Unhandled plugin event kind", "plugin_id", req.PluginID, "kind", req.Kind)
			return
		}
		viewing := h.paneViewers.ViewingPlugin(req.ViewerID, c.PluginID)
		if viewing == nil {
			MsgLog.Debug("Dropped plugin event for a non-viewer", "plugin_id", c.PluginID, "viewer_id", req.ViewerID, "kind", req.Kind)
			return
		}
		if err := h.dispatchTo(viewing, protocol.EventPluginEvent, req); err != nil {
			MsgLog.Error("Failed to relay plugin event to viewer", "plugin_id", req.PluginID, "viewer_id", req.ViewerID, "kind", req.Kind, "error", err)
		}
	}
}

// ownedPluginChannel loads channelID and returns it if it belongs to the
// plugin connection c.
func (h *Handlers) ownedPluginChannel(c *Client, channelID uuid.UUID) *models.Channel {
	channel, err := h.db.GetChannelByID(channelID)
	if err != nil || channel.Type != models.ChannelTypePlugin || channel.PluginID != c.PluginID {
		c.sendError(protocol.ErrorCodeForbidden, "That isn't one of your plugin's channels")
		return nil
	}
	return channel
}

// handlePluginNotify posts a system message into the plugin's
// admin-configured activity channel, reusing sendSystemMessage -- the same
// function moderation actions use.
func (h *Handlers) handlePluginNotify(c *Client, req protocol.PluginEventPayload) {
	var notify protocol.PluginNotifyEventPayload
	if err := json.Unmarshal(req.Payload, &notify); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid notify payload")
		return
	}
	config, err := h.db.GetPluginServerConfig(req.PluginID)
	if err != nil {
		MsgLog.Error("Failed to load plugin server config", "plugin_id", req.PluginID, "error", err)
		return
	}
	channelID, err := uuid.Parse(config["activity_notify_channel"])
	if err != nil {
		return // Admin hasn't configured a notification channel — silently drop
	}
	h.sendSystemMessage(channelID, notify.Content)
}

// handlePluginNotifyUser delivers a targeted toast to one member who can see
// one of the plugin's channels.
func (h *Handlers) handlePluginNotifyUser(c *Client, req protocol.PluginEventPayload) {
	var p protocol.PluginNotifyUserPayload
	if err := json.Unmarshal(req.Payload, &p); err != nil || p.UserID == uuid.Nil || p.Content == "" {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid notify_user payload")
		return
	}
	channel := h.ownedPluginChannel(c, p.ChannelID)
	if channel == nil {
		return
	}
	if err := h.hasChannelPermission(p.UserID, channel, models.PermissionViewChannels); err != nil {
		MsgLog.Debug("Dropped notify_user for a member who can't see the channel", "plugin_id", c.PluginID, "user_id", p.UserID)
		return
	}
	if len(p.Content) > 300 {
		p.Content = p.Content[:300]
	}
	payload, _ := json.Marshal(p)
	out := protocol.PluginEventPayload{PluginID: c.PluginID, Kind: protocol.PluginEventNotifyUser, Payload: payload, ViewerID: p.UserID}
	if err := h.hub.SendToUser(p.UserID, protocol.EventPluginEvent, out); err != nil {
		MsgLog.Error("Failed to deliver notify_user", "plugin_id", c.PluginID, "user_id", p.UserID, "error", err)
	}
}

// handlePluginMembers answers with the (non-banned, human) members who can
// see one of the plugin's channels, and whether each is online and has the
// pane open.
func (h *Handlers) handlePluginMembers(c *Client, req protocol.PluginEventPayload) {
	var p protocol.PluginMembersRequest
	if err := json.Unmarshal(req.Payload, &p); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid members payload")
		return
	}
	channel := h.ownedPluginChannel(c, p.ChannelID)
	if channel == nil {
		return
	}
	members, err := h.db.GetServerMembers(channel.ServerID)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to load members")
		return
	}
	viewing := map[uuid.UUID]bool{}
	for _, v := range h.paneViewers.ChannelViewers(channel.ID, c.PluginID) {
		viewing[v.userID] = true
	}
	resp := protocol.PluginMembersResponse{ChannelID: channel.ID, RequestID: p.RequestID, Members: []protocol.PluginMember{}}
	for _, m := range members {
		if m.IsBanned {
			continue
		}
		user, err := h.db.GetUserByID(m.UserID)
		if err != nil || user.IsServiceAccount {
			continue
		}
		if h.hasChannelPermission(user.ID, channel, models.PermissionViewChannels) != nil {
			continue
		}
		display := m.Nickname
		if display == "" {
			display = user.GetDisplayName()
		}
		resp.Members = append(resp.Members, protocol.PluginMember{
			UserID:      user.ID,
			Username:    user.Username,
			DisplayName: display,
			Online:      h.hub.IsUserOnline(user.ID),
			Viewing:     viewing[user.ID],
		})
	}
	payload, _ := json.Marshal(resp)
	out := protocol.PluginEventPayload{PluginID: c.PluginID, Kind: protocol.PluginEventMembers, Payload: payload}
	if err := h.hub.SendToUser(c.UserID, protocol.EventPluginEvent, out); err != nil {
		MsgLog.Error("Failed to reply to members request", "plugin_id", c.PluginID, "error", err)
	}
}

// handlePluginPaneTitle relays a pane title to one viewer (ViewerID set) or
// every viewer of the channel.
func (h *Handlers) handlePluginPaneTitle(c *Client, req protocol.PluginEventPayload) {
	var p protocol.PluginPaneTitlePayload
	if err := json.Unmarshal(req.Payload, &p); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid pane_title payload")
		return
	}
	for _, v := range h.viewersFor(c.PluginID, p.ChannelID, req.ViewerID) {
		out := req
		out.ViewerID = v.userID
		if err := h.dispatchTo(v.conn, protocol.EventPluginEvent, out); err != nil {
			MsgLog.Error("Failed to relay pane title", "plugin_id", c.PluginID, "viewer_id", v.userID, "error", err)
		}
	}
}
