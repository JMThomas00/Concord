package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Account verification and recovery over HTTP (2026-09-30). When the
// server can send mail ([mail] in concord-server.toml), new accounts get a
// 6-character code by email and can't sign in until they enter it (unless
// the admin turned verification off), and a forgotten password is reset
// with a code too. A server without mail works as before.
//
// Routes (JSON in and out; errors are {"error": "...", "code": "..."}):
//
//	POST /api/account/verify          {email, code}                      → {user, token}
//	POST /api/account/resend          {email, password}                  → {sent}
//	POST /api/account/fix             {email, password, new_email?, new_username?}  (unverified only)
//	POST /api/password/forgot         {email}                            → {sent}
//	POST /api/password/reset          {email, code, new_password}        → {user, token}
//	POST /api/account/password        Bearer; {current_password, new_password}
//	POST /api/account/update          Bearer; {current_password, username?, email?}
//	POST /api/account/confirm-email   Bearer; {code}                     (after an email change)

// Error codes the client acts on.
const (
	errCodeVerificationRequired = "verification_required"
	errCodeMailUnavailable      = "mail_unavailable"
	errCodeBadCode              = "bad_code"
	errCodeRateLimited          = "rate_limited"
)

// Code purposes.
const (
	purposeVerify      = "verify"
	purposeReset       = "reset"
	purposeChangeEmail = "change_email"
)

const (
	codeLength     = 6
	codeAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O or 1/I
	codeTTL        = 15 * time.Minute
	codeMaxGuesses = 5
	resendCooldown = 60 * time.Second
	minPassword    = 8
)

// --- helpers ---

// writeJSON writes v with status.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeAPIError writes {"error": msg, "code": code}.
func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// remoteIP is the client's address without its port.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// normalizeEmail trims and lowercases an address and checks its shape.
func normalizeEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s[strings.Index(s, "@")+1:], ".") && !strings.HasSuffix(s, "@localhost") {
		return s, false
	}
	return s, true
}

// newCode returns a random 6-character code.
func newCode() (string, error) {
	b := make([]byte, codeLength)
	max := big.NewInt(int64(len(codeAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b), nil
}

// cleanCode normalizes what someone typed: case, spaces and dashes.
func cleanCode(s string) string {
	s = strings.ToUpper(s)
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, s)
}

func hashCode(userID uuid.UUID, purpose, code string) string {
	h := sha256.Sum256([]byte(userID.String() + ":" + purpose + ":" + cleanCode(code)))
	return hex.EncodeToString(h[:])
}

func hashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// accountLimiter is a per-IP token bucket for the account endpoints, so a
// code can't be guessed or a mailbox flooded from one address.
type accountLimiter struct {
	mu      sync.Mutex
	buckets map[string]*limitBucket
	burst   float64
	perSec  float64
}

type limitBucket struct {
	tokens float64
	last   time.Time
}

func newAccountLimiter(burst int, perMinute float64) *accountLimiter {
	return &accountLimiter{buckets: map[string]*limitBucket{}, burst: float64(burst), perSec: perMinute / 60}
}

func (l *accountLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 10000 { // forget idle addresses now and then
			for k, v := range l.buckets {
				if now.Sub(v.last) > 10*time.Minute {
					delete(l.buckets, k)
				}
			}
		}
		b = &limitBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.perSec
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// limited answers 429 and reports true when r's address is over the limit.
func (s *Server) limited(w http.ResponseWriter, r *http.Request) bool {
	if s.accountLimits.allow(remoteIP(r)) {
		return false
	}
	AuthLog.Warn("Account request rate-limited", "from", remoteIP(r), "path", r.URL.Path)
	writeAPIError(w, http.StatusTooManyRequests, errCodeRateLimited, "Too many attempts. Wait a minute and try again.")
	return true
}

// decode reads a JSON body into v, answering 400 on failure.
func decode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method", "Method not allowed")
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return false
	}
	return true
}

// --- codes ---

