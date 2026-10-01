package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMailer records what the server would have emailed.
type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

type sentMail struct{ to, subject, body string }

func (f *fakeMailer) Send(to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMail{to, subject, body})
	return nil
}

var codeInMail = regexp.MustCompile(`(?m)^    ([A-Z0-9]{3}) ([A-Z0-9]{3})$`)

// lastCode waits for the latest email to address and returns its code.
func (f *fakeMailer) lastCode(t *testing.T, address string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for i := len(f.sent) - 1; i >= 0; i-- {
			if f.sent[i].to == address {
				m := codeInMail.FindStringSubmatch(f.sent[i].body)
				f.mu.Unlock()
				if m == nil {
					t.Fatalf("no code in email:\n%s", f.sent[i].body)
				}
				return m[1] + m[2]
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("nothing was emailed to %s", address)
	return ""
}

func (f *fakeMailer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// accountServer is a test server with mail; verification as given.
func accountServer(t *testing.T, mailOn bool) (*Server, *fakeMailer, *httptest.Server) {
	t.Helper()
	s, cleanup := createTestServer(t)
	t.Cleanup(cleanup)
	mailer := &fakeMailer{}
	if mailOn {
		s.config.Mail = MailConfig{SMTPHost: "smtp.example.com", From: "Concord <chat@example.com>"}
		s.mailer = mailer
	}
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, mailer, ts
}

// call POSTs body to path (with a bearer token if given) and decodes the
// JSON answer.
func call(t *testing.T, ts *httptest.Server, path, token string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestWithoutMailNothingChanges(t *testing.T) {
	_, _, ts := accountServer(t, false)
	code, resp := call(t, ts, "/api/register", "", map[string]string{"username": "amy", "email": "Amy@Example.com", "password": "password123"})
	if code != http.StatusOK || resp["token"] == nil {
		t.Fatalf("register without mail: %d %v", code, resp)
	}
	if code, _ := call(t, ts, "/api/login", "", map[string]string{"email": "amy@example.com", "password": "password123"}); code != http.StatusOK {
		t.Fatalf("login (email in another case) = %d", code)
	}
	code, resp = call(t, ts, "/api/password/forgot", "", map[string]string{"email": "amy@example.com"})
	if code != http.StatusNotImplemented || resp["code"] != errCodeMailUnavailable {
		t.Fatalf("forgot without mail: %d %v", code, resp)
	}
}

func TestNewAccountsVerifyBeforeSigningIn(t *testing.T) {
	s, mailer, ts := accountServer(t, true)
	s.config.AdminEmail = "boss@example.com"

	// The first account becomes the owner regardless; register a second.
	call(t, ts, "/api/register", "", map[string]string{"username": "owner", "email": "owner@example.com", "password": "password123"})
	code, resp := call(t, ts, "/api/register", "", map[string]string{"username": "bos", "email": "Boss@Example.com", "password": "password123"})
	if code != http.StatusAccepted || resp["verification_required"] != true || resp["token"] != nil {
		t.Fatalf("register: %d %v", code, resp)
	}
	if mailer.lastCode(t, "boss@example.com") == "" {
		t.Fatal("no code")
	}
	code, resp = call(t, ts, "/api/login", "", map[string]string{"email": "boss@example.com", "password": "password123"})
	if code != http.StatusForbidden || resp["code"] != errCodeVerificationRequired {
		t.Fatalf("login before verifying: %d %v", code, resp)
	}
	if isAdmin(t, s, "boss@example.com") {
		t.Fatal("admin-by-email granted before the address was proven")
	}

	// Typo in the username: fixed before verifying; a fresh code replaces the old.
	first := mailer.lastCode(t, "boss@example.com")
	code, resp = call(t, ts, "/api/account/fix", "", map[string]string{"email": "boss@example.com", "password": "password123", "new_username": "boss"})
	if code != http.StatusOK {
		t.Fatalf("fix: %d %v", code, resp)
	}
	second := mailer.lastCode(t, "boss@example.com")
	if code, _ := call(t, ts, "/api/account/verify", "", map[string]string{"email": "boss@example.com", "code": first}); code == http.StatusOK && first != second {
		t.Fatal("the replaced code still worked")
	}

	code, resp = call(t, ts, "/api/account/verify", "", map[string]string{"email": "boss@example.com", "code": strings.ToLower(second[:3]) + " " + second[3:]})
	if code != http.StatusOK || resp["token"] == nil {
		t.Fatalf("verify: %d %v", code, resp)
	}
	if user := resp["user"].(map[string]interface{}); user["username"] != "boss" {
		t.Fatalf("username not fixed: %v", user)
	}
	if !isAdmin(t, s, "boss@example.com") {
		t.Fatal("admin-by-email not granted after verifying")
	}
	if code, _ := call(t, ts, "/api/login", "", map[string]string{"email": "boss@example.com", "password": "password123"}); code != http.StatusOK {
		t.Fatalf("login after verifying = %d", code)
	}

	// Existing accounts and accounts made before mail was set up are fine.
	off := false
	s.config.Mail.RequireVerification = &off
	code, _ = call(t, ts, "/api/register", "", map[string]string{"username": "casual", "email": "casual@example.com", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register with verification turned off = %d", code)
	}
}

func TestWrongCodesRunOut(t *testing.T) {
	_, mailer, ts := accountServer(t, true)
	call(t, ts, "/api/register", "", map[string]string{"username": "dee", "email": "dee@example.com", "password": "password123"})
	good := mailer.lastCode(t, "dee@example.com")
	for i := 0; i < codeMaxGuesses; i++ {
		code, resp := call(t, ts, "/api/account/verify", "", map[string]string{"email": "dee@example.com", "code": "WRONG1"})
		if code != http.StatusBadRequest || resp["code"] != errCodeBadCode {
			t.Fatalf("guess %d: %d %v", i, code, resp)
		}
	}
	if code, _ := call(t, ts, "/api/account/verify", "", map[string]string{"email": "dee@example.com", "code": good}); code == http.StatusOK {
		t.Fatal("the right code worked after the guesses ran out")
	}
	// A resend right away is refused (cooldown), even with the password.
	code, resp := call(t, ts, "/api/account/resend", "", map[string]string{"email": "dee@example.com", "password": "password123"})
	if code == http.StatusOK || !strings.Contains(resp["error"].(string), "seconds") {
		t.Fatalf("resend inside the cooldown: %d %v", code, resp)
	}
}

func TestForgotPasswordResetsAndSignsOutEverywhere(t *testing.T) {
	_, mailer, ts := accountServer(t, true)
	// Create a verified account by registering then verifying.
	call(t, ts, "/api/register", "", map[string]string{"username": "eve", "email": "eve@example.com", "password": "password123"})
	_, resp := call(t, ts, "/api/account/verify", "", map[string]string{"email": "eve@example.com", "code": mailer.lastCode(t, "eve@example.com")})
	oldToken := resp["token"].(string)

	before := mailer.count()
	code, _ := call(t, ts, "/api/password/forgot", "", map[string]string{"email": "nobody@example.com"})
	if code != http.StatusOK {
		t.Fatalf("forgot for an unknown address should look the same: %d", code)
	}
	code, _ = call(t, ts, "/api/password/forgot", "", map[string]string{"email": "eve@example.com"})
	if code != http.StatusOK {
		t.Fatalf("forgot = %d", code)
	}
	var resetCode string
	deadline := time.Now().Add(2 * time.Second)
	for mailer.count() == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	resetCode = mailer.lastCode(t, "eve@example.com")
	if mailer.count() != before+1 {
		t.Fatalf("emails sent: %d, want exactly one (none for the unknown address)", mailer.count()-before)
	}

	code, resp = call(t, ts, "/api/password/reset", "", map[string]string{"email": "eve@example.com", "code": resetCode, "new_password": "newpassword9"})
	if code != http.StatusOK || resp["token"] == nil {
		t.Fatalf("reset: %d %v", code, resp)
	}
	if code, _ := call(t, ts, "/api/login", "", map[string]string{"email": "eve@example.com", "password": "password123"}); code == http.StatusOK {
		t.Fatal("old password still works")
	}
	if code, _ := call(t, ts, "/api/login", "", map[string]string{"email": "eve@example.com", "password": "newpassword9"}); code != http.StatusOK {
		t.Fatal("new password doesn't work")
	}
	if code, _ := call(t, ts, "/api/account/password", oldToken, map[string]string{"current_password": "newpassword9", "new_password": "x12345678"}); code != http.StatusUnauthorized {
		t.Fatalf("a session from before the reset still works: %d", code)
	}
}

func TestSignedInPasswordAndAccountChanges(t *testing.T) {
	_, mailer, ts := accountServer(t, true)
	call(t, ts, "/api/register", "", map[string]string{"username": "fay", "email": "fay@example.com", "password": "password123"})
	_, resp := call(t, ts, "/api/account/verify", "", map[string]string{"email": "fay@example.com", "code": mailer.lastCode(t, "fay@example.com")})
	tokenA := resp["token"].(string)
	_, resp = call(t, ts, "/api/login", "", map[string]string{"email": "fay@example.com", "password": "password123"})
	tokenB := resp["token"].(string)

	if code, _ := call(t, ts, "/api/account/password", tokenA, map[string]string{"current_password": "wrong-one", "new_password": "newpassword9"}); code != http.StatusUnauthorized {
		t.Fatalf("change with a wrong current password = %d", code)
	}
	if code, _ := call(t, ts, "/api/account/password", tokenA, map[string]string{"current_password": "password123", "new_password": "newpassword9"}); code != http.StatusOK {
		t.Fatalf("change password = %d", code)
	}
	// The other session is signed out; this one isn't.
	if code, _ := call(t, ts, "/api/account/update", tokenB, map[string]string{"current_password": "newpassword9", "username": "fae"}); code != http.StatusUnauthorized {
		t.Fatalf("other session after a password change = %d", code)
	}

	// Username changes at once; a new email waits for its code.
	code, resp := call(t, ts, "/api/account/update", tokenA, map[string]string{"current_password": "newpassword9", "username": "fae", "email": "fae@example.com"})
	if code != http.StatusOK || resp["verification_required"] != true {
		t.Fatalf("update: %d %v", code, resp)
	}
	if code, _ := call(t, ts, "/api/login", "", map[string]string{"email": "fae@example.com", "password": "newpassword9"}); code == http.StatusOK {
		t.Fatal("the new email worked before it was confirmed")
	}
	code, resp = call(t, ts, "/api/account/confirm-email", tokenA, map[string]string{"code": mailer.lastCode(t, "fae@example.com")})
	if code != http.StatusOK {
		t.Fatalf("confirm email: %d %v", code, resp)
	}
	code, resp = call(t, ts, "/api/login", "", map[string]string{"email": "fae@example.com", "password": "newpassword9"})
	if code != http.StatusOK || resp["user"].(map[string]interface{})["username"] != "fae" {
		t.Fatalf("login with the new email: %d %v", code, resp)
	}
}

func TestAccountRequestsAreRateLimited(t *testing.T) {
	_, _, ts := accountServer(t, true)
	limited := false
	for i := 0; i < 40 && !limited; i++ {
		code, _ := call(t, ts, "/api/account/verify", "", map[string]string{"email": "x@example.com", "code": "AAAAAA"})
		limited = code == http.StatusTooManyRequests
	}
	if !limited {
		t.Fatal("40 rapid attempts from one address were never limited")
	}
}

func isAdmin(t *testing.T, s *Server, email string) bool {
	t.Helper()
	user, _, err := s.db.FindUserByEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	srv, _, _ := s.db.EnsureDefaultServer(s.config.ServerName)
	admin, err := s.db.GetOrCreateAdminRole(srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.db.GetServerMember(srv.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range member.RoleIDs {
		if id == admin.ID {
			return true
		}
	}
	return false
}

func TestCodeEmailAndMessageFormat(t *testing.T) {
	subject, body := codeEmail("Café Server", "amy", purposeVerify, "ABC234")
	if !strings.Contains(subject, "ABC 234") || !codeInMail.MatchString(body) {
		t.Fatalf("subject %q body %q", subject, body)
	}
	msg := string(buildMessage(mustAddr("Concord <chat@example.com>"), mustAddr("amy@example.com"), subject, body))
	if !strings.Contains(msg, "Subject: =?utf-8?q?") || !strings.Contains(msg, "\r\n\r\nWelcome") || strings.Contains(strings.ReplaceAll(msg, "\r\n", ""), "\n") {
		t.Fatalf("message:\n%s", msg)
	}
}

func mustAddr(s string) *mail.Address {
	a, err := mail.ParseAddress(s)
	if err != nil {
		panic(err)
	}
	return a
}
