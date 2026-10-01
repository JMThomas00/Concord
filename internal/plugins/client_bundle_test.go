package plugins

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientDefValidation(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	key := "ed25519:" + base64.StdEncoding.EncodeToString(pub)
	for _, c := range []struct {
		def  ClientDef
		want string // "" = valid
	}{
		{ClientDef{}, ""},
		{ClientDef{Capabilities: []string{"images", "sound"}}, ""},
		{ClientDef{Capabilities: []string{"filesystem"}}, "unknown [client] capability"},
		{ClientDef{WASM: "client/plugin.wasm", PublisherKey: key}, ""},
		{ClientDef{WASM: "client/plugin.wasm"}, "needs a publisher_key"},
		{ClientDef{WASM: "plugin.wasm", PublisherKey: key}, "must be a .wasm file in client/"},
		{ClientDef{PublisherKey: "rsa:abc"}, "must start with ed25519:"},
		{ClientDef{PublisherKey: "ed25519:c2hvcnQ="}, "not a base64 ed25519 public key"},
	} {
		err := c.def.validate("p")
		if (err == nil) != (c.want == "") || (err != nil && !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%+v: err %v, want %q", c.def, err, c.want)
		}
	}
}

func TestIndexClientBundle(t *testing.T) {
	dir := t.TempDir()
	if b, err := IndexClientBundle(dir); b != nil || err != nil {
		t.Fatalf("no client folder: %v %v", b, err)
	}
	write := func(rel string, size int) {
		p := filepath.Join(dir, ClientDir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("b.wav", 10)
	write("a/x.png", 20)
	write(".git/config.json", 5)
	write("run.sh", 5)
	b, err := IndexClientBundle(dir)
	if err != nil || b == nil || len(b.Files) != 2 || b.Files[0].Path != "a/x.png" || b.Size != 30 {
		t.Fatalf("bundle %+v err %v", b, err)
	}
	first := b.Hash
	if again, _ := IndexClientBundle(dir); again.Hash != first {
		t.Fatal("hash isn't stable")
	}
	write("b.wav", 11)
	if changed, _ := IndexClientBundle(dir); changed.Hash == first {
		t.Fatal("hash didn't change with a file")
	}
	if ClientFilePath(dir, b, "../plugin.toml") != "" || ClientFilePath(dir, b, "run.sh") != "" || ClientFilePath(dir, b, "a/x.png") == "" {
		t.Fatal("ClientFilePath allowed a file outside the bundle")
	}

	write("big.wav", MaxClientBundleBytes)
	if _, err := IndexClientBundle(dir); err == nil {
		t.Fatal("a bundle over the cap was accepted")
	}
}
