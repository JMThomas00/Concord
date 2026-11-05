package server

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func TestInviteCodeFrom(t *testing.T) {
	for in, want := range map[string]string{
		"abcd-efgh":                        "ABCDEFGH",
		" vineyard.example:8090/K7Q2M8PX ": "K7Q2M8PX",
		"concord://host:1/invite/AB CD":    "ABCD",
		"":                                 "",
	} {
		if got := inviteCodeFrom(in); got != want {
			t.Errorf("inviteCodeFrom(%q) = %q, want %q", in, got, want)
		}
	}
	code, err := newInviteCode()
	if err != nil || len(code) != inviteCodeLength || strings.ContainsAny(code, "01OI") {
		t.Errorf("newInviteCode = %q, %v", code, err)
	}
}

// joinTest is a server answering HTTP, with its first account (the owner
// in effect) registered.
func joinTest(t *testing.T, mailOn bool) (*Server, *fakeMailer, func(path string, body map[string]string) (int, map[string]interface{}), *models.Server, *models.User) {
	t.Helper()
	s, mailer, ts := accountServer(t, mailOn)
	post := func(path string, body map[string]string) (int, map[string]interface{}) {
		return call(t, ts, path, "", body)
	}
	if code, resp := post("/api/register", map[string]string{"username": "owner", "email": "owner@example.com", "password": "password123"}); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("first registration: %d %v", code, resp)
	}
	srv, _, err := s.db.EnsureDefaultServer(s.config.ServerName)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := s.db.FindUserByEmail("owner@example.com")
	return s, mailer, post, srv, owner
}

func makeInvite(t *testing.T, s *Server, srv *models.Server, by uuid.UUID, maxUses int, expires time.Time, email string) string {
	t.Helper()
	code, _ := newInviteCode()
	if err := s.db.CreateInvite(&database.Invite{Code: code, ServerID: srv.ID, InviterID: by, MaxUses: maxUses, ExpiresAt: expires, Email: email}); err != nil {
		t.Fatal(err)
	}
	return code
}

func isMember(s *Server, srv *models.Server, email string) bool {
	u, _, err := s.db.FindUserByEmail(email)
	if err != nil {
		return false
	}
	_, err = s.db.GetServerMember(srv.ID, u.ID)
	return err == nil
}

func reg(name, invite string) map[string]string {
	return map[string]string{"username": name, "email": name + "@example.com", "password": "password123", "invite": invite}
}

func TestInviteOnlyRegistration(t *testing.T) {
	s, _, post, srv, owner := joinTest(t, false)
	if err := s.db.SetJoinMode(srv.ID, database.JoinInvite); err != nil {
		t.Fatal(err)
	}

	// No code: no account at all.
	if code, resp := post("/api/register", reg("bob", "")); code != http.StatusForbidden || resp["code"] != errCodeInviteRequired {
		t.Fatalf("register without a code: %d %v", code, resp)
	}
	if _, _, err := s.db.FindUserByEmail("bob@example.com"); err == nil {
		t.Fatal("an account was made without a code")
	}
	if code, resp := post("/api/register", reg("bob", "NOPENOPE")); code != http.StatusForbidden || resp["code"] != errCodeInviteInvalid {
		t.Fatalf("a made-up code: %d %v", code, resp)
	}

	// A one-use code, pasted as address/CODE, lets one person in.
	one := makeInvite(t, s, srv, owner.ID, 1, time.Now().Add(time.Hour), "")
	if code, resp := post("/api/register", reg("bob", "vineyard.example:8090/"+strings.ToLower(one))); code != http.StatusOK || resp["token"] == nil {
		t.Fatalf("register with a code: %d %v", code, resp)
	}
	if !isMember(s, srv, "bob@example.com") {
		t.Fatal("bob isn't a member")
	}
	inviters, _ := s.db.MemberInviters(srv.ID)
	if bob, _, _ := s.db.FindUserByEmail("bob@example.com"); inviters[bob.ID] != owner.ID {
		t.Errorf("invited by %v, want the owner", inviters[bob.ID])
	}
	if code, resp := post("/api/register", reg("carol", one)); code != http.StatusForbidden || resp["code"] != errCodeInviteInvalid {
		t.Fatalf("a used-up code: %d %v", code, resp)
	}
	old := makeInvite(t, s, srv, owner.ID, 0, time.Now().Add(-time.Minute), "")
	if code, resp := post("/api/register", reg("carol", old)); code != http.StatusForbidden || !strings.Contains(resp["error"].(string), "expired") {
		t.Fatalf("an expired code: %d %v", code, resp)
	}

	// Members keep signing in; the admin email always gets in.
	if code, _ := post("/api/login", map[string]string{"email": "bob@example.com", "password": "password123"}); code != http.StatusOK {
		t.Fatalf("a member signing in = %d", code)
	}
	s.config.AdminEmail = "boss@example.com"
	if code, resp := post("/api/register", reg("boss", "")); code != http.StatusOK {
		t.Fatalf("the admin email registering: %d %v", code, resp)
	}

	// An account that isn't a member (made while the server was open, or
	// let go) needs a code to sign in.
	if err := s.db.SetJoinMode(srv.ID, database.JoinOpen); err != nil {
		t.Fatal(err)
	}
	post("/api/register", reg("dan", ""))
	dan, _, _ := s.db.FindUserByEmail("dan@example.com")
	if _, err := s.db.Exec(`DELETE FROM server_members WHERE user_id = ?`, dan.ID.String()); err != nil {
		t.Fatal(err)
	}
	s.db.SetJoinMode(srv.ID, database.JoinInvite)
	if code, resp := post("/api/login", map[string]string{"email": "dan@example.com", "password": "password123"}); code != http.StatusForbidden || resp["code"] != errCodeInviteRequired {
		t.Fatalf("a non-member signing in: %d %v", code, resp)
	}
	many := makeInvite(t, s, srv, owner.ID, 0, time.Time{}, "")
	if code, _ := post("/api/login", map[string]string{"email": "dan@example.com", "password": "password123", "invite": many}); code != http.StatusOK || !isMember(s, srv, "dan@example.com") {
		t.Fatalf("signing in with a code = %d", code)
	}

	// Bans still apply.
	s.db.AddBan(srv.ID, dan.ID, owner.ID, "")
	if code, resp := post("/api/login", map[string]string{"email": "dan@example.com", "password": "password123", "invite": many}); code != http.StatusForbidden || resp["code"] != errCodeBanned {
		t.Fatalf("banned: %d %v", code, resp)
	}
}

