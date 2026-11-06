package client

import "github.com/concord-chat/concord/internal/protocol"

// voiceSignalAllowed reports whether a voice signal comes from someone in
// this client's own call: both people in the voice channel the signal
// names, as this connection's voice states say. Anything else is dropped
// before the engine sees it, so nobody outside the call can open a peer
// connection and receive this microphone (security audit 2026-11-05,
// finding 1; the server checks the same, this covers older or hostile
// servers).
func voiceSignalAllowed(sc *ServerConnection, sig protocol.VoiceSignalRelayPayload) bool {
	if sc == nil {
		return false
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.User == nil || sig.SourceUserID == sc.User.ID {
		return false
	}
	me, ok := sc.VoiceStates[sc.User.ID]
	if !ok || me == nil || me.ChannelID != sig.ChannelID {
		return false
	}
	them, ok := sc.VoiceStates[sig.SourceUserID]
	return ok && them != nil && them.ChannelID == sig.ChannelID
}
