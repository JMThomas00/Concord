package client

// Connecting through Tor (Privacy Pack 1, 2026-11-05). Every connection the
// client makes to a server (sign-in, the WebSocket, plugin pictures and
// records) and, in Tor-only mode, everything else (Grapevine, update checks)
// goes through netDial, which picks the route:
//
//   - an .onion address always goes through Tor;
//   - in Tor-only mode (NetworkTorOnly) everything does, and a server whose
//     onion address is known is reached there instead of its normal one;
//   - otherwise, a server set to "over Tor" (ClientServerInfo.OverTor) uses
//     its onion address, and the rest connect directly.
//
// Tor is the program installed on this computer (the installer offers it),
// run by Concord with its own settings in ~/.concord/tor, started the first
// time something needs it. Access keys for onion servers (from invites,
// SERVER_CREATE and ONION_ACCESS) are kept in its ClientOnionAuthDir and
// handed to the running tor at once.
//
// A connection over Tor turns voice off and sends files through the server
// (Tor mode): voice and file transfers otherwise connect computers directly,
// which would show each side's address to the other.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"github.com/concord-chat/concord/internal/tor"
)

// Network modes (AppConfig.Network.Mode).
const (
	NetworkNormal  = ""     // direct; onion servers (and servers set to "over Tor") through Tor
	NetworkTorOnly = "tor"  // everything through Tor
)

// NetworkConfig is how the client reaches the network.
type NetworkConfig struct {
	Mode    string `json:"mode,omitempty"`
	TorPath string `json:"tor_path,omitempty"` // default: found on the PATH
	// Bridges: "" none, "snowflake", "obfs4" (built-in lines) or "custom"
	// (BridgeLines, pasted).
	Bridges     string   `json:"bridges,omitempty"`
	BridgeLines []string `json:"bridge_lines,omitempty"`
}

// torNetwork is the client's tor and the routing decisions. One per
// process: postAPI and friends are plain functions.
type torNetwork struct {
	mu   sync.Mutex
	cfg  NetworkConfig
	dir  string // ~/.concord/tor
	proc *tor.Process
	opts tor.Options
	err  error // why tor couldn't start
	keys map[string]tor.ClientKey // onion host -> access key
}

var clientNet = &torNetwork{keys: map[string]tor.ClientKey{}}

// configureNetwork sets the mode and bridges (at start, and from Settings ›
// Network). A running tor whose settings changed is stopped; the next
// connection that needs it starts it again.
func configureNetwork(cfg NetworkConfig, concordDir string) {
	n := clientNet
	n.mu.Lock()
	changed := n.cfg.TorPath != cfg.TorPath || n.cfg.Bridges != cfg.Bridges || strings.Join(n.cfg.BridgeLines, "\n") != strings.Join(cfg.BridgeLines, "\n")
	n.cfg = cfg
	if concordDir != "" {
		n.dir = filepath.Join(concordDir, "tor")
	}
	p := n.proc
	if changed {
		n.proc, n.err = nil, nil
	}
	n.mu.Unlock()
	if changed && p != nil {
		go p.Stop()
	}
}

// networkMode is the current mode.
func networkMode() string {
	clientNet.mu.Lock()
	defer clientNet.mu.Unlock()
	return clientNet.cfg.Mode
}

// torOnly reports whether everything goes through Tor.
func torOnly() bool { return networkMode() == NetworkTorOnly }

// viaTor reports whether a connection to host goes through Tor.
func viaTor(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return tor.IsOnion(host) || torOnly()
}

// rememberOnionKey keeps an access key for an onion address (host or
// host:port), and gives it to tor if it's running.
func rememberOnionKey(onion, keyText string) {
	if h, _, err := net.SplitHostPort(onion); err == nil {
		onion = h
	}
	onion = strings.ToLower(onion)
	if !tor.IsOnion(onion) || keyText == "" {
		return
	}
	k, err := tor.ParseClientKey(keyText)
	if err != nil {
		return
	}
	n := clientNet
	n.mu.Lock()
	old, had := n.keys[onion]
	n.keys[onion] = k
	dir, p := n.dir, n.proc
	n.mu.Unlock()
	if had && old.PrivateText() == k.PrivateText() {
		return
	}
	if dir != "" {
		_ = writeOnionAuth(filepath.Join(dir, "auth"), onion, k)
	}
	if p != nil {
		_ = p.AddClientAuth(onion, k)
	}
}

// writeOnionAuth writes one onion address's key for tor's
// ClientOnionAuthDir.
func writeOnionAuth(dir, onion string, k tor.ClientKey) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := strings.TrimSuffix(onion, ".onion") + ".auth_private"
	return os.WriteFile(filepath.Join(dir, name), []byte(k.ClientAuthLine(onion)+"\n"), 0o600)
}

// torStatus is how the client's tor is doing, for Settings › Network.
func torStatus() (running bool, st tor.Status, err error) {
	n := clientNet
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.proc == nil {
		return false, tor.Status{}, n.err
	}
	return true, n.proc.Status(), n.err
}

