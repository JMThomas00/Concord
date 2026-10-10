package server

import (
	"sync"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/protocol"
)

// paneViewer is one user with a plugin channel's remote pane open. enter
// holds the last Enter/Resize state (size, theme, names) so it can be
// replayed to the plugin after the plugin reconnects. conn is the
// connection that opened it: frames go only there, and only it can type.
type paneViewer struct {
	pluginID string
	enter    protocol.PluginPaneEnterPayload
	conn     *Client
}

// PaneViewers tracks who has which plugin pane open. It's the server's
// source of truth for routing: input is only relayed from, and frames only
// delivered to, connections registered here for that channel -- and since
// the owning plugin is recorded at Enter (after a DB check), the per-key
// and per-frame paths never need to touch the database.
//
// Plugins see viewers as users, so a user views at most one pane at a
// time: opening a pane from another device (another connection) moves the
// pane there, and the earlier device stops receiving frames.
type PaneViewers struct {
	mu        sync.Mutex
	byUser    map[uuid.UUID]*paneViewer
	byChannel map[uuid.UUID]map[uuid.UUID]*paneViewer
}

func NewPaneViewers() *PaneViewers {
	return &PaneViewers{
		byUser:    make(map[uuid.UUID]*paneViewer),
		byChannel: make(map[uuid.UUID]map[uuid.UUID]*paneViewer),
	}
}

// Enter registers enter.ViewerID, on connection conn, as viewing
// enter.ChannelID, owned by pluginID. If they were viewing a different
// pane, that one is returned so the caller can tell its plugin they left.
func (p *PaneViewers) Enter(pluginID string, enter protocol.PluginPaneEnterPayload, conn *Client) (previous *paneViewer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if old, ok := p.byUser[enter.ViewerID]; ok {
		p.removeLocked(enter.ViewerID, old.enter.ChannelID)
		if old.enter.ChannelID != enter.ChannelID {
			previous = old
		}
	}
	v := &paneViewer{pluginID: pluginID, enter: enter, conn: conn}
	p.byUser[enter.ViewerID] = v
	if p.byChannel[enter.ChannelID] == nil {
		p.byChannel[enter.ChannelID] = make(map[uuid.UUID]*paneViewer)
	}
	p.byChannel[enter.ChannelID][enter.ViewerID] = v
	return previous
}

// Update applies a change (a Resize) sent on conn to its viewer's state and
// returns the result; ok is false if conn isn't the one viewing channelID.
func (p *PaneViewers) Update(conn *Client, channelID uuid.UUID, apply func(*protocol.PluginPaneEnterPayload)) (state protocol.PluginPaneEnterPayload, pluginID string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[conn.UserID]
	if !ok || v.conn != conn || v.enter.ChannelID != channelID {
		return state, "", false
	}
	apply(&v.enter)
	return v.enter, v.pluginID, true
}

// Viewer returns userID's registration, owning plugin and viewing
// connection if they're viewing channelID.
func (p *PaneViewers) Viewer(userID, channelID uuid.UUID) (state protocol.PluginPaneEnterPayload, pluginID string, conn *Client, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[userID]
	if !ok || v.enter.ChannelID != channelID {
		return state, "", nil, false
	}
	return v.enter, v.pluginID, v.conn, true
}

// ViewingPlugin returns the connection on which userID has one of
// pluginID's panes open, or nil.
func (p *PaneViewers) ViewingPlugin(userID uuid.UUID, pluginID string) *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.byUser[userID]; ok && v.pluginID == pluginID {
		return v.conn
	}
	return nil
}

// Leave unregisters conn's viewer from channelID; ok reports whether it was
// registered there (and so whether the plugin needs telling). A Leave from
// a device the pane has since moved away from is ignored.
func (p *PaneViewers) Leave(conn *Client, channelID uuid.UUID) (pluginID string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[conn.UserID]
	if !ok || v.conn != conn || v.enter.ChannelID != channelID {
		return "", false
	}
	p.removeLocked(conn.UserID, channelID)
	return v.pluginID, true
}

// LeaveConn unregisters whatever pane conn had open (the connection closed)
// and returns it, if any.
func (p *PaneViewers) LeaveConn(conn *Client) *paneViewer {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[conn.UserID]
	if !ok || v.conn != conn {
		return nil
	}
	p.removeLocked(conn.UserID, v.enter.ChannelID)
	return v
}

// DropChannel forgets every viewer of a deleted channel.
func (p *PaneViewers) DropChannel(channelID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for userID := range p.byChannel[channelID] {
		delete(p.byUser, userID)
	}
	delete(p.byChannel, channelID)
}

// channelViewer is one viewer of a channel and the connection to reach.
type channelViewer struct {
	userID uuid.UUID
	conn   *Client
}

// ChannelViewers returns everyone currently viewing channelID, if it's
// owned by pluginID (otherwise none).
func (p *PaneViewers) ChannelViewers(channelID uuid.UUID, pluginID string) []channelViewer {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]channelViewer, 0, len(p.byChannel[channelID]))
	for userID, v := range p.byChannel[channelID] {
		if v.pluginID == pluginID {
			out = append(out, channelViewer{userID, v.conn})
		}
	}
	return out
}

// PluginViewers returns the Enter state of every viewer of pluginID's
// channels, for replaying to the plugin when it (re)connects.
func (p *PaneViewers) PluginViewers(pluginID string) []protocol.PluginPaneEnterPayload {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []protocol.PluginPaneEnterPayload
	for _, v := range p.byUser {
		if v.pluginID == pluginID {
			out = append(out, v.enter)
		}
	}
	return out
}

func (p *PaneViewers) removeLocked(userID, channelID uuid.UUID) {
	delete(p.byUser, userID)
	if viewers := p.byChannel[channelID]; viewers != nil {
		delete(viewers, userID)
		if len(viewers) == 0 {
			delete(p.byChannel, channelID)
		}
	}
}