// sendCode makes a new code for (user, purpose), stores its hash and emails
// it to address. force skips the resend cooldown (a fresh registration).
func (s *Server) sendCode(user *models.User, purpose, address string, force bool) error {
	if !s.config.Mail.Enabled() || s.mailer == nil {
		return errors.New("this server can't send email")
	}
	if !force {
		if old, err := s.db.GetAccountCode(user.ID, purpose); err == nil && time.Since(old.SentAt) < resendCooldown {
			wait := resendCooldown - time.Since(old.SentAt)
			return fmt.Errorf("a code was just sent; try again in %d seconds", int(wait.Seconds())+1)
		}
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	now := time.Now()
	if err := s.db.SaveAccountCode(database.AccountCode{
		UserID: user.ID, Purpose: purpose, CodeHash: hashCode(user.ID, purpose, code),
		ExpiresAt: now.Add(codeTTL), SentAt: now,
	}); err != nil {
		return err
	}
	subject, body := codeEmail(s.config.ServerName, user.Username, purpose, code)
	if err := s.mailer.Send(address, subject, body); err != nil {
		AuthLog.Error("Failed to send account email", "user_id", user.ID, "purpose", purpose, "error", err)
		return errors.New("the server couldn't send the email; try again later or ask its admin")
	}
	AuthLog.Info("Account code emailed", "user_id", user.ID, "purpose", purpose)
	return nil
}

// codeEmail is the subject and text of a code email.
func codeEmail(serverName, username, purpose, code string) (string, string) {
	spaced := code[:3] + " " + code[3:]
	switch purpose {
	case purposeReset:
		return fmt.Sprintf("Your %s password reset code: %s", serverName, spaced),
			fmt.Sprintf("Hi %s,\n\nSomeone asked to reset your password on the Concord server %q.\n\n"+
				"Your code is:\n\n    %s\n\nEnter it in Concord within 15 minutes. If this wasn't you, ignore this email; your password hasn't changed.\n",
				username, serverName, spaced)
	case purposeChangeEmail:
		return fmt.Sprintf("Confirm your new email for %s: %s", serverName, spaced),
			fmt.Sprintf("Hi %s,\n\nTo use this address for your account on the Concord server %q, enter this code in Concord within 15 minutes:\n\n    %s\n\nIf this wasn't you, ignore this email.\n",
				username, serverName, spaced)
	default:
		return fmt.Sprintf("Your %s verification code: %s", serverName, spaced),
			fmt.Sprintf("Welcome to %s, %s!\n\nYour verification code is:\n\n    %s\n\nEnter it in Concord within 15 minutes to finish creating your account. If you didn't sign up, ignore this email.\n",
				serverName, username, spaced)
	}
}

// checkCode verifies a typed code, counting wrong guesses; on success the
// code is used up.
func (s *Server) checkCode(userID uuid.UUID, purpose, typed string) error {
	c, err := s.db.GetAccountCode(userID, purpose)
	if err != nil {
		return errors.New("no code is waiting; ask for a new one")
	}
	if time.Now().After(c.ExpiresAt) {
		return errors.New("that code has expired; ask for a new one")
	}
	if c.Attempts >= codeMaxGuesses {
		return errors.New("too many wrong codes; ask for a new one")
	}
	if hashCode(userID, purpose, typed) != c.CodeHash {
		_ = s.db.CountCodeAttempt(userID, purpose)
		left := codeMaxGuesses - c.Attempts - 1
		if left <= 0 {
			return errors.New("that code is wrong, and that was the last try; ask for a new one")
		}
		return fmt.Errorf("that code is wrong (%d tries left)", left)
	}
	_ = s.db.DeleteAccountCode(userID, purpose)
	return nil
}

// --- account activation ---

// accountActivated runs once an account can be used: right after
// registration on a server that doesn't verify, or after verifying. Admin
// rights for the configured admin email only ever go to an address that's
// been proven, when the server verifies.
func (s *Server) accountActivated(user *models.User) {
	if s.config.AdminEmail != "" && strings.EqualFold(user.Email, s.config.AdminEmail) {
		if err := s.db.EnsureAdminRole(user.Email); err != nil {
			AuthLog.Error("Failed to grant admin role to configured admin email", "email", user.Email, "error", err)
		} else {
			AuthLog.Info("Admin role granted to user matching configured admin email", "email", user.Email)
		}
	}
}

// issueSession answers {user, token} for a fresh session.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user *models.User) {
	token, err := s.handlers.CreateAuthToken(user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		AuthLog.Error("Failed to create auth token", "user_id", user.ID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"user": user, "token": token})
}

