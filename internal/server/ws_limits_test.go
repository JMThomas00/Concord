package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/concord-chat/concord/internal/protocol"
)

// Finding 7 (security audit 2026-11-05): no rate limit on anything sent over
// the WebSocket, no enforced connection limit, and a connection could stay
// open without ever identifying.

func TestOpLimiter(t *testing.T) {
	var l opLimiter
	now := time.Now()
	rate := opRate{burst: 10, perSec: 5}
	n := 0
	for i := 0; i < 50; i++ {
		if l.allow(protocol.OpTypingStart, rate, now) {
			n++
		}
	}
	if n != 10 {
		t.Fatalf("a burst let %d through, want 10", n)
	}
	if !l.allow(protocol.OpHeartbeat, rate, now) {
		t.Fatal("a heartbeat was refused")
	}
	if !l.allow(protocol.OpTypingStart, rate, now.Add(time.Second)) {
		t.Fatal("nothing refilled after a second")
	}
	l = opLimiter{}
	if l.allow(protocol.OpSearch, opRate{burst: 5, perSec: 1}, now) {
		t.Fatal("a search cost less than it should")
	}
}

func TestConnLimits(t *testing.T) {
	l := newConnLimits(5, 2)
	if !l.acquire("203.0.113.9") || !l.acquire("203.0.113.9") {
		t.Fatal("refused under the limit")
	}
	if l.acquire("203.0.113.9") {
		t.Fatal("a third connection from one address got in")
	}
	for i := 0; i < 3; i++ {
		if !l.acquire("127.0.0.1") {
			t.Fatal("loopback (Tor) was capped per address")
		}
	}
	if l.acquire("198.51.100.1") {
		t.Fatal("went over max_connections")
	}
	l.release("203.0.113.9")
	if !l.acquire("198.51.100.1") {
		t.Fatal("a released slot wasn't reused")
	}
}

func TestFloodingIsRateLimited(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	s, channel, everyone := setupFileTransferTestServer(t, srv)
	_, tok := addMemberWithEveryone(t, srv, s, everyone, "flood")
	c := newTestWSClient(t, wsURL)
	c.identify(tok)
	for i := 0; i < 400; i++ {
		c.send(protocol.OpTypingStart, protocol.TypingStartPayload{ChannelID: channel.ID})
	}
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == "" && strings.Contains(string(m.Data), "too fast")
	})
	if got == nil {
		t.Fatal("400 operations at once weren't limited")
	}
}

func TestAConnectionMustIdentify(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	old := identifyWithin
	identifyWithin = 300 * time.Millisecond
	t.Cleanup(func() { identifyWithin = old })
	_, wsURL := startPlainTestServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if strings.Contains(err.Error(), "timeout") {
				t.Fatal("an unidentified connection stayed open")
			}
			return
		}
	}
}
