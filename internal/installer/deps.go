package installer

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// The client is the only part with system libraries to find: voice
// (opus) and the Save As dialog (GTK on Linux). The server and hub are
// self-contained.

// linuxPackages maps a missing library to the package that provides it,
// per package manager.
var linuxPackages = map[string]map[string]string{
	"libopus.so.0":     {"apt": "libopus0", "dnf": "opus", "pacman": "opus", "zypper": "libopus0"},
	"libopusfile.so.0": {"apt": "libopusfile0", "dnf": "opusfile", "pacman": "opusfile", "zypper": "libopusfile0"},
	"libogg.so.0":      {"apt": "libogg0", "dnf": "libogg", "pacman": "libogg", "zypper": "libogg0"},
	"libgtk-3.so.0":    {"apt": "libgtk-3-0", "dnf": "gtk3", "pacman": "gtk3", "zypper": "libgtk-3-0"},
	"libgdk-3.so.0":    {"apt": "libgtk-3-0", "dnf": "gtk3", "pacman": "gtk3", "zypper": "libgtk-3-0"},
	"libasound.so.2":   {"apt": "libasound2", "dnf": "alsa-lib", "pacman": "alsa-lib", "zypper": "libasound2"},
	"libX11.so.6":      {"apt": "libx11-6", "dnf": "libX11", "pacman": "libx11", "zypper": "libX11-6"},
}

// MissingLibraries lists the shared libraries a program needs that this
// computer doesn't have (Linux: ldd; macOS: otool). Windows builds carry
// theirs.
func MissingLibraries(p Platform, binary string) ([]string, error) {
	switch p.OS {
	case Linux:
		out, err := exec.Command("ldd", binary).CombinedOutput()
		if err != nil && len(out) == 0 {
			return nil, fmt.Errorf("couldn't check the client's libraries: %w", err)
		}
		return parseLdd(out), nil
	case MacOS:
		out, err := exec.Command("otool", "-L", binary).Output()
		if err != nil {
			return nil, nil // no developer tools: nothing to check with, carry on
		}
		return parseOtool(out, func(path string) bool { _, err := os.Stat(path); return err == nil }), nil
	}
	return nil, nil
}

func parseLdd(out []byte) []string {
	var missing []string
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if lib, rest, ok := strings.Cut(line, "=>"); ok && strings.Contains(rest, "not found") {
			missing = append(missing, strings.TrimSpace(lib))
		}
	}
	return missing
}

// parseOtool lists libraries outside the system (Homebrew's) that aren't
// where the program expects them.
func parseOtool(out []byte, exists func(string) bool) []string {
	var missing []string
	s := bufio.NewScanner(bytes.NewReader(out))
	s.Scan() // the program's own name
	for s.Scan() {
		path, _, _ := strings.Cut(strings.TrimSpace(s.Text()), " (")
		if path == "" || strings.HasPrefix(path, "/usr/lib/") || strings.HasPrefix(path, "/System/") || strings.HasPrefix(path, "@") {
			continue
		}
		if !exists(path) {
			missing = append(missing, path)
		}
	}
	return missing
}

// InstallCommand is the command that installs the packages providing the
// missing libraries, or an error saying what to do by hand.
func InstallCommand(p Platform, missing []string) ([]string, error) {
	if len(missing) == 0 {
		return nil, nil
	}
	if p.OS == MacOS {
		if _, err := exec.LookPath("brew"); err != nil {
			return nil, fmt.Errorf("the client needs Opus from Homebrew: install Homebrew (https://brew.sh), then run: brew install opus opusfile")
		}
		return []string{"brew", "install", "opus", "opusfile"}, nil
	}
	set := map[string]bool{}
	var unknown []string
	for _, lib := range missing {
		pkg := linuxPackages[lib][p.PkgManager]
		if pkg == "" {
			unknown = append(unknown, lib)
			continue
		}
		set[pkg] = true
	}
	if p.PkgManager == "" || len(unknown) > 0 {
		libs := missing
		if len(unknown) > 0 {
			libs = unknown
		}
		return nil, fmt.Errorf("the client needs these libraries, which you'll have to install with your package manager: %s", strings.Join(libs, ", "))
	}
	pkgs := make([]string, 0, len(set))
	for k := range set {
		pkgs = append(pkgs, k)
	}
	sort.Strings(pkgs)
	switch p.PkgManager {
	case "apt":
		return append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, pkgs...), nil
	case "dnf":
		return append([]string{"dnf", "install", "-y"}, pkgs...), nil
	case "pacman":
		return append([]string{"pacman", "-S", "--needed", "--noconfirm"}, pkgs...), nil
	}
	return append([]string{"zypper", "--non-interactive", "install"}, pkgs...), nil
}
