// Package codesign is how a plugin's client code is signed and checked.
//
// A plugin that ships WebAssembly for members' clients names a publisher
// key in plugin.toml ([client] publisher_key = "ed25519:<base64>") and
// signs the module with the matching private key, putting the signature
// next to it as <module>.sig. Concord's server refuses to advertise code
// whose signature doesn't check out, and every client checks it again
// before running anything. Clients remember which key they agreed to run
// a plugin's code from, so new code signed by anyone else asks again.
//
// Create a key with `concord-plugin keygen` and sign with
// `concord-plugin sign client/plugin.wasm`.
package codesign

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// PublicKeyPrefix starts a publisher key in plugin.toml.
const PublicKeyPrefix = "ed25519:"

// privateKeyPrefix starts a private key file.
const privateKeyPrefix = "concord-publisher-key:"

// ParsePublicKey decodes "ed25519:<base64 of 32 bytes>".
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b64, ok := strings.CutPrefix(strings.TrimSpace(s), PublicKeyPrefix)
	if !ok {
		return nil, errors.New("must start with " + PublicKeyPrefix)
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("not a base64 ed25519 public key")
	}
	return ed25519.PublicKey(key), nil
}

// FormatPublicKey is the plugin.toml form of a public key.
func FormatPublicKey(pub ed25519.PublicKey) string {
	return PublicKeyPrefix + base64.StdEncoding.EncodeToString(pub)
}

// Fingerprint is a short, readable form of a key for people to compare:
// the first 16 bytes of its SHA-256 in groups of four hex digits.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	h := hex.EncodeToString(sum[:16])
	var parts []string
	for i := 0; i < len(h); i += 4 {
		parts = append(parts, h[i:i+4])
	}
	return strings.Join(parts, " ")
}

// GenerateKey makes a new publisher key pair: the plugin.toml public key,
// and the contents of a private key file to keep secret.
func GenerateKey() (public string, privateFile []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, err
	}
	return FormatPublicKey(pub), []byte(privateKeyPrefix + base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"), nil
}

// ParsePrivateKey reads a private key file made by GenerateKey.
func ParsePrivateKey(file []byte) (ed25519.PrivateKey, error) {
	b64, ok := strings.CutPrefix(strings.TrimSpace(string(file)), privateKeyPrefix)
	if !ok {
		return nil, errors.New("not a Concord publisher key file")
	}
	seed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("damaged publisher key file")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Sign returns the .sig file contents for module: its ed25519 signature,
// base64, on one line.
func Sign(priv ed25519.PrivateKey, module []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, module)) + "\n")
}

// Verify checks a .sig file against module and the publisher key from
// plugin.toml.
func Verify(publisherKey string, module, sigFile []byte) error {
	pub, err := ParsePublicKey(publisherKey)
	if err != nil {
		return fmt.Errorf("publisher key: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(sigFile)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("the .sig file isn't an ed25519 signature")
	}
	if !ed25519.Verify(pub, module, sig) {
		return errors.New("the signature doesn't match the code and publisher key")
	}
	return nil
}
