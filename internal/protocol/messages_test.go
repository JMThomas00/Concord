package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/testutil"
	"github.com/google/uuid"
)

// =============================================================================
// Message Envelope Tests
// =============================================================================

func TestNewMessage(t *testing.T) {
	// Test with nil data
	msg, err := NewMessage(OpIdentify, nil)
	testutil.AssertNoError(t, err)
	testutil.AssertNotNil(t, msg)
	testutil.AssertEqual(t, OpIdentify, msg.Op)
	// Data should be nil or empty when created with nil
	testutil.AssertTrue(t, msg.Data == nil || len(msg.Data) == 0, "data should be nil or empty")
}

func TestNewMessageWithData(t *testing.T) {
	data := SendMessagePayload{
		ChannelID: uuid.New(),
		Content:   "Hello, world!",
	}

	msg, err := NewMessage(OpSendMessage, data)
	testutil.AssertNoError(t, err)
	testutil.AssertNotNil(t, msg)
	testutil.AssertEqual(t, OpSendMessage, msg.Op)
	testutil.AssertNotNil(t, msg.Data)

	// Verify data can be unmarshaled
	var decoded SendMessagePayload
	err = json.Unmarshal(msg.Data, &decoded)
	testutil.AssertNoError(t, err)
	testutil.AssertEqualUUID(t, data.ChannelID, decoded.ChannelID)
	testutil.AssertEqual(t, data.Content, decoded.Content)
}

func TestNewDispatch(t *testing.T) {
	data := map[string]string{"test": "data"}
	seq := int64(42)

	msg, err := NewDispatch(EventMessageCreate, seq, data)
	testutil.AssertNoError(t, err)
	testutil.AssertNotNil(t, msg)
	testutil.AssertEqual(t, OpDispatch, msg.Op)
	testutil.AssertEqual(t, EventMessageCreate, msg.Type)
	testutil.AssertNotNil(t, msg.Seq)
	testutil.AssertEqual(t, seq, *msg.Seq)
}

func TestMessageJSONRoundtrip(t *testing.T) {
	original := &Message{
		Op:   OpSendMessage,
		Type: EventMessageCreate,
	}
	seq := int64(123)
	original.Seq = &seq

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	testutil.AssertNoError(t, err)

	// Unmarshal back
	var decoded Message
	err = json.Unmarshal(jsonData, &decoded)
	testutil.AssertNoError(t, err)

	// Verify fields
	testutil.AssertEqual(t, original.Op, decoded.Op)
	testutil.AssertEqual(t, original.Type, decoded.Type)
	testutil.AssertNotNil(t, decoded.Seq)
	testutil.AssertEqual(t, *original.Seq, *decoded.Seq)
}

func TestOpCodeSerialization(t *testing.T) {
	tests := []struct {
		name   string
		opCode OpCode
	}{
		{"OpIdentify", OpIdentify},
		{"OpHeartbeat", OpHeartbeat},
		{"OpSendMessage", OpSendMessage},
		{"OpDispatch", OpDispatch},
		{"OpReady", OpReady},
		{"OpChannelCreate", OpChannelCreate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{Op: tt.opCode}

			// Marshal and unmarshal
			data, err := json.Marshal(msg)
			testutil.AssertNoError(t, err)

			var decoded Message
			err = json.Unmarshal(data, &decoded)
			testutil.AssertNoError(t, err)

			testutil.AssertEqual(t, tt.opCode, decoded.Op)
		})
	}
}

func TestEventTypeSerialization(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
	}{
		{"EventReady", EventReady},
		{"EventMessageCreate", EventMessageCreate},
		{"EventChannelCreate", EventChannelCreate},
		{"EventPresenceUpdate", EventPresenceUpdate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				Op:   OpDispatch,
				Type: tt.eventType,
			}

			// Marshal and unmarshal
			data, err := json.Marshal(msg)
			testutil.AssertNoError(t, err)

			var decoded Message
			err = json.Unmarshal(data, &decoded)
			testutil.AssertNoError(t, err)

			testutil.AssertEqual(t, tt.eventType, decoded.Type)
		})
	}
}

