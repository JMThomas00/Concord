package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/concord-chat/concord/internal/official"
	"github.com/pelletier/go-toml/v2"
)

// Step is one line of the install's checklist.
type Step struct {
	Title string
	Run   func(ctx context.Context) error
}

// Runner carries an install out. The UI supplies the callbacks.
type Runner struct {
	Plan *Plan
	// Progress reports how far through the current step it is (0–1) and
	// what it's doing; Log adds a line under it.
	Progress func(frac float64, detail string)
	Log      func(line string)
	// Sudo makes sure commands can run as root (Linux/macOS), asking for
	// the password if it must. Nil means it can't.
	Sudo func() error

	Result Result

	staging string
	found   []string
}

// Result is what the finale needs to tell people.
type Result struct {
	Tag       string
	Installed []string        // components put in place
	Updated   map[string]bool // ...that were already there
	Missing   []string        // asked for, but not in this platform's release
	Running   map[string]bool // started and answering
	HubToken  string          // a new hub's admin token, shown once
	NewPath   bool            // PATH changed: a new terminal is needed
	Notes     []string        // anything else worth saying
}

// Steps is the checklist for the plan, in order.
func (r *Runner) Steps() []Step {
	pl := r.Plan
	r.Result.Updated, r.Result.Running = map[string]bool{}, map[string]bool{}
	steps := []Step{{"Fetching Concord", r.fetch}}
	for _, c := range []string{Hub, Server, Client} {
		if !pl.Has(c) {
			continue
		}
		c := c
		steps = append(steps, Step{"Planting the " + c, func(ctx context.Context) error { return r.place(c) }})
		switch c {
		case Client:
			steps = append(steps,
				Step{"Gathering what the client needs", r.clientDeps},
				Step{"Putting concord on your PATH", r.clientPath})
		case Server, Hub:
			steps = append(steps,
				Step{"Writing the " + c + "'s settings", func(ctx context.Context) error { return r.configure(c) }},
				Step{"Waking the " + c, func(ctx context.Context) error { return r.autostart(ctx, c) }})
		}
	}
	return steps
}

func (r *Runner) progress(f float64, detail string) {
	if r.Progress != nil {
		r.Progress(f, detail)
	}
}

func (r *Runner) log(format string, args ...any) {
	if r.Log != nil {
		r.Log(fmt.Sprintf(format, args...))
	}
}

// change does something to the computer, or (dry run) says it would.
func (r *Runner) change(what string, do func() error) error {
	if r.Plan.DryRun {
		r.log("would %s", what)
		return nil
	}
	return do()
}

// run runs a command, returning its output in the error when it fails.
func (r *Runner) run(name string, args ...string) error {
	return r.change(strings.Join(append([]string{"run:", name}, args...), " "), func() error {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if len(msg) > 400 {
				msg = msg[len(msg)-400:]
			}
			return fmt.Errorf("%s failed: %v\n%s", name, err, msg)
		}
		return nil
	})
}

// launch runs a command that starts something in the background. Its
// output isn't captured: what it starts would inherit the pipe and keep it
// open, and waiting for the pipe to close would wait for ever.
func (r *Runner) launch(name string, args ...string) error {
	return r.change(strings.Join(append([]string{"run:", name}, args...), " "), func() error {
		if err := exec.Command(name, args...).Run(); err != nil {
			return fmt.Errorf("%s failed: %v", name, err)
		}
		return nil
	})
}

// root runs a command as root: directly when already root, otherwise
// through sudo once the password has been given.
func (r *Runner) root(name string, args ...string) error {
	if r.Plan.Platform.Admin || r.Plan.DryRun {
		return r.run(name, args...)
	}
	if r.Sudo == nil {
		return fmt.Errorf("this needs administrator rights (sudo), which aren't available here")
	}
	if err := r.Sudo(); err != nil {
		return err
	}
	return r.run("sudo", append([]string{"-n", name}, args...)...)
}

// --- fetching -------------------------------------------------------------

