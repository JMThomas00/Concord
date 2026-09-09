package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// buildTestPluginZip builds an in-memory zip containing a minimal, valid
// plugin.toml declaring pluginID, and returns its bytes and hex SHA256 --
// mirrors internal/plugins/install_test.go's own fixture, kept local here
// since it's an internal (non-exported) test helper on that side.
func buildTestPluginZip(t *testing.T, pluginID string) (data []byte, sha256Hex string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("plugin.toml")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	manifest := "[plugin]\nid = \"" + pluginID + "\"\nname = \"Test\"\nversion = \"1.0.0\"\n\n" +
		"[process]\n[process.entrypoint.windows]\nbin = \"test.exe\"\n" +
		"[process.entrypoint.linux]\nbin = \"test\"\n[process.entrypoint.darwin]\nbin = \"test\"\n"
	if _, err := f.Write([]byte(manifest)); err != nil {
		t.Fatalf("write plugin.toml: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// TestHandlePluginInstallEndToEnd drives OpPluginInstall over a real
// WebSocket connection: a server admin points it at a local httptest
// server serving a valid plugin archive, and confirms the archive actually
// gets fetched, verified, and extracted into the server's configured
// plugins directory -- the real gap item 10/13 closes (there was
// previously no protocol surface for this at all, only the fully-manual
// drop-a-folder-in workflow).
func TestHandlePluginInstallEndToEnd(t *testing.T) {
	srv, wsURL := startPlainTestServer(t)

	admin, token := createTestUserAndToken(t, srv, "plugin-install-admin")
	testServer := models.NewServer("Plugin Install Test", admin.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	everyone := models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(admin.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add owner as member: %v", err)
	}

	data, sum := buildTestPluginZip(t, "newplug")
	archiveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer archiveSrv.Close()

	client := newTestWSClient(t, wsURL)
	client.identify(token)

	client.send(protocol.OpPluginInstall, protocol.PluginInstallRequest{
		ServerID:  testServer.ID,
		PluginID:  "newplug",
		SourceURL: archiveSrv.URL,
		SHA256:    sum,
	})
	resp := client.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginConfigUpdate })
	var list protocol.PluginConfigListPayload
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	manifestPath := filepath.Join(srv.handlers.pluginsDir, "newplug", "plugin.toml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected the plugin to be extracted to %q: %v", manifestPath, err)
	}
}

// TestHandlePluginInstallRequiresPermission confirms a non-admin member is
// rejected before any download is even attempted -- OpPluginInstall must
// gate behind PermissionManageServer exactly like OpPluginConfigGet/Set.
func TestHandlePluginInstallRequiresPermission(t *testing.T) {
	srv, wsURL := startPlainTestServer(t)

	owner, _ := createTestUserAndToken(t, srv, "owner")
	member, memberToken := createTestUserAndToken(t, srv, "regular-member")

	testServer := models.NewServer("Plugin Install Perm Test", owner.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	everyone := models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(member.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	client := newTestWSClient(t, wsURL)
	client.identify(memberToken)

	client.send(protocol.OpPluginInstall, protocol.PluginInstallRequest{
		ServerID:  testServer.ID,
		PluginID:  "shouldnotinstall",
		SourceURL: "http://example.invalid/should-not-be-fetched.zip",
		SHA256:    "irrelevant",
	})
	// sendError responds with a plain OpDispatch carrying an empty Type
	// (see Client.sendError, client.go) -- unlike every real event, which
	// always sets Type, so that's what distinguishes an error reply here.
	resp := client.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == ""
	})
	var errPayload protocol.ErrorPayload
	if err := json.Unmarshal(resp.Data, &errPayload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if errPayload.Code != protocol.ErrorCodeForbidden {
		t.Errorf("expected ErrorCodeForbidden, got %d (%s)", errPayload.Code, errPayload.Message)
	}

	if _, err := os.Stat(filepath.Join(srv.handlers.pluginsDir, "shouldnotinstall")); !os.IsNotExist(err) {
		t.Error("expected no plugin folder to be created for a forbidden request")
	}
}