func TestMessageWithNilSequence(t *testing.T) {
	msg := &Message{
		Op: OpHeartbeat,
	}

	// Marshal and unmarshal
	data, err := json.Marshal(msg)
	testutil.AssertNoError(t, err)

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	// Check Seq is nil
	testutil.AssertTrue(t, decoded.Seq == nil, "sequence should be nil")
}

// =============================================================================
// Payload Marshaling Tests
// =============================================================================

func TestIdentifyPayloadMarshal(t *testing.T) {
	payload := IdentifyPayload{
		Token: "test_token_123",
		Properties: ConnectionProperties{
			OS:      "linux",
			Browser: "concord-cli",
			Device:  "terminal",
		},
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded IdentifyPayload
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqual(t, payload.Token, decoded.Token)
	testutil.AssertEqual(t, payload.Properties.OS, decoded.Properties.OS)
}

func TestSendMessagePayloadMarshal(t *testing.T) {
	channelID := uuid.New()
	replyID := uuid.New()

	payload := SendMessagePayload{
		ChannelID: channelID,
		Content:   "Hello, world!",
		ReplyToID: &replyID,
		Nonce:     "nonce123",
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded SendMessagePayload
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ChannelID, decoded.ChannelID)
	testutil.AssertEqual(t, payload.Content, decoded.Content)
	testutil.AssertNotNil(t, decoded.ReplyToID)
	testutil.AssertEqualUUID(t, *payload.ReplyToID, *decoded.ReplyToID)
	testutil.AssertEqual(t, payload.Nonce, decoded.Nonce)
}

func TestEditMessagePayloadMarshal(t *testing.T) {
	payload := EditMessagePayload{
		MessageID: uuid.New(),
		ChannelID: uuid.New(),
		Content:   "Edited content",
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded EditMessagePayload
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.MessageID, decoded.MessageID)
	testutil.AssertEqualUUID(t, payload.ChannelID, decoded.ChannelID)
	testutil.AssertEqual(t, payload.Content, decoded.Content)
}

func TestPresenceUpdatePayloadMarshal(t *testing.T) {
	payload := PresenceUpdatePayload{
		Status:     models.StatusDND,
		StatusText: "Working on tests",
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded PresenceUpdatePayload
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqual(t, payload.Status, decoded.Status)
	testutil.AssertEqual(t, payload.StatusText, decoded.StatusText)
}

func TestChannelCreateRequestMarshal(t *testing.T) {
	categoryID := uuid.New()

	payload := ChannelCreateRequest{
		ServerID:   uuid.New(),
		Name:       "new-channel",
		Type:       models.ChannelTypeText,
		CategoryID: &categoryID,
		Position:   5,
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded ChannelCreateRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ServerID, decoded.ServerID)
	testutil.AssertEqual(t, payload.Name, decoded.Name)
	testutil.AssertEqual(t, payload.Type, decoded.Type)
	testutil.AssertNotNil(t, decoded.CategoryID)
	testutil.AssertEqualUUID(t, *payload.CategoryID, *decoded.CategoryID)
}

func TestChannelUpdateRequestMarshal(t *testing.T) {
	name := "renamed-channel"
	sortOrder := 10

	payload := ChannelUpdateRequest{
		ServerID:  uuid.New(),
		ChannelID: uuid.New(),
		Name:      &name,
		SortOrder: &sortOrder,
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded ChannelUpdateRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ServerID, decoded.ServerID)
	testutil.AssertEqualUUID(t, payload.ChannelID, decoded.ChannelID)
	testutil.AssertNotNil(t, decoded.Name)
	testutil.AssertEqual(t, *payload.Name, *decoded.Name)
	testutil.AssertNotNil(t, decoded.SortOrder)
	testutil.AssertEqual(t, *payload.SortOrder, *decoded.SortOrder)
}

func TestMessageHistoryRequestMarshal(t *testing.T) {
	beforeID := uuid.New()

	payload := MessageHistoryRequest{
		ChannelID: uuid.New(),
		Limit:     50,
		Before:    &beforeID,
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded MessageHistoryRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ChannelID, decoded.ChannelID)
	testutil.AssertEqual(t, payload.Limit, decoded.Limit)
	testutil.AssertNotNil(t, decoded.Before)
	testutil.AssertEqualUUID(t, *payload.Before, *decoded.Before)
}

func TestRoleAssignRequestMarshal(t *testing.T) {
	payload := RoleAssignRequest{
		ServerID: uuid.New(),
		UserID:   uuid.New(),
		RoleName: "Moderator",
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded RoleAssignRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ServerID, decoded.ServerID)
	testutil.AssertEqualUUID(t, payload.UserID, decoded.UserID)
	testutil.AssertEqual(t, payload.RoleName, decoded.RoleName)
}

func TestKickMemberRequestMarshal(t *testing.T) {
	payload := KickMemberRequest{
		ServerID:  uuid.New(),
		ChannelID: uuid.New(),
		UserID:    uuid.New(),
		Reason:    "Violation of rules",
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded KickMemberRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ServerID, decoded.ServerID)
	testutil.AssertEqualUUID(t, payload.UserID, decoded.UserID)
	testutil.AssertEqual(t, payload.Reason, decoded.Reason)
}

func TestMuteMemberRequestMarshal(t *testing.T) {
	payload := MuteMemberRequest{
		ServerID:  uuid.New(),
		ChannelID: uuid.New(),
		UserID:    uuid.New(),
		Mute:      true,
		Duration:  60,
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded MuteMemberRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ServerID, decoded.ServerID)
	testutil.AssertTrue(t, decoded.Mute, "should be muted")
	testutil.AssertEqual(t, payload.Duration, decoded.Duration)
}

func TestWhisperPayloadMarshal(t *testing.T) {
	payload := WhisperPayload{
		TargetUserID: uuid.New(),
		ChannelID:    uuid.New(),
		Content:      "Secret message",
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded WhisperPayload
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.TargetUserID, decoded.TargetUserID)
	testutil.AssertEqualUUID(t, payload.ChannelID, decoded.ChannelID)
	testutil.AssertEqual(t, payload.Content, decoded.Content)
}

func TestCreateRoleRequestMarshal(t *testing.T) {
	displayOrder := 5

	payload := CreateRoleRequest{
		ServerID:      uuid.New(),
		ChannelID:     uuid.New(),
		Name:          "Custom Role",
		Permissions:   uint64(models.PermissionsModerator),
		Color:         0xFF0000,
		DisplayOrder:  &displayOrder,
		IsHoisted:     true,
		IsMentionable: true,
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded CreateRoleRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ServerID, decoded.ServerID)
	testutil.AssertEqual(t, payload.Name, decoded.Name)
	testutil.AssertEqual(t, payload.Permissions, decoded.Permissions)
	testutil.AssertEqual(t, payload.Color, decoded.Color)
	testutil.AssertNotNil(t, decoded.DisplayOrder)
	testutil.AssertEqual(t, *payload.DisplayOrder, *decoded.DisplayOrder)
	testutil.AssertTrue(t, decoded.IsHoisted, "should be hoisted")
	testutil.AssertTrue(t, decoded.IsMentionable, "should be mentionable")
}

func TestPinMessageRequestMarshal(t *testing.T) {
	payload := PinMessageRequest{
		ChannelID: uuid.New(),
		MessageID: uuid.New(),
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded PinMessageRequest
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqualUUID(t, payload.ChannelID, decoded.ChannelID)
	testutil.AssertEqualUUID(t, payload.MessageID, decoded.MessageID)
}

// =============================================================================
// Payload Marshaling - Table-Driven Tests
// =============================================================================

func TestAllPayloadsMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		payload interface{}
	}{
		{"HeartbeatPayload", HeartbeatPayload{LastSequence: nil}},
		{"TypingStartPayload", TypingStartPayload{ChannelID: uuid.New()}},
		{"ChannelDeleteRequest", ChannelDeleteRequest{ServerID: uuid.New(), ChannelID: uuid.New()}},
		{"DeleteMessagePayload", DeleteMessagePayload{MessageID: uuid.New(), ChannelID: uuid.New()}},
		{"RoleRemoveRequest", RoleRemoveRequest{ServerID: uuid.New(), UserID: uuid.New(), RoleName: "test"}},
		{"UnpinMessageRequest", UnpinMessageRequest{ChannelID: uuid.New(), MessageID: uuid.New()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.payload)
			testutil.AssertNoError(t, err)
			testutil.AssertTrue(t, len(data) > 0, "marshaled data should not be empty")

			// Unmarshal back to verify it's valid JSON
			var decoded map[string]interface{}
			err = json.Unmarshal(data, &decoded)
			testutil.AssertNoError(t, err)
		})
	}
}

