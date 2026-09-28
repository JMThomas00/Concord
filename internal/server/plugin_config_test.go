package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
)

// Config saves are validated against the manifest before anything is
// stored, and secret values are encrypted at rest, hidden from admins'
// clients, and handed to the plugin in plaintext.
func TestPluginConfigValidationAndSecrets(t *testing.T) {
	srv, defaultServer, wsURL := startTestPluginServer(t)
	admin, token := createTestUserAndToken(t, srv, "config-admin")
	if err := srv.db.UpdateServerOwner(defaultServer.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	client := newTestWSClient(t, wsURL)
	client.identify(token)

	result := func() protocol.PluginManageResult {
		t.Helper()
		m := client.readUntil(10*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginManageResult })
		var r protocol.PluginManageResult
		_ = json.Unmarshal(m.Data, &r)
		return r
	}

	// A channel name (what old clients stored) and an unknown key are both refused.
	client.send(protocol.OpPluginConfigSet, protocol.PluginConfigSetRequest{
		ServerID: defaultServer.ID, PluginID: "HelloPlugin",
		Config: map[string]string{"activity_notify_channel": "plugin-activity", "nope": "x"},
	})
	r := result()
	if r.OK || r.FieldErrors["activity_notify_channel"] == "" || r.FieldErrors["nope"] == "" {
		t.Fatalf("invalid save accepted or errors missing: %+v", r)
	}
	if stored, _ := srv.db.GetPluginServerConfig("HelloPlugin"); stored["nope"] != "" {
		t.Fatal("a rejected save still stored a value")
	}

	// A secret is stored sealed and never shown back to the admin.
	client.send(protocol.OpPluginConfigSet, protocol.PluginConfigSetRequest{
		ServerID: defaultServer.ID, PluginID: "HelloPlugin", Config: map[string]string{"api_key": "sk-live-123"},
	})
	if r := result(); !r.OK {
		t.Fatalf("secret save failed: %+v", r)
	}
	stored, _ := srv.db.GetPluginServerConfig("HelloPlugin")
	if !strings.HasPrefix(stored["api_key"], "enc:v1:") {
		t.Fatalf("secret stored as %q, want it encrypted", stored["api_key"])
	}
	list := client.readUntil(10*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginConfigUpdate })
	if strings.Contains(string(list.Data), "sk-live-123") || strings.Contains(string(list.Data), "enc:v1:") {
		t.Fatalf("the admin's plugin list leaked the secret: %s", list.Data)
	}
	var payload protocol.PluginConfigListPayload
	_ = json.Unmarshal(list.Data, &payload)
	if len(payload.Plugins) == 0 || len(payload.Plugins[0].SecretsSet) != 1 || payload.Plugins[0].SecretsSet[0] != "api_key" {
		t.Fatalf("SecretsSet = %+v", payload.Plugins)
	}

	installed, _ := srv.db.GetInstalledPlugin("HelloPlugin")
	if got := srv.handlers.buildPluginInfo(installed, true).ConfigValues["api_key"]; got != "sk-live-123" {
		t.Fatalf("the plugin would receive %q, want the plaintext", got)
	}
}
