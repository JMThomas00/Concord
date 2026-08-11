package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// setupOverwriteTestServer creates a server with an owner, an @everyone
// role, one regular member, and one text channel — the minimum fixture
// hasChannelPermission needs (server owner lookup, member lookup, roles,
// the channel's own overwrites).
func setupOverwriteTestServer(t *testing.T) (srv *Server, owner *models.User, member *models.User, everyone *models.Role, channel *models.Channel, cleanup func()) {
	t.Helper()
	srv, cleanup = createTestServer(t)

	owner = models.NewUser("owner", "owner@overwrite-test.com")
	if err := srv.db.CreateUser(owner, "password123"); err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}

	testServer := models.NewServer("Overwrite Test Server", owner.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	everyone = models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}

	member = models.NewUser("member", "member@overwrite-test.com")
	if err := srv.db.CreateUser(member, "password123"); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(member.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(owner.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add owner as member: %v", err)
	}

	channel = models.NewTextChannel(testServer.ID, "overwrite-test-channel")
	if err := srv.db.CreateChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	return srv, owner, member, everyone, channel, cleanup
}

func TestHasChannelPermission_DefaultEveryoneCanSendMessages(t *testing.T) {
	srv, _, member, _, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	if err := srv.handlers.hasChannelPermission(member.ID, channel, models.PermissionSendMessages); err != nil {
		t.Errorf("expected a plain member to have Send Messages via the default @everyone role, got error: %v", err)
	}
}

func TestHasChannelPermission_EveryoneOverwriteCanDenySendMessages(t *testing.T) {
	srv, _, member, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}

	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}
	if len(reloaded.PermissionOverwrites) != 1 {
		t.Fatalf("expected GetChannelByID to populate PermissionOverwrites, got %d entries", len(reloaded.PermissionOverwrites))
	}

	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err == nil {
		t.Error("expected Send Messages to be denied once @everyone's channel overwrite denies it")
	}
}

func TestHasChannelPermission_MemberOverwriteAllowsDespiteEveryoneDeny(t *testing.T) {
	srv, _, member, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set everyone deny overwrite: %v", err)
	}
	allow := models.PermissionOverwrite{ID: member.ID, Type: "member", Allow: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, allow); err != nil {
		t.Fatalf("failed to set member allow overwrite: %v", err)
	}

	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}
	if len(reloaded.PermissionOverwrites) != 2 {
		t.Fatalf("expected 2 overwrites, got %d", len(reloaded.PermissionOverwrites))
	}

	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("expected the member-specific allow overwrite to win over @everyone's deny (member overwrites apply last), got error: %v", err)
	}
}

func TestHasChannelPermission_ServerOwnerBypassesOverwrites(t *testing.T) {
	srv, owner, _, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}
	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}

	if err := srv.handlers.hasChannelPermission(owner.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("expected the server owner to bypass channel overwrites entirely, got error: %v", err)
	}
}

func TestChannelPermissionOverwriteDelete(t *testing.T) {
	srv, _, member, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}
	if err := srv.db.DeleteChannelPermissionOverwrite(channel.ID, everyone.ID); err != nil {
		t.Fatalf("failed to delete overwrite: %v", err)
	}

	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}
	if len(reloaded.PermissionOverwrites) != 0 {
		t.Fatalf("expected overwrite to be gone after delete, got %d entries", len(reloaded.PermissionOverwrites))
	}
	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("expected Send Messages to be allowed again once the denying overwrite was deleted, got error: %v", err)
	}
}

func TestChannelPermissionOverwriteUpsertReplacesPreviousValue(t *testing.T) {
	srv, _, _, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	first := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Allow: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, first); err != nil {
		t.Fatalf("failed to set first overwrite: %v", err)
	}
	second := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, second); err != nil {
		t.Fatalf("failed to set second (replacing) overwrite: %v", err)
	}

	overwrites, err := srv.db.GetChannelPermissionOverwrites(channel.ID)
	if err != nil {
		t.Fatalf("failed to get overwrites: %v", err)
	}
	if len(overwrites) != 1 {
		t.Fatalf("expected upsert to replace, not duplicate — got %d rows", len(overwrites))
	}
	if overwrites[0].Allow != 0 || overwrites[0].Deny != int64(models.PermissionSendMessages) {
		t.Errorf("expected the second Set call's values to win, got Allow=%d Deny=%d", overwrites[0].Allow, overwrites[0].Deny)
	}
}

func TestHasChannelPermission_UnknownTargetIsIgnored(t *testing.T) {
	// A random UUID that doesn't match any real role/member shouldn't panic
	// or otherwise affect the outcome for an unrelated user.
	srv, _, member, _, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	stray := models.PermissionOverwrite{ID: uuid.New(), Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, stray); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}
	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}

	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("an overwrite targeting an unrelated role ID should not affect this member, got error: %v", err)
	}
}