// ensureTor starts tor if it isn't running and waits until it's connected
// to the Tor network (or ctx ends). It returns the SOCKS address.
func (n *torNetwork) ensureTor(ctx context.Context) (string, error) {
	n.mu.Lock()
	if n.proc == nil {
		if err := n.startLocked(); err != nil {
			n.err = err
			n.mu.Unlock()
			return "", err
		}
	}
	p, socks := n.proc, n.opts.SocksAddr()
	n.mu.Unlock()
	if err := p.WaitReady(ctx); err != nil {
		st := p.Status()
		msg := fmt.Sprintf("Tor isn't connected yet (%d%%", st.Bootstrapped)
		if st.Err != "" {
			msg += ": " + st.Err
		}
		return "", errors.New(msg + ")")
	}
	return socks, nil
}

func (n *torNetwork) startLocked() error {
	if n.dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		n.dir = filepath.Join(home, ".concord", "tor")
	}
	path, err := tor.Find(n.cfg.TorPath)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	socks := ln.Addr().String()
	ln.Close()
	auth := filepath.Join(n.dir, "auth")
	if err := os.MkdirAll(auth, 0o700); err != nil {
		return err
	}
	for onion, k := range n.keys {
		_ = writeOnionAuth(auth, onion, k)
	}
	lines := []string{
		// A circuit per destination, so one server can't be linked to
		// another by sharing an exit.
		"SocksPort " + socks + " IsolateDestAddr",
		"ClientOnionAuthDir " + tor.Quote(auth),
	}
	bridges, err := tor.BridgeSettings(n.cfg.Bridges, n.cfg.BridgeLines, path, filepath.Join(n.dir, "bridges.json"))
	if err != nil {
		return err
	}
	lines = append(lines, bridges...)
	n.opts = tor.Options{TorPath: path, Dir: n.dir, Lines: lines}
	p, err := tor.Start(n.opts)
	if err != nil {
		return err
	}
	n.proc, n.err = p, nil
	if n.cfg.Bridges == tor.BridgesSnowflake || n.cfg.Bridges == tor.BridgesObfs4 {
		go n.refreshBridges(p)
	}
	return nil
}

// refreshBridges fetches the current built-in bridge lines through Tor
// once it's connected, for next time.
func (n *torNetwork) refreshBridges(p *tor.Process) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if p.WaitReady(ctx) != nil {
		return
	}
	n.mu.Lock()
	dir := n.dir
	n.mu.Unlock()
	_ = tor.RefreshBuiltin(ctx, newHTTPClient(time.Minute, true), filepath.Join(dir, "bridges.json"))
}

// stopTor stops the client's tor (the app is closing).
func stopTor() {
	clientNet.mu.Lock()
	p := clientNet.proc
	clientNet.proc = nil
	clientNet.mu.Unlock()
	if p != nil {
		p.Stop()
	}
}

// torWait is how long a connection waits for tor to connect.
const torWait = 2 * time.Minute

// netDial dials addr directly or through Tor (see the top of this file).
func netDial(ctx context.Context, network, addr string) (net.Conn, error) {
	return dialRoute(ctx, network, addr, viaTor(addr))
}

func dialRoute(ctx context.Context, network, addr string, overTor bool) (net.Conn, error) {
	if !overTor {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	wait, cancel := context.WithTimeout(ctx, torWait)
	defer cancel()
	socks, err := clientNet.ensureTor(wait)
	if err != nil {
		return nil, err
	}
	d, err := proxy.SOCKS5("tcp", socks, nil, &net.Dialer{Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	// The host name goes to tor unresolved, so lookups don't leak.
	c, err := d.(proxy.ContextDialer).DialContext(ctx, network, addr)
	if err != nil {
		return nil, torDialError(addr, err)
	}
	return c, nil
}

// torDialError says what a failed Tor connection probably means.
func torDialError(addr string, err error) error {
	host, _, _ := net.SplitHostPort(addr)
	if tor.IsOnion(host) {
		clientNet.mu.Lock()
		_, hasKey := clientNet.keys[strings.ToLower(host)]
		clientNet.mu.Unlock()
		if !hasKey {
			return fmt.Errorf("couldn't reach %s through Tor: it may need the access key from an invite (paste the whole invite into Add Server) (%v)", host, err)
		}
		return fmt.Errorf("couldn't reach %s through Tor: the server may be offline, or the access key out of date (%v)", host, err)
	}
	return fmt.Errorf("couldn't connect through Tor: %w", err)
}

// httpClientFor is an http.Client for requests to target (a URL or
// host:port), routed like netDial; its timeout grows by the time tor may
// take to connect when target goes through Tor.
func httpClientFor(target string, timeout time.Duration) *http.Client {
	return newHTTPClient(timeout, viaTor(hostOf(target)))
}

// hostOf is a URL's host:port, or target itself.
func hostOf(target string) string {
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		return u.Host
	}
	return target
}

// newHTTPClient is an http.Client that routes like netDial, or sends
// everything through Tor with overTor.
func newHTTPClient(timeout time.Duration, overTor bool) *http.Client {
	t := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialRoute(ctx, network, addr, overTor || viaTor(addr))
		},
		Proxy: func(req *http.Request) (*url.URL, error) {
			if overTor || viaTor(req.URL.Host) {
				return nil, nil // never past Tor to a proxy from the environment
			}
			return http.ProxyFromEnvironment(req)
		},
		TLSHandshakeTimeout: 15 * time.Second,
		MaxIdleConns:        4,
		IdleConnTimeout:     90 * time.Second,
	}
	if timeout > 0 && (overTor || torOnly()) {
		timeout += torWait // the first request also waits for tor to connect
	}
	return &http.Client{Timeout: timeout, Transport: t}
}