func (r *Runner) fetch(ctx context.Context) error {
	pl := r.Plan
	dir, err := os.MkdirTemp("", "concord-install-*")
	if err != nil {
		return err
	}
	r.staging = filepath.Join(dir, "unpacked")
	src := pl.Source
	if src == "" {
		r.progress(0, "asking GitHub for the newest release")
		rel, err := FetchRelease(ctx, pl.Release)
		if err != nil {
			return err
		}
		r.Result.Tag = rel.Tag
		name := pl.Platform.ArchiveName()
		asset, ok := rel.Assets[name]
		if !ok {
			return fmt.Errorf("release %s has nothing for %s yet (no %s)", rel.Tag, pl.Platform.Name(), name)
		}
		if asset.SHA256 == "" {
			r.log("no published checksum for %s; trusting https alone", name)
		}
		src, err = Download(ctx, asset, dir, func(done, total int64) {
			f := 0.0
			if total > 0 {
				f = float64(done) / float64(total)
			}
			r.progress(f, fmt.Sprintf("%s  %s of %s", rel.Tag, mb(done), mb(total)))
		})
		if err != nil {
			return err
		}
		r.log("downloaded %s %s (checksum verified)", name, rel.Tag)
	} else {
		r.Result.Tag = "local build"
		r.log("installing from %s", src)
	}
	r.progress(1, "unpacking")
	found, err := Unpack(src, r.staging)
	if err != nil {
		return err
	}
	r.found = found
	for _, c := range pl.Components {
		if !contains(found, c) {
			r.Result.Missing = append(r.Result.Missing, c)
		}
	}
	if len(r.Result.Missing) == len(pl.Components) {
		return fmt.Errorf("this release has no %s for %s yet", strings.Join(pl.Components, " or "), pl.Platform.Name())
	}
	return nil
}

func mb(n int64) string {
	if n <= 0 {
		return "?"
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (r *Runner) available(c string) bool { return contains(r.found, c) }

// --- placing --------------------------------------------------------------

// place puts a component's program in its folder, stopping a copy that's
// already running there first.
func (r *Runner) place(c string) error {
	if !r.available(c) {
		r.log("skipped: no %s in this release for %s", c, r.Plan.Platform.Name())
		return nil
	}
	pl := r.Plan
	if pl.Existing(c) {
		r.Result.Updated[c] = true
		if c != Client {
			r.progress(.2, "pausing the running "+c)
			r.stop(c)
		}
	}
	dst := pl.Binary(c)
	src := filepath.Join(r.staging, filepath.Base(dst))
	r.progress(.5, dst)
	err := r.change("put "+filepath.Base(dst)+" in "+filepath.Dir(dst), func() error {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		// Copy beside it, then swap: a running program's file can be
		// renamed on every OS, but not overwritten on Windows.
		tmp := dst + ".new"
		if err := copyFile(src, tmp, 0o755); err != nil {
			return err
		}
		old := dst + ".old"
		os.Remove(old)
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, old); err != nil {
				return err
			}
		}
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		os.Remove(old) // fails while the old one still runs; it goes next time
		return nil
	})
	if err != nil {
		return err
	}
	r.Result.Installed = append(r.Result.Installed, c)
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// --- the client -------------------------------------------------------------

func (r *Runner) clientDeps(ctx context.Context) error {
	pl := r.Plan
	if !r.available(Client) || pl.Platform.OS == Windows {
		r.log("nothing extra needed")
		return nil
	}
	bin := pl.Binary(Client)
	if pl.DryRun {
		bin = filepath.Join(r.staging, "concord-client")
	}
	if runtime.GOOS != map[string]string{Linux: "linux", MacOS: "darwin"}[pl.Platform.OS] {
		return nil // can't inspect another OS's program (tests)
	}
	missing, err := MissingLibraries(pl.Platform, bin)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		r.log("everything the client needs is already here")
		return nil
	}
	r.log("missing: %s", strings.Join(missing, ", "))
	cmd, err := InstallCommand(pl.Platform, missing)
	if err != nil {
		return err
	}
	r.progress(.3, strings.Join(cmd, " "))
	if pl.Platform.OS == MacOS {
		return r.run(cmd[0], cmd[1:]...) // Homebrew refuses to run as root
	}
	if pl.Platform.PkgManager == "apt" {
		if err := r.root("apt-get", "update", "-qq"); err != nil {
			return err
		}
	}
	return r.root(cmd[0], cmd[1:]...)
}

