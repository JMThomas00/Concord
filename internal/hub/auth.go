package hub

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateSecret returns a cryptographically random 32-byte hex string.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SignBody computes HMAC-SHA256(secret, body) and returns it as a lowercase hex string.
// This is the same computation a registered server performs when sending X-Grapevine-Sig.
func SignBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyRequest checks that sigHeader equals HMAC-SHA256(storedSecret, body).
// Uses constant-time comparison to prevent timing attacks.
func VerifyRequest(storedSecret string, body []byte, sigHeader string) bool {
	expected := SignBody(storedSecret, body)
	return hmac.Equal([]byte(expected), []byte(sigHeader))
}
