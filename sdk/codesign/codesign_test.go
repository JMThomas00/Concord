package codesign

import (
	"strings"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	pub, privFile, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	priv, err := ParsePrivateKey(privFile)
	if err != nil {
		t.Fatal(err)
	}
	module := []byte("\x00asm fake module")
	sig := Sign(priv, module)
	if err := Verify(pub, module, sig); err != nil {
		t.Fatal(err)
	}
	if Verify(pub, append(module, 1), sig) == nil {
		t.Fatal("changed code verified")
	}
	other, _, _ := GenerateKey()
	if Verify(other, module, sig) == nil {
		t.Fatal("another key verified")
	}
	if Verify(pub, module, []byte("nonsense")) == nil {
		t.Fatal("a junk .sig verified")
	}

	key, _ := ParsePublicKey(pub)
	if fp := Fingerprint(key); len(strings.Fields(fp)) != 8 {
		t.Fatalf("fingerprint %q", fp)
	}
	if _, err := ParsePublicKey("rsa:abc"); err == nil {
		t.Fatal("a non-ed25519 key parsed")
	}
}