// =============================================================================
// Event Data Serialization Tests
// =============================================================================

func TestWhisperCreatePayloadSerialization(t *testing.T) {
	fromUser := testutil.NewTestUser("sender")
	toUser := testutil.NewTestUser("recipient")

	payload := WhisperCreatePayload{
		FromUser:  fromUser,
		ToUser:    toUser,
		ChannelID: uuid.New(),
		Content:   "Whisper message",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	var decoded WhisperCreatePayload
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertNotNil(t, decoded.FromUser)
	testutil.AssertNotNil(t, decoded.ToUser)
	testutil.AssertEqual(t, payload.Content, decoded.Content)
	testutil.AssertEqualUUID(t, fromUser.ID, decoded.FromUser.ID)
	testutil.AssertEqualUUID(t, toUser.ID, decoded.ToUser.ID)
}

func TestConnectionPropertiesSerialization(t *testing.T) {
	props := ConnectionProperties{
		OS:      "windows",
		Browser: "concord-tui",
		Device:  "desktop",
	}

	data, err := json.Marshal(props)
	testutil.AssertNoError(t, err)

	var decoded ConnectionProperties
	err = json.Unmarshal(data, &decoded)
	testutil.AssertNoError(t, err)

	testutil.AssertEqual(t, props.OS, decoded.OS)
	testutil.AssertEqual(t, props.Browser, decoded.Browser)
	testutil.AssertEqual(t, props.Device, decoded.Device)
}

func TestEmptyPayloadSerialization(t *testing.T) {
	// Test that empty structs can be marshaled
	payloads := []interface{}{
		HeartbeatPayload{},
		TypingStartPayload{},
		PresenceUpdatePayload{},
	}

	for _, payload := range payloads {
		data, err := json.Marshal(payload)
		testutil.AssertNoError(t, err)
		testutil.AssertTrue(t, len(data) > 0, "empty payload should marshal to valid JSON")
	}
}

func TestOptionalFieldsOmitted(t *testing.T) {
	// Test that optional fields are omitted when nil/empty
	payload := SendMessagePayload{
		ChannelID: uuid.New(),
		Content:   "Test",
		// ReplyToID and Nonce are omitted
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	// Verify reply_to_id and nonce are not in JSON
	jsonStr := string(data)
	testutil.AssertFalse(t, containsString(jsonStr, "reply_to_id"), "reply_to_id should be omitted")
	testutil.AssertFalse(t, containsString(jsonStr, "nonce"), "nonce should be omitted when empty")
}

func TestPayloadWithAllOptionalFields(t *testing.T) {
	// Test that optional fields are included when present
	replyID := uuid.New()
	payload := SendMessagePayload{
		ChannelID: uuid.New(),
		Content:   "Test",
		ReplyToID: &replyID,
		Nonce:     "test-nonce",
	}

	data, err := json.Marshal(payload)
	testutil.AssertNoError(t, err)

	// Verify fields are in JSON
	jsonStr := string(data)
	testutil.AssertTrue(t, containsString(jsonStr, "reply_to_id"), "reply_to_id should be present")
	testutil.AssertTrue(t, containsString(jsonStr, "nonce"), "nonce should be present")
}

// Helper function to check if a string contains a substring
func containsString(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
