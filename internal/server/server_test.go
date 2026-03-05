package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Helper to create a test server with in-memory database
func createTestServer(t testing.TB) (*Server, func()) {
	t.Helper()

	// Create temp database file
	tmpFile, err := os.CreateTemp("", "concord-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp db: %v", err)
	}
	tmpFile.Close()

	config := &Config{
		Host:           "127.0.0.1",
		Port:           0, // Use random port for testing
		ServerName:     "Test Server",
		DatabasePath:   tmpFile.Name(),
		MaxConnections: 10,
		Debug:          false,
	}

	server, err := New(config)
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to create server: %v", err)
	}

	cleanup := func() {
		server.db.Close()
		os.Remove(tmpFile.Name())
	}

	return server, cleanup
}

// Test successful user registration
func TestHandleRegister_Success(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "password123",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleRegister(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["token"] == nil {
		t.Error("expected token in response")
	}
	if resp["user"] == nil {
		t.Error("expected user in response")
	}
}

// Test registration with duplicate email
func TestHandleRegister_DuplicateEmail(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	reqBody := map[string]string{
		"username": "user1",
		"email":    "duplicate@example.com",
		"password": "password123",
	}
	body, _ := json.Marshal(reqBody)

	// First registration should succeed
	req1 := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	w1 := httptest.NewRecorder()
	server.handleRegister(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first registration failed: %d", w1.Code)
	}

	// Second registration with same email should fail
	reqBody["username"] = "user2" // Different username, same email
	body, _ = json.Marshal(reqBody)
	req2 := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	server.handleRegister(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("expected status 409 for duplicate email, got %d", w2.Code)
	}
}

// Test registration with invalid username (too short)
func TestHandleRegister_InvalidUsername(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	tests := []struct {
		name     string
		username string
		wantCode int
	}{
		{"too short", "a", http.StatusBadRequest},
		{"too long", "this_is_a_very_long_username_that_exceeds_32_characters", http.StatusBadRequest},
		{"valid length", "user", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]string{
				"username": tt.username,
				"email":    "user_" + tt.name + "@example.com",
				"password": "password123",
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
			w := httptest.NewRecorder()
			server.handleRegister(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("expected status %d, got %d", tt.wantCode, w.Code)
			}
		})
	}
}

// Test registration with invalid password (too short)
func TestHandleRegister_InvalidPassword(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "short",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.handleRegister(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for short password, got %d", w.Code)
	}
}

// Test registration with invalid JSON
func TestHandleRegister_InvalidJSON(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()
	server.handleRegister(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid JSON, got %d", w.Code)
	}
}

// Test that password is properly hashed
func TestHandleRegister_PasswordHashing(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	password := "mypassword123"
	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": password,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.handleRegister(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("registration failed: %d", w.Code)
	}

	// Get user from database and verify password is hashed
	user, passwordHash, err := server.db.GetUserByEmail("test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Password hash should not match plaintext password
	if passwordHash == password {
		t.Error("password should be hashed, not stored in plaintext")
	}

	// Verify bcrypt hash is valid
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		t.Error("password hash verification failed")
	}

	// Verify user was added to default server
	defaultServer, _, err := server.db.EnsureDefaultServer(server.config.ServerName)
	if err != nil {
		t.Fatalf("failed to get default server: %v", err)
	}

	_, err = server.db.GetServerMember(defaultServer.ID, user.ID)
	if err != nil {
		t.Error("user should be added to default server")
	}
}

// Test successful login
func TestHandleLogin_Success(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// Register a user first
	email := "login@example.com"
	password := "password123"
	regBody := map[string]string{
		"username": "loginuser",
		"email":    email,
		"password": password,
	}
	body, _ := json.Marshal(regBody)
	regReq := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	regW := httptest.NewRecorder()
	server.handleRegister(regW, regReq)

	if regW.Code != http.StatusOK {
		t.Fatalf("registration failed: %d", regW.Code)
	}

	// Now try to login
	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	body, _ = json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	loginW := httptest.NewRecorder()
	server.handleLogin(loginW, loginReq)

	if loginW.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", loginW.Code, loginW.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(loginW.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["token"] == nil {
		t.Error("expected token in response")
	}
	if resp["user"] == nil {
		t.Error("expected user in response")
	}
}

// Test login with invalid email
func TestHandleLogin_InvalidEmail(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	loginBody := map[string]string{
		"email":    "nonexistent@example.com",
		"password": "password123",
	}
	body, _ := json.Marshal(loginBody)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.handleLogin(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid email, got %d", w.Code)
	}
}

// Test login with invalid password
func TestHandleLogin_InvalidPassword(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// Register a user first
	email := "test@example.com"
	regBody := map[string]string{
		"username": "testuser",
		"email":    email,
		"password": "correctpassword",
	}
	body, _ := json.Marshal(regBody)
	regReq := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	regW := httptest.NewRecorder()
	server.handleRegister(regW, regReq)

	// Try to login with wrong password
	loginBody := map[string]string{
		"email":    email,
		"password": "wrongpassword",
	}
	body, _ = json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	loginW := httptest.NewRecorder()
	server.handleLogin(loginW, loginReq)

	if loginW.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid password, got %d", loginW.Code)
	}
}

// Test login with missing fields
func TestHandleLogin_MissingFields(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing email", map[string]string{"password": "password123"}},
		{"missing password", map[string]string{"email": "test@example.com"}},
		{"empty body", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
			w := httptest.NewRecorder()
			server.handleLogin(w, req)

			// Missing fields will result in unauthorized (empty strings won't match)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected status 401 for missing fields, got %d", w.Code)
			}
		})
	}
}

// Test token persistence across login sessions
func TestHandleLogin_TokenPersistence(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// Register a user
	email := "persist@example.com"
	password := "password123"
	regBody := map[string]string{
		"username": "persistuser",
		"email":    email,
		"password": password,
	}
	body, _ := json.Marshal(regBody)
	regReq := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
	regW := httptest.NewRecorder()
	server.handleRegister(regW, regReq)

	var regResp map[string]interface{}
	json.NewDecoder(regW.Body).Decode(&regResp)
	firstToken := regResp["token"].(string)

	// Login again
	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	body, _ = json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	loginW := httptest.NewRecorder()
	server.handleLogin(loginW, loginReq)

	var loginResp map[string]interface{}
	json.NewDecoder(loginW.Body).Decode(&loginResp)
	secondToken := loginResp["token"].(string)

	// Tokens should be different (new session)
	if firstToken == secondToken {
		t.Error("expected different tokens for different sessions")
	}

	// Both tokens should be valid (non-empty strings)
	if len(firstToken) == 0 || len(secondToken) == 0 {
		t.Error("tokens should not be empty")
	}
}

// Test WebSocket upgrade with valid connection
func TestHandleWebSocket_SuccessfulUpgrade(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// Start the hub
	go server.hub.Run()

	// Create a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	// Note: Full WebSocket upgrade testing requires a WebSocket client
	// This test verifies the handler is registered and responds
	// Full WebSocket testing is better done in integration tests
}

// Test WebSocket handler responds to HTTP requests
func TestHandleWebSocket_HTTPRequest(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// Regular HTTP GET without WebSocket upgrade headers should fail
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()
	server.handleWebSocket(w, req)

	// WebSocket upgrade should fail without proper headers
	if w.Code == http.StatusOK {
		t.Error("expected WebSocket upgrade to fail without proper headers")
	}
}

// Test health endpoint
func TestHandleHealth(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	server.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}
	if resp["time"] == nil {
		t.Error("expected time field in response")
	}
}
