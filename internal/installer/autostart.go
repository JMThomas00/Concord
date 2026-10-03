package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Starting the server and hub by themselves:
//
//	Linux    systemd: a system unit (at boot, runs as you) or a user unit (at sign-in)
//	macOS    launchd: a LaunchDaemon (at boot, runs as you) or a LaunchAgent (at sign-in)
//	Windows  a launcher script, run by a scheduled task at boot (needs an elevated
//	         prompt) or by the Run key at sign-in
//
// Each keeps it running from its own folder (where its settings, database
// and plugins are) and writes its output to a log there.

// ServiceName is a component's systemd unit name and Windows task name.
func ServiceName(component string) string { return "concord-" + component }

// launchdLabel is its launchd label.
func launchdLabel(component string) string { return "chat.concord." + component }

// windowsName is how Windows lists it (scheduled task, Run key).
func windowsName(component string) string {
	return "Concord " + strings.ToUpper(component[:1]) + component[1:]
}

// LogFile is where a running server or hub writes its output.
func (pl *Plan) LogFile(component string) string {
	return filepath.Join(pl.Dir(component), component+".log")
}

func (pl *Plan) start(component string) string {
	if component == Server {
		return pl.ServerStart
	}
	return pl.HubStart
}

// SystemdUnit is the unit file for a component: a system unit runs at
// boot as the installing user; a user unit runs while they're signed in.
func (pl *Plan) SystemdUnit(component string, system bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Written by concord-install.\n[Unit]\nDescription=Concord %s\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\n", component)
	if system {
		fmt.Fprintf(&b, "User=%s\n", pl.Platform.User)
	}
	fmt.Fprintf(&b, "WorkingDirectory=%s\nExecStart=%s\nRestart=on-failure\nRestartSec=3\n", systemdQuote(pl.Dir(component)), systemdQuote(pl.Binary(component)))
	fmt.Fprintf(&b, "StandardOutput=append:%s\nStandardError=append:%s\n\n[Install]\n", pl.LogFile(component), pl.LogFile(component))
	if system {
		b.WriteString("WantedBy=multi-user.target\n")
	} else {
		b.WriteString("WantedBy=default.target\n")
	}
	return b.String()
}

func systemdQuote(s string) string {
	if strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// UnitPath is where a unit file goes.
func (pl *Plan) UnitPath(component string, system bool) string {
	if system {
		return "/etc/systemd/system/" + ServiceName(component) + ".service"
	}
	return filepath.Join(pl.Platform.Home, ".config", "systemd", "user", ServiceName(component)+".service")
}

// LaunchdPlist is the launchd job for a component: a LaunchDaemon (boot)
// names the user to run as; a LaunchAgent runs as whoever signs in.
func (pl *Plan) LaunchdPlist(component string, daemon bool) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	kv := func(k, v string) { fmt.Fprintf(&b, "\t<key>%s</key>\n\t<string>%s</string>\n", k, xmlEscape(v)) }
	kv("Label", launchdLabel(component))
	fmt.Fprintf(&b, "\t<key>ProgramArguments</key>\n\t<array>\n\t\t<string>%s</string>\n\t</array>\n", xmlEscape(pl.Binary(component)))
	kv("WorkingDirectory", pl.Dir(component))
	if daemon {
		kv("UserName", pl.Platform.User)
	}
	kv("StandardOutPath", pl.LogFile(component))
	kv("StandardErrorPath", pl.LogFile(component))
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n</dict>\n</plist>\n")
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// PlistPath is where a launchd job goes.
func (pl *Plan) PlistPath(component string, daemon bool) string {
	if daemon {
		return "/Library/LaunchDaemons/" + launchdLabel(component) + ".plist"
	}
	return filepath.Join(pl.Platform.Home, "Library", "LaunchAgents", launchdLabel(component)+".plist")
}

// WindowsLauncher is the PowerShell script that starts a component hidden,
// from its folder, with its output in its log (start-<component>.ps1).
func (pl *Plan) WindowsLauncher(component string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	errLog := strings.TrimSuffix(pl.LogFile(component), ".log") + "-errors.log"
	return fmt.Sprintf("# Written by concord-install: starts the Concord %s in the background.\n"+
		"Start-Process -FilePath %s -WorkingDirectory %s -WindowStyle Hidden -RedirectStandardOutput %s -RedirectStandardError %s\n",
		component, q(pl.Binary(component)), q(pl.Dir(component)), q(pl.LogFile(component)), q(errLog))
}

// LauncherPath is where that script goes.
func (pl *Plan) LauncherPath(component string) string {
	return filepath.Join(pl.Dir(component), "start-"+component+".ps1")
}

// launcherCommand runs the launcher script without a window.
func (pl *Plan) launcherCommand(component string) string {
	return fmt.Sprintf(`powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "%s"`, pl.LauncherPath(component))
}

// StartCommand is what to type to start a component by hand.
func (pl *Plan) StartCommand(component string) string {
	if pl.Platform.OS == Windows {
		return fmt.Sprintf(`cd "%s"; .\%s`, pl.Dir(component), pl.Platform.Exe("concord-"+component))
	}
	return fmt.Sprintf("cd %s && ./concord-%s", shellQuote(pl.Dir(component)), component)
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " '\"$`\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeFile writes a file, making its folder.
func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}