func (r *Runner) clientPath(ctx context.Context) error {
	pl := r.Plan
	if !r.available(Client) {
		return nil
	}
	dir := pl.ClientDir
	if pl.Platform.OS == Windows {
		// concord.cmd beside concord-client.exe, so both names work.
		if err := r.change("add the concord command", func() error {
			return writeFile(filepath.Join(dir, "concord.cmd"), "@\"%~dp0concord-client.exe\" %*\r\n", 0o644)
		}); err != nil {
			return err
		}
		if err := r.change("add "+dir+" to your PATH", func() error {
			changed, err := addToUserPath(dir)
			r.Result.NewPath = r.Result.NewPath || changed
			return err
		}); err != nil {
			return err
		}
		return r.change("add a Concord profile to Windows Terminal", func() error { return r.terminalProfile() })
	}
	bin := filepath.Join(pl.Platform.Home, ".local", "bin")
	if err := r.change("link concord and concord-client into "+bin, func() error {
		if err := os.MkdirAll(bin, 0o755); err != nil {
			return err
		}
		for _, name := range []string{"concord", "concord-client"} {
			link := filepath.Join(bin, name)
			os.Remove(link)
			if err := os.Symlink(pl.Binary(Client), link); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if onPath(bin) {
		return nil
	}
	rc := shellRC(pl.Platform)
	return r.change("add "+bin+" to your PATH in "+rc, func() error {
		f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		line := "\n# Added by concord-install\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"
		if strings.HasSuffix(rc, "config.fish") {
			line = "\n# Added by concord-install\nfish_add_path $HOME/.local/bin\n"
		}
		if _, err := f.WriteString(line); err != nil {
			return err
		}
		r.Result.NewPath = true
		return nil
	})
}

func onPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// shellRC is the startup file of the shell you use.
func shellRC(p Platform) string {
	switch sh := filepath.Base(os.Getenv("SHELL")); {
	case sh == "zsh" || (sh == "." && p.OS == MacOS):
		return filepath.Join(p.Home, ".zshrc")
	case sh == "fish":
		return filepath.Join(p.Home, ".config", "fish", "config.fish")
	}
	if p.OS == MacOS {
		return filepath.Join(p.Home, ".zshrc")
	}
	return filepath.Join(p.Home, ".bashrc")
}

// terminalProfile adds Concord to Windows Terminal's new-tab menu, with a
// grape-purple colour scheme (a fragment, which Terminal picks up itself).
func (r *Runner) terminalProfile() error {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return nil
	}
	exe := strings.ReplaceAll(r.Plan.Binary(Client), `\`, `\\`)
	frag := `{
  "profiles": [
    {
      "name": "Concord",
      "commandline": "\"` + exe + `\"",
      "icon": "🍇",
      "colorScheme": "Concord Grape",
      "tabTitle": "Concord",
      "suppressApplicationTitle": true
    }
  ],
  "schemes": [
    {
      "name": "Concord Grape",
      "background": "#1E1A2B", "foreground": "#F8F8F2", "cursorColor": "#BD93F9", "selectionBackground": "#44475A",
      "black": "#21222C", "red": "#FF5555", "green": "#50FA7B", "yellow": "#F1FA8C",
      "blue": "#BD93F9", "purple": "#FF79C6", "cyan": "#8BE9FD", "white": "#F8F8F2",
      "brightBlack": "#6272A4", "brightRed": "#FF6E6E", "brightGreen": "#69FF94", "brightYellow": "#FFFFA5",
      "brightBlue": "#D6ACFF", "brightPurple": "#FF92DF", "brightCyan": "#A4FFFF", "brightWhite": "#FFFFFF"
    }
  ]
}
`
	return writeFile(filepath.Join(base, "Microsoft", "Windows Terminal", "Fragments", "Concord", "concord.json"), frag, 0o644)
}

// --- the server and hub -----------------------------------------------------

// configure writes a new server's or hub's settings; an existing one keeps
// its own.
func (r *Runner) configure(c string) error {
	pl := r.Plan
	if !r.available(c) {
		return nil
	}
	if r.Result.Updated[c] {
		r.log("kept your existing settings")
		return nil
	}
	path := filepath.Join(pl.Dir(c), ServerConfigFile)
	var data []byte
	var err error
	if c == Server {
		data, err = pl.ServerConfigTOML(official.HubURL)
	} else {
		path = filepath.Join(pl.Dir(c), HubConfigFile)
		var token string
		data, token, err = pl.HubConfigTOML(official.HubName, official.HubURL)
		r.Result.HubToken = token
	}
	if err != nil {
		return err
	}
	if err := r.change("write "+path, func() error { return writeFile(path, string(data), 0o600) }); err != nil {
		return err
	}
	if c == Server {
		return r.change("make the Plugins folder", func() error { return os.MkdirAll(filepath.Join(pl.ServerDir, "Plugins"), 0o755) })
	}
	return nil
}

// port is the port a server or hub listens on: from its settings file
// when it already had one.
func (r *Runner) port(c string) int {
	pl := r.Plan
	file := filepath.Join(pl.Dir(c), ServerConfigFile)
	want := pl.ServerPortNum()
	if c == Hub {
		file, want = filepath.Join(pl.Dir(c), HubConfigFile), pl.HubPortNum()
	}
	if b, err := os.ReadFile(file); err == nil {
		var v struct {
			Port int `toml:"port"`
		}
		if toml.Unmarshal(b, &v) == nil && v.Port > 0 {
			return v.Port
		}
	}
	return want
}

// HealthURL is where a running server or hub answers.
func (r *Runner) HealthURL(c string) string {
	if c == Server {
		return fmt.Sprintf("http://127.0.0.1:%d/api/health", r.port(c))
	}
	return fmt.Sprintf("http://127.0.0.1:%d/v1/health", r.port(c))
}

// autostart sets a server or hub to start the way the plan says (clearing
// any other way it was set up before), starts it, and waits for it to
// answer.
func (r *Runner) autostart(ctx context.Context, c string) error {
	pl := r.Plan
	if !r.available(c) {
		return nil
	}
	mode := pl.start(c)
	r.progress(.1, "clearing old start-up settings")
	r.clearAutostart(c, mode)
	var err error
	switch pl.Platform.OS {
	case Linux:
		err = r.autostartLinux(c, mode)
	case MacOS:
		err = r.autostartMac(c, mode)
	case Windows:
		err = r.autostartWindows(c, mode)
	}
	if err != nil {
		return err
	}
	if mode == StartNever {
		r.log("start it yourself with: %s", pl.StartCommand(c))
		return nil
	}
	if pl.DryRun {
		return nil
	}
	r.progress(.6, "waiting for it to answer at "+r.HealthURL(c))
	if err := waitHealthy(ctx, r.HealthURL(c), 25*time.Second); err != nil {
		return fmt.Errorf("the %s didn't answer (%v); its log is %s", c, err, pl.LogFile(c))
	}
	r.Result.Running[c] = true
	return nil
}

func waitHealthy(ctx context.Context, url string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	client := &http.Client{Timeout: 2 * time.Second}
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = errors.New(resp.Status)
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	return fmt.Errorf("gave up after %s: %v", limit, last)
}

// stop stops a running server or hub before its program is replaced.
func (r *Runner) stop(c string) {
	pl := r.Plan
	if pl.DryRun {
		r.log("would stop the running %s", c)
		return
	}
	switch pl.Platform.OS {
	case Linux:
		if fileExists(pl.UnitPath(c, true)) {
			_ = r.root("systemctl", "stop", ServiceName(c))
		}
		if fileExists(pl.UnitPath(c, false)) {
			_ = r.run("systemctl", "--user", "stop", ServiceName(c))
		}
	case MacOS:
		if fileExists(pl.PlistPath(c, true)) {
			_ = r.root("launchctl", "bootout", "system/"+launchdLabel(c))
		}
		if fileExists(pl.PlistPath(c, false)) {
			_ = r.run("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel(c)))
		}
	case Windows:
		_ = r.run("powershell.exe", "-NoProfile", "-Command",
			fmt.Sprintf("Get-Process | Where-Object { $_.Path -eq '%s' } | Stop-Process -Force", strings.ReplaceAll(pl.Binary(c), "'", "''")))
	}
	time.Sleep(500 * time.Millisecond)
}

// clearAutostart removes the ways of starting c other than keep.
func (r *Runner) clearAutostart(c, keep string) {
	pl := r.Plan
	switch pl.Platform.OS {
	case Linux:
		if keep != StartAtBoot && fileExists(pl.UnitPath(c, true)) {
			_ = r.root("systemctl", "disable", "--now", ServiceName(c))
			_ = r.root("rm", "-f", pl.UnitPath(c, true))
		}
		if keep != StartAtLogin && fileExists(pl.UnitPath(c, false)) {
			_ = r.run("systemctl", "--user", "disable", "--now", ServiceName(c))
			_ = r.change("remove "+pl.UnitPath(c, false), func() error { return os.Remove(pl.UnitPath(c, false)) })
		}
	case MacOS:
		if keep != StartAtBoot && fileExists(pl.PlistPath(c, true)) {
			_ = r.root("launchctl", "bootout", "system/"+launchdLabel(c))
			_ = r.root("rm", "-f", pl.PlistPath(c, true))
		}
		if keep != StartAtLogin && fileExists(pl.PlistPath(c, false)) {
			_ = r.run("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel(c)))
			_ = r.change("remove "+pl.PlistPath(c, false), func() error { return os.Remove(pl.PlistPath(c, false)) })
		}
	case Windows:
		if keep != StartAtLogin {
			_ = r.change("remove the sign-in entry", func() error { return setRunAtLogin(windowsName(c), "") })
		}
		if keep != StartAtBoot && pl.Platform.Admin {
			_ = r.run("schtasks.exe", "/Delete", "/TN", windowsName(c), "/F")
		}
	}
}

func (r *Runner) autostartLinux(c, mode string) error {
	pl := r.Plan
	switch mode {
	case StartAtBoot:
		tmp := filepath.Join(filepath.Dir(r.staging), ServiceName(c)+".service")
		if err := r.change("write the service file", func() error { return writeFile(tmp, pl.SystemdUnit(c, true), 0o644) }); err != nil {
			return err
		}
		r.progress(.3, "registering "+ServiceName(c)+" with systemd")
		for _, args := range [][]string{
			{"install", "-m", "644", tmp, pl.UnitPath(c, true)},
			{"systemctl", "daemon-reload"},
			{"systemctl", "enable", ServiceName(c)},
			{"systemctl", "restart", ServiceName(c)},
		} {
			if err := r.root(args[0], args[1:]...); err != nil {
				return err
			}
		}
	case StartAtLogin:
		if err := r.change("write "+pl.UnitPath(c, false), func() error { return writeFile(pl.UnitPath(c, false), pl.SystemdUnit(c, false), 0o644) }); err != nil {
			return err
		}
		r.progress(.3, "registering "+ServiceName(c)+" with systemd")
		for _, args := range [][]string{
			{"--user", "daemon-reload"},
			{"--user", "enable", ServiceName(c)},
			{"--user", "restart", ServiceName(c)},
		} {
			if err := r.run("systemctl", args...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Runner) autostartMac(c, mode string) error {
	pl := r.Plan
	switch mode {
	case StartAtBoot:
		tmp := filepath.Join(filepath.Dir(r.staging), launchdLabel(c)+".plist")
		if err := r.change("write the launchd job", func() error { return writeFile(tmp, pl.LaunchdPlist(c, true), 0o644) }); err != nil {
			return err
		}
		_ = r.root("launchctl", "bootout", "system/"+launchdLabel(c))
		for _, args := range [][]string{
			{"install", "-m", "644", "-o", "root", "-g", "wheel", tmp, pl.PlistPath(c, true)},
			{"launchctl", "bootstrap", "system", pl.PlistPath(c, true)},
		} {
			if err := r.root(args[0], args[1:]...); err != nil {
				return err
			}
		}
	case StartAtLogin:
		if err := r.change("write "+pl.PlistPath(c, false), func() error { return writeFile(pl.PlistPath(c, false), pl.LaunchdPlist(c, false), 0o644) }); err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		_ = r.run("launchctl", "bootout", domain+"/"+launchdLabel(c))
		return r.run("launchctl", "bootstrap", domain, pl.PlistPath(c, false))
	}
	return nil
}

func (r *Runner) autostartWindows(c, mode string) error {
	pl := r.Plan
	if mode == StartNever {
		return nil
	}
	if err := r.change("write "+pl.LauncherPath(c), func() error { return writeFile(pl.LauncherPath(c), pl.WindowsLauncher(c), 0o644) }); err != nil {
		return err
	}
	if pl.Platform.Admin {
		// Let people on the network reach it, without Windows asking.
		_ = r.run("netsh", "advfirewall", "firewall", "delete", "rule", "name="+windowsName(c))
		_ = r.run("netsh", "advfirewall", "firewall", "add", "rule", "name="+windowsName(c), "dir=in", "action=allow", "program="+pl.Binary(c), "enable=yes")
	} else {
		r.Result.Notes = append(r.Result.Notes, fmt.Sprintf("If Windows asks whether %s may use the network, allow it (private networks), so others can reach it.", windowsName(c)))
	}
	switch mode {
	case StartAtBoot:
		if err := r.run("schtasks.exe", "/Create", "/TN", windowsName(c), "/TR", pl.launcherCommand(c), "/SC", "ONSTART", "/RU", "SYSTEM", "/F"); err != nil {
			return err
		}
		return r.run("schtasks.exe", "/Run", "/TN", windowsName(c))
	default:
		if err := r.change("start it when you sign in", func() error { return setRunAtLogin(windowsName(c), pl.launcherCommand(c)) }); err != nil {
			return err
		}
		return r.launch("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", pl.LauncherPath(c))
	}
}
