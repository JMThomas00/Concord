package client

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/server"
)

func TestSplitInvite(t *testing.T) {
	for in, want := range map[string][2]string{
		"vineyard.example:8090/K7Q2M8PX":    {"vineyard.example:8090", "K7Q2M8PX"},
		" chat.example.com:8090/abcd-efgh ": {"chat.example.com:8090", "ABCDEFGH"},
		"concord://host:1/invite/ABCDEFGH":  {"host:1", "ABCDEFGH"},
		"192.168.1.66":                      {"192.168.1.66", ""},
		"host:8090/ABC":                     {"host:8090/ABC", ""},
	} {
		addr, code := splitInvite(in)
		if addr != want[0] || code != want[1] {
			t.Errorf("splitInvite(%q) = %q, %q; want %q, %q", in, addr, code, want[0], want[1])
		}
	}
}

// Pasting an invite into Add Server's address fills the address, port,
// name and the code.
func TestAddServerTakesAnInvite(t *testing.T) {
	a := newEmojiTestApp(t)
	a.initAddServerForm()
	a.addServerFocus = 1
	a.addServerAddress.SetValue("vineyard.example:8090/k7q2-m8px")
	a.takePastedInvite()
	if a.addServerAddress.Value() != "vineyard.example" || a.addServerPort.Value() != "8090" || a.addServerInvite != "K7Q2M8PX" || a.addServerName.Value() != "vineyard.example" {
		t.Fatalf("address %q port %q invite %q name %q", a.addServerAddress.Value(), a.addServerPort.Value(), a.addServerInvite, a.addServerName.Value())
	}
	if !strings.Contains(ansi.Strip(a.renderAddServerView()), "K7Q2-M8PX") {
		t.Error("the form doesn't show the invite")
	}
}

// startInviteServer is a real server, plus its database opened alongside
// to set the join mode and make invites.
func startInviteServer(t *testing.T) (addr string, db *database.DB) {
	t.Helper()
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	config := server.DefaultConfig()
	config.Host, config.Port, config.ServerName = "127.0.0.1", port, "Vineyard"
	config.DatabasePath = filepath.Join(dir, "concord.db")
	config.PluginsDir = filepath.Join(dir, "Plugins")
	config.MessagePruning.Enabled = false
	srv, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Run() }()
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
			c.Close()
			db, err := database.New(config.DatabasePath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			return fmt.Sprintf("http://127.0.0.1:%d", port), db
		}
	}
	t.Fatal("server didn't start")
	return "", nil
}

func TestSignInWithAnInvite(t *testing.T) {
	addr, db := startInviteServer(t)
	cm, id := managerFor(t, addr)
	owner := &LocalIdentity{ID: "p0", Alias: "owner", Email: "owner@example.com", Password: "password123"}
	if r := cm.AutoConnectHTTP(id, owner, nil); r.Err != nil {
		t.Fatalf("owner: %+v", r)
	}
	srv, _, err := db.EnsureDefaultServer("Vineyard")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetJoinMode(srv.ID, database.JoinInvite); err != nil {
		t.Fatal(err)
	}
	ownerUser, _, _ := db.FindUserByEmail("owner@example.com")

	// No code: the server says so, and no account is made.
	cm2, id2 := managerFor(t, addr)
	pal := &LocalIdentity{ID: "p1", Alias: "pal", Email: "pal@example.com", Password: "password123"}
	r := cm2.AutoConnectHTTP(id2, pal, nil)
	if r.Join != joinInviteRequired || r.Token != "" {
		t.Fatalf("without a code: %+v", r)
	}

	// The code pasted into Add Server (kept on the server's info) is sent.
	code := "PALCODE2"
	if err := db.CreateInvite(&database.Invite{Code: code, ServerID: srv.ID, InviterID: ownerUser.ID, MaxUses: 1}); err != nil {
		t.Fatal(err)
	}
	cm2.GetConnection(id2).ServerInfo.Invite = code
	r = cm2.AutoConnectHTTP(id2, pal, nil)
	if r.Err != nil || r.Token == "" || !r.UsedInvite {
		t.Fatalf("with the code: %+v", r)
	}

	// Approval: a code typed on the join screen (SetJoin) with a note.
	db.SetJoinMode(srv.ID, database.JoinApproval)
	cm3, id3 := managerFor(t, addr)
	kim := &LocalIdentity{ID: "p2", Alias: "kim", Email: "kim@example.com", Password: "password123"}
	cm3.SetJoin(id3, JoinDetails{Note: "hi, I'm pal's friend"})
	if r := cm3.AutoConnectHTTP(id3, kim, nil); r.Join != joinApprovalPending {
		t.Fatalf("approval: %+v", r)
	}
	reqs, _ := db.ListJoinRequests(srv.ID)
	if len(reqs) != 1 || reqs[0].Note != "hi, I'm pal's friend" {
		t.Fatalf("requests %+v", reqs)
	}
	everyone, _ := db.GetEveryoneRole(srv.ID)
	db.AddServerMember(&models.ServerMember{UserID: reqs[0].UserID, ServerID: srv.ID, JoinedAt: time.Now()})
	db.AddMemberRole(reqs[0].UserID, srv.ID, everyone.ID)
	db.DeleteJoinRequest(srv.ID, reqs[0].UserID)
	if r := cm3.AutoConnectHTTP(id3, kim, &SavedCredentials{ProfileID: "p2", Email: "kim@example.com"}); r.Err != nil || r.Token == "" {
		t.Fatalf("after approval: %+v", r)
	}
}