func TestApprovalMode(t *testing.T) {
	s, _, post, srv, owner := joinTest(t, false)
	s.db.SetJoinMode(srv.ID, database.JoinApproval)

	// Anyone may ask; the account waits.
	body := reg("erin", "")
	body["note"] = "a friend of the owner"
	if code, resp := post("/api/register", body); code != http.StatusForbidden || resp["code"] != errCodeApprovalPending {
		t.Fatalf("register: %d %v", code, resp)
	}
	login := map[string]string{"email": "erin@example.com", "password": "password123"}
	if code, resp := post("/api/login", login); code != http.StatusForbidden || resp["code"] != errCodeApprovalPending {
		t.Fatalf("login while waiting: %d %v", code, resp)
	}
	reqs, _ := s.db.ListJoinRequests(srv.ID)
	if len(reqs) != 1 || reqs[0].Note != "a friend of the owner" {
		t.Fatalf("requests %+v", reqs)
	}

	// Turned down: they stay out, and no new request appears.
	s.db.DenyJoinRequest(srv.ID, reqs[0].UserID)
	if code, resp := post("/api/login", login); code != http.StatusForbidden || resp["code"] != errCodeJoinDenied {
		t.Fatalf("login after being turned down: %d %v", code, resp)
	}
	if reqs, _ := s.db.ListJoinRequests(srv.ID); len(reqs) != 0 {
		t.Fatalf("a denied request is still listed: %+v", reqs)
	}

	// A code skips the wait (even after a no).
	login["invite"] = makeInvite(t, s, srv, owner.ID, 0, time.Time{}, "")
	if code, _ := post("/api/login", login); code != http.StatusOK || !isMember(s, srv, "erin@example.com") {
		t.Fatalf("login with a code = %d", code)
	}
	if r, _ := s.db.GetJoinRequest(srv.ID, reqs[0].UserID); r != nil {
		t.Error("the request is left over after joining")
	}
	if code, resp := post("/api/register", reg("finn", login["invite"])); code != http.StatusOK || resp["token"] == nil {
		t.Fatalf("register with a code in approval mode: %d %v", code, resp)
	}
}

