package client

import (
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// Voice stays with the server you joined it on. Switching servers (to read
// a message, answer one, or follow a toast) leaves the call running; you
// leave it when you choose to, by leaving the channel, joining voice
// somewhere else, or if that server drops. The status bar says where your
// voice is while you look at another server.

// voiceServer is the connection your voice belongs to: the one you're in
// voice on, or else the server on screen (where a new call would start).
func (a *App) voiceServer() *ServerConnection {
	if a.voiceConn != nil {
		return a.voiceConn
	}
	return a.activeConn
}

// myVoiceState is your voice state on the voice server, or nil.
func (a *App) myVoiceState() (*ServerConnection, *voiceStateView) {
	sc := a.voiceServer()
	if sc == nil {
		return nil, nil
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.User == nil {
		return sc, nil
	}
	vs := sc.VoiceStates[sc.User.ID]
	if vs == nil {
		return sc, nil
	}
	return sc, &voiceStateView{ServerID: vs.ServerID, ChannelID: vs.ChannelID, Muted: vs.IsSelfMuted, Deafened: vs.IsSelfDeafened}
}

// voiceStateView is a copy of your voice state, read under the lock.
type voiceStateView struct {
	ServerID, ChannelID uuid.UUID
	Muted, Deafened     bool
}

// sendMyVoiceState updates your mute/deafen on the voice server.
func (a *App) sendMyVoiceState(muted, deafened bool) {
	sc, vs := a.myVoiceState()
	if sc == nil || vs == nil {
		return
	}
	ch := vs.ChannelID
	_ = a.connMgr.SendVoiceStateUpdate(sc.ServerID, &protocol.VoiceStateUpdatePayload{
		ServerID: vs.ServerID, ChannelID: &ch, IsSelfMuted: muted, IsSelfDeafened: deafened})
}

// leaveVoice leaves the voice channel you're in, on whichever server it
// is. True if there was one to leave.
func (a *App) leaveVoice() bool {
	sc, vs := a.myVoiceState()
	if sc == nil || vs == nil {
		a.stopVoiceEngine()
		return false
	}
	_ = a.connMgr.SendVoiceStateUpdate(sc.ServerID, &protocol.VoiceStateUpdatePayload{ServerID: vs.ServerID, ChannelID: nil})
	a.stopVoiceEngine()
	return true
}

// leaveVoiceElsewhere leaves voice on another server before joining voice
// on the one on screen: you're only ever in one call.
func (a *App) leaveVoiceElsewhere() {
	if a.voiceConn != nil && a.voiceConn != a.activeConn {
		a.leaveVoice()
	}
}

// voiceStatus is the status bar's note of where your voice is, when that's
// not the server on screen ("" otherwise).
func (a *App) voiceStatus() string {
	sc := a.voiceConn
	if sc == nil || sc == a.activeConn {
		return ""
	}
	_, vs := a.myVoiceState()
	if vs == nil {
		return ""
	}
	server, channel := "another server", "voice"
	if sc.ServerInfo != nil {
		server = sc.ServerInfo.Name
	}
	for _, ch := range sc.GetChannels(vs.ServerID) {
		if ch.ID == vs.ChannelID {
			channel = ch.Name
		}
	}
	return "🔊 " + server + " › " + channel
}
