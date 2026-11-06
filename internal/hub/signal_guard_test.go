package hub

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

// Finding 6 (security audit 2026-11-05): anyone can register a "server" at
// any host and port, and the hub POSTs to it when someone joins, so a
// listing at 127.0.0.1 or 192.168.x.x aimed the hub at its own network. Only
// a hub with allow_private_servers contacts private addresses.
func TestJoinSignalsOnlyReachPublicServers(t *testing.T) {
	var hits atomic.Int32
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer inside.Close()
	host, port, _ := net.SplitHostPort(inside.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	srv := &RegisteredServer{ID: "x", Host: host, Port: p, RegistrationSecret: "s"}

	h, _ := newTestHub(t, "guarded")
	if err := h.signalServer(srv, "token"); err == nil || hits.Load() != 0 {
		t.Fatalf("the hub reached a loopback \"server\": err=%v hits=%d", err, hits.Load())
	}
	h.config.AllowPrivateServers = true
	if err := h.signalServer(srv, "token"); err != nil || hits.Load() != 1 {
		t.Fatalf("with allow_private_servers the LAN server wasn't reached: err=%v hits=%d", err, hits.Load())
	}
}
