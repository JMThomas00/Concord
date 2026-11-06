package client

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/concord-chat/concord/internal/tor"
)

var testOnion = strings.Repeat("b", 54) + "id.onion"

func withNetwork(t *testing.T, cfg NetworkConfig) {
	t.Helper()
	configureNetwork(cfg, t.TempDir())
	t.Cleanup(func() { configureNetwork(NetworkConfig{}, "") })
}

func TestRouting(t *testing.T) {
	withNetwork(t, NetworkConfig{})
	if viaTor("example.com:8080") || !viaTor(testOnion+":8090") || !viaTor(testOnion) {
		t.Fatal("normal mode: only onion addresses go through Tor")
	}
	both := &ClientServerInfo{Address: "chat.example.com", Port: 8080, Onion: testOnion + ":8090"}
	if both.GetHTTPURL() != "http://chat.example.com:8080" || both.overTor() {
		t.Fatalf("not over Tor: %s", both.GetHTTPURL())
	}
	both.OverTor = true
	if both.GetWebSocketURL() != "ws://"+testOnion+":8090/ws" || !both.overTor() {
		t.Fatalf("over Tor: %s", both.GetWebSocketURL())
	}
	both.OverTor = false
	plain := &ClientServerInfo{Address: "10.0.0.5", Port: 8080}

	withNetwork(t, NetworkConfig{Mode: NetworkTorOnly})
	if !viaTor("example.com:443") {
		t.Fatal("Tor only: everything goes through Tor")
	}
	if both.dialHostPort() != testOnion+":8090" {
		t.Fatal("Tor only: a known onion address is used")
	}
	if plain.GetHTTPURL() != "http://10.0.0.5:8080" || !plain.overTor() {
		t.Fatal("Tor only: a server with no onion address goes through Tor at its normal address")
	}
	if why := voiceOffOverTor(&ServerConnection{ServerInfo: plain}); why == "" {
		t.Fatal("voice stays on over Tor")
	}
}

func TestTorInvites(t *testing.T) {
	k, _ := tor.NewClientKey()
	rest, key := splitTorInvite(testOnion + ":8090/ABCD2345#" + strings.ToLower(k.PrivateText()))
	if rest != testOnion+":8090/ABCD2345" || key != k.PrivateText() {
		t.Fatalf("split %q %q", rest, key)
	}
	if rest, key := splitTorInvite("chat.example.com:8080/ABCD2345"); key != "" || rest != "chat.example.com:8080/ABCD2345" {
		t.Fatalf("a normal invite: %q %q", rest, key)
	}

	withNetwork(t, NetworkConfig{})
	a := &App{}
	a.initAddServerForm()
	a.addServerAddress.SetValue(testOnion + ":8090/ABCD2345#" + k.PrivateText())
	a.takePastedInvite()
	if a.addServerAddress.Value() != testOnion || a.addServerPort.Value() != "8090" || a.addServerInvite != "ABCD2345" || a.addServerOnionKey != k.PrivateText() {
		t.Fatalf("pasted: %q %q %q %q", a.addServerAddress.Value(), a.addServerPort.Value(), a.addServerInvite, a.addServerOnionKey)
	}
}

func TestOnionKeysReachTor(t *testing.T) {
	dir := t.TempDir()
	configureNetwork(NetworkConfig{}, dir)
	defer configureNetwork(NetworkConfig{}, "")
	k, _ := tor.NewClientKey()
	info := &ClientServerInfo{Onion: testOnion + ":8090", OnionKey: k.PrivateText()}
	info.rememberKey()
	data, err := os.ReadFile(filepath.Join(dir, "tor", "auth", strings.TrimSuffix(testOnion, ".onion")+".auth_private"))
	if err != nil || strings.TrimSpace(string(data)) != k.ClientAuthLine(testOnion) {
		t.Fatalf("auth file %q %v", data, err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "tor", "auth")); fi.Mode().Perm() != 0o700 {
		t.Fatalf("auth folder mode %v", fi.Mode())
	}
	// A failed onion connection says what's likely wrong.
	if err := torDialError(testOnion+":8090", context.DeadlineExceeded); !strings.Contains(err.Error(), "out of date") {
		t.Errorf("with a key: %v", err)
	}
	other := strings.Repeat("c", 54) + "id.onion"
	if err := torDialError(other+":8090", context.DeadlineExceeded); !strings.Contains(err.Error(), "access key") {
		t.Errorf("without a key: %v", err)
	}
}

func TestShortOnion(t *testing.T) {
	if got := shortOnion(testOnion + ":8090"); got != "bbbbbbbb…bbid.onion:8090" {
		t.Fatalf("%q", got)
	}
}
