package plugins

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestValidateValues(t *testing.T) {
	channel := uuid.New()
	fields := []ConfigField{
		{Key: "count", Type: "number"},
		{Key: "on", Type: "boolean"},
		{Key: "mode", Type: "select", Options: []string{"seats", "private"}},
		{Key: "activity", Type: "channel_select"},
		{Key: "api_key", Type: "secret", Required: true},
		{Key: "name", Type: "text", Required: true},
	}
	isText := func(id uuid.UUID) bool { return id == channel }
	stored := map[string]string{"api_key": "enc:v1:xyz", "name": "Bob"}

	good := map[string]string{"count": "3.5", "on": "true", "mode": "private", "activity": channel.String()}
	if p := ValidateValues(fields, good, stored, isText); len(p) != 0 {
		t.Fatalf("valid values rejected: %v", p)
	}

	bad := map[string]string{"count": "lots", "on": "yes", "mode": "hotseat", "activity": uuid.New().String(), "name": "", "bogus": "x"}
	p := ValidateValues(fields, bad, stored, isText)
	for _, key := range []string{"count", "on", "mode", "activity", "name", "bogus"} {
		if p[key] == "" {
			t.Errorf("%s: expected a problem, got none (%v)", key, p)
		}
	}
	if _, ok := p["api_key"]; ok {
		t.Error("an untouched, already-set secret shouldn't count as missing")
	}

	if p := ValidateValues(fields, map[string]string{"name": "x"}, map[string]string{}, isText); p["api_key"] != "required" {
		t.Errorf("unset required secret not reported: %v", p)
	}
}

func TestSecretBoxRoundTripAndKeyPersistence(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "plugin-secrets.key")
	box, err := LoadOrCreateSecretBox(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("sk-live-123")
	if err != nil || sealed == "sk-live-123" || sealed[:7] != "enc:v1:" {
		t.Fatalf("Seal = %q, %v", sealed, err)
	}
	again, err := LoadOrCreateSecretBox(keyPath) // same key on the next start
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := again.Open(sealed); err != nil || plain != "sk-live-123" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	if plain, _ := again.Open("legacy-plaintext"); plain != "legacy-plaintext" {
		t.Fatal("unsealed legacy values should pass through")
	}
	other, _ := LoadOrCreateSecretBox(filepath.Join(t.TempDir(), "k"))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("a different key must not open the value")
	}
}