// startPlainTestServer boots a real, fully-running Concord server (real
// net.Listen port, real Hub.Run loop) with no plugins installed — a
// lighter-weight sibling of plugin_platform_test.go's startTestPluginServer
// for tests that only need real WebSocket clients, not a spawned plugin
// process.
func startPlainTestServer(t *testing.T) (srv *Server, wsURL string) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := tmpDir + string(os.PathSeparator) + "concord.db"
	pluginsDir := tmpDir + string(os.PathSeparator) + "Plugins" // deliberately left empty — no plugins for this test

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	config := DefaultConfig()
	config.Host = "127.0.0.1"
	config.Port = port
	config.DatabasePath = dbPath
	config.ServerName = "Overwrite Wire Test Server"
	config.PluginsDir = pluginsDir
	config.MessagePruning.Enabled = false

	srv, err = New(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	go func() {
		_ = srv.Run()
	}()
	t.Cleanup(func() {
		if srv.httpServer != nil {
			srv.httpServer.Close()
		}
		srv.db.Close()
	})

	wsURL = fmt.Sprintf("ws://127.0.0.1:%d/ws", port)

	// Wait for the HTTP server to actually be accepting connections before
	// handing back the URL — srv.Run() starts listening asynchronously.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
			conn.Close()
			return srv, wsURL
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("test server never started accepting connections")
	return nil, ""
}

// TestChannelOverwriteEndToEndOverWebSocket drives the actual wire path a
// real Concord client uses — not the hasChannelPermission/DB functions
// directly like every test above, but a real WebSocket connection sending
// the real OpUpdateChannelOverwrite opcode through client.go's dispatch
// switch into HandleUpdateChannelOverwrite, then a second real connection
// sending a real OpSendMessage that HandleSendMessage must reject. This is
// the one thing the direct-function tests above structurally can't prove:
// that the new opcode is actually wired into the dispatch switch at all.
func TestChannelOverwriteEndToEndOverWebSocket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wire-level integration test in short mode")
	}

	srv, wsURL := startPlainTestServer(t)

	owner, ownerToken := createTestUserAndToken(t, srv, "overwrite-owner")
	member, memberToken := createTestUserAndToken(t, srv, "overwrite-member")

	testServer := models.NewServer("Overwrite Wire Test", owner.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	everyone := models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(owner.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add owner as member: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(member.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	channel := models.NewTextChannel(testServer.ID, "wire-test-channel")
	if err := srv.db.CreateChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	ownerClient := newTestWSClient(t, wsURL)
	ownerClient.identify(ownerToken)
	memberClient := newTestWSClient(t, wsURL)
	memberClient.identify(memberToken)

	// Sanity check first: the member can send before any overwrite exists.
	memberClient.send(protocol.OpSendMessage, protocol.SendMessagePayload{
		ChannelID: channel.ID,
		Content:   "should go through",
	})
	created := memberClient.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventMessageCreate
	})
	if created == nil {
		t.Fatal("expected the pre-overwrite message to be created")
	}

	// Owner denies Send Messages for @everyone on this channel, over the
	// real wire — this is the line that only this test, not the direct
	// hasChannelPermission tests above, actually proves is reachable.
	ownerClient.send(protocol.OpUpdateChannelOverwrite, protocol.UpdateChannelOverwriteRequest{
		ServerID:   testServer.ID,
		ChannelID:  channel.ID,
		TargetID:   everyone.ID,
		TargetType: "role",
		Deny:       int64(models.PermissionSendMessages),
	})
	updated := ownerClient.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventChannelUpdate
	})
	if updated == nil {
		t.Fatal("expected a CHANNEL_UPDATE broadcast after setting the overwrite")
	}

	// The member's send must now be rejected, over the real wire — a real
	// ErrorPayload back on their own connection, not a message create.
	memberClient.send(protocol.OpSendMessage, protocol.SendMessagePayload{
		ChannelID: channel.ID,
		Content:   "should be rejected",
	})
	rejection := memberClient.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == ""
	})
	if rejection == nil {
		t.Fatal("expected an error response after Send Messages was denied")
	}
	var errPayload protocol.ErrorPayload
	if err := json.Unmarshal(rejection.Data, &errPayload); err != nil {
		t.Fatalf("failed to decode error payload: %v", err)
	}
	if errPayload.Code != protocol.ErrorCodeForbidden {
		t.Errorf("expected ErrorCodeForbidden, got %d: %s", errPayload.Code, errPayload.Message)
	}

	// Confirm the DB agrees with the wire result — the message never landed.
	messages, err := srv.db.GetChannelMessages(channel.ID, 10, nil, uuid.Nil)
	if err != nil {
		t.Fatalf("failed to list channel messages: %v", err)
	}
	for _, m := range messages {
		if m.Content == "should be rejected" {
			t.Error("the denied message was persisted despite the wire-level rejection")
		}
	}
}
