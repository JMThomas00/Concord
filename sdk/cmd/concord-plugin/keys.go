package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"
	"regexp"

	"github.com/JMThomas00/Concord/sdk/codesign"
)

// Publisher keys sign a plugin's client code (client/*.wasm). Concord only
// runs code whose signature matches the plugin.toml publisher_key, and
// members' clients remember the key they agreed to.

const defaultKeyFile = "publisher.key"

// keygen makes a publisher key pair.
func keygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", defaultKeyFile, "where to write the private key")
	_ = fs.Parse(args)
	if _, err := os.Stat(*out); err == nil {
		fail("%s already exists; a plugin keeps one key for good (members' clients remember it)", *out)
	}
	public, private, err := codesign.GenerateKey()
	if err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(*out, private, 0o600); err != nil {
		fail("%v", err)
	}
	pub, _ := codesign.ParsePublicKey(public)
	fmt.Printf(`Wrote the private key to %s. Keep it secret and keep it safe:
  - never commit it (add %s to .gitignore);
  - back it up: code signed with a new key makes every member agree again;
  - for releases from CI, store its contents as a secret named
    CONCORD_PUBLISHER_KEY (concord-plugin sign reads that too).

Add the public key to plugin.toml:

[client]
publisher_key = "%s"

Fingerprint (what members see): %s
`, *out, *out, public, codesign.Fingerprint(pub))
}

var publisherKeyLine = regexp.MustCompile(`(?m)^\s*publisher_key\s*=\s*"([^"]+)"`)

// sign writes <module>.sig for each module given.
func sign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("key", defaultKeyFile, "the private key file (or set CONCORD_PUBLISHER_KEY to its contents)")
	_ = fs.Parse(reorder(args))
	if fs.NArg() == 0 {
		fail("usage: concord-plugin sign [--key publisher.key] client/plugin.wasm ...")
	}
	keyData := []byte(os.Getenv("CONCORD_PUBLISHER_KEY"))
	if len(keyData) == 0 {
		var err error
		if keyData, err = os.ReadFile(*keyPath); err != nil {
			fail("no key: %v (make one with concord-plugin keygen)", err)
		}
	}
	priv, err := codesign.ParsePrivateKey(keyData)
	if err != nil {
		fail("%v", err)
	}
	public := codesign.FormatPublicKey(priv.Public().(ed25519.PublicKey))
	// Catch the commonest mistake: a key that isn't the one plugin.toml names.
	if manifest, err := os.ReadFile("plugin.toml"); err == nil {
		if m := publisherKeyLine.FindSubmatch(manifest); m != nil && string(m[1]) != public {
			fail("this key isn't plugin.toml's publisher_key, so Concord would refuse the code.\n  plugin.toml: %s\n  this key:    %s", m[1], public)
		}
	}
	for _, path := range fs.Args() {
		module, err := os.ReadFile(path)
		if err != nil {
			fail("%v", err)
		}
		sig := codesign.Sign(priv, module)
		if err := os.WriteFile(path+".sig", sig, 0o644); err != nil {
			fail("%v", err)
		}
		fmt.Printf("signed %s → %s.sig\n", path, path)
	}
}