// An emailed invite works only for its address, and proves it: no
// separate verification code.
func TestEmailedInviteVerifies(t *testing.T) {
	s, mailer, post, srv, owner := joinTest(t, true)
	if code, resp := post("/api/account/verify", map[string]string{"email": "owner@example.com", "code": mailer.lastCode(t, "owner@example.com")}); code != http.StatusOK {
		t.Fatalf("owner verify: %d %v", code, resp)
	}
	s.db.SetJoinMode(srv.ID, database.JoinInvite)
	code := makeInvite(t, s, srv, owner.ID, 1, time.Now().Add(time.Hour), "gail@example.com")

	if c, resp := post("/api/register", reg("hank", code)); c != http.StatusForbidden || !strings.Contains(resp["error"].(string), "different email") {
		t.Fatalf("someone else's emailed code: %d %v", c, resp)
	}
	sent := mailer.count()
	c, resp := post("/api/register", reg("gail", code))
	if c != http.StatusOK || resp["token"] == nil {
		t.Fatalf("register with an emailed code: %d %v", c, resp)
	}
	if mailer.count() != sent {
		t.Error("a verification email was sent anyway")
	}
	if !isMember(s, srv, "gail@example.com") {
		t.Fatal("not a member")
	}

	// Verification still applies to everyone else (a plain code doesn't prove an address).
	plain := makeInvite(t, s, srv, owner.ID, 0, time.Time{}, "")
	if c, resp := post("/api/register", reg("ivy", plain)); c != http.StatusAccepted || resp["verification_required"] != true {
		t.Fatalf("register with a plain code: %d %v", c, resp)
	}
	if !isMember(s, srv, "ivy@example.com") {
		t.Fatal("ivy's code wasn't used")
	}
	if c, resp := post("/api/account/verify", map[string]string{"email": "ivy@example.com", "code": mailer.lastCode(t, "ivy@example.com")}); c != http.StatusOK || resp["token"] == nil {
		t.Fatalf("verify: %d %v", c, resp)
	}
}

// --- OpInvites over the wire ---

func invitesAsk(t *testing.T, c *testWSClient, req protocol.InvitesRequest) (protocol.InvitesResult, *protocol.InvitesPayload) {
	t.Helper()
	c.send(protocol.OpInvites, req)
	var res protocol.InvitesResult
	if req.Action != protocol.InvitesList {
		m := c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventInvitesResult })
		if m == nil {
			t.Fatalf("no INVITES_RESULT for %s", req.Action)
		}
		mustUnmarshal(t, m.Data, &res)
		if !res.OK {
			return res, nil
		}
	}
	m := c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventInvites })
	if m == nil {
		t.Fatalf("no INVITES after %s", req.Action)
	}
	var p protocol.InvitesPayload
	mustUnmarshal(t, m.Data, &p)
	return res, &p
}

