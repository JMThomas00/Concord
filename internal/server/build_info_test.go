package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
)

// TestReadyPayloadIncludesServerBuildInfo drives a real WebSocket identify
// against a real running server (startPlainTestServer, the same helper
// permission_overwrites_test.go uses) and asserts the OpReady response
// actually carries the server's build identity set via SetBuildInfo --
// this is a real regression test: before ServerVersion/ServerGitCommit/
// ServerBuildTime existed on ReadyPayload, this would fail to compile, and
// before HandleIdentify's two ReadyPayload{...} construction sites read
// Handlers.version/gitCommit/buildTime, the fields would come back empty.
func TestReadyPayloadIncludesServerBuildInfo(t *testing.T) {
	srv, wsURL := startPlainTestServer(t)
	srv.SetBuildInfo("9.9.9", "abc1234", "2026-09-08T00:00:00Z")

	_, token := createTestUserAndToken(t, srv, "buildinfouser")

	client := newTestWSClient(t, wsURL)
	client.send(protocol.OpIdentify, protocol.IdentifyPayload{Token: token})
	readyMsg := client.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Op == protocol.OpReady })

	var ready protocol.ReadyPayload
	if err := json.Unmarshal(readyMsg.Data, &ready); err != nil {
		t.Fatalf("failed to unmarshal ReadyPayload: %v", err)
	}

	if ready.ServerVersion != "9.9.9" {
		t.Errorf("ServerVersion = %q, want %q", ready.ServerVersion, "9.9.9")
	}
	if ready.ServerGitCommit != "abc1234" {
		t.Errorf("ServerGitCommit = %q, want %q", ready.ServerGitCommit, "abc1234")
	}
	if ready.ServerBuildTime != "2026-09-08T00:00:00Z" {
		t.Errorf("ServerBuildTime = %q, want %q", ready.ServerBuildTime, "2026-09-08T00:00:00Z")
	}
}
