package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// setupFileTransferTestServer creates a server + @everyone role + text
// channel, mirroring the other wire-test setups in this package.
func setupFileTransferTestServer(t *testing.T, srv *Server) (testServer *models.Server, channel *models.Channel, everyone *models.Role) {
	t.Helper()
	owner, _ := createTestUserAndToken(t, srv, "filetransfer-owner")

	testServer = models.NewServer("File Transfer Test Server", owner.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	everyone = models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}
	channel = models.NewTextChannel(testServer.ID, "general")
	if err := srv.db.CreateChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
	return testServer, channel, everyone
}

func addMemberWithEveryone(t *testing.T, srv *Server, testServer *models.Server, everyone *models.Role, username string) (*models.User, string) {
	t.Helper()
	user, token := createTestUserAndToken(t, srv, username)
	if err := srv.db.AddServerMember(models.NewServerMember(user.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	if err := srv.db.AddMemberRole(user.ID, testServer.ID, everyone.ID); err != nil {
		t.Fatalf("failed to assign @everyone: %v", err)
	}
	return user, token
}

// TestSendMessageWithAttachmentRequiresPermission drives the real wire path:
// a plain @everyone member (which grants PermissionSendMessages but NOT
// PermissionAttachFiles by default, see models.NewEveryoneRole) is rejected
// when trying to post a message with an attachment manifest, then succeeds
// once granted a role with PermissionAttachFiles -- and the manifest
// (not the file's bytes, which never touch the server) is what gets persisted.
func TestSendMessageWithAttachmentRequiresPermission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wire-level integration test in short mode")
	}

	srv, wsURL := startPlainTestServer(t)
	testServer, channel, everyone := setupFileTransferTestServer(t, srv)
	member, memberToken := addMemberWithEveryone(t, srv, testServer, everyone, "attach-member")

	client := newTestWSClient(t, wsURL)
	client.identify(memberToken)

	attachmentID := uuid.New()
	sendAttachment := func(match func(*protocol.Message) bool) *protocol.Message {
		client.send(protocol.OpSendMessage, protocol.SendMessagePayload{
			ChannelID: channel.ID,
			Content:   "check this out",
			Nonce:     uuid.New().String(),
			Attachments: []models.Attachment{{
				ID:          attachmentID,
				Filename:    "photo.png",
				Size:        4096,
				ContentHash: "abc123",
				ContentType: "image/png",
			}},
		})
		return client.readUntil(5*time.Second, match)
	}

	// Negative case: @everyone alone doesn't grant PermissionAttachFiles.
	// The error dispatch has an empty Type field (see ErrorPayload handling
	// throughout this package, e.g. nickname_test.go's rejection case).
	rejection := sendAttachment(func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == ""
	})
	var errPayload protocol.ErrorPayload
	if err := json.Unmarshal(rejection.Data, &errPayload); err != nil {
		t.Fatalf("failed to decode error payload: %v", err)
	}
	if errPayload.Code != protocol.ErrorCodeForbidden {
		t.Errorf("expected ErrorCodeForbidden attaching without PermissionAttachFiles, got %d: %s", errPayload.Code, errPayload.Message)
	}

	// Grant PermissionAttachFiles and retry.
	attachRole := models.NewRole(testServer.ID, "Attacher")
	attachRole.Permissions = models.PermissionAttachFiles
	if err := srv.db.CreateRole(attachRole); err != nil {
		t.Fatalf("failed to create attach role: %v", err)
	}
	if err := srv.db.AddMemberRole(member.ID, testServer.ID, attachRole.ID); err != nil {
		t.Fatalf("failed to assign attach role: %v", err)
	}

	created := sendAttachment(func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventMessageCreate
	})
	var createPayload protocol.MessageCreatePayload
	if err := json.Unmarshal(created.Data, &createPayload); err != nil {
		t.Fatalf("failed to decode message create payload: %v", err)
	}
	if len(createPayload.Message.Attachments) != 1 || createPayload.Message.Attachments[0].Filename != "photo.png" {
		t.Fatalf("expected attachment manifest on the broadcast message, got %+v", createPayload.Message.Attachments)
	}
	if createPayload.Message.Attachments[0].SenderID != member.ID {
		t.Errorf("expected server to stamp SenderID as the sending user, got %s", createPayload.Message.Attachments[0].SenderID)
	}

	// Persisted, not just broadcast -- reload from the DB directly.
	stored, err := srv.db.GetMessage(createPayload.Message.ID)
	if err != nil {
		t.Fatalf("failed to reload message: %v", err)
	}
	if len(stored.Attachments) != 1 || stored.Attachments[0].ContentHash != "abc123" {
		t.Fatalf("expected attachment persisted in DB, got %+v", stored.Attachments)
	}
}

