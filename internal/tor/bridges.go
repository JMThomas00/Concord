package tor

// Bridges: for places that block Tor. A bridge is an unlisted entry relay,
// reached through a pluggable transport that disguises the traffic:
// obfs4 (looks like random bytes; the lyrebird or obfs4proxy program) or
// Snowflake (looks like a video call, through volunteers' browsers; the
// snowflake-client program). Tor Browser's own built-in bridge lines ship
// with Concord (bridges_builtin.json, from the Tor Project's
// bridges.torproject.org/moat/circumvention/builtin on 2026-11-05) and are
// refreshed from there when it can be reached. People can also paste lines
// they got from bridges.torproject.org, @GetBridgesBot on Telegram, or by
// emailing bridges@torproject.org.

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Bridge kinds.
const (
	BridgesNone      = ""
	BridgesSnowflake = "snowflake"
	BridgesObfs4     = "obfs4"
	BridgesCustom    = "custom" // lines the person pasted
)

//go:embed bridges_builtin.json
var builtinJSON []byte

// BuiltinBridges are the built-in bridge lines by transport.
type BuiltinBridges map[string][]string

// Builtin is the built-in lines: from cache (a newer copy fetched before)
// if there is one, else those Concord shipped with.
func Builtin(cache string) BuiltinBridges {
	if cache != "" {
		if data, err := os.ReadFile(cache); err == nil {
			var b BuiltinBridges
			if json.Unmarshal(data, &b) == nil && len(b[BridgesSnowflake])+len(b[BridgesObfs4]) > 0 {
				return b
			}
		}
	}
	var b BuiltinBridges
	_ = json.Unmarshal(builtinJSON, &b)
	return b
}

// MoatBuiltinURL answers the built-in bridge lines Tor Browser uses.
const MoatBuiltinURL = "https://bridges.torproject.org/moat/circumvention/builtin"

// RefreshBuiltin fetches the current built-in lines with client (through
// Tor once it's connected, so the request doesn't stand out) and saves them
// to cache.
func RefreshBuiltin(ctx context.Context, client *http.Client, cache string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, MoatBuiltinURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/vnd.api+json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bridges.torproject.org answered %s", resp.Status)
	}
	var all map[string][]string
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&all); err != nil {
		return err
	}
	b := BuiltinBridges{}
	for _, kind := range []string{BridgesSnowflake, BridgesObfs4} {
		seen := map[string]bool{}
		for _, l := range all[kind] {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, kind+" ") && !seen[l] {
				seen[l] = true
				b[kind] = append(b[kind], l)
			}
		}
	}
	if len(b[BridgesSnowflake])+len(b[BridgesObfs4]) == 0 {
		return errors.New("bridges.torproject.org sent no bridges")
	}
	data, _ := json.MarshalIndent(b, "", " ")
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		return err
	}
	return os.WriteFile(cache, data, 0o600)
}

// ParseBridgeLines reads pasted bridge lines: one per line, with or without
// a leading "Bridge ", blank lines and comments skipped.
func ParseBridgeLines(text string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		l = strings.TrimSpace(strings.TrimPrefix(l, "Bridge "))
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// transportOf is a bridge line's transport ("" for a plain relay address).
func transportOf(line string) string {
	f := strings.Fields(line)
	if len(f) == 0 || strings.ContainsAny(f[0], ".:[") {
		return ""
	}
	return f[0]
}

// transportPrograms are the programs that speak each transport.
var transportPrograms = map[string][]string{
	BridgesObfs4:     {"lyrebird", "obfs4proxy"},
	BridgesSnowflake: {"snowflake-client"},
}

// FindTransport finds the program for a transport: on the PATH, the usual
// places, or a Tor Expert Bundle's pluggable_transports beside torPath.
func FindTransport(kind, torPath string) (string, error) {
	names := transportPrograms[kind]
	if len(names) == 0 {
		return "", fmt.Errorf("Concord doesn't know the %q transport", kind)
	}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	var dirs []string
	if torPath != "" {
		d := filepath.Dir(torPath)
		dirs = append(dirs, filepath.Join(d, "pluggable_transports"), filepath.Join(d, "..", "pluggable_transports"),
			filepath.Join(d, "PluggableTransports"), d)
	}
	dirs = append(dirs, "/usr/bin", "/usr/local/bin", "/usr/sbin", "/opt/homebrew/bin")
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
		for _, d := range dirs {
			p := filepath.Join(d, n+exe)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s bridges need %s, which isn't installed", kind, strings.Join(names, " or "))
}

// BridgeSettings are the torrc lines for bridges of kind (custom: lines),
// with the transports they need.
func BridgeSettings(kind string, lines []string, torPath, cache string) ([]string, error) {
	switch kind {
	case BridgesNone:
		return nil, nil
	case BridgesSnowflake, BridgesObfs4:
		lines = Builtin(cache)[kind]
		if len(lines) == 0 {
			return nil, fmt.Errorf("no built-in %s bridges", kind)
		}
	case BridgesCustom:
		if len(lines) == 0 {
			return nil, errors.New("paste at least one bridge line")
		}
	default:
		return nil, fmt.Errorf("unknown bridge kind %q", kind)
	}
	out := []string{"UseBridges 1"}
	plugged := map[string]bool{}
	for _, l := range lines {
		if t := transportOf(l); t != "" && !plugged[t] {
			prog, err := FindTransport(t, torPath)
			if err != nil {
				return nil, err
			}
			// tor splits this setting on spaces and can't quote, so a
			// program in a folder with a space in its name can't be used.
			if strings.ContainsAny(prog, " \t") {
				return nil, fmt.Errorf("tor can't run %s from a folder with a space in its name; move it, or put it on the PATH", prog)
			}
			out = append(out, "ClientTransportPlugin "+t+" exec "+prog)
			plugged[t] = true
		}
	}
	for _, l := range lines {
		out = append(out, "Bridge "+l)
	}
	return out, nil
}
