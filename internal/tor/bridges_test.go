package tor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuiltinBridges(t *testing.T) {
	b := Builtin("")
	if len(b[BridgesSnowflake]) == 0 || len(b[BridgesObfs4]) == 0 {
		t.Fatalf("built-in bridges missing: %v", b)
	}
	for kind, lines := range b {
		for _, l := range lines {
			if !strings.HasPrefix(l, kind+" ") {
				t.Errorf("%s line %q", kind, l)
			}
		}
	}
	// A newer cached copy wins.
	cache := filepath.Join(t.TempDir(), "bridges.json")
	os.WriteFile(cache, []byte(`{"snowflake":["snowflake 192.0.2.9:80 AAAA"]}`), 0o600)
	if got := Builtin(cache)[BridgesSnowflake]; len(got) != 1 || !strings.Contains(got[0], "192.0.2.9") {
		t.Fatalf("cache ignored: %v", got)
	}
}

func TestParseBridgeLines(t *testing.T) {
	got := ParseBridgeLines("# from the bot\r\nBridge obfs4 1.2.3.4:443 FP cert=x iat-mode=0\n\n  obfs4 5.6.7.8:80 FP2 cert=y iat-mode=1  \n")
	if len(got) != 2 || got[0] != "obfs4 1.2.3.4:443 FP cert=x iat-mode=0" || got[1] != "obfs4 5.6.7.8:80 FP2 cert=y iat-mode=1" {
		t.Fatalf("%q", got)
	}
}

func TestBridgeSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a stand-in program on the PATH")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "snowflake-client"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "lyrebird"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", dir)

	lines, err := BridgeSettings(BridgesSnowflake, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "UseBridges 1") || !strings.Contains(joined, "ClientTransportPlugin snowflake exec "+filepath.Join(dir, "snowflake-client")) || !strings.Contains(joined, "Bridge snowflake ") {
		t.Fatalf("snowflake:\n%s", joined)
	}
	lines, err = BridgeSettings(BridgesCustom, []string{"obfs4 1.2.3.4:443 FP cert=x iat-mode=0", "5.6.7.8:9001"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.Join(lines, "\n"), "ClientTransportPlugin") != 1 {
		t.Fatalf("custom:\n%s", strings.Join(lines, "\n"))
	}
	if none, err := BridgeSettings(BridgesNone, nil, "", ""); err != nil || none != nil {
		t.Fatal("no bridges isn't empty")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := FindTransport(BridgesObfs4, ""); err == nil {
		return // installed system-wide on this machine
	}
	if _, err := BridgeSettings(BridgesObfs4, nil, "", ""); err == nil || !strings.Contains(err.Error(), "lyrebird or obfs4proxy") {
		t.Fatalf("missing transport: %v", err)
	}
}
