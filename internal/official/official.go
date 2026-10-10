// Package official names the project's own Concord server and Grapevine
// hub: the ones the installer offers to join and register with, and the
// hub every client and server falls back to.
//
// The domain is concordchat.cc (Cloudflare); the server and hub reach the
// internet through a Cloudflare Tunnel (vault: "Concord - Official Server
// and Hub Setup"). Change them here and nowhere else.
package official

const (
	// ServerName is how the official server appears in a client's list.
	ServerName = "Concord"
	// ServerHost and ServerPort reach the official server (TLS, behind
	// Cloudflare).
	ServerHost = "server.concordchat.cc"
	ServerPort = 443
	ServerTLS  = true

	// HubURL is the official Grapevine hub.
	HubURL  = "https://grapevine.concordchat.cc"
	HubName = "Official Grapevine Hub"
)
