package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/tor"
)

var testOnionHost = strings.Repeat("a", 54) + "id.onion"

// fakeOnion gives srv an onion service with no tor behind it: the keys and
// the address tor would have written.
func fakeOnion(t *testing.T, srv *Server, cfg TorConfig) *onionService {
	t.Helper()
	cfg.Enabled = true
	srv.config.Tor = cfg
	o := &onionService{cfg: cfg, port: 8090, dir: filepath.Join(t.TempDir(), "tor")}
	if err := os.MkdirAll(o.hsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := o.loadKeys(); err != nil {
		t.Fatal(err)
	}
	if err := o.writeAuth(); err != nil {
		t.Fatal(err)
	}
	srv.onion = o
	return o
}

func authFile(o *onionService, name string) string {
	data, _ := os.ReadFile(filepath.Join(o.hsDir(), "authorized_clients", name))
	return strings.TrimSpace(string(data))
}

func TestOnionAccessKeys(t *testing.T) {
	srv := idleServer(t)
	o := fakeOnion(t, srv, TorConfig{})

	if srv.onionAccess() != nil {
		t.Fatal("access before tor has made the address")
	}
	os.WriteFile(filepath.Join(o.hsDir(), "hostname"), []byte(testOnionHost+"\n"), 0o600)
	a := srv.onionAccess()
	if a == nil || a.Address != testOnionHost+":8090" || a.Key == "" || a.Only {
		t.Fatalf("access %+v", a)
	}
	key, err := tor.ParseClientKey(a.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got := authFile(o, accessAuthFile); got != key.ServiceAuthLine() {
		t.Fatalf("authorized client %q, want %q", got, key.ServiceAuthLine())
	}
	if fi, _ := os.Stat(filepath.Join(o.dir, accessKeysName)); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Fatal("the access keys must be private")
	}
	if got := onionInvite(a, "ABCD2345"); got != a.Address+"/ABCD2345#"+a.Key {
		t.Fatalf("invite %q", got)
	}
	if inviteCodeFrom(onionInvite(a, "ABCD2345")) != "ABCD2345" {
		t.Fatal("the server doesn't read the code out of a Tor invite")
	}

	// A new key: the old one stays authorized for a week.
	if err := o.rotate(); err != nil {
		t.Fatal(err)
	}
	b := srv.onionAccess()
	if b.Key == a.Key {
		t.Fatal("rotate kept the key")
	}
	if authFile(o, accessAuthOldFile) != key.ServiceAuthLine() {
		t.Fatal("the old key stopped working at once")
	}
	// Kept across a restart.
	o2 := &onionService{cfg: o.cfg, port: o.port, dir: o.dir}
	if err := o2.loadKeys(); err != nil || o2.keys.Current != b.Key || o2.keys.Previous != a.Key {
		t.Fatalf("reloaded keys %+v %v", o2.keys, err)
	}
	// A week later it's gone.
	o.keys.PreviousUntil = time.Now().Add(-time.Minute).Unix()
	o.writeAuth()
	if authFile(o, accessAuthOldFile) != "" {
		t.Fatal("the old key outlived its week")
	}

	// client_auth off: no authorized clients, no key in invites.
	off := false
	o.cfg.ClientAuth = &off
	o.writeAuth()
	if authFile(o, accessAuthFile) != "" {
		t.Fatal("client_auth off still lists a key")
	}
	if a := o.access(); a.Key != "" {
		t.Fatalf("a key without client auth: %+v", a)
	}
	if err := o.rotate(); err == nil {
		t.Fatal("rotated a key that isn't used")
	}
}

func TestTorOnlyAndGrapevine(t *testing.T) {
	srv := idleServer(t)
	srv.config.Host = "0.0.0.0"
	for _, c := range []struct {
		tor       TorConfig
		host      string
		grapevine bool
	}{
		{TorConfig{}, "0.0.0.0", true},
		{TorConfig{Enabled: true}, "0.0.0.0", false},
		{TorConfig{Enabled: true, Grapevine: true}, "0.0.0.0", true},
		{TorConfig{Enabled: true, Only: true, Grapevine: true}, "127.0.0.1", false},
	} {
		srv.config.Tor = c.tor
		if h := srv.listenHost(); h != c.host {
			t.Errorf("%+v listens on %s", c.tor, h)
		}
		if g := srv.grapevineAllowed(); g != c.grapevine {
			t.Errorf("%+v grapevine %v", c.tor, g)
		}
	}
}

func TestOnionInviteEmail(t *testing.T) {
	onion := testOnionHost + ":8090/ABCD2345#KEY"
	both := inviteEmail("Vineyard", "jordan", "chat.example.com:8090", onion, "ABCD2345", "", time.Time{})
	if !strings.Contains(both.Text, "chat.example.com:8090/ABCD2345") || !strings.Contains(both.Text, "through Tor instead, paste this:\n\n    "+onion) {
		t.Errorf("both:\n%s", both.Text)
	}
	only := inviteEmail("Vineyard", "jordan", "", onion, "ABCD2345", "", time.Time{})
	if !strings.Contains(only.Text, "paste:\n\n    "+onion) || strings.Contains(only.Text, "instead") {
		t.Errorf("tor only:\n%s", only.Text)
	}
	if !strings.Contains(only.HTML, testOnionHost) {
		t.Error("the HTML doesn't carry the onion invite")
	}
}

// Members get the address and key with SERVER_CREATE, invites carry them,
// and a new key reaches everyone online.
func TestOnionAccessOverTheWire(t *testing.T) {
	srv, wsURL, _, tokens, ch, _ := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions|models.PermissionCreateInvite)
	o := fakeOnion(t, srv, TorConfig{})
	os.WriteFile(filepath.Join(o.hsDir(), "hostname"), []byte(testOnionHost+"\n"), 0o600)
	sid := ch.ServerID

	owner, alice := newTestWSClient(t, wsURL), newTestWSClient(t, wsURL)
	alice.send(protocol.OpIdentify, protocol.IdentifyPayload{Token: tokens[1]})
	m := alice.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventServerCreate })
	if m == nil {
		t.Fatal("no SERVER_CREATE")
	}
	var sc protocol.ServerCreatePayload
	mustUnmarshal(t, m.Data, &sc)
	if sc.Onion == nil || sc.Onion.Address != testOnionHost+":8090" || sc.Onion.Key != o.keys.Current || sc.Onion.ServerID != sid.String() {
		t.Fatalf("SERVER_CREATE onion %+v", sc.Onion)
	}

	res, p := invitesAsk(t, alice, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesCreate, MaxAge: 3600})
	if !res.OK || p.Onion != testOnionHost+":8090" || p.OnionKey != o.keys.Current {
		t.Fatalf("invites payload %+v", p)
	}

	owner.identify(tokens[0])
	old := o.keys.Current
	owner.send(protocol.OpServerAdmin, protocol.ServerAdminRequest{ServerID: sid, Action: protocol.ServerAdminTorKey})
	r := owner.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventServerAdminResult })
	var ar protocol.ServerAdminResult
	mustUnmarshal(t, r.Data, &ar)
	if !ar.OK {
		t.Fatalf("rotate: %+v", ar)
	}
	m = alice.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventOnionAccess })
	if m == nil {
		t.Fatal("alice wasn't given the new key")
	}
	var na protocol.OnionAccess
	mustUnmarshal(t, m.Data, &na)
	if na.Key == old || na.Key != o.keys.Current || na.ServerID != sid.String() {
		t.Fatalf("new access %+v", na)
	}
}
