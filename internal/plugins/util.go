package plugins

import (
	"os"
	"path/filepath"
)

func isAbsPath(p string) bool {
	return filepath.IsAbs(p)
}

func joinPath(dir, p string) string {
	return filepath.Join(dir, p)
}

// expandEnv resolves ${VAR} placeholders in manifest strings (entrypoint
// args, env values) against the plugin-specific env map Concord generated
// (token, ids, URLs) — not the OS environment.
func expandEnv(s string, env map[string]string) string {
	return os.Expand(s, func(key string) string {
		return env[key]
	})
}
