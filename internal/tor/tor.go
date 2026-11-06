// Package tor runs an installed tor program for Concord (Privacy Pack 1,
// 2026-11-05): the server's onion service, and (later) the client's
// connection through Tor. Concord never ships tor itself; it runs the one
// installed on the machine (the installer offers to add it), with a
// settings file of its own in a folder of its own, so it never touches a
// system tor's configuration.
//
// Client authorization: an onion service can be told to publish its
// descriptor encrypted to a list of x25519 keys, so only someone holding
// one of the private keys can find or reach it at all. Knowing the .onion
// address alone isn't enough. Concord puts the private key in its invites.
package tor

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// b32 is tor's base32: RFC 4648, upper case, no padding.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// ClientKey is one client authorization key (x25519).
type ClientKey struct {
	Private []byte // 32 bytes
	Public  []byte // 32 bytes
}

// NewClientKey makes a client authorization key.
func NewClientKey() (ClientKey, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return ClientKey{}, err
	}
	return ClientKey{Private: k.Bytes(), Public: k.PublicKey().Bytes()}, nil
}

// ParseClientKey reads a private key written by PrivateText (52 base32
// characters; case and surrounding spaces don't matter).
func ParseClientKey(text string) (ClientKey, error) {
	raw, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(text)))
	if err != nil || len(raw) != 32 {
		return ClientKey{}, errors.New("that isn't a Tor access key")
	}
	k, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return ClientKey{}, err
	}
	return ClientKey{Private: k.Bytes(), Public: k.PublicKey().Bytes()}, nil
}

// PrivateText is the private key as tor writes it (what invites carry).
func (k ClientKey) PrivateText() string { return b32.EncodeToString(k.Private) }

// PublicText is the public key as tor writes it.
func (k ClientKey) PublicText() string { return b32.EncodeToString(k.Public) }

// ServiceAuthLine is the service's authorized_clients/<name>.auth line.
func (k ClientKey) ServiceAuthLine() string { return "descriptor:x25519:" + k.PublicText() }

// ClientAuthLine is a client's ClientOnionAuthDir/<name>.auth_private line
// for the onion address (with or without ".onion").
func (k ClientKey) ClientAuthLine(onion string) string {
	return strings.TrimSuffix(strings.ToLower(onion), ".onion") + ":descriptor:x25519:" + k.PrivateText()
}

// IsOnion reports whether host is a v3 onion address.
func IsOnion(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".onion")
	if len(h) != 56 || !strings.HasSuffix(strings.ToLower(host), ".onion") {
		return false
	}
	_, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(h))
	return err == nil
}

// ErrNotInstalled: no tor program was found.
var ErrNotInstalled = errors.New("tor isn't installed (install it from your package manager or torproject.org, or set tor_path)")

// Find is the tor program to run: path if given, else tor on the PATH,
// else the usual places it's installed.
func Find(path string) (string, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("tor_path %s: %w", path, err)
		}
		return path, nil
	}
	if p, err := exec.LookPath("tor"); err == nil {
		return p, nil
	}
	var places []string
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base != "" {
				places = append(places, filepath.Join(base, "Tor", "tor.exe"), filepath.Join(base, "Tor", "tor", "tor.exe"))
			}
		}
		if home, err := os.UserHomeDir(); err == nil {
			places = append(places,
				filepath.Join(home, ".concord", "tor", "tor", "tor.exe"),
				filepath.Join(home, "Desktop", "Tor Browser", "Browser", "TorBrowser", "Tor", "tor.exe"))
		}
	case "darwin":
		places = []string{"/opt/homebrew/bin/tor", "/usr/local/bin/tor", "/Applications/Tor Browser.app/Contents/MacOS/Tor/tor"}
	default:
		places = []string{"/usr/bin/tor", "/usr/sbin/tor", "/usr/local/bin/tor"}
	}
	for _, p := range places {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", ErrNotInstalled
}
