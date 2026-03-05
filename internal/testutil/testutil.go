// Package testutil provides common utilities and helpers for testing Concord.
//
// This package offers assertion helpers and builder functions to simplify
// test setup across the codebase. Database-specific test utilities should
// be defined in the database package's test files to avoid import cycles.
package testutil

import (
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// NewTestUser creates a user with a random UUID suitable for testing.
// The user's email is set to username@test.com and discriminator is auto-generated.
func NewTestUser(username string) *models.User {
	user := models.NewUser(username, username+"@test.com")
	user.Status = models.StatusOnline
	return user
}

// NewTestServer creates a server with a random UUID suitable for testing.
func NewTestServer(ownerID uuid.UUID, name string) *models.Server {
	return models.NewServer(name, ownerID)
}

// NewTestChannel creates a text channel with a random UUID suitable for testing.
func NewTestChannel(serverID uuid.UUID, name string) *models.Channel {
	return models.NewTextChannel(serverID, name)
}

// NewTestRole creates a role with default text permissions suitable for testing.
func NewTestRole(serverID uuid.UUID, name string) *models.Role {
	return models.NewRole(serverID, name)
}

// NewTestMessage creates a message suitable for testing.
func NewTestMessage(channelID, authorID uuid.UUID, content string) *models.Message {
	return &models.Message{
		ID:        uuid.New(),
		ChannelID: channelID,
		AuthorID:  authorID,
		Content:   content,
		CreatedAt: time.Now(),
	}
}

// AssertNoError fails the test if err is not nil.
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertError fails the test if err is nil.
func AssertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// AssertEqual fails the test if expected != actual.
func AssertEqual(t *testing.T, expected, actual interface{}) {
	t.Helper()
	if expected != actual {
		t.Errorf("expected %v, got %v", expected, actual)
	}
}

// AssertEqualUUID fails the test if the UUIDs are not equal.
func AssertEqualUUID(t *testing.T, expected, actual uuid.UUID) {
	t.Helper()
	if expected != actual {
		t.Errorf("expected UUID %v, got %v", expected, actual)
	}
}

// AssertNotNil fails the test if value is nil.
func AssertNotNil(t *testing.T, value interface{}) {
	t.Helper()
	if value == nil {
		t.Fatal("expected non-nil value, got nil")
	}
}

// AssertNil fails the test if value is not nil.
func AssertNil(t *testing.T, value interface{}) {
	t.Helper()
	if value != nil {
		t.Errorf("expected nil, got %v", value)
	}
}

// AssertTrue fails the test if condition is false.
func AssertTrue(t *testing.T, condition bool, message string) {
	t.Helper()
	if !condition {
		t.Error(message)
	}
}

// AssertFalse fails the test if condition is true.
func AssertFalse(t *testing.T, condition bool, message string) {
	t.Helper()
	if condition {
		t.Error(message)
	}
}

// AssertContains fails the test if haystack doesn't contain needle.
func AssertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if len(needle) == 0 {
		return
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return
		}
	}
	t.Errorf("expected %q to contain %q", haystack, needle)
}

// AssertLen fails the test if the slice length doesn't match expected.
func AssertLen(t *testing.T, slice interface{}, expectedLen int) {
	t.Helper()
	switch v := slice.(type) {
	case []models.User:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	case []models.Channel:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	case []models.Message:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	case []models.Role:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	case []*models.User:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	case []*models.Channel:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	case []*models.Message:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	case []*models.Role:
		if len(v) != expectedLen {
			t.Errorf("expected length %d, got %d", expectedLen, len(v))
		}
	default:
		t.Errorf("AssertLen: unsupported type %T", slice)
	}
}
