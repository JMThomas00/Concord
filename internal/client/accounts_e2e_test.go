package client

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/server"
)

// mailbox is a minimal SMTP server (no TLS, no login) that keeps every
// message it's given.
type mailbox struct {
	port int
	msgs chan string
}

func startMailbox(t *testing.T) *mailbox {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	mb := &mailbox{port: ln.Addr().(*net.TCPAddr).Port, msgs: make(chan string, 32)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go mb.serve(conn)
		}
	}()
	return mb
}

func (mb *mailbox) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	say("220 mailbox")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250 mailbox")
		case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
			say("250 OK")
		case cmd == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			mb.msgs <- b.String()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 OK")
		}
	}
}

var mailedCode = regexp.MustCompile(`(?m)^    ([A-Z0-9]{3}) ([A-Z0-9]{3})\r?$`)

// code waits for the next email to address and returns its code.
func (mb *mailbox) code(t *testing.T, address string) string {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case msg := <-mb.msgs:
			if !strings.Contains(msg, "<"+address+">") {
				continue
			}
			m := mailedCode.FindStringSubmatch(msg)
			if m == nil {
				t.Fatalf("no code in:\n%s", msg)
			}
			return m[1] + m[2]
		case <-timeout:
			t.Fatalf("no email to %s", address)
			return ""
		}
	}
}

// startAccountServer runs a real server; with mb, it sends mail there and
// requires verification.
func startAccountServer(t *testing.T, name string, mb *mailbox) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "concord-accounts-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	config := server.DefaultConfig()
	config.Host, config.Port, config.ServerName = "127.0.0.1", port, name
	config.DatabasePath = filepath.Join(tmpDir, "concord.db")
	config.PluginsDir = filepath.Join(tmpDir, "Plugins")
	config.MessagePruning.Enabled = false
	if mb != nil {
		config.Mail = server.MailConfig{SMTPHost: "127.0.0.1", SMTPPort: mb.port, Security: "none", From: "Concord <chat@example.com>"}
	}
	srv, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Run() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
			c.Close()
			return fmt.Sprintf("http://127.0.0.1:%d", port)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server didn't start")
	return ""
}

// tempConfig is a ConfigManager on a temporary folder (never ~/.concord).
func tempConfig(t *testing.T) *ConfigManager {
	t.Helper()
	dir := t.TempDir()
	return &ConfigManager{
		serversFilePath:     filepath.Join(dir, "servers.json"),
		configFilePath:      filepath.Join(dir, "config.json"),
		sharedFilesFilePath: filepath.Join(dir, "shared_files.json"),
	}
}

// managerFor is a ConnectionManager that knows one server at addr.
func managerFor(t *testing.T, addr string) (*ConnectionManager, uuid.UUID) {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(addr, "http://"))
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	info := NewClientServerInfo("test", host, port, false)
	cm := NewConnectionManager(make(chan tea.Msg, 16))
	cm.AddServer(info)
	return cm, info.ID
}

func TestAutoConnectOnlyRegistersWhereTheProfileIsNew(t *testing.T) {
	addr := startAccountServer(t, "Plain", nil)
	cm, id := managerFor(t, addr)
	alice := &LocalIdentity{ID: "p1", Alias: "alice", Email: "alice@example.com", Password: "password123"}

	r := cm.AutoConnectHTTP(id, alice, nil)
	if r.Err != nil || r.Token == "" || r.Username != "alice" {
		t.Fatalf("first sign-in: %+v", r)
	}
	saved := &SavedCredentials{ProfileID: "p1", Email: r.Email, Token: r.Token}

	// A wrong password on a server this profile knows is reported, not
	// turned into a second account.
	wrong := *alice
	wrong.Password = "not-it-at-all"
	r = cm.AutoConnectHTTP(id, &wrong, saved)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "doesn't match") {
		t.Fatalf("known server, wrong password: %+v", r)
	}

	// Another profile on this computer with the same email: not silently
	// signed in, and no duplicate either.
	other := &LocalIdentity{ID: "p2", Alias: "notalice", Email: "alice@example.com", Password: "different99"}
	r = cm.AutoConnectHTTP(id, other, saved)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "already has an account") {
		t.Fatalf("same email, other profile: %+v", r)
	}

	// The same email and password from another profile signs in as the
	// existing account; the app warns about the name (see AutoConnectMsg).
	same := &LocalIdentity{ID: "p3", Alias: "notalice", Email: "ALICE@example.com", Password: "password123"}
	r = cm.AutoConnectHTTP(id, same, nil)
	if r.Err != nil || r.Username != "alice" {
		t.Fatalf("same credentials: %+v", r)
	}
}

