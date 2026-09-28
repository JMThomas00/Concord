package plugins

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// SecretBox encrypts plugin "secret" config values at rest (API keys and
// the like), so they aren't sitting readable in concord.db or its backups.
// The key lives in its own 0600 file next to the database; losing it means
// re-entering the secrets, nothing worse.
type SecretBox struct {
	aead cipher.AEAD
}

// sealedPrefix marks an encrypted value in the database. Values without it
// are treated as plaintext (e.g. set before a field became a secret) and
// get sealed the next time they're saved.
const sealedPrefix = "enc:v1:"

// LoadOrCreateSecretBox reads the 32-byte key at keyPath, creating it on
// first use.
func LoadOrCreateSecretBox(keyPath string) (*SecretBox, error) {
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyPath, key, 0o600); err != nil {
			return nil, fmt.Errorf("failed to write plugin secret key: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to read plugin secret key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("plugin secret key %s is %d bytes, want 32", keyPath, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

// Seal encrypts a value for storage. An empty value stays empty.
func (b *SecretBox) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return sealedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a stored value; a value that was never sealed is returned
// as-is.
func (b *SecretBox) Open(stored string) (string, error) {
	if !strings.HasPrefix(stored, sealedPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedPrefix))
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", fmt.Errorf("corrupt sealed value")
	}
	nonce, ct := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("sealed value can't be decrypted with this server's key")
	}
	return string(plain), nil
}
