package protocol

// OnionAccess is how a member reaches a server over Tor (Privacy Pack 1):
// its onion address and, when the server uses client authorization, the
// access key without which Tor can't find it. Sent to members in
// SERVER_CREATE and again (EventOnionAccess) when an administrator makes a
// new key.
type OnionAccess struct {
	ServerID string `json:"server_id,omitempty"`
	Address  string `json:"address"`        // xxxx.onion:port
	Key      string `json:"key,omitempty"`  // the x25519 private key, base32
	Only     bool   `json:"only,omitempty"` // the server is reachable only over Tor
}

// EventOnionAccess carries a new OnionAccess to everyone online.
const EventOnionAccess EventType = "ONION_ACCESS"
