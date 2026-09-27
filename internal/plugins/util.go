package plugins

import (
	"os"
	"path/filepath"
	"strings"
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

// inheritedEnvVars are the only server environment variables a plugin
// process inherits. Everything else -- including anything secret the
// server itself was started with -- stays out of reach unless the plugin's
// own manifest [process.env] sets it. The list covers what programs need to
// run at all (paths, locale, temp dirs, TLS roots, proxies) on Linux, macOS
// and Windows. Compared case-insensitively, as Windows does.
var inheritedEnvVars = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "LANGUAGE": true, "TZ": true, "TMPDIR": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
	// Windows
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true, "COMSPEC": true,
	"PATHEXT": true, "TEMP": true, "TMP": true, "USERPROFILE": true,
	"APPDATA": true, "LOCALAPPDATA": true, "PROGRAMDATA": true,
	"PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "HOMEDRIVE": true, "HOMEPATH": true,
	"NUMBER_OF_PROCESSORS": true, "PROCESSOR_ARCHITECTURE": true, "OS": true,
}

// pluginBaseEnv filters environ (os.Environ() format) down to
// inheritedEnvVars, plus the LC_* locale family.
func pluginBaseEnv(environ []string) []string {
	var out []string
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		upper := strings.ToUpper(name)
		if inheritedEnvVars[upper] || strings.HasPrefix(upper, "LC_") {
			out = append(out, kv)
		}
	}
	return out
}
