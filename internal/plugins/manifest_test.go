package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "plugin.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write manifest fixture: %v", err)
	}
	return path
}

func TestLoadManifest_Valid(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `
[plugin]
id = "example"
name = "Example"
version = "1.0.0"

[process]
[process.entrypoint.windows]
bin = "example.exe"
[process.entrypoint.linux]
bin = "example"
[process.entrypoint.darwin]
bin = "example"

[[channel_kind]]
kind = "board"
display_name = "Board"
remote_pane = true

[[channel_kind.create_field]]
key = "board_name"
label = "Board Name"
type = "text"
required = true
`)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if m.Plugin.ID != "example" {
		t.Errorf("expected id 'example', got %q", m.Plugin.ID)
	}
	if len(m.ChannelKinds) != 1 || m.ChannelKinds[0].Kind != "board" {
		t.Errorf("expected one 'board' channel kind, got %+v", m.ChannelKinds)
	}
}

func TestValidate_MissingID(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `
[plugin]
name = "Example"
version = "1.0.0"
[process.entrypoint.windows]
bin = "example.exe"
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected Validate to fail on missing [plugin].id")
	}
}

func TestValidate_MissingEntrypointForHostOS(t *testing.T) {
	dir := t.TempDir()
	// Only declares an entrypoint for an OS that will never match runtime.GOOS.
	path := writeManifest(t, dir, `
[plugin]
id = "example"
name = "Example"
version = "1.0.0"
[process.entrypoint.plan9]
bin = "example"
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected Validate to fail when no entrypoint matches the host OS")
	}
}

func TestValidate_DuplicateChannelKind(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `
[plugin]
id = "example"
name = "Example"
version = "1.0.0"
[process.entrypoint.windows]
bin = "example.exe"
[process.entrypoint.linux]
bin = "example"
[process.entrypoint.darwin]
bin = "example"

[[channel_kind]]
kind = "board"
[[channel_kind]]
kind = "board"
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected Validate to fail on duplicate channel_kind")
	}
}

func TestValidate_MissingRequiredManifestFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "missing name",
			content: `
[plugin]
id = "example"
version = "1.0.0"
[process.entrypoint.windows]
bin = "example.exe"
`,
		},
		{
			name: "missing version",
			content: `
[plugin]
id = "example"
name = "Example"
[process.entrypoint.windows]
bin = "example.exe"
`,
		},
		{
			name: "select field with no options",
			content: `
[plugin]
id = "example"
name = "Example"
version = "1.0.0"
[process.entrypoint.windows]
bin = "example.exe"
[process.entrypoint.linux]
bin = "example"
[process.entrypoint.darwin]
bin = "example"

[[server_config_field]]
key = "mode"
label = "Mode"
type = "select"
`,
		},
		{
			name: "field with unknown type",
			content: `
[plugin]
id = "example"
name = "Example"
version = "1.0.0"
[process.entrypoint.windows]
bin = "example.exe"
[process.entrypoint.linux]
bin = "example"
[process.entrypoint.darwin]
bin = "example"

[[server_config_field]]
key = "mode"
label = "Mode"
type = "wat"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeManifest(t, dir, tt.content)
			m, err := LoadManifest(path)
			if err != nil {
				t.Fatalf("LoadManifest failed: %v", err)
			}
			if err := m.Validate(); err == nil {
				t.Fatal("expected Validate to fail")
			}
		})
	}
}