// The join screen: opened by a refusal, sends what's typed, closes on
// joining.
func TestJoinScreen(t *testing.T) {
	a := newEmojiTestApp(t)
	id := uuid.New()
	a.clientServers = []*ClientServerInfo{{ID: id, Name: "Vineyard"}}
	a.configMgr.AddServer(a.clientServers[0])
	a.localIdentity = &LocalIdentity{ID: "p", Alias: "pal", Email: "pal@example.com", Password: "password123"}
	a.connMgr = NewConnectionManager(make(chan tea.Msg, 16))
	a.connMgr.AddServer(a.clientServers[0])

	a.joinBlocked(AutoConnectMsg{ServerID: id, Join: joinInviteRequired, Err: &APIError{Code: joinInviteRequired, Message: "Vineyard is invite only."}})
	if a.view != ViewAccountCode || a.codeState == nil || a.codeState.Mode != codeModeJoin {
		t.Fatalf("view %v state %+v", a.view, a.codeState)
	}
	if out := ansi.Strip(a.renderAccountCodeView()); !strings.Contains(out, "Vineyard is invite only") {
		t.Errorf("screen:\n%s", out)
	}
	for _, r := range "abcd" {
		a.handleAccountCodeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	a.handleAccountCodeKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !a.codeState.IsErr || a.codeState.Busy {
		t.Error("a 4-character code was sent")
	}
	a.codeState.Code.SetValue("host:1/ABCD-EFGH")
	if cmd := a.handleAccountCodeKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || !a.codeState.Busy {
		t.Fatal("the code wasn't sent")
	}
	if j := a.connMgr.GetConnection(id).joinDetails(); j.Invite != "ABCDEFGH" {
		t.Errorf("join details %+v", j)
	}

	// Waiting for a moderator: checked again later; joining closes it.
	cmd := a.joinBlocked(AutoConnectMsg{ServerID: id, Join: joinApprovalPending, Err: &APIError{Message: "waiting"}})
	if cmd == nil || a.codeState.Busy {
		t.Fatal("no check scheduled")
	}
	if out := ansi.Strip(a.renderAccountCodeView()); !strings.Contains(out, "waiting for a moderator") {
		t.Errorf("waiting screen:\n%s", out)
	}
	a.joinedServer(id)
	if a.codeState != nil || a.view != ViewMain || a.pendingJoin[id] != nil {
		t.Errorf("after joining: view %v state %+v", a.view, a.codeState)
	}
}

func TestInvitePeopleView(t *testing.T) {
	a := newEmojiTestApp(t)
	a.width, a.height = 120, 40
	sc := &ServerConnection{ServerID: uuid.New(), ServerInfo: &ClientServerInfo{Name: "Vineyard", Address: "vineyard.example", Port: 8090}, ServerVersion: "0.1.9"}
	a.activeConn = sc
	a.openInvitePeople()
	v := a.invitePeople
	if v == nil {
		t.Fatal("not open")
	}
	ex := time.Now().Add(26*time.Hour + time.Minute)
	a.handleInvitesEvent(sc, mustMessage(t, protocol.EventInvites, protocol.InvitesPayload{
		CanInvite: true, CanManage: true, MailEnabled: true, JoinMode: protocol.JoinApproval,
		Invites:  []protocol.InviteInfo{{Code: "ABCDEFGH", InviterName: "owner", MaxUses: 5, Uses: 2, ExpiresAt: ex, Usable: true}},
		Requests: []protocol.JoinRequestInfo{{UserID: uuid.New(), Username: "kim", Note: "hello"}},
	}))
	if got := v.tabs(); len(got) != 4 {
		t.Fatalf("tabs %v", got)
	}
	out := ansi.Strip(a.renderInvitePeople())
	for _, want := range []string{"Invite people to Vineyard", "Make a code", "Email an invite", "All invites", "Requests (1)", "7 days"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	// A new code is shown as the line to share.
	a.handleInvitesEvent(sc, mustMessage(t, protocol.EventInvitesResult, protocol.InvitesResult{Action: protocol.InvitesCreate, OK: true, Code: "NEWCODE2"}))
	if out := ansi.Strip(a.renderInvitePeople()); !strings.Contains(out, "vineyard.example:8090/NEWCODE2") {
		t.Errorf("no invite line:\n%s", out)
	}
	// The list: uses and time left; X twice cancels.
	a.handleInvitePeopleKey(tea.KeyMsg{Type: tea.KeyTab})
	a.handleInvitePeopleKey(tea.KeyMsg{Type: tea.KeyTab})
	if v.tab != inviteTabList {
		t.Fatalf("tab %d", v.tab)
	}
	if out := ansi.Strip(a.renderInvitePeople()); !strings.Contains(out, "ABCD-EFGH") || !strings.Contains(out, "2/5 used") || !strings.Contains(out, "26h left") {
		t.Errorf("list:\n%s", out)
	}
	a.handleInvitePeopleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if v.confirm != "ABCDEFGH" {
		t.Error("X didn't ask first")
	}
	// Requests.
	a.handleInvitePeopleKey(tea.KeyMsg{Type: tea.KeyTab})
	if out := ansi.Strip(a.renderInvitePeople()); !strings.Contains(out, "kim") || !strings.Contains(out, "hello") {
		t.Errorf("requests:\n%s", out)
	}
	a.handleInvitePeopleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if a.invitePeople != nil {
		t.Error("Esc didn't close")
	}

	// Typing in the overlay never reaches the message box.
	a.openInvitePeople()
	a.input.SetValue("")
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if a.input.Value() != "" {
		t.Errorf("the message box got %q", a.input.Value())
	}
}

func mustMessage(t *testing.T, ev protocol.EventType, payload any) *protocol.Message {
	t.Helper()
	m, err := protocol.NewDispatch(ev, 0, payload)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestServerInvitesPage(t *testing.T) {
	a := newEmojiTestApp(t)
	sc := &ServerConnection{ServerID: uuid.New(), ServerInfo: &ClientServerInfo{Name: "Vineyard", Address: "v.example", Port: 8090}, ServerVersion: "dev"}
	a.activeConn = sc
	a.serverManagementState = &ServerManagementState{SelectedCategory: 5, FocusOnForm: true}
	a.openServerInvites()
	st := a.serverManagementState.Invites
	a.handleInvitesEvent(sc, mustMessage(t, protocol.EventInvites, protocol.InvitesPayload{
		CanInvite: true, CanManage: true, CanSettings: true, JoinMode: protocol.JoinInvite,
		MailEnabled: true, MailFrom: "concord.admin@example.com", RequireVerification: true, CanSaveSettings: true,
	}))
	if joinModes[st.pick].mode != protocol.JoinInvite {
		t.Errorf("the chooser starts at %q", joinModes[st.pick].mode)
	}
	out := ansi.Strip(a.renderServerInvitesContent(100, 40))
	for _, want := range []string{"How people join", "●Invite only", "Verify emails", "on", "sending from concord.admin@example.com", "All invites"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	// Turning verification off asks first.
	st.row = srvInvRowVerify
	a.handleServerInvitesKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !st.confirm {
		t.Error("turning verification off didn't ask")
	}
	// ↓ past the settings goes into the list, ↑ from its top comes back.
	st.row = srvInvRowTestMail
	a.handleServerInvitesKey(tea.KeyMsg{Type: tea.KeyDown})
	a.handleServerInvitesKey(tea.KeyMsg{Type: tea.KeyUp})
	if st.row != srvInvRowTestMail {
		t.Errorf("row %d", st.row)
	}
}
