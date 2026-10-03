// Package installer does the work behind concord-install: it finds out
// what this computer is, fetches a release, puts the hub, server and client
// in place with their settings, takes care of what they need to run, and
// starts them. cmd/install is its face (the form, the progress, the
// finale); everything here runs without a terminal, so it can be tested.
package installer

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OS names, as the form shows them and as release assets spell them.
const (
	Linux   = "linux"
	MacOS   = "macos"
	Windows = "windows"
)

// Platform is what the installer knows about this computer.
type Platform struct {
	OS     string // Linux, MacOS or Windows; "" when it couldn't tell
	Arch   string // amd64 or arm64
	Distro string // Linux: the PRETTY_NAME from /etc/os-release
	// PkgManager installs the client's system libraries on Linux: apt,
	// dnf, pacman or zypper, "" when none was found.
	PkgManager string
	Systemd    bool // Linux: systemd is running (WSL often has none)
	WSL        bool
	Admin      bool // running as root or an elevated Windows prompt
	Home       string
	User       string
}

// Detect looks at the computer it's running on.
func Detect() Platform {
	p := Platform{Arch: runtime.GOARCH, Admin: isAdmin()}
	switch runtime.GOOS {
	case "linux":
		p.OS = Linux
	case "darwin":
		p.OS = MacOS
	case "windows":
		p.OS = Windows
	}
	p.Home, _ = os.UserHomeDir()
	p.User = os.Getenv("USER")
	if p.User == "" {
		p.User = os.Getenv("USERNAME")
	}
	if p.OS == Linux {
		p.Distro = osRelease()["PRETTY_NAME"]
		for _, m := range []string{"apt-get", "dnf", "pacman", "zypper"} {
			if _, err := exec.LookPath(m); err == nil {
				p.PkgManager = strings.TrimSuffix(m, "-get")
				break
			}
		}
		if st, err := os.Stat("/run/systemd/system"); err == nil && st.IsDir() {
			p.Systemd = true
		}
		if b, err := os.ReadFile("/proc/version"); err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft") {
			p.WSL = true
		}
	}
	return p
}

// Name is the OS as people say it.
func (p Platform) Name() string {
	switch p.OS {
	case Linux:
		if p.Distro != "" {
			return p.Distro
		}
		return "Linux"
	case MacOS:
		return "macOS"
	case Windows:
		return "Windows"
	}
	return "an unknown system"
}

// Exe is a program's file name here (".exe" on Windows).
func (p Platform) Exe(name string) string {
	if p.OS == Windows {
		return name + ".exe"
	}
	return name
}

// ArchiveName is the release asset with this platform's binaries, as
// .github/workflows/release.yml names them.
func (p Platform) ArchiveName() string {
	switch p.OS {
	case Windows:
		return "concord-windows-amd64.zip"
	case MacOS:
		if p.Arch == "amd64" {
			return "concord-macos-x86_64.tar.gz"
		}
		return "concord-macos-arm64.tar.gz"
	}
	return "concord-linux-" + p.Arch + ".tar.gz"
}

// CanStartAtBoot reports whether a service can start before anyone signs
// in: Linux needs systemd. Setting it up needs an administrator, which
// the installer asks for (sudo, or Windows' permission prompt).
func (p Platform) CanStartAtBoot() bool {
	switch p.OS {
	case Linux:
		return p.Systemd
	}
	return true
}

// CanStartAtLogin reports whether a service can start when you sign in.
func (p Platform) CanStartAtLogin() bool {
	return p.OS != Linux || p.Systemd
}

func osRelease() map[string]string {
	out := map[string]string{}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return out
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if k, v, ok := strings.Cut(s.Text(), "="); ok {
			out[k] = strings.Trim(v, `"'`)
		}
	}
	return out
}
