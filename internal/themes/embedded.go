package themes

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

//go:embed themes/*.toml
var embeddedThemes embed.FS

// GetTheme loads a theme by name. Lookup order:
//  1. ~/.concord/themes/<name>.toml  (user override)
//  2. Embedded themes/<name>.toml    (bundled)
//  3. GetDefaultTheme()              (hardcoded Dracula fallback)
func GetTheme(name string) (*Theme, error) {
	if name == "" {
		name = "dracula"
	}

	// 1. Try user override directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(homeDir, ".concord", "themes", name+".toml")
		if data, err := os.ReadFile(userPath); err == nil {
			var t Theme
			if err := toml.Unmarshal(data, &t); err == nil {
				return &t, nil
			}
		}
	}

	// 2. Try embedded themes
	data, err := embeddedThemes.ReadFile("themes/" + name + ".toml")
	if err == nil {
		var t Theme
		if err := toml.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("failed to parse embedded theme %q: %w", name, err)
		}
		return &t, nil
	}

	// 3. Fallback to hardcoded Dracula
	if name != "dracula" {
		return nil, fmt.Errorf("theme %q not found", name)
	}
	return GetDefaultTheme(), nil
}

// ListAvailableThemes returns theme names from embedded themes plus any user themes.
// The returned names can be passed directly to GetTheme().
func ListAvailableThemes() []string {
	seen := make(map[string]bool)
	var names []string

	// Collect embedded theme names first (deterministic order)
	entries, _ := fs.ReadDir(embeddedThemes, "themes")
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			name := strings.TrimSuffix(e.Name(), ".toml")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}

	// Append user themes (overrides won't duplicate since they share the same name key)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userThemesDir := filepath.Join(homeDir, ".concord", "themes")
		userEntries, _ := os.ReadDir(userThemesDir)
		for _, e := range userEntries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
				name := strings.TrimSuffix(e.Name(), ".toml")
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
	}

	return names
}

// GetThemeDisplayName returns the human-readable name for a theme slug.
// If the theme can be loaded its Meta.Name is used; otherwise the slug is title-cased.
func GetThemeDisplayName(slug string) string {
	t, err := GetTheme(slug)
	if err == nil && t.Meta.Name != "" {
		return t.Meta.Name
	}
	return slug
}