// userByPassword finds the account for email and checks its password.
func (s *Server) userByPassword(email, password string) (*models.User, bool) {
	user, hash, err := s.db.FindUserByEmail(email)
	if err != nil {
		return nil, false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, false
	}
	return user, true
}

// --- unauthenticated routes ---

// handleVerifyAccount confirms a new account's email with its code and
// signs it in.
func (s *Server) handleVerifyAccount(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Code string }
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	user, _, err := s.db.FindUserByEmail(req.Email)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, errCodeBadCode, "That code is wrong.")
		return
	}
	if verified, _ := s.db.IsEmailVerified(user.ID); verified {
		s.issueSession(w, r, user)
		return
	}
	if err := s.checkCode(user.ID, purposeVerify, req.Code); err != nil {
		writeAPIError(w, http.StatusBadRequest, errCodeBadCode, capitalize(err.Error())+".")
		return
	}
	if err := s.db.SetEmailVerified(user.ID, true); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}
	AuthLog.Info("Email verified", "user_id", user.ID, "username", user.Username, "from", remoteIP(r))
	s.accountActivated(user)
	s.issueSession(w, r, user)
}

// handleResendCode emails a new verification code.
func (s *Server) handleResendCode(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Password string }
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	user, ok := s.userByPassword(req.Email, req.Password)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "bad_login", "Invalid email or password")
		return
	}
	if verified, _ := s.db.IsEmailVerified(user.ID); verified {
		writeJSON(w, http.StatusOK, map[string]interface{}{"sent": false, "verified": true})
		return
	}
	if err := s.sendCode(user, purposeVerify, user.Email, false); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, errCodeMailUnavailable, capitalize(err.Error())+".")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sent": true, "email": user.Email})
}

// handleFixRegistration corrects a typo in a not-yet-verified account's
// email or username, and sends a fresh code to the (new) address.
func (s *Server) handleFixRegistration(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email, Password string
		NewEmail        string `json:"new_email"`
		NewUsername     string `json:"new_username"`
	}
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	user, ok := s.userByPassword(req.Email, req.Password)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "bad_login", "Invalid email or password")
		return
	}
	if verified, _ := s.db.IsEmailVerified(user.ID); verified {
		writeAPIError(w, http.StatusConflict, "already_verified", "This account is already verified; change its email or username from the account settings instead.")
		return
	}
	if name := strings.TrimSpace(req.NewUsername); name != "" && name != user.Username {
		if len(name) < 2 || len(name) > 32 {
			writeAPIError(w, http.StatusBadRequest, "bad_username", "Username must be 2-32 characters")
			return
		}
		disc, err := s.db.RenameUser(user.ID, name)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal", "Couldn't change the username")
			return
		}
		user.Username, user.Discriminator = name, disc
	}
	if strings.TrimSpace(req.NewEmail) != "" {
		email, valid := normalizeEmail(req.NewEmail)
		if !valid {
			writeAPIError(w, http.StatusBadRequest, "bad_email", "That doesn't look like an email address")
			return
		}
		if !strings.EqualFold(email, user.Email) {
			if taken, _ := s.db.EmailInUse(email, user.ID); taken {
				writeAPIError(w, http.StatusConflict, "email_taken", "Another account already uses that email")
				return
			}
			if err := s.db.UpdateUserEmail(user.ID, email); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "internal", "Couldn't change the email")
				return
			}
			user.Email = email
		}
	}
	AuthLog.Info("Unverified account corrected", "user_id", user.ID, "username", user.Username, "from", remoteIP(r))
	resp := map[string]interface{}{"user": user, "sent": true, "email": user.Email}
	if err := s.sendCode(user, purposeVerify, user.Email, true); err != nil {
		resp["sent"], resp["mail_error"] = false, capitalize(err.Error())+"."
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleForgotPassword emails a reset code. It answers the same whether or
// not the address has an account, so it can't be used to find out.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email string }
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	if !s.config.Mail.Enabled() {
		writeAPIError(w, http.StatusNotImplemented, errCodeMailUnavailable, s.config.ServerName+" can't send email, so it can't reset passwords. Ask its admin.")
		return
	}
	AuthLog.Info("Password reset requested", "from", remoteIP(r))
	if user, _, err := s.db.FindUserByEmail(req.Email); err == nil && !user.IsServiceAccount && !user.IsBot {
		go func() {
			if err := s.sendCode(user, purposeReset, user.Email, false); err != nil {
				AuthLog.Warn("Password reset code not sent", "user_id", user.ID, "error", err)
			}
		}()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sent": true})
}

// handleResetPassword sets a new password with a reset code, signs out
// every other session and signs this one in. Receiving the code also
// proves the address, so an unverified account becomes verified.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email, Code string
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	if len(req.NewPassword) < minPassword {
		writeAPIError(w, http.StatusBadRequest, "weak_password", "Password must be at least 8 characters")
		return
	}
	user, _, err := s.db.FindUserByEmail(req.Email)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, errCodeBadCode, "That code is wrong.")
		return
	}
	if err := s.checkCode(user.ID, purposeReset, req.Code); err != nil {
		writeAPIError(w, http.StatusBadRequest, errCodeBadCode, capitalize(err.Error())+".")
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err == nil {
		err = s.db.UpdatePasswordHash(user.ID, hash)
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}
	_ = s.db.DeleteUserSessions(user.ID)
	if verified, _ := s.db.IsEmailVerified(user.ID); !verified {
		_ = s.db.SetEmailVerified(user.ID, true)
		s.accountActivated(user)
	}
	AuthLog.Info("Password reset", "user_id", user.ID, "username", user.Username, "from", remoteIP(r))
	s.issueSession(w, r, user)
}

