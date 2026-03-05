package server

import (
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// BenchmarkMessageBroadcast benchmarks message broadcasting to multiple clients
func BenchmarkMessageBroadcast(b *testing.B) {
	hub := NewHub()
	go hub.Run()

	// Create 100 mock clients
	clients := make([]*Client, 100)
	channelID := uuid.New()
	for i := 0; i < 100; i++ {
		client := &Client{
			UserID:    uuid.New(),
			ServerIDs: []uuid.UUID{uuid.New()},
			send:      make(chan *protocol.Message, 256),
		}
		clients[i] = client
		hub.register <- client

		// Drain messages to prevent blocking
		go func(c *Client) {
			for range c.send {
			}
		}(client)
	}

	// Let hub process registrations
	// time.Sleep(10 * time.Millisecond)

	msg := &protocol.Message{
		Op:   protocol.OpDispatch,
		Type: protocol.EventMessageCreate,
		Data: []byte(`{"content":"test"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.BroadcastToChannel(channelID, protocol.EventMessageCreate, msg.Data, nil)
	}
	b.StopTimer()

	// Cleanup
	for _, client := range clients {
		hub.unregister <- client
		close(client.send)
	}
}

// BenchmarkDatabaseInsert benchmarks database insert operations
func BenchmarkDatabaseInsert(b *testing.B) {
	server, cleanup := createTestServer(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user := models.NewUser("benchuser", "bench@example.com")
		_ = server.db.CreateUser(user, "password123")
	}
}

// BenchmarkPermissionCheck benchmarks permission calculations
func BenchmarkPermissionCheck(b *testing.B) {
	serverID := uuid.New()
	userID := uuid.New()
	ownerID := uuid.New()

	everyoneRole := models.NewEveryoneRole(serverID)

	role1 := models.NewRole(serverID, "Role1")
	role1.SetPermissions(models.PermissionsText)

	role2 := models.NewRole(serverID, "Role2")
	role2.SetPermissions(models.PermissionsModerator)

	member := &models.ServerMember{
		UserID:   userID,
		ServerID: serverID,
		RoleIDs:  []uuid.UUID{role1.ID, role2.ID},
	}

	calc := models.NewPermissionCalculator(ownerID, everyoneRole)
	roles := []*models.Role{role1, role2}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calc.ComputeBasePermissions(member, roles)
	}
}

// BenchmarkMessageParseMentions benchmarks mention parsing
func BenchmarkMessageParseMentions(b *testing.B) {
	userID1 := uuid.New()
	userID2 := uuid.New()
	roleID := uuid.New()

	content := "Hello <@" + userID1.String() + "> and <@" + userID2.String() + "> check <@&" + roleID.String() + "> @everyone"

	msg := &models.Message{
		ID:        uuid.New(),
		ChannelID: uuid.New(),
		AuthorID:  uuid.New(),
		Content:   content,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.ParseMentions()
	}
}

// BenchmarkChannelTreeBuild benchmarks building a hierarchical channel tree
func BenchmarkChannelTreeBuild(b *testing.B) {
	server, cleanup := createTestServer(b)
	defer cleanup()

	// Create test data
	user := models.NewUser("treeuser", "tree@test.com")
	_ = server.db.CreateUser(user, "password123")

	testServer := models.NewServer("Tree Test", user.ID)
	_ = server.db.CreateServer(testServer)

	// Create 10 categories with 10 channels each
	for i := 0; i < 10; i++ {
		category := models.NewCategory(testServer.ID, "Category")
		_ = server.db.CreateChannel(category)

		for j := 0; j < 10; j++ {
			channel := models.NewTextChannel(testServer.ID, "channel")
			channel.CategoryID = category.ID
			_ = server.db.CreateChannel(channel)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = server.db.GetServerChannels(testServer.ID)
	}
}

// BenchmarkTokenHashing benchmarks token hashing operations
func BenchmarkTokenHashing(b *testing.B) {
	token := "test_token_1234567890abcdef"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hashToken(token)
	}
}

// BenchmarkJSONMarshaling benchmarks protocol message marshaling
func BenchmarkJSONMarshaling(b *testing.B) {
	payload := protocol.SendMessagePayload{
		ChannelID: uuid.New(),
		Content:   "This is a test message with some content",
		Nonce:     "test-nonce-123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = protocol.NewMessage(protocol.OpSendMessage, payload)
	}
}
