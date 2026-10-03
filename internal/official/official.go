// Package official names the project's own Concord server and Grapevine
// hub: the ones the installer offers to join and register with, and the
// hub every client and server falls back to.
//
// They're placeholders until the official VPS is up (vault: To Do G, "the
// official server and hub"); change them here and nowhere else.
package official

const (
	// ServerName is how the official server appears in a client's list.
	ServerName = "Concord"
	// ServerHost and ServerPort reach the official server (TLS, behind
	// Cloudflare).
	ServerHost = "concord.chat"
	ServerPort = 443
	ServerTLS  = true

	// HubURL is the official Grapevine hub.
	HubURL  = "https://grapevine.concord.chat"
	HubName = "Official Grapevine Hub"
)
