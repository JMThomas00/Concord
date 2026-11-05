package server

// The Tor server (Privacy Pack 1, 2026-11-05). With [tor] enabled, the
// server runs the installed tor program (internal/tor) with an onion
// service pointing at itself, so it gets a .onion address. With
// client_auth (the default), the onion service is published encrypted to
// an access key: without the key, Tor can't find the server at all, even
// knowing its address. Invites carry the key (host:port/CODE#KEY), and
// every member gets it when they sign in, so they can switch to Tor.
//
// A new access key (Server Settings › About, T twice) is for when one has
// leaked: members online get it straight away and everyone else when they
// next sign in, and the old one keeps working for a week meanwhile.
//
// [tor] only = true listens on 127.0.0.1 alone, so the server can be
// reached through Tor and nothing else. An onion server stays off
// Grapevine unless [tor] grapevine = true (and never when only).
//
// tor's folder (tor/ beside the database) holds the onion service's own
// key (its address), the access keys, and tor's state. Backups carry the
// first two.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/tor"
)

// TorConfig is [tor] in concord-server.toml.
type TorConfig struct {
	// Enabled gives the server an onion address.
	Enabled bool `toml:"enabled"`
	// Only: reachable through Tor alone (listens on 127.0.0.1).
	Only bool `toml:"only,omitempty"`
	// TorPath is the tor program (default: found on the PATH or where it's
	// usually installed).
	TorPath string `toml:"tor_path,omitempty"`
	// ClientAuth: only people with the access key can find the server
	// (default true).
	ClientAuth *bool `toml:"client_auth,omitempty"`
	// Grapevine: list the server's normal address on Grapevine anyway
	// (default false; never with only). The onion address is never listed.
	Grapevine bool `toml:"grapevine,omitempty"`
}

func (t TorConfig) clientAuth() bool { return t.ClientAuth == nil || *t.ClientAuth }

const (
	torDirName        = "tor"
	onionDirName      = "onion" // tor's HiddenServiceDir
	accessKeysName    = "access.json"
	accessGrace       = 7 * 24 * time.Hour // an old access key keeps working this long
	accessAuthFile    = "concord.auth"
	accessAuthOldFile = "concord-previous.auth"
)

// accessKeys are the onion service's client authorization keys.
type accessKeys struct {
	Current       string `json:"current"`                  // private key text
	Previous      string `json:"previous,omitempty"`       // still accepted until PreviousUntil
	PreviousUntil int64  `json:"previous_until,omitempty"` // Unix seconds
	Made          int64  `json:"made"`
}

// onionService is the server's onion service.
type onionService struct {
	cfg  TorConfig
	port int
	dir  string // tor/ beside the database

	mu      sync.Mutex
	proc    *tor.Process
	address string // xxxx.onion, once tor has made it
	keys    accessKeys
	err     string // why it isn't running
}

// torDir is tor's folder for a server whose database is at dbPath.
func torDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), torDirName) }

// startTor starts the onion service when [tor] is on. It doesn't wait for
// tor to connect; About shows how far it has got.
func (s *Server) startTor() {
	if !s.config.Tor.Enabled || s.onion != nil {
		return
	}
	o := &onionService{cfg: s.config.Tor, port: s.config.Port, dir: torDir(s.config.DatabasePath)}
	s.onion = o
	if err := o.start(); err != nil {
		o.err = err.Error()
		Logger.Error("Tor isn't running, so the server has no onion address", "error", err)
		return
	}
	go o.expireOldKeys(s)
}

func (o *onionService) hsDir() string { return filepath.Join(o.dir, onionDirName) }

func (o *onionService) start() error {
	if err := os.MkdirAll(o.hsDir(), 0o700); err != nil {
		return err
	}
	_ = os.Chmod(o.dir, 0o700)
	_ = os.Chmod(o.hsDir(), 0o700)
	if err := o.loadKeys(); err != nil {
		return err
	}
	if err := o.writeAuth(); err != nil {
		return err
	}
	path, err := tor.Find(o.cfg.TorPath)
	if err != nil {
		return err
	}
	proc, err := tor.Start(tor.Options{
		TorPath: path,
		Dir:     o.dir,
		Lines: []string{
			"SocksPort 0", // the server only answers; it never connects out through Tor
			"HiddenServiceDir " + tor.Quote(o.hsDir()),
			"HiddenServiceVersion 3",
			fmt.Sprintf("HiddenServicePort %d 127.0.0.1:%d", o.port, o.port),
		},
		Log: func(level, msg string) {
			switch level {
			case "error":
				Logger.Error(msg)
			case "warn":
				Logger.Warn(msg)
			default:
				Logger.Info(msg)
			}
		},
	})
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.proc = proc
	o.mu.Unlock()
	Logger.Info("Starting tor for the onion service", "tor", path, "client_auth", o.cfg.clientAuth(), "only", o.cfg.Only)
	return nil
}

