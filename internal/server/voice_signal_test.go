package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Security audit 2026-11-05, finding 1: voice signals used to be relayed
// from anyone to anyone, so someone outside a call could send an offer to a
// person in it, get a peer connection, and receive their microphone. Now
// signals pass only between two people in the voice channel they name.
func TestVoiceSignalsOnlyBetweenPeopleInTheCall(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	s, _, everyone := setupFileTransferTestServer(t, srv)
	alice, aliceTok := addMemberWithEveryone(t, srv, s, everyone, "alice")
	bob, bobTok := addMemberWithEveryone(t, srv, s, everyone, "bob")
	_, eveTok := addMemberWithEveryone(t, srv, s, everyone, "eve")
	lounge := models.NewVoiceChannel(s.ID, "Lounge")
	if err := srv.db.CreateChannel(lounge); err != nil {
		t.Fatal(err)
	}
	other := models.NewVoiceChannel(s.ID, "Other")
	if err := srv.db.CreateChannel(other); err != nil {
		t.Fatal(err)
	}

	a, b, e := newTestWSClient(t, wsURL), newTestWSClient(t, wsURL), newTestWSClient(t, wsURL)
	a.identify(aliceTok)
	b.identify(bobTok)
	e.identify(eveTok)
	join := func(c *testWSClient, ch *models.Channel) {
		t.Helper()
		c.send(protocol.OpVoiceStateUpdate, protocol.VoiceStateUpdatePayload{ServerID: s.ID, ChannelID: &ch.ID})
		if c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventVoiceStateUpdate }) == nil {
			t.Fatal("no voice state after joining")
		}
	}
	join(a, lounge)
	join(b, lounge)

	signalFrom := func(c *testWSClient, wait time.Duration) *protocol.VoiceSignalRelayPayload {
		m := c.readUntil(wait, func(m *protocol.Message) bool { return m.Type == protocol.EventVoiceSignal })
		if m == nil {
			return nil
		}
		var p protocol.VoiceSignalRelayPayload
		json.Unmarshal(m.Data, &p)
		return &p
	}
	offer := func(c *testWSClient, to models.User, ch *models.Channel) {
		c.send(protocol.OpVoiceSignal, protocol.VoiceSignalPayload{TargetUserID: to.ID, ChannelID: ch.ID, Type: "offer", SDP: "v=0"})
	}

	// Eve isn't in voice at all, then she's in another voice channel naming
	// the call's. Neither offer may arrive: the first signal alice gets must
	// be bob's, sent after both of eve's.
	offer(e, *alice, lounge)
	join(e, other)
	offer(e, *alice, lounge)
	offer(b, *alice, lounge)
	if p := signalFrom(a, 5*time.Second); p == nil || p.SourceUserID != bob.ID {
		t.Fatalf("the first signal to reach alice wasn't bob's: %+v", p)
	}

	// Eve's "speaking" in a channel she isn't in reaches nobody (finding 13):
	// the first speaking event bob sees must be alice's.
	e.send(protocol.OpVoiceSpeaking, protocol.VoiceSpeakingPayload{ChannelID: lounge.ID, IsSpeaking: true})
	a.send(protocol.OpVoiceSpeaking, protocol.VoiceSpeakingPayload{ChannelID: lounge.ID, IsSpeaking: true})
	m := b.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventVoiceSpeaking })
	var sp protocol.VoiceSpeakingEventPayload
	json.Unmarshal(m.Data, &sp)
	if sp.UserID != alice.ID {
		t.Fatalf("eve faked speaking in the call: %s", m.Data)
	}
}
