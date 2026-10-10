package installer

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestBootStart checks "Always: in the background from startup" across a
// real restart, in two runs with a reboot between (as root on Linux, as an
// administrator on Windows):
//
//	CONCORD_E2E_BOOT=install CONCORD_E2E_SRC=<binaries> CONCORD_E2E_DIR=<folder> go test -run TestBootStart ./internal/installer
//	(restart the computer)
//	CONCORD_E2E_BOOT=check CONCORD_E2E_DIR=<folder> go test -run TestBootStart ./internal/installer
//
// install puts the server and hub in <folder> on ports 18090/17777 and
// leaves them; check finds both answering, then uninstalls everything.
func TestBootStart(t *testing.T) {
	stage, dir := os.Getenv("CONCORD_E2E_BOOT"), os.Getenv("CONCORD_E2E_DIR")
	if stage == "" || dir == "" {
		t.Skip("set CONCORD_E2E_BOOT=install|check and CONCORD_E2E_DIR to test starting at boot")
	}
	const serverPort, hubPort = 18090, 17777
	p := Detect()
	if !p.CanStartAtBoot() {
		t.Skip("this computer can't start programs at boot")
	}
	// A home of its own inside dir: uninstalling clears the home's .concord, and
	// on a real computer that would be the user's profiles and servers.
	p.Home = filepath.Join(dir, "home")
	if err := os.MkdirAll(p.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	pl := NewPlan(p)
	pl.Components = []string{Server, Hub}
	pl.ServerDir, pl.HubDir = filepath.Join(dir, "server"), filepath.Join(dir, "hub")
	pl.ServerPort, pl.HubPort = strconv.Itoa(serverPort), strconv.Itoa(hubPort)
	pl.TermsAccepted = true
	pl.ServerStart, pl.HubStart = StartAtBoot, StartAtBoot
	logf := func(l string) { t.Log("  " + l) }

	switch stage {
	case "install":
		pl.Source = os.Getenv("CONCORD_E2E_SRC")
		r := &Runner{Plan: pl, Log: logf}
		for _, s := range r.Steps() {
			t.Log(s.Title)
			if err := s.Run(context.Background()); err != nil {
				t.Fatalf("%s: %v", s.Title, err)
			}
		}
		if !r.Result.Running[Server] || !r.Result.Running[Hub] {
			t.Fatalf("not running after the install: %v", r.Result.Running)
		}
		t.Log("installed; restart the computer, then run with CONCORD_E2E_BOOT=check")
	case "check":
		// Both should be answering on their own: give a slow boot a minute.
		for _, u := range []string{
			"http://127.0.0.1:" + strconv.Itoa(serverPort) + "/api/health",
			"http://127.0.0.1:" + strconv.Itoa(hubPort) + "/v1/health",
		} {
			if err := waitHealthy(context.Background(), u, time.Minute); err != nil {
				t.Errorf("after the restart, %s: %v", u, err)
			} else {
				t.Logf("after the restart, %s answers", u)
			}
		}
		rec := LoadRecord(p.Home)
		again := NewPlan(p)
		again.ApplyRecord(rec)
		r := &Runner{Plan: again, Log: logf}
		for _, s := range r.UninstallSteps() {
			t.Log(s.Title)
			if err := s.Run(context.Background()); err != nil {
				t.Fatalf("uninstall, %s: %v", s.Title, err)
			}
		}
		for _, c := range pl.Components {
			if _, err := os.Stat(pl.Dir(c)); err == nil {
				t.Errorf("the %s's folder is still there", c)
			}
		}
	default:
		t.Fatalf("CONCORD_E2E_BOOT=%q: want install or check", stage)
	}
}

