package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

// Calls to a server's account API (verification, password reset, account
// changes). See internal/server/accounts.go for the server side.

// APIError is an error answer from a server's HTTP API.
type APIError struct {
	Status  int
	Code    string // e.g. "verification_required", "mail_unavailable", "bad_code"
	Message string
	Email   string // for verification_required: where the code went
}

func (e *APIError) Error() string { return e.Message }

// Codes the client acts on (internal/server/accounts.go).
const (
	apiVerificationRequired = "verification_required"
	apiMailUnavailable      = "mail_unavailable"
)

// IsVerificationRequired reports whether err means the account must enter
// an emailed code before it can sign in.
func IsVerificationRequired(err error) (*APIError, bool) {
	if e, ok := err.(*APIError); ok && e.Code == apiVerificationRequired {
		return e, true
	}
	return nil, false
}

// apiURL turns a server's address into the URL of an API path.
func apiURL(serverAddr, path string) (string, error) {
	u, err := url.Parse(serverAddr)
	if err != nil {
		return "", fmt.Errorf("invalid server address: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	u.Path = path
	return u.String(), nil
}

// postAPI POSTs body as JSON to path and decodes a 2xx answer into out.
// Any other answer comes back as an *APIError.
func postAPI(serverAddr, path, token string, body, out interface{}) (int, error) {
	target, err := apiURL(serverAddr, path)
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, apiErrorFrom(resp.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("failed to parse response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// apiErrorFrom reads a {"error", "code"} answer, or plain text from older
// servers and older routes.
func apiErrorFrom(status int, raw []byte) *APIError {
	var body struct {
		Error, Code, Email string
	}
	if json.Unmarshal(raw, &body) == nil && body.Error != "" {
		return &APIError{Status: status, Code: body.Code, Message: body.Error, Email: body.Email}
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &APIError{Status: status, Message: msg}
}

// sessionResponse is a {user, token} answer.
type sessionResponse struct {
	User  *models.User `json:"user"`
	Token string       `json:"token"`
}

// AccountAPI calls the account routes of one server.
type AccountAPI struct {
	Addr string // the server's ws:// or http:// address
}

// VerifyAccount enters the code a new account was emailed; it signs in.
func (a AccountAPI) VerifyAccount(email, code string) (*models.User, string, error) {
	var r sessionResponse
	_, err := postAPI(a.Addr, "/api/account/verify", "", map[string]string{"email": email, "code": code}, &r)
	return r.User, r.Token, err
}

// ResendCode emails a fresh verification code.
func (a AccountAPI) ResendCode(email, password string) error {
	_, err := postAPI(a.Addr, "/api/account/resend", "", map[string]string{"email": email, "password": password}, nil)
	return err
}

// FixRegistration corrects a not-yet-verified account's email and/or
// username and sends a fresh code. It returns where the code went.
func (a AccountAPI) FixRegistration(email, password, newEmail, newUsername string) (string, error) {
	var r struct {
		Email     string `json:"email"`
		Sent      bool   `json:"sent"`
		MailError string `json:"mail_error"`
	}
	_, err := postAPI(a.Addr, "/api/account/fix", "", map[string]string{
		"email": email, "password": password, "new_email": newEmail, "new_username": newUsername,
	}, &r)
	if err == nil && !r.Sent && r.MailError != "" {
		err = &APIError{Code: apiMailUnavailable, Message: r.MailError}
	}
	return r.Email, err
}

// ForgotPassword asks the server to email a reset code.
func (a AccountAPI) ForgotPassword(email string) error {
	_, err := postAPI(a.Addr, "/api/password/forgot", "", map[string]string{"email": email}, nil)
	return err
}

// ResetPassword sets a new password with a reset code; it signs in.
func (a AccountAPI) ResetPassword(email, code, newPassword string) (*models.User, string, error) {
	var r sessionResponse
	_, err := postAPI(a.Addr, "/api/password/reset", "", map[string]string{"email": email, "code": code, "new_password": newPassword}, &r)
	return r.User, r.Token, err
}

// ChangePassword changes the signed-in account's password.
func (a AccountAPI) ChangePassword(token, current, newPassword string) error {
	_, err := postAPI(a.Addr, "/api/account/password", token, map[string]string{"current_password": current, "new_password": newPassword}, nil)
	return err
}

// AccountUpdate is the answer to UpdateAccount.
type AccountUpdate struct {
	User                 *models.User `json:"user"`
	VerificationRequired bool         `json:"verification_required"`
	PendingEmail         string       `json:"pending_email"`
}

// UpdateAccount changes the signed-in account's username and/or email.
func (a AccountAPI) UpdateAccount(token, current, username, email string) (AccountUpdate, error) {
	var r AccountUpdate
	_, err := postAPI(a.Addr, "/api/account/update", token, map[string]string{"current_password": current, "username": username, "email": email}, &r)
	return r, err
}

// ConfirmEmail applies a pending email change with its code.
func (a AccountAPI) ConfirmEmail(token, code string) error {
	_, err := postAPI(a.Addr, "/api/account/confirm-email", token, map[string]string{"code": code}, nil)
	return err
}

// accountAPIFor is the AccountAPI of a known server.
func (cm *ConnectionManager) accountAPIFor(serverID uuid.UUID) (AccountAPI, bool) {
	sc := cm.GetConnection(serverID)
	if sc == nil {
		return AccountAPI{}, false
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return AccountAPI{Addr: sc.ServerInfo.GetHTTPURL()}, true
}