func TestInvitesOverTheWire(t *testing.T) {
	srv, wsURL, users, tokens, ch, _ := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions|models.PermissionCreateInvite)
	sid := ch.ServerID
	owner, alice, bob := newTestWSClient(t, wsURL), newTestWSClient(t, wsURL), newTestWSClient(t, wsURL)
	owner.identify(tokens[0])
	alice.identify(tokens[1])
	bob.identify(tokens[2])

	// Members with Create Invite make codes and see only their own.
	res, p := invitesAsk(t, alice, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesCreate, MaxAge: 3600, MaxUses: 5})
	if !res.OK || len(res.Code) != inviteCodeLength || p == nil || len(p.Invites) != 1 || p.CanManage || p.CanSettings || !p.CanInvite {
		t.Fatalf("alice creates: %+v %+v", res, p)
	}
	if in := p.Invites[0]; in.MaxUses != 5 || in.ExpiresAt.Sub(in.CreatedAt) != time.Hour || in.InviterName != users[1].Username || !in.Usable {
		t.Errorf("invite %+v", in)
	}
	aliceCode := res.Code
	if _, p := invitesAsk(t, bob, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesList}); len(p.Invites) != 0 {
		t.Errorf("bob sees others' invites: %+v", p.Invites)
	}
	if res, _ := invitesAsk(t, bob, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesRevoke, Code: aliceCode}); res.OK {
		t.Error("bob cancelled alice's invite")
	}
	if res, _ := invitesAsk(t, bob, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesSettings, JoinMode: ptr(protocol.JoinInvite)}); res.OK {
		t.Error("bob changed the join mode")
	}

	// The owner sees and manages everything.
	_, p = invitesAsk(t, owner, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesList})
	if !p.CanManage || !p.CanSettings || len(p.Invites) != 1 || p.JoinMode != protocol.JoinOpen {
		t.Fatalf("owner list: %+v", p)
	}
	if res, p := invitesAsk(t, owner, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesSettings, JoinMode: ptr(protocol.JoinApproval)}); !res.OK || p.JoinMode != protocol.JoinApproval {
		t.Fatalf("set approval: %+v %+v", res, p)
	}
	if res, _ := invitesAsk(t, owner, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesSettings, RequireVerification: ptr(true)}); res.OK || !strings.Contains(res.Message, "isn't set up") {
		t.Errorf("verification without mail: %+v", res)
	}
	if res, _ := invitesAsk(t, owner, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesEmail, Email: "x@example.com"}); res.OK {
		t.Error("an email invite was made without mail")
	}

	// Join requests: listed for moderators, who are told when one comes in.
	newcomer, _ := createTestUserAndToken(t, srv, "newcomer-"+uuid.NewString()[:6])
	srv.db.AddJoinRequest(database.JoinRequest{UserID: newcomer.ID, ServerID: sid, Note: "hello"})
	srv.handlers.notifyJoinRequest(sid, newcomer.Username)
	if m := owner.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventJoinRequest }); m == nil {
		t.Error("the owner wasn't told about the request")
	}
	// bob isn't: anything sent to him arrives before the answer to this.
	bobTold := false
	bob.send(protocol.OpInvites, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesList})
	bob.readUntil(5*time.Second, func(m *protocol.Message) bool {
		bobTold = bobTold || m.Type == protocol.EventJoinRequest
		return m.Type == protocol.EventInvites
	})
	if bobTold {
		t.Error("bob was told about the request")
	}
	_, p = invitesAsk(t, owner, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesList})
	if len(p.Requests) != 1 || p.Requests[0].Note != "hello" || p.Requests[0].Username != newcomer.Username {
		t.Fatalf("requests %+v", p.Requests)
	}
	if res, _ := invitesAsk(t, bob, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesApprove, UserID: newcomer.ID}); res.OK {
		t.Error("bob approved a request")
	}
	if res, p := invitesAsk(t, owner, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesApprove, UserID: newcomer.ID}); !res.OK || len(p.Requests) != 0 {
		t.Fatalf("approve: %+v %+v", res, p)
	}
	if _, err := srv.db.GetServerMember(sid, newcomer.ID); err != nil {
		t.Error("approved, but not a member")
	}

	// Revoking: the owner may cancel anyone's.
	if res, p := invitesAsk(t, owner, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesRevoke, Code: aliceCode}); !res.OK || len(p.Invites) != 0 {
		t.Fatalf("revoke: %+v %+v", res, p)
	}

	// With mail: an emailed invite, sent once per address, carrying the
	// pasteable address/CODE.
	mailer := &fakeMailer{}
	srv.config.Mail = MailConfig{SMTPHost: "smtp.example.com", From: "Concord <chat@example.com>"}
	srv.mailer = mailer
	res, p = invitesAsk(t, alice, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesEmail, Email: "Pal@Example.com", Note: "come play dominoes", Address: "vineyard.example:8090"})
	if !res.OK || len(p.Invites) != 1 || p.Invites[0].Email != "pal@example.com" || p.Invites[0].MaxUses != 1 {
		t.Fatalf("email invite: %+v %+v", res, p)
	}
	if mailer.count() != 1 || !strings.Contains(mailer.sent[0].body, "vineyard.example:8090/"+res.Code) || !strings.Contains(mailer.sent[0].body, "come play dominoes") || mailer.sent[0].to != "pal@example.com" {
		t.Errorf("mail %+v", mailer.sent)
	}
	if res, _ := invitesAsk(t, alice, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesEmail, Email: "pal@example.com"}); res.OK || !strings.Contains(res.Message, "already has an invite") {
		t.Errorf("a second invite to one address: %+v", res)
	}
	if res, _ := invitesAsk(t, alice, protocol.InvitesRequest{ServerID: sid, Action: protocol.InvitesEmail, Email: users[2].Email}); res.OK || !strings.Contains(res.Message, "already a member") {
		t.Errorf("inviting a member: %+v", res)
	}
}

func ptr[T any](v T) *T { return &v }

// The verification switch is saved to the settings file.
func TestVerificationSwitchIsSaved(t *testing.T) {
	s, _, _ := accountServer(t, true)
	path := t.TempDir() + "/concord-server.toml"
	s.SetConfigPath(path)
	if err := s.setVerificationRequired(false); err != nil {
		t.Fatal(err)
	}
	if s.verificationRequired() {
		t.Fatal("still on")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Mail.VerificationRequired() {
		t.Error("the file still says on")
	}
	s.config.Mail = MailConfig{}
	if err := s.setVerificationRequired(true); err == nil {
		t.Error("turned on without mail")
	}
}