// --- signed-in routes ---

// bearerUser authenticates the request's "Authorization: Bearer" token.
func (s *Server) bearerUser(w http.ResponseWriter, r *http.Request) (*models.User, string, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if token == "" {
		writeAPIError(w, http.StatusUnauthorized, "no_token", "Sign in first")
		return nil, "", false
	}
	userID, err := s.db.GetSessionByToken(hashToken(token))
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "bad_token", "Your session has expired; sign in again")
		return nil, "", false
	}
	user, err := s.db.GetUserByID(userID)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "bad_token", "Your session has expired; sign in again")
		return nil, "", false
	}
	return user, token, true
}

// checkCurrentPassword answers 401 and reports false when pw is wrong.
func (s *Server) checkCurrentPassword(w http.ResponseWriter, user *models.User, pw string) bool {
	hash, err := s.db.GetPasswordHash(user.ID)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil {
		writeAPIError(w, http.StatusUnauthorized, "bad_password", "Your current password is wrong")
		return false
	}
	return true
}

// handleChangePassword changes the signed-in user's password and signs out
// their other sessions.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	user, token, ok := s.bearerUser(w, r)
	if !ok || !s.checkCurrentPassword(w, user, req.Current) {
		return
	}
	if len(req.New) < minPassword {
		writeAPIError(w, http.StatusBadRequest, "weak_password", "Password must be at least 8 characters")
		return
	}
	hash, err := hashPassword(req.New)
	if err == nil {
		err = s.db.UpdatePasswordHash(user.ID, hash)
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}
	_ = s.db.DeleteUserSessionsExcept(user.ID, hashToken(token))
	AuthLog.Info("Password changed", "user_id", user.ID, "username", user.Username, "from", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleUpdateAccount changes the signed-in user's username and/or email.
// On a server that verifies, a new email only takes effect once the code
// sent to it is entered (handleConfirmEmail).
func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current  string `json:"current_password"`
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	user, _, ok := s.bearerUser(w, r)
	if !ok || !s.checkCurrentPassword(w, user, req.Current) {
		return
	}
	resp := map[string]interface{}{}
	if name := strings.TrimSpace(req.Username); name != "" && name != user.Username {
		if len(name) < 2 || len(name) > 32 {
			writeAPIError(w, http.StatusBadRequest, "bad_username", "Username must be 2-32 characters")
			return
		}
		disc, err := s.db.RenameUser(user.ID, name)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal", "Couldn't change the username")
			return
		}
		AuthLog.Info("Username changed", "user_id", user.ID, "from_name", user.Username, "to_name", name)
		user.Username, user.Discriminator = name, disc
		s.handlers.broadcastUserRenamed(user)
	}
	if strings.TrimSpace(req.Email) != "" {
		email, valid := normalizeEmail(req.Email)
		if !valid {
			writeAPIError(w, http.StatusBadRequest, "bad_email", "That doesn't look like an email address")
			return
		}
		if !strings.EqualFold(email, user.Email) {
			if taken, _ := s.db.EmailInUse(email, user.ID); taken {
				writeAPIError(w, http.StatusConflict, "email_taken", "Another account already uses that email")
				return
			}
			if s.config.Mail.VerificationRequired() {
				if err := s.db.SetPendingEmail(user.ID, email); err != nil {
					writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
					return
				}
				if err := s.sendCode(user, purposeChangeEmail, email, true); err != nil {
					writeAPIError(w, http.StatusServiceUnavailable, errCodeMailUnavailable, capitalize(err.Error())+".")
					return
				}
				resp["verification_required"], resp["pending_email"] = true, email
			} else {
				if err := s.db.UpdateUserEmail(user.ID, email); err != nil {
					writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
					return
				}
				AuthLog.Info("Email changed", "user_id", user.ID)
				user.Email = email
			}
		}
	}
	resp["user"] = user
	writeJSON(w, http.StatusOK, resp)
}

