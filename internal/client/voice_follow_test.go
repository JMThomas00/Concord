package client

import (
	"strings"
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// Your voice stays with its server while you look at another, and the
// status bar says where it is.
func TestVoiceStaysWithItsServer(t *testing.T) {
	me := &models.User{ID: uuid.New()}
	protoServer, voiceChan := uuid.New(), uuid.New()
	redOak := &ServerConnection{
		ServerID:   uuid.New(),
		ServerInfo: &ClientServerInfo{Name: "RedOak"},
		User:       me,
		Channels:   map[uuid.UUID][]*models.Channel{protoServer: {{ID: voiceChan, Name: "Voice Channel 1"}}},
		VoiceStates: map[uuid.UUID]*models.VoiceState{
			me.ID: {UserID: me.ID, ServerID: protoServer, ChannelID: voiceChan, IsSelfMuted: true},
		},
	}
	sequoia := &ServerConnection{ServerID: uuid.New(), User: &models.User{ID: uuid.New()}}
	a := &App{activeConn: redOak, voiceConn: redOak}

	if a.voiceStatus() != "" {
		t.Fatal("a voice note while looking at the call's own server")
	}
	a.activeConn = sequoia // switch servers
	if a.voiceServer() != redOak {
		t.Fatal("voice moved with the screen")
	}
	if s := a.voiceStatus(); !strings.Contains(s, "RedOak") || !strings.Contains(s, "Voice Channel 1") {
		t.Fatalf("status %q", s)
	}
	if sc, vs := a.myVoiceState(); sc != redOak || vs == nil || !vs.Muted || vs.ChannelID != voiceChan {
		t.Fatalf("state %+v", vs)
	}
	a.voiceConn = nil // not in a call: a new one starts on the server on screen
	if a.voiceServer() != sequoia {
		t.Fatal("no call, but voice isn't on the server on screen")
	}
}