// stop stops tor (Shutdown; a restart starts it again).
func (o *onionService) stop() {
	o.mu.Lock()
	p := o.proc
	o.mu.Unlock()
	if p != nil {
		p.Stop()
	}
}

// onionAddress is the .onion address, once tor has written it.
func (o *onionService) onionAddress() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.address != "" {
		return o.address
	}
	data, err := os.ReadFile(filepath.Join(o.hsDir(), "hostname"))
	if err != nil {
		return ""
	}
	if a := strings.TrimSpace(string(data)); tor.IsOnion(a) {
		o.address = a
	}
	return o.address
}

// loadKeys reads the access keys, making the first one.
func (o *onionService) loadKeys() error {
	path := filepath.Join(o.dir, accessKeysName)
	data, err := os.ReadFile(path)
	if err == nil {
		var k accessKeys
		if err := json.Unmarshal(data, &k); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if _, err := tor.ParseClientKey(k.Current); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		o.mu.Lock()
		o.keys = k
		o.mu.Unlock()
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !o.cfg.clientAuth() {
		return nil // made if it's ever turned on
	}
	k, err := tor.NewClientKey()
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.keys = accessKeys{Current: k.PrivateText(), Made: time.Now().Unix()}
	o.mu.Unlock()
	return o.saveKeys()
}

func (o *onionService) saveKeys() error {
	o.mu.Lock()
	data, err := json.MarshalIndent(o.keys, "", "  ")
	o.mu.Unlock()
	if err != nil {
		return err
	}
	path := filepath.Join(o.dir, accessKeysName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeAuth writes the onion service's authorized clients: the current key
// and an old one still in its week, or none with client_auth off.
func (o *onionService) writeAuth() error {
	dir := filepath.Join(o.hsDir(), "authorized_clients")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	o.mu.Lock()
	k := o.keys
	o.mu.Unlock()
	write := func(name, text string, on bool) error {
		path := filepath.Join(dir, name)
		if !on || text == "" {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			return nil
		}
		key, err := tor.ParseClientKey(text)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(key.ServiceAuthLine()+"\n"), 0o600)
	}
	auth := o.cfg.clientAuth()
	if err := write(accessAuthFile, k.Current, auth); err != nil {
		return err
	}
	oldOn := auth && k.Previous != "" && time.Now().Unix() < k.PreviousUntil
	return write(accessAuthOldFile, k.Previous, oldOn)
}

// rotate makes a new access key; the old one works for another week.
func (o *onionService) rotate() error {
	if !o.cfg.clientAuth() {
		return errors.New("this server's onion address doesn't use an access key (client_auth is off)")
	}
	k, err := tor.NewClientKey()
	if err != nil {
		return err
	}
	o.mu.Lock()
	old := o.keys
	o.keys = accessKeys{Current: k.PrivateText(), Previous: old.Current, PreviousUntil: time.Now().Add(accessGrace).Unix(), Made: time.Now().Unix()}
	o.mu.Unlock()
	if err := o.saveKeys(); err != nil {
		o.mu.Lock()
		o.keys = old
		o.mu.Unlock()
		return err
	}
	if err := o.writeAuth(); err != nil {
		return err
	}
	return o.reload()
}

func (o *onionService) reload() error {
	o.mu.Lock()
	p := o.proc
	o.mu.Unlock()
	if p == nil {
		return nil // read when tor next starts
	}
	return p.Reload()
}

// expireOldKeys stops accepting an old access key once its week is over.
func (o *onionService) expireOldKeys(s *Server) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		if s.admin.stopping.Load() {
			return
		}
		o.mu.Lock()
		k := o.keys
		o.mu.Unlock()
		if k.Previous == "" || time.Now().Unix() < k.PreviousUntil {
			continue
		}
		o.mu.Lock()
		o.keys.Previous, o.keys.PreviousUntil = "", 0
		o.mu.Unlock()
		if err := o.saveKeys(); err == nil {
			_ = o.writeAuth()
			_ = o.reload()
			Logger.Info("The old Tor access key stopped working (its week is over)")
		}
	}
}

