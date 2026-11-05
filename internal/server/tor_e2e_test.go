package server

// A real onion service with client authorization, reached through a real
// Tor client. Needs tor installed and the Tor network:
//
//	CONCORD_TOR_E2E=1 go test -tags novoice -run TestTorEndToEnd -v -timeout 20m ./internal/server/

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/concord-chat/concord/internal/tor"
)

// torClient runs a tor client with a SOCKS port, holding key (if any) for
// the onion address.
func torClient(t *testing.T, name, onion string, key *tor.ClientKey) string {
	t.Helper()
	path, err := tor.Find("")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	socks := ln.Addr().String()
	ln.Close()
	dir := filepath.Join(t.TempDir(), name)
	auth := filepath.Join(dir, "auth")
	os.MkdirAll(auth, 0o700)
	if key != nil {
		os.WriteFile(filepath.Join(auth, "concord.auth_private"), []byte(key.ClientAuthLine(onion)+"\n"), 0o600)
	}
	p, err := tor.Start(tor.Options{TorPath: path, Dir: dir, Lines: []string{
		"SocksPort " + socks,
		"ClientOnionAuthDir " + tor.Quote(auth),
	}, Log: func(level, msg string) { t.Logf("[%s %s] %s", name, level, msg) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := p.WaitReady(ctx); err != nil {
		t.Fatalf("%s tor didn't connect: %v", name, err)
	}
	return socks
}

// fetchOver gets url through the SOCKS port.
func fetchOver(socks, url string) (string, error) {
	d, err := proxy.SOCKS5("tcp", socks, nil, &net.Dialer{Timeout: 60 * time.Second})
	if err != nil {
		return "", err
	}
	c := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{Dial: d.Dial}}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

// keepTrying fetches until it works or the time is up.
func keepTrying(t *testing.T, socks, url string, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		body, err := fetchOver(socks, url)
		if err == nil {
			return body, nil
		}
		last = err
		t.Logf("not yet: %v", err)
		time.Sleep(5 * time.Second)
	}
	return "", last
}

func TestTorEndToEnd(t *testing.T) {
	if os.Getenv("CONCORD_TOR_E2E") == "" {
		t.Skip("set CONCORD_TOR_E2E=1 (needs tor and the Tor network)")
	}
	// The thing being served: a page only the right people should reach.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "concord over tor") }))
	defer ln.Close()

	o := &onionService{cfg: TorConfig{Enabled: true}, port: port, dir: filepath.Join(t.TempDir(), "tor")}
	if err := o.start(); err != nil {
		t.Fatal(err)
	}
	defer o.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := o.proc.WaitReady(ctx); err != nil {
		t.Fatalf("the server's tor didn't connect: %v", err)
	}
	var onion string
	for i := 0; i < 60 && onion == ""; i++ {
		onion = o.onionAddress()
		time.Sleep(time.Second)
	}
	if onion == "" {
		t.Fatal("tor never wrote the onion address")
	}
	t.Logf("onion service %s:%d", onion, port)
	url := fmt.Sprintf("http://%s:%d/", onion, port)
	first, _ := tor.ParseClientKey(o.access().Key)

	// With the key: reached.
	withKey := torClient(t, "with-key", onion, &first)
	start := time.Now()
	body, err := keepTrying(t, withKey, url, 5*time.Minute)
	if err != nil || body != "concord over tor" {
		t.Fatalf("with the access key: %q %v", body, err)
	}
	t.Logf("reached with the key in %s", time.Since(start).Round(time.Second))

	// Without it: Tor can't even find the service.
	without := torClient(t, "no-key", onion, nil)
	for i := 0; i < 3; i++ {
		if body, err := fetchOver(without, url); err == nil {
			t.Fatalf("reached without the access key: %q", body)
		} else {
			t.Logf("without the key, as expected: %v", err)
		}
	}
	wrong, _ := tor.NewClientKey()
	wrongKey := torClient(t, "wrong-key", onion, &wrong)
	if body, err := fetchOver(wrongKey, url); err == nil {
		t.Fatalf("reached with someone else's key: %q", body)
	}

	// A new key: it works, and so does the old one, for its week.
	if err := o.rotate(); err != nil {
		t.Fatal(err)
	}
	second, _ := tor.ParseClientKey(o.access().Key)
	if second.PrivateText() == first.PrivateText() {
		t.Fatal("rotate kept the key")
	}
	newKey := torClient(t, "new-key", onion, &second)
	if body, err := keepTrying(t, newKey, url, 5*time.Minute); err != nil || !strings.Contains(body, "concord") {
		t.Fatalf("with the new key: %q %v", body, err)
	}
	oldAgain := torClient(t, "old-key-again", onion, &first)
	if body, err := keepTrying(t, oldAgain, url, 3*time.Minute); err != nil || !strings.Contains(body, "concord") {
		t.Fatalf("the old key stopped working at once: %q %v", body, err)
	}
}
