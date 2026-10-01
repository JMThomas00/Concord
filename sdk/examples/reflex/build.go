//go:build ignore

// build builds Reflex: the client code (WebAssembly, signed with
// publisher.key or $CONCORD_PUBLISHER_KEY, whose public key it writes into
// plugin.toml) and the server half for this computer.
//
//	go run ../../cmd/concord-plugin keygen   # once
//	go run build.go
package main

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"

	"github.com/JMThomas00/Concord/sdk/codesign"
)

func main() {
	keyData := []byte(os.Getenv("CONCORD_PUBLISHER_KEY"))
	if len(keyData) == 0 {
		var err error
		if keyData, err = os.ReadFile("publisher.key"); err != nil {
			fail("no publisher.key: make one with concord-plugin keygen")
		}
	}
	priv, err := codesign.ParsePrivateKey(keyData)
	check(err)

	run(map[string]string{"GOOS": "wasip1", "GOARCH": "wasm"}, "go", "build", "-o", "client/plugin.wasm", "./clientcode")
	module, err := os.ReadFile("client/plugin.wasm")
	check(err)
	check(os.WriteFile("client/plugin.wasm.sig", codesign.Sign(priv, module), 0o644))

	public := codesign.FormatPublicKey(priv.Public().(ed25519.PublicKey))
	manifest, err := os.ReadFile("plugin.toml")
	check(err)
	manifest = regexp.MustCompile(`(?m)^publisher_key = "[^"]*"`).ReplaceAll(manifest, []byte(`publisher_key = "`+public+`"`))
	check(os.WriteFile("plugin.toml", manifest, 0o644))

	bin := "reflex"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	run(map[string]string{"CGO_ENABLED": "0"}, "go", "build", "-o", bin, ".")
	fmt.Println("built client/plugin.wasm (signed) and", bin)
}

func run(env map[string]string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	check(cmd.Run())
}

func check(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "build: "+format+"\n", args...)
	os.Exit(1)
}
