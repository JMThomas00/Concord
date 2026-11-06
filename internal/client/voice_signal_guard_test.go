package client

import (
	"testing"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Finding 1 (security audit 2026-11-05), the client's half: only someone in
// this client's own voice channel may start a peer connection with it.
func TestVoiceSignalAllowedOnlyFromTheCall(t *testing.T) {
	me, friend, outsider, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	call, elsewhere := uuid.New(), uuid.New()
	sc := &ServerConnection{
		User: &models.User{ID: me},
		VoiceStates: map[uuid.UUID]*models.VoiceState{
			me:       {UserID: me, ChannelID: call},
			friend:   {UserID: friend, ChannelID: call},
			outsider: {UserID: outsider, ChannelID: elsewhere},
		},
	}
	sig := func(from, ch uuid.UUID) protocol.VoiceSignalRelayPayload {
		return protocol.VoiceSignalRelayPayload{SourceUserID: from, ChannelID: ch, Type: "offer"}
	}
	cases := []struct {
		name string
		sig  protocol.VoiceSignalRelayPayload
		want bool
	}{
		{"someone in the call", sig(friend, call), true},
		{"someone in another voice channel", sig(outsider, call), false},
		{"someone in another channel naming theirs", sig(outsider, elsewhere), false},
		{"someone not in voice", sig(stranger, call), false},
		{"a friend naming a channel I'm not in", sig(friend, elsewhere), false},
		{"myself", sig(me, call), false},
	}
	for _, c := range cases {
		if got := voiceSignalAllowed(sc, c.sig); got != c.want {
			t.Errorf("%s: allowed = %v, want %v", c.name, got, c.want)
		}
	}
	delete(sc.VoiceStates, me)
	if voiceSignalAllowed(sc, sig(friend, call)) {
		t.Error("a signal was accepted while not in voice")
	}
}
