package installer

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Components.
const (
	Client = "client"
	Server = "server"
	Hub    = "hub"
)

// When a server or hub starts by itself.
const (
	StartAtBoot  = "boot"  // when the computer starts, before anyone signs in
	StartAtLogin = "login" // when you sign in
	StartNever   = "never" // only when you run it
)

// Plan is everything the form asked, and all the installer needs to know.
type Plan struct {
	Platform   Platform
	Components []string

	ClientDir string

	ServerDir     string
	ServerName    string
	ServerPort    string // as typed; Port() reads it
	AdminEmail    string
	ServerStart   string
	ServerOnHub   bool   // list it on the official Grapevine hub
	PublicHost    string // the address people use to reach it, for the hub
	Description   string
	Category      string
	TermsAccepted bool

	HubDir      string
	HubName     string
	HubPort     string
	HubStart    string
	HubFederate bool // peer with the official hub

	// Release to install: "" for the latest. Source, when set, is a local
	// archive or folder of binaries to install instead of downloading.
	Release string
	Source  string
	// DryRun does everything except changing the computer: the steps
	// describe what they'd do.
	DryRun bool
}

// NewPlan starts a plan with this platform's defaults.
func NewPlan(p Platform) *Plan {
	pl := &Plan{
		Platform:    p,
		ServerName:  defaultServerName(),
		ServerPort:  "8080",
		ServerStart: StartAtLogin,
		Category:    "General",
		HubName:     "Grapevine Hub",
		HubPort:     "7777",
		HubStart:    StartAtLogin,
	}
	if !p.CanStartAtLogin() {
		pl.ServerStart, pl.HubStart = StartNever, StartNever
	}
	pl.ClientDir, pl.ServerDir, pl.HubDir = p.DefaultDir(Client), p.DefaultDir(Server), p.DefaultDir(Hub)
	return pl
}

func defaultServerName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		h = strings.Split(h, ".")[0]
		return h + "'s Concord"
	}
	return "My Concord Server"
}

// DefaultDir is where a component goes unless you say otherwise.
func (p Platform) DefaultDir(component string) string {
	if p.OS == Windows {
		switch component {
		case Client:
			base := os.Getenv("LOCALAPPDATA")
			if base == "" {
				base = filepath.Join(p.Home, "AppData", "Local")
			}
			return filepath.Join(base, "Programs", "Concord")
		case Server:
			return filepath.Join(p.Home, "Concord", "Server")
		}
		return filepath.Join(p.Home, "Concord", "Hub")
	}
	switch component {
	case Client:
		return filepath.Join(p.Home, ".local", "share", "concord")
	case Server:
		return filepath.Join(p.Home, "concord", "server")
	}
	return filepath.Join(p.Home, "concord", "hub")
}

// Has reports whether the plan installs a component.
func (pl *Plan) Has(component string) bool {
	for _, c := range pl.Components {
		if c == component {
			return true
		}
	}
	return false
}

// Dir is a component's folder.
func (pl *Plan) Dir(component string) string {
	switch component {
	case Client:
		return pl.ClientDir
	case Server:
		return pl.ServerDir
	}
	return pl.HubDir
}

// Binary is a component's program file.
func (pl *Plan) Binary(component string) string {
	return filepath.Join(pl.Dir(component), pl.Platform.Exe("concord-"+component))
}

// Existing reports whether a component is already set up in its folder
// (its settings file is there), so installing again updates it and keeps
// its settings and data.
func (pl *Plan) Existing(component string) bool {
	switch component {
	case Server:
		return fileExists(filepath.Join(pl.ServerDir, ServerConfigFile))
	case Hub:
		return fileExists(filepath.Join(pl.HubDir, HubConfigFile))
	}
	return fileExists(pl.Binary(Client))
}

// ServerPortNum and HubPortNum read the ports (validated by the form).
func (pl *Plan) ServerPortNum() int { n, _ := strconv.Atoi(strings.TrimSpace(pl.ServerPort)); return n }
func (pl *Plan) HubPortNum() int    { n, _ := strconv.Atoi(strings.TrimSpace(pl.HubPort)); return n }

// ValidatePort accepts a TCP port people can use without special rights.
func ValidatePort(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("a port is a number from 1 to 65535")
	}
	return nil
}

// ValidateEmail accepts an empty address (it's optional) or a real one.
func ValidateEmail(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(s)); err != nil {
		return fmt.Errorf("that doesn't look like an email address")
	}
	return nil
}

// ValidateDir accepts a folder that exists or can be made.
func ValidateDir(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("pick a folder")
	}
	if !filepath.IsAbs(ExpandHome(s)) {
		return fmt.Errorf("use a full path, like %s", filepath.Join(string(filepath.Separator)+"home", "you", "concord"))
	}
	if st, err := os.Stat(ExpandHome(s)); err == nil && !st.IsDir() {
		return fmt.Errorf("that's a file, not a folder")
	}
	return nil
}

// ExpandHome turns a leading ~ into the home folder.
func ExpandHome(s string) string {
	s = strings.TrimSpace(s)
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, s[1:])
		}
	}
	return s
}

// Tidy cleans up what was typed: paths expanded and made absolute,
// spaces trimmed.
func (pl *Plan) Tidy() {
	for _, d := range []*string{&pl.ClientDir, &pl.ServerDir, &pl.HubDir} {
		*d = filepath.Clean(ExpandHome(*d))
	}
	for _, s := range []*string{&pl.ServerName, &pl.AdminEmail, &pl.PublicHost, &pl.Description, &pl.Category, &pl.HubName, &pl.ServerPort, &pl.HubPort} {
		*s = strings.TrimSpace(*s)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Tilde shortens a path in the home folder to ~/…, for showing.
func Tilde(path string) string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return path
	}
	if rel, err := filepath.Rel(h, path); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		if rel == "." {
			return "~"
		}
		return "~" + string(filepath.Separator) + rel
	}
	return path
}
