package installer

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRealInstall installs a real server and hub (and, on Linux and
// macOS, the client) into a temporary home, starts them the way people
// would, checks they answer, then takes everything down again. It changes
// the computer, so it only runs when asked:
//
//	CONCORD_E2E_SRC=<folder with concord-server, -hub, -client> go test -run TestRealInstall -v ./internal/installer
//
// CONCORD_E2E_START picks how they start (default: login where possible).
func TestRealInstall(t *testing.T) {
	src := os.Getenv("CONCORD_E2E_SRC")
	if src == "" {
		t.Skip("set CONCORD_E2E_SRC to run a real install")
	}
	home := t.TempDir()
	p := Detect()
	// User services must sit in your real home for systemd to find them:
	// CONCORD_E2E_REAL_HOME=1 keeps it (the folders stay temporary).
	realHome := os.Getenv("CONCORD_E2E_REAL_HOME") != ""
	if !realHome {
		p.Home = home
	}
	pl := NewPlan(p)
	pl.Source = src
	pl.Components = []string{Server, Hub}
	if runtime.GOOS != "windows" && !realHome {
		pl.Components = append(pl.Components, Client) // with your real home it would change your PATH
		t.Setenv("HOME", home)
	}
	pl.ServerDir, pl.HubDir = filepath.Join(home, "concord", "server"), filepath.Join(home, "concord", "hub")
	pl.ClientDir = filepath.Join(home, ".local", "share", "concord")
	pl.ServerPort, pl.HubPort = freePort(t), freePort(t)
	pl.TermsAccepted = true
	pl.ServerStart, pl.HubStart = StartAtLogin, StartAtLogin
	if s := os.Getenv("CONCORD_E2E_START"); s != "" {
		pl.ServerStart, pl.HubStart = s, s
	}
	if !p.CanStartAtLogin() && pl.ServerStart == StartAtLogin {
		pl.ServerStart, pl.HubStart = StartNever, StartNever
	}

	r := &Runner{Plan: pl, Log: func(l string) { t.Log("  " + l) }}
	t.Cleanup(func() { takeDown(t, r) })
	for _, s := range r.Steps() {
		t.Log(s.Title)
		if err := s.Run(context.Background()); err != nil {
			for _, c := range []string{Server, Hub} {
				if b, e := os.ReadFile(pl.LogFile(c)); e == nil {
					t.Logf("%s log:\n%s", c, b)
				}
			}
			t.Fatalf("%s: %v", s.Title, err)
		}
	}
	if pl.ServerStart != StartNever && (!r.Result.Running[Server] || !r.Result.Running[Hub]) {
		t.Fatalf("not running: %v", r.Result.Running)
	}
	for _, c := range pl.Components {
		if _, err := os.Stat(pl.Binary(c)); err != nil {
			t.Fatalf("no %s program: %v", c, err)
		}
	}
	if _, err := os.Stat(filepath.Join(pl.ServerDir, "concord.db")); pl.ServerStart != StartNever && err != nil {
		t.Fatal("the server isn't running from its own folder (no database there)")
	}

	// Installing again updates in place: the settings survive.
	before, _ := os.ReadFile(filepath.Join(pl.ServerDir, ServerConfigFile))
	r2 := &Runner{Plan: pl, Log: func(l string) { t.Log("  " + l) }}
	for _, s := range r2.Steps() {
		if err := s.Run(context.Background()); err != nil {
			t.Fatalf("update, %s: %v", s.Title, err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(pl.ServerDir, ServerConfigFile))
	if !r2.Result.Updated[Server] || string(before) != string(after) {
		t.Fatal("updating didn't keep the server's settings")
	}
	if pl.ServerStart != StartNever && !r2.Result.Running[Server] {
		t.Fatal("the server didn't come back after the update")
	}
	t.Logf("result: %+v", r2.Result)
}

func freePort(t *testing.T) string {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// takeDown stops what the test started and removes its start-up entries.
func takeDown(t *testing.T, r *Runner) {
	pl := r.Plan
	for _, c := range []string{Server, Hub} {
		r.stop(c)
		r.clearAutostart(c, "none")
		if runtime.GOOS == "linux" {
			exec.Command("systemctl", "--user", "daemon-reload").Run()
		}
	}
	if runtime.GOOS == "windows" {
		// The launcher started them detached; make sure they're gone so
		// the temporary folder can be removed.
		for _, c := range []string{Server, Hub} {
			exec.Command("powershell.exe", "-NoProfile", "-Command",
				"Get-Process | Where-Object { $_.Path -eq '"+strings.ReplaceAll(pl.Binary(c), "'", "''")+"' } | Stop-Process -Force").Run()
		}
		time.Sleep(time.Second)
	}
}
