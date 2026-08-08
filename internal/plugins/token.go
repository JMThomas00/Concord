package plugins

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// IssueToken generates a new random plugin auth token, returning both the
// plaintext (handed to the plugin process once, over its environment — never
// persisted) and its SHA-256 hash (the only form stored in the database,
// mirroring how Concord's own session tokens are hashed at rest).
func IssueToken() (plain string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("failed to generate plugin token: %w", err)
	}
	plain = hex.EncodeToString(buf)
	hash = HashToken(plain)
	return plain, hash, nil
}

// HashToken hashes a plugin token for lookup/storage.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
