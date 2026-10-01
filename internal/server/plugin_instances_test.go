package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// An admin adds, renames and removes instances of a plugin that allows
// them, over the real WebSocket; clients learn the instance's channel kind.
func TestPluginInstancesOverTheWire(t *testing.T) {
	srv, wsURL := startPlainTestServer(t)
	admin, token := createTestUserAndToken(t, srv, "instances-admin")
	testServer := models.NewServer("Instances", admin.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.CreateRole(models.NewEveryoneRole(testServer.ID)); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(admin.ID, testServer.ID)); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("persona/plugin.toml")
	_, _ = f.Write([]byte("[plugin]\nid = \"persona\"\nname = \"Persona\"\nversion = \"1.0.0\"\ninstances = true\n\n" +
		"[process]\n[process.entrypoint.windows]\nbin = \"persona.exe\"\n[process.entrypoint.linux]\nbin = \"persona\"\n[process.entrypoint.darwin]\nbin = \"persona\"\n\n" +
		"[[channel_kind]]\nkind = \"chat\"\ndisplay_name = \"Persona\"\n"))
	for _, name := range []string{"persona/persona", "persona/persona.exe"} {
		b, _ := w.Create(name)
		_, _ = b.Write([]byte("fake binary"))
	}
	_ = w.Close()
	archive := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _, _ = rw.Write(buf.Bytes()) }))
	defer archive.Close()

	client := newTestWSClient(t, wsURL)
	client.identify(token)
	act := func(req protocol.PluginManageRequest) protocol.PluginManageResult {
		t.Helper()
		req.ServerID = testServer.ID
		client.send(protocol.OpPluginManage, req)
		m := client.readUntil(10*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginManageResult })
		var r protocol.PluginManageResult
		_ = json.Unmarshal(m.Data, &r)
		return r
	}

	if r := act(protocol.PluginManageRequest{Action: protocol.PluginActionInstall, SourceURL: archive.URL}); !r.OK {
		t.Fatalf("install: %+v", r)
	}
	if r := act(protocol.PluginManageRequest{Action: protocol.PluginActionAddInstance, PluginID: "persona", Name: "Alice"}); !r.OK {
		t.Fatalf("add_instance: %+v", r)
	}
	var instanceKind *protocol.PluginChannelKindInfo
	for _, k := range srv.handlers.PluginChannelKindInfos() {
		if k.PluginID == "persona-alice" {
			k := k
			instanceKind = &k
		}
	}
	if instanceKind == nil || instanceKind.BaseID != "persona" || instanceKind.InstanceName != "Alice" || instanceKind.DisplayName != "Persona" {
		t.Fatalf("instance channel kind %+v", instanceKind)
	}

	if r := act(protocol.PluginManageRequest{Action: protocol.PluginActionRenameInstance, PluginID: "persona-alice", Name: "Alicia"}); !r.OK {
		t.Fatalf("rename_instance: %+v", r)
	}
	if m, _ := srv.plugins.Registry().Manifest("persona-alice"); m == nil || m.Plugin.Name != "Alicia" {
		t.Fatal("rename didn't take")
	}
	if r := act(protocol.PluginManageRequest{Action: protocol.PluginActionRemoveInstance, PluginID: "persona"}); r.OK {
		t.Fatal("removed the base plugin as an instance")
	}
	if r := act(protocol.PluginManageRequest{Action: protocol.PluginActionRemoveInstance, PluginID: "persona-alice"}); !r.OK {
		t.Fatalf("remove_instance: %+v", r)
	}
	if _, ok := srv.plugins.Registry().Manifest("persona-alice"); ok {
		t.Fatal("removed instance is still registered")
	}
}