// access is what members get: the address and current key ("" until tor
// has made the address).
func (o *onionService) access() *protocol.OnionAccess {
	addr := o.onionAddress()
	if addr == "" {
		return nil
	}
	a := &protocol.OnionAccess{Address: addr + ":" + strconv.Itoa(o.port), Only: o.cfg.Only}
	if o.cfg.clientAuth() {
		o.mu.Lock()
		a.Key = o.keys.Current
		o.mu.Unlock()
	}
	return a
}

// onionInvite is an invite to paste: host:port/CODE#KEY.
func onionInvite(a *protocol.OnionAccess, code string) string {
	if a == nil {
		return ""
	}
	s := a.Address + "/" + code
	if a.Key != "" {
		s += "#" + a.Key
	}
	return s
}

// onionAccess is the server's OnionAccess (nil without an onion address).
func (s *Server) onionAccess() *protocol.OnionAccess {
	if s.onion == nil {
		return nil
	}
	return s.onion.access()
}

// rotateTorKey makes a new access key and gives it to every member online.
func (s *Server) rotateTorKey() error {
	if s.onion == nil {
		return errors.New("this server has no onion address ([tor] isn't on)")
	}
	if err := s.onion.rotate(); err != nil {
		return err
	}
	a := s.onion.access()
	if a == nil {
		return nil
	}
	for _, srv := range s.serversForBroadcast() {
		p := *a
		p.ServerID = srv.String()
		for _, uid := range s.hub.GetOnlineUsers(srv) {
			_ = s.hub.SendToUser(uid, protocol.EventOnionAccess, p)
		}
	}
	return nil
}

// serversForBroadcast lists the servers this process hosts.
func (s *Server) serversForBroadcast() []uuid.UUID {
	srvs, err := s.db.GetAllServers()
	if err != nil {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(srvs))
	for _, srv := range srvs {
		ids = append(ids, srv.ID)
	}
	return ids
}

// torStatus is the onion service's line on About.
func (s *Server) torStatus(st *protocol.ServerStatusPayload) {
	if !s.config.Tor.Enabled {
		return
	}
	st.TorEnabled = true
	st.TorOnly = s.config.Tor.Only
	st.TorClientAuth = s.config.Tor.clientAuth()
	o := s.onion
	if o == nil {
		return
	}
	st.TorAddress = o.onionAddress()
	if st.TorAddress != "" {
		st.TorAddress += ":" + strconv.Itoa(o.port)
	}
	o.mu.Lock()
	p, errText, k := o.proc, o.err, o.keys
	o.mu.Unlock()
	if p != nil {
		ps := p.Status()
		st.TorRunning = ps.Running
		st.TorBootstrapped = ps.Bootstrapped
		if errText == "" && ps.Bootstrapped < 100 {
			errText = ps.Err
		}
	}
	st.TorError = errText
	if k.Previous != "" && time.Now().Unix() < k.PreviousUntil {
		st.TorOldKeyUntil = time.Unix(k.PreviousUntil, 0).UTC()
	}
}

// grapevineAllowed: an onion server stays off Grapevine unless its owner
// chose otherwise, and one reachable only over Tor always does.
func (s *Server) grapevineAllowed() bool {
	t := s.config.Tor
	if !t.Enabled {
		return true
	}
	return t.Grapevine && !t.Only
}

// listenHost is where the server listens: 127.0.0.1 alone when it's
// reachable only through Tor.
func (s *Server) listenHost() string {
	if s.config.Tor.Enabled && s.config.Tor.Only {
		return "127.0.0.1"
	}
	return s.config.Host
}

// onionFor is the OnionAccess members of serverID get (nil without one).
func (h *Handlers) onionFor(serverID uuid.UUID) *protocol.OnionAccess {
	if h.onion == nil {
		return nil
	}
	a := h.onion()
	if a == nil {
		return nil
	}
	c := *a
	c.ServerID = serverID.String()
	return &c
}

// hostOnly is the host part of host:port.
func hostOnly(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}
