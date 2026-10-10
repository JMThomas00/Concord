package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// A plugin's client/ folder is advertised to clients and served to
// signed-in members: only files of known types, and nothing outside it.
func TestPluginClientPartIsAdvertisedAndServed(t *testing.T) {
	srv, wsURL := startPlainTestServer(t)
	admin, token := createTestUserAndToken(t, srv, "client-admin")
	testServer := models.NewServer("Client", admin.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.CreateRole(models.NewEveryoneRole(testServer.ID)); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(admin.ID, testServer.ID)); err != nil {
		t.Fatal(err)
	}

	png := []byte("\x89PNG fake image bytes")
	files := map[string][]byte{
		"pieces/plugin.toml": []byte("[plugin]\nid = \"pieces\"\nname = \"Pieces\"\nversion = \"1.0.0\"\n\n" +
			"[process]\n[process.entrypoint.windows]\nbin = \"pieces.exe\"\n[process.entrypoint.linux]\nbin = \"pieces\"\n[process.entrypoint.darwin]\nbin = \"pieces\"\n\n" +
			"[client]\ncapabilities = [\"images\", \"sound\"]\n"),
		"pieces/pieces":                 []byte("fake binary"),
		"pieces/pieces.exe":             []byte("fake binary"),
		"pieces/client/assets/king.png": png,
		"pieces/client/sounds/move.wav": []byte("RIFF fake wav"),
		"pieces/client/.secret.png":     []byte("hidden"),
		"pieces/client/tool.exe":        []byte("not allowed"),
		"pieces/client/notes.txt":       []byte("not allowed either"),
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		f, _ := w.Create(name)
		_, _ = f.Write(data)
	}
	_ = w.Close()
	archive := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _, _ = rw.Write(buf.Bytes()) }))
	defer archive.Close()

	client := newTestWSClient(t, wsURL)
	client.identify(token)
	client.send(protocol.OpPluginManage, protocol.PluginManageRequest{ServerID: testServer.ID, Action: protocol.PluginActionInstall, SourceURL: archive.URL})
	m := client.readUntil(10*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginRegistryUpdate })
	var reg protocol.PluginRegistryPayload
	_ = json.Unmarshal(m.Data, &reg)

	var info *protocol.PluginClientInfo
	for i := range reg.PluginClients {
		if reg.PluginClients[i].PluginID == "pieces" {
			info = &reg.PluginClients[i]
		}
	}
	if info == nil {
		t.Fatalf("no client part advertised: %+v", reg.PluginClients)
	}
	var paths []string
	for _, f := range info.Files {
		paths = append(paths, f.Path)
	}
	if len(paths) != 2 || paths[0] != "assets/king.png" || paths[1] != "sounds/move.wav" {
		t.Fatalf("advertised files %v; want only the image and the sound", paths)
	}
	sum := sha256.Sum256(png)
	if info.Files[0].SHA256 != hex.EncodeToString(sum[:]) || info.Hash == "" || len(info.Capabilities) != 2 {
		t.Fatalf("descriptor %+v", info)
	}

	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	api := httptest.NewServer(mux)
	defer api.Close()
	get := func(path, tok string) (int, string, []byte) {
		req, _ := http.NewRequest(http.MethodGet, api.URL+path, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), body
	}
	if code, _, _ := get("/api/plugins/client/pieces/assets/king.png", ""); code != http.StatusUnauthorized {
		t.Fatalf("without signing in: %d", code)
	}
	code, ctype, body := get("/api/plugins/client/pieces/assets/king.png", token)
	if code != http.StatusOK || ctype != "image/png" || !bytes.Equal(body, png) {
		t.Fatalf("image: %d %q %q", code, ctype, body)
	}
	for _, p := range []string{"pieces/tool.exe", "pieces/notes.txt", "pieces/.secret.png", "pieces/../plugin.toml", "pieces/..%2fplugin.toml", "nope/assets/king.png"} {
		if code, _, _ := get("/api/plugins/client/"+p, token); code != http.StatusNotFound {
			t.Fatalf("%s: %d, want 404", p, code)
		}
	}
}
