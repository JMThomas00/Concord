package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Finding 3 (security audit 2026-11-05): terminal escapes in what people
// type reached other people's terminals. OSC 52 in a code span writes the
// clipboard in several terminals.
func TestTypedTextLosesTerminalEscapes(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	s, channel, everyone := setupFileTransferTestServer(t, srv)
	eve, eveTok := addMemberWithEveryone(t, srv, s, everyone, "eve")
	_, bobTok := addMemberWithEveryone(t, srv, s, everyone, "bob")
	e, b := newTestWSClient(t, wsURL), newTestWSClient(t, wsURL)
	e.identify(eveTok)
	b.identify(bobTok)

	osc52 := "\x1b]52;c;Y3VybCBldmlsIHwgc2g=\x07"
	e.send(protocol.OpSetNickname, protocol.SetNicknameRequest{ServerID: s.ID, Nickname: "eve" + osc52})
	got := b.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventNicknameUpdate })
	if strings.Contains(string(got.Data), `\u001b`) || strings.Contains(string(got.Data), `\u0007`) {
		t.Fatalf("a nickname kept its escapes: %s", got.Data)
	}

	e.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: channel.ID, Content: "copy this: `" + osc52 + "`\r\nok"})
	got = b.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMessageCreate })
	if strings.Contains(string(got.Data), `\u001b`) || strings.Contains(string(got.Data), `\r`) {
		t.Fatalf("a message kept its escapes: %s", got.Data)
	}
	msgs, _ := srv.db.GetChannelMessages(channel.ID, 10, nil, eve.ID)
	if len(msgs) != 1 || protocol.HasControl(msgs[0].Content) {
		t.Fatalf("stored with escapes: %+v", msgs)
	}
	_ = models.Message{}
}

func TestUsernamesRefuseControlCharacters(t *testing.T) {
	_, _, ts := accountServer(t, false)
	code, _ := call(t, ts, "/api/register", "", map[string]string{"username": "eve\x1b[2J", "email": "eve@example.com", "password": "password123"})
	if code != http.StatusBadRequest {
		t.Fatalf("registered a username with an escape: %d", code)
	}
	code, resp := call(t, ts, "/api/register", "", map[string]string{"username": "eve", "email": "eve@example.com", "password": "password123"})
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("plain register: %d %v", code, resp)
	}
	tok, _ := resp["token"].(string)
	if code, _ := call(t, ts, "/api/account/update", tok, map[string]string{"current_password": "password123", "username": "eve\u009b31m"}); code != http.StatusBadRequest {
		t.Fatalf("renamed to a username with an escape: %d", code)
	}
}
