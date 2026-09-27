package server

import (
	"sync"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/protocol"
)

// paneViewer is one user with a plugin channel's remote pane open. enter
// holds the last Enter/Resize state (size, theme, names) so it can be
// replayed to the plugin after the plugin reconnects.
type paneViewer struct {
	pluginID string
	enter    protocol.PluginPaneEnterPayload
}

// PaneViewers tracks who has which plugin pane open. A user views at most
// one pane at a time (one connection per user). It's the server's source of
// truth for routing: input is only relayed from, and frames only delivered
// to, users registered here for that channel -- and since the owning plugin
// is recorded at Enter (after a DB check), the per-key and per-frame paths
// never need to touch the database.
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

// Enter registers enter.ViewerID as viewing enter.ChannelID, owned by
// pluginID. If they were viewing a different pane, that one is returned so
// the caller can tell its plugin they left.
func (p *PaneViewers) Enter(pluginID string, enter protocol.PluginPaneEnterPayload) (previous *paneViewer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if old, ok := p.byUser[enter.ViewerID]; ok {
		p.removeLocked(enter.ViewerID, old.enter.ChannelID)
		if old.enter.ChannelID != enter.ChannelID {
			previous = old
		}
	}
	v := &paneViewer{pluginID: pluginID, enter: enter}
	p.byUser[enter.ViewerID] = v
	if p.byChannel[enter.ChannelID] == nil {
		p.byChannel[enter.ChannelID] = make(map[uuid.UUID]*paneViewer)
	}
	p.byChannel[enter.ChannelID][enter.ViewerID] = v
	return previous
}

// Update applies a change (a Resize) to a registered viewer's state and
// returns the result; ok is false if userID isn't viewing channelID.
func (p *PaneViewers) Update(userID, channelID uuid.UUID, apply func(*protocol.PluginPaneEnterPayload)) (state protocol.PluginPaneEnterPayload, pluginID string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[userID]
	if !ok || v.enter.ChannelID != channelID {
		return state, "", false
	}
	apply(&v.enter)
	return v.enter, v.pluginID, true
}

// Viewer returns userID's registration and owning plugin if they're viewing
// channelID.
func (p *PaneViewers) Viewer(userID, channelID uuid.UUID) (state protocol.PluginPaneEnterPayload, pluginID string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[userID]
	if !ok || v.enter.ChannelID != channelID {
		return state, "", false
	}
	return v.enter, v.pluginID, true
}

// ViewingPlugin reports whether userID currently has one of pluginID's
// panes open.
func (p *PaneViewers) ViewingPlugin(userID uuid.UUID, pluginID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[userID]
	return ok && v.pluginID == pluginID
}

// Leave unregisters userID from channelID; ok reports whether they were
// registered there (and so whether the plugin needs telling).
func (p *PaneViewers) Leave(userID, channelID uuid.UUID) (pluginID string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[userID]
	if !ok || v.enter.ChannelID != channelID {
		return "", false
	}
	p.removeLocked(userID, channelID)
	return v.pluginID, true
}

// LeaveAll unregisters userID from whatever pane they had open (their
// connection closed) and returns it, if any.
func (p *PaneViewers) LeaveAll(userID uuid.UUID) *paneViewer {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.byUser[userID]
	if !ok {
		return nil
	}
	p.removeLocked(userID, v.enter.ChannelID)
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

// ChannelViewers returns the users currently viewing channelID, if it's
// owned by pluginID (otherwise none).
func (p *PaneViewers) ChannelViewers(channelID uuid.UUID, pluginID string) []uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]uuid.UUID, 0, len(p.byChannel[channelID]))
	for userID, v := range p.byChannel[channelID] {
		if v.pluginID == pluginID {
			out = append(out, userID)
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
