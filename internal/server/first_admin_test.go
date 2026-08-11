package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestFirstRealUserBecomesServerOwner verifies the "very first real user"
// auto-admin/auto-owner logic already present in handleRegister
// (server.go:573-580, via CountRealUsers + EnsureAdminRole) actually works
// end-to-end, not just that the code exists — CountRealUsers/UpdateServerOwner
// were previously completely untested.
func TestFirstRealUserBecomesServerOwner(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// createTestServer -> New(config) already ran EnsureDefaultServer once at
	// construction (server.go:97), so the default server exists, owned by
	// the system placeholder, before any registration happens — same as a
	// real boot.
	defaultServer, _, err := server.db.EnsureDefaultServer(server.config.ServerName)
	if err != nil {
		t.Fatalf("failed to look up default server: %v", err)
	}
	systemUserID := "00000000-0000-0000-0000-000000000001"
	if defaultServer.OwnerID.String() != systemUserID {
		t.Fatalf("expected the default server to start owned by the system placeholder, got %v", defaultServer.OwnerID)
	}

	registerUser := func(username, email string) map[string]interface{} {
		t.Helper()
		body, _ := json.Marshal(map[string]string{
			"username": username,
			"email":    email,
			"password": "password123",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.handleRegister(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("registration of %s failed: %d: %s", username, w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		return resp
	}

	firstResp := registerUser("firstuser", "first@example.com")
	firstUser := firstResp["user"].(map[string]interface{})
	firstUserID := firstUser["id"].(string)

	// Reload the server — OwnerID should now be the first real user, not
	// the system placeholder.
	reloaded, err := server.db.GetServerByID(defaultServer.ID)
	if err != nil {
		t.Fatalf("failed to reload server: %v", err)
	}
	if reloaded.OwnerID.String() != firstUserID {
		t.Errorf("expected the first real registrant (%s) to become server owner, got owner %v", firstUserID, reloaded.OwnerID)
	}

	// They should also hold the Admin role, not just raw ownership — the
	// role editor and anything checking checkPermission (which only
	// consults member.RoleIDs, not owner status — see Part 1's findings)
	// needs this to actually work day-to-day.
	adminRole, err := server.db.GetOrCreateAdminRole(defaultServer.ID)
	if err != nil {
		t.Fatalf("failed to look up admin role: %v", err)
	}
	member, err := server.db.GetServerMember(defaultServer.ID, reloaded.OwnerID)
	if err != nil {
		t.Fatalf("failed to load first user's membership: %v", err)
	}
	hasAdmin := false
	for _, rid := range member.RoleIDs {
		if rid == adminRole.ID {
			hasAdmin = true
		}
	}
	if !hasAdmin {
		t.Error("expected the first real registrant to hold the Admin role, not just raw server ownership")
	}

	// Second registrant must NOT become owner or get Admin.
	secondResp := registerUser("seconduser", "second@example.com")
	secondUser := secondResp["user"].(map[string]interface{})
	secondUserID := secondUser["id"].(string)

	reloaded2, err := server.db.GetServerByID(defaultServer.ID)
	if err != nil {
		t.Fatalf("failed to reload server after second registration: %v", err)
	}
	if reloaded2.OwnerID.String() != firstUserID {
		t.Errorf("ownership should still belong to the first registrant after a second one joins, got %v", reloaded2.OwnerID)
	}
	secondID, err := uuid.Parse(secondUserID)
	if err != nil {
		t.Fatalf("failed to parse second user ID: %v", err)
	}
	secondMember, err := server.db.GetServerMember(defaultServer.ID, secondID)
	if err != nil {
		t.Fatalf("failed to load second user's membership: %v", err)
	}
	for _, rid := range secondMember.RoleIDs {
		if rid == adminRole.ID {
			t.Error("the second registrant should not have received the Admin role")
		}
	}
}
