package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every template renders completely: no template syntax left behind, and
// the files a plugin repo needs are all there.
func TestTemplatesRender(t *testing.T) {
	for kind := range kinds {
		dir := t.TempDir()
		wd, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		p := Project{ID: "demo-" + kind, Name: "Demo", GoName: "demo", Module: "github.com/x/demo-" + kind,
			Kind: kind, SDKVersion: "v0.1.0", ChannelKind: "thing"}
		err := render(p)
		_ = os.Chdir(wd)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		for _, f := range []string{"go.mod", "main.go", "plugin.toml", "release.go", "README.md", ".gitignore", ".github/workflows/release.yml"} {
			data, err := os.ReadFile(filepath.Join(dir, p.ID, f))
			if err != nil {
				t.Errorf("%s: missing %s", kind, f)
				continue
			}
			if strings.Contains(string(data), "<%") || strings.Contains(string(data), "%>") {
				t.Errorf("%s/%s has unrendered template syntax", kind, f)
			}
		}
		manifest, _ := os.ReadFile(filepath.Join(dir, p.ID, "plugin.toml"))
		if !strings.Contains(string(manifest), `id = "demo-`+kind+`"`) || !strings.Contains(string(manifest), "[[channel_kind]]") {
			t.Errorf("%s: plugin.toml looks wrong:\n%s", kind, manifest)
		}
	}
}
