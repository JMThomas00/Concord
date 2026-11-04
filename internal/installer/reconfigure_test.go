package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveRCBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	before := "alias ll='ls -l'\n"
	os.WriteFile(path, []byte(before+"\n# Added by concord-install\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"), 0o644)
	if err := removeRCBlock(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != before {
		t.Fatalf("left %q", b)
	}
}
