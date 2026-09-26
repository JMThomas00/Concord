package server

import (
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
)

// TestUserStatusPersistsOfflineOnDisconnect is a regression test for a real,
// live-reported bug (2026-09-13): a member who had connected at least once
// showed as permanently "online" in the member list even though their
// machine had been off for weeks. Root cause: unregisterClient (hub.go)
// only ever *broadcast* an offline presence update to whichever clients
// happened to be connected at that exact moment -- it never persisted the
// offline status to the database. SetOnline()+UpdateUserStatus() (client.go,
// on identify) was the only place status was ever written to storage, so
// once a user connected once, their DB row stayed "online" forever; the
// next person to connect would read that stale value straight out of
// SERVER_CREATE's Users[] list.
//
// This drives the real path: identify (confirms online is persisted, same
// as before the fix), then an abrupt connection close (no clean WebSocket
// close handshake -- the realistic shape of "the laptop's power went out"),
// and confirms the database now actually reflects offline.
func TestUserStatusPersistsOfflineOnDisconnect(t *testing.T) {
	srv, wsURL := startPlainTestServer(t)
	user, token := createTestUserAndToken(t, srv, "presencetester")

	// unregisterClient (hub.go) only runs its offline-persistence path when
	// the disconnecting client belonged to at least one server -- a bare
	// user with no server membership (which createTestUserAndToken alone
	// produces) never exercises it, so give this user a real membership,
	// same as a real connected member would have.
	testServer := models.NewServer("Presence Offline Test", user.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(user.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add server member: %v", err)
	}

	client := newTestWSClient(t, wsURL)
	client.identify(token)

	reloaded, err := srv.db.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID after identify: %v", err)
	}
	if reloaded.Status != models.StatusOnline {
		t.Fatalf("test setup bug: expected status to be persisted online after identify, got %q", reloaded.Status)
	}

	// A brief pause ensures the hub has actually finished processing the
	// register message queued during identify() before we disconnect --
	// register/unregister travel on two separate channels with no ordering
	// guarantee between them if both fire back-to-back, which is purely a
	// test-timing artifact (a real user stays connected far longer than
	// this in practice) rather than anything the fix itself needs to guard.
	time.Sleep(100 * time.Millisecond)

	// Abrupt close -- no OpClose/clean handshake -- mirroring a machine
	// losing power rather than the client shutting down gracefully.
	client.conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		reloaded, err = srv.db.GetUserByID(user.ID)
		if err != nil {
			t.Fatalf("GetUserByID after disconnect: %v", err)
		}
		if reloaded.Status == models.StatusOffline {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("expected status to be persisted offline after disconnect, still got %q after 5s -- offline status is only being broadcast live, not saved to the database", reloaded.Status)
}