func TestVerifyResetAndSyncEndToEnd(t *testing.T) {
	mb := startMailbox(t)
	mailAddr := startAccountServer(t, "Mailing", mb)
	plainAddr := startAccountServer(t, "Plain", nil)
	cm, mailID := managerFor(t, mailAddr)
	bob := &LocalIdentity{ID: "b", Alias: "bob", Email: "bob@example.com", Password: "password123"}

	// New account on a server that verifies: held until the code is in.
	r := cm.AutoConnectHTTP(mailID, bob, nil)
	if !r.NeedsVerification || r.Token != "" || r.Email != "bob@example.com" {
		t.Fatalf("register on a verifying server: %+v", r)
	}
	api := AccountAPI{Addr: mailAddr}
	// A typo fix sends a fresh code to the corrected address.
	if sentTo, err := api.FixRegistration("bob@example.com", "password123", "bobby@example.com", "bobby"); err != nil || sentTo != "bobby@example.com" {
		t.Fatalf("fix: %q %v", sentTo, err)
	}
	mb.code(t, "bob@example.com") // the first code, now replaced
	user, token, err := api.VerifyAccount("bobby@example.com", mb.code(t, "bobby@example.com"))
	if err != nil || token == "" || user.Username != "bobby" {
		t.Fatalf("verify: %v %v", user, err)
	}
	bob.Email, bob.Alias = "bobby@example.com", "bobby"

	// The same profile on a server without mail.
	cm2, plainID := managerFor(t, plainAddr)
	if r := cm2.AutoConnectHTTP(plainID, bob, nil); r.Err != nil {
		t.Fatalf("plain server: %+v", r)
	}

	// Forgot password: reset on the mailing server, then the other server
	// follows (signed in there with the old password).
	if err := api.ForgotPassword("bobby@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.ResetPassword("bobby@example.com", mb.code(t, "bobby@example.com"), "newpassword9"); err != nil {
		t.Fatal(err)
	}
	lines, ok := syncPassword([]syncTarget{{name: "Plain", email: "bobby@example.com", api: AccountAPI{Addr: plainAddr}}}, "password123", "newpassword9")
	if ok != 1 {
		t.Fatalf("sync: %v", lines)
	}
	for _, a := range []string{mailAddr, plainAddr} {
		conn := NewConnection("")
		conn.serverAddr = a
		if _, _, err := conn.Login("bobby@example.com", "newpassword9"); err != nil {
			t.Fatalf("new password on %s: %v", a, err)
		}
	}

	// A server without mail can't send reset codes, and says so.
	if err := (AccountAPI{Addr: plainAddr}).ForgotPassword("bobby@example.com"); err == nil || err.(*APIError).Code != apiMailUnavailable {
		t.Fatalf("forgot without mail: %v", err)
	}
}

func TestProfilesKeepStableIDsAndForgetTheirSignIns(t *testing.T) {
	cfg := tempConfig(t)
	// A config from before profiles: one identity, no IDs.
	if err := cfg.SaveAppConfig(&AppConfig{Version: 1, Identity: &LocalIdentity{Alias: "amy", Email: "amy@example.com", Password: "password123"}}); err != nil {
		t.Fatal(err)
	}
	first := cfg.GetIdentity()
	if first == nil || first.ID == "" || cfg.GetIdentity().ID != first.ID {
		t.Fatalf("legacy identity didn't get a stable ID: %+v", first)
	}

	ben := &LocalIdentity{Alias: "ben", Email: "ben@example.com", Password: "password456"}
	if err := cfg.SaveIdentity(ben); err != nil {
		t.Fatal(err)
	}
	list, active := cfg.Profiles()
	if len(list) != 2 || active != ben.ID {
		t.Fatalf("profiles %+v active %s", list, active)
	}

	// Ben's saved sign-in goes with him.
	info := NewClientServerInfo("s", "127.0.0.1", 1, false)
	if err := cfg.SaveServers(&ServersConfig{Version: 1, Servers: []*ClientServerInfo{info}}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveServerSignIn(info.ID, ben.ID, ben.Email, "tok", uuid.New()); err != nil {
		t.Fatal(err)
	}
	next, err := cfg.ForgetProfile(ben.ID)
	if err != nil || next != first.ID {
		t.Fatalf("forget: next %q err %v", next, err)
	}
	if s := cfg.GetClientServers(); len(s) != 1 || s[0].SavedCredentials != nil {
		t.Fatalf("Ben's sign-in survived: %+v", s[0].SavedCredentials)
	}
	if cfg.GetIdentity().ID != first.ID {
		t.Fatal("the remaining profile isn't active")
	}
}
