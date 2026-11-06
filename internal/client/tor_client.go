package client

// The app's side of Tor (tor_net.go has the network): keeping each server's
// onion address and access key, and Tor mode's rules.

import (
	"strings"

	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/tor"
)

// takeOnionAccess keeps a server's onion address and access key (sent with
// SERVER_CREATE, and again as ONION_ACCESS when an administrator makes a new
// key), so this client can reach it over Tor.
func (a *App) takeOnionAccess(sc *ServerConnection, oa *protocol.OnionAccess) {
	if sc == nil || oa == nil || sc.ServerInfo == nil || oa.Address == "" {
		return
	}
	info := sc.ServerInfo
	if info.Onion == oa.Address && info.OnionKey == oa.Key {
		return
	}
	info.Onion, info.OnionKey = oa.Address, oa.Key
	if oa.Only {
		info.OverTor = true // the normal address won't answer
	}
	info.rememberKey()
	if a.configMgr != nil {
		_ = a.configMgr.UpdateServer(info)
	}
}

// voiceOffOverTor says why voice is off for a server reached over Tor, or
// "" if it isn't.
func voiceOffOverTor(sc *ServerConnection) string {
	if sc == nil || sc.ServerInfo == nil || !sc.ServerInfo.overTor() {
		return ""
	}
	return "Voice is off over Tor: calls connect computers directly, which would show your address"
}

// splitTorInvite takes an invite's access key off the end
// (host.onion:port/CODE#KEY), returning the rest and the key.
func splitTorInvite(s string) (rest, key string) {
	s = strings.TrimSpace(s)
	i := strings.LastIndex(s, "#")
	if i < 0 {
		return s, ""
	}
	if _, err := tor.ParseClientKey(s[i+1:]); err != nil {
		return s[:i], ""
	}
	return s[:i], strings.ToUpper(strings.TrimSpace(s[i+1:]))
}

// toggleOverTor switches a server between its normal address and its onion
// address (Manage Servers › O); it applies when it next connects.
func (a *App) toggleOverTor(info *ClientServerInfo) {
	switch {
	case info.Onion == "":
		a.statusMessage = "This server has no onion address (its admin can turn on [tor])"
		return
	case info.Address == "" || tor.IsOnion(info.Address):
		a.statusMessage = "This server is only reachable over Tor"
		return
	case torOnly():
		a.statusMessage = "Everything goes through Tor (Settings › Network), so this server uses its onion address"
		return
	}
	info.OverTor = !info.OverTor
	if a.configMgr != nil {
		_ = a.configMgr.UpdateServer(info)
	}
	if info.OverTor {
		a.statusMessage = info.Name + " connects over Tor from its next connection (voice off, files through the server)"
	} else {
		a.statusMessage = info.Name + " connects directly from its next connection"
	}
}

// shortOnion shortens an onion address for lists: abcdef…xyz.onion:port.
func shortOnion(hostPort string) string {
	host, port := hostPort, ""
	if i := strings.LastIndex(hostPort, ":"); i > 0 {
		host, port = hostPort[:i], hostPort[i:]
	}
	if h := strings.TrimSuffix(host, ".onion"); len(h) > 14 && h != host {
		host = h[:8] + "…" + h[len(h)-4:] + ".onion"
	}
	return host + port
}
