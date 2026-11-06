package client

// The client reaching a real Concord server's onion service through a real
// tor, with the access key from an invite. Needs tor and the Tor network:
//
//	CONCORD_TOR_E2E=1 go test -tags novoice -run TestClientOverTor -v -timeout 20m ./internal/client/
//
// CONCORD_TOR_BRIDGES=snowflake (or obfs4) has the client connect through
// those bridges (their transport program must be installed).

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/server"
)

func TestClientOverTor(t *testing.T) {
	if os.Getenv("CONCORD_TOR_E2E") == "" {
		t.Skip("set CONCORD_TOR_E2E=1 (needs tor and the Tor network)")
	}
	dir := t.TempDir()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := server.DefaultConfig()
	cfg.Host, cfg.Port = "127.0.0.1", port
	cfg.DatabasePath = filepath.Join(dir, "concord.db")
	cfg.PluginsDir = filepath.Join(dir, "Plugins")
	cfg.MessagePruning.Enabled = false
	cfg.Tor.Enabled, cfg.Tor.Only = true, true
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Run()
	defer srv.Shutdown()

	// What an invite carries: the onion address and the access key.
	var onion string
	for i := 0; i < 120 && onion == ""; i++ {
		b, _ := os.ReadFile(filepath.Join(dir, "tor", "onion", "hostname"))
		onion = strings.TrimSpace(string(b))
		time.Sleep(time.Second)
	}
	if onion == "" {
		t.Fatal("the server's tor never made an onion address")
	}
	var keys struct{ Current string }
	b, _ := os.ReadFile(filepath.Join(dir, "tor", "access.json"))
	json.Unmarshal(b, &keys)
	invite := fmt.Sprintf("%s:%d/ABCDEFGH#%s", onion, port, keys.Current)
	t.Logf("invite %s…", invite[:70])

	configureNetwork(NetworkConfig{Bridges: os.Getenv("CONCORD_TOR_BRIDGES")}, filepath.Join(dir, "client"))
	defer stopTor()
	rest, key := splitTorInvite(invite)
	host, _, _ := strings.Cut(rest, "/")
	info := &ClientServerInfo{Address: onion, Port: port, Onion: host, OnionKey: key, OverTor: true}
	info.rememberKey()
	if !info.overTor() {
		t.Fatal("an onion server isn't over Tor")
	}

	conn := NewConnection(info.GetHTTPURL())
	start := time.Now()
	var token string
	for time.Since(start) < 8*time.Minute {
		_, token, err = conn.Register("onion-user", "onion@tor.test", "password12345")
		if err == nil {
			break
		}
		t.Logf("not yet (%s): %v", time.Since(start).Round(time.Second), err)
		time.Sleep(5 * time.Second)
	}
	if token == "" {
		t.Fatalf("never signed up over Tor: %v", err)
	}
	t.Logf("signed up over Tor after %s", time.Since(start).Round(time.Second))

	got := make(chan *protocol.Message, 64)
	conn.SetHandlers(func(m *protocol.Message) { got <- m }, nil, nil, nil)
	if err := conn.Connect(); err != nil {
		t.Fatalf("WebSocket over Tor: %v", err)
	}
	defer conn.Disconnect()
	if err := conn.Identify(token); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Minute)
	for {
		select {
		case m := <-got:
			if m.Type == protocol.EventServerCreate {
				var sc protocol.ServerCreatePayload
				json.Unmarshal(m.Data, &sc)
				if sc.Onion == nil || sc.Onion.Key != keys.Current || !sc.Onion.Only {
					t.Fatalf("SERVER_CREATE onion %+v", sc.Onion)
				}
				t.Logf("READY and SERVER_CREATE over Tor in %s", time.Since(start).Round(time.Second))
				return
			}
		case <-deadline:
			t.Fatal("no SERVER_CREATE over Tor")
		}
	}
}