// handleConfirmEmail applies a pending email change with its code.
func (s *Server) handleConfirmEmail(w http.ResponseWriter, r *http.Request) {
	var req struct{ Code string }
	if !decode(w, r, &req) || s.limited(w, r) {
		return
	}
	user, _, ok := s.bearerUser(w, r)
	if !ok {
		return
	}
	pending, _ := s.db.GetPendingEmail(user.ID)
	if pending == "" {
		writeAPIError(w, http.StatusBadRequest, errCodeBadCode, "No email change is waiting.")
		return
	}
	if err := s.checkCode(user.ID, purposeChangeEmail, req.Code); err != nil {
		writeAPIError(w, http.StatusBadRequest, errCodeBadCode, capitalize(err.Error())+".")
		return
	}
	if taken, _ := s.db.EmailInUse(pending, user.ID); taken {
		writeAPIError(w, http.StatusConflict, "email_taken", "Another account took that email meanwhile")
		return
	}
	if err := s.db.UpdateUserEmail(user.ID, pending); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}
	user.Email = pending
	AuthLog.Info("Email changed", "user_id", user.ID, "from", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]interface{}{"user": user})
}

// registerAPIRoutes adds the HTTP API to mux (every run mode shares it).
func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/account/verify", s.handleVerifyAccount)
	mux.HandleFunc("/api/account/resend", s.handleResendCode)
	mux.HandleFunc("/api/account/fix", s.handleFixRegistration)
	mux.HandleFunc("/api/account/password", s.handleChangePassword)
	mux.HandleFunc("/api/account/update", s.handleUpdateAccount)
	mux.HandleFunc("/api/account/confirm-email", s.handleConfirmEmail)
	mux.HandleFunc("/api/password/forgot", s.handleForgotPassword)
	mux.HandleFunc("/api/password/reset", s.handleResetPassword)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}


// broadcastUserRenamed tells everyone sharing a server with user about a
// new username (USER_UPDATE).
func (h *Handlers) broadcastUserRenamed(user *models.User) {
	servers, err := h.db.GetUserServers(user.ID)
	if err != nil {
		return
	}
	for _, srv := range servers {
		if err := h.hub.BroadcastToServer(srv.ID, protocol.EventUserUpdate, user, nil); err != nil {
			AuthLog.Warn("Failed to broadcast username change", "user_id", user.ID, "error", err)
		}
	}
}

// ResetPasswordOffline gives the account for email a new random password
// and signs it out everywhere (concord-server --reset-password), for a
// server that can't email reset codes. It returns the new password.
func ResetPasswordOffline(db *database.DB, email string) (string, error) {
	user, _, err := db.FindUserByEmail(email)
	if err != nil {
		return "", fmt.Errorf("no account uses %s", email)
	}
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 12)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	pw := string(b[:4]) + "-" + string(b[4:8]) + "-" + string(b[8:])
	hash, err := hashPassword(pw)
	if err != nil {
		return "", err
	}
	if err := db.UpdatePasswordHash(user.ID, hash); err != nil {
		return "", err
	}
	_ = db.DeleteUserSessions(user.ID)
	_ = db.SetEmailVerified(user.ID, true)
	return pw, nil
}