// TestFileTransferSignalOfflineThenRelay covers HandleFileTransferSignal's
// two behaviors: an immediate "offline" reply when the file's sender has no
// active connection, and a normal dumb relay (mirroring OpVoiceSignal) once
// they do -- the server never inspects SDP/candidate content, just forwards it.
func TestFileTransferSignalOfflineThenRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wire-level integration test in short mode")
	}

	srv, wsURL := startPlainTestServer(t)
	testServer, _, everyone := setupFileTransferTestServer(t, srv)
	_, requesterToken := addMemberWithEveryone(t, srv, testServer, everyone, "downloader")
	sender, senderToken := addMemberWithEveryone(t, srv, testServer, everyone, "sharer")

	attachmentID := uuid.New()

	requester := newTestWSClient(t, wsURL)
	requester.identify(requesterToken)

	// Sender is not connected yet -- server must short-circuit with "offline"
	// rather than trying to relay to nobody.
	requester.send(protocol.OpFileTransferSignal, protocol.FileTransferSignalPayload{
		TargetUserID: sender.ID,
		AttachmentID: attachmentID,
		Type:         "request",
	})
	offlineMsg := requester.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventFileTransferSignal
	})
	var offlinePayload protocol.FileTransferSignalRelayPayload
	if err := json.Unmarshal(offlineMsg.Data, &offlinePayload); err != nil {
		t.Fatalf("failed to decode offline payload: %v", err)
	}
	if offlinePayload.Type != "offline" || offlinePayload.AttachmentID != attachmentID {
		t.Fatalf("expected offline relay for attachment %s, got %+v", attachmentID, offlinePayload)
	}

	// Now the sender connects and the same request should relay through.
	senderClient := newTestWSClient(t, wsURL)
	senderClient.identify(senderToken)

	requester.send(protocol.OpFileTransferSignal, protocol.FileTransferSignalPayload{
		TargetUserID: sender.ID,
		AttachmentID: attachmentID,
		Type:         "request",
	})
	requestMsg := senderClient.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventFileTransferSignal
	})
	var requestPayload protocol.FileTransferSignalRelayPayload
	if err := json.Unmarshal(requestMsg.Data, &requestPayload); err != nil {
		t.Fatalf("failed to decode request payload: %v", err)
	}
	if requestPayload.Type != "request" || requestPayload.AttachmentID != attachmentID {
		t.Fatalf("expected sender to receive the relayed request, got %+v", requestPayload)
	}

	// Sender answers with an offer -- server relays SDP content untouched.
	senderClient.send(protocol.OpFileTransferSignal, protocol.FileTransferSignalPayload{
		TargetUserID: requestPayload.SourceUserID,
		AttachmentID: attachmentID,
		Type:         "offer",
		SDP:          "v=0 fake-sdp-body",
	})
	offerMsg := requester.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventFileTransferSignal
	})
	var offerPayload protocol.FileTransferSignalRelayPayload
	if err := json.Unmarshal(offerMsg.Data, &offerPayload); err != nil {
		t.Fatalf("failed to decode offer payload: %v", err)
	}
	if offerPayload.Type != "offer" || offerPayload.SDP != "v=0 fake-sdp-body" || offerPayload.SourceUserID != sender.ID {
		t.Fatalf("expected untouched offer relay from sender, got %+v", offerPayload)
	}
}
