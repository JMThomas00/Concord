package plugins

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	charmlog "github.com/charmbracelet/log"
	"github.com/concord-chat/concord/internal/models"
)

// TestHelperProcess is the stand-in plugin process for supervisor tests:
// the test binary re-runs itself with PLUGIN_HELPER_MODE set.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("PLUGIN_HELPER_MODE") {
	case "":
		return // an ordinary test run
	case "exit":
		os.Exit(3)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

// helperManifest describes a plugin whose entrypoint is this test binary
// running TestHelperProcess in the given mode.
func helperManifest(t *testing.T, mode string, maxRestarts, backoffSeconds int) *Manifest {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ep := &EntrypointDef{Bin: exe, Args: []string{"-test.run=^TestHelperProcess$"}}
	m := &Manifest{Dir: t.TempDir()}
	m.Plugin.ID = "helper"
	m.Process.RestartOnCrash = true
	m.Process.MaxRestarts = maxRestarts
	m.Process.RestartBackoffSeconds = backoffSeconds
	m.Process.Env = map[string]string{"PLUGIN_HELPER_MODE": mode}
	m.Process.Entrypoint.Windows, m.Process.Entrypoint.Linux, m.Process.Entrypoint.Darwin = ep, ep, ep
	return m
}

// statusLog records every status a supervisor reports.
type statusLog struct {
	mu       sync.Mutex
	statuses []string
}

func (l *statusLog) record(status, _ string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statuses = append(l.statuses, status)
}

func (l *statusLog) count(status string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.statuses {
		if s == status {
			n++
		}
	}
	return n
}

func (l *statusLog) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.statuses) == 0 {
		return ""
	}
	return l.statuses[len(l.statuses)-1]
}

func quietLogger() *charmlog.Logger { return charmlog.New(io.Discard) }

func waitDone(t *testing.T, s *Supervisor, within time.Duration) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(within):
		t.Fatalf("supervisor still running after %v", within)
	}
}

func TestSupervisorStopKillsRunningProcess(t *testing.T) {
	var log statusLog
	s := NewSupervisor("helper", helperManifest(t, "sleep", 5, 1), nil, quietLogger(), log.record)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for log.count(models.PluginStatusSpawned) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitDone(t, s, time.Second)
	if got := log.last(); got != models.PluginStatusStopped {
		t.Fatalf("final status %q, want stopped", got)
	}
	if s.IsRunning() {
		t.Fatal("process still tracked as running after Stop")
	}
}

// Stop during a long backoff sleep must return at once, and nothing may be
// spawned afterwards (the old Stop returned immediately when no process
// happened to be running and left the loop free to start another).
func TestSupervisorStopDuringBackoffReturnsPromptlyAndSpawnsNothingMore(t *testing.T) {
	var log statusLog
	s := NewSupervisor("helper", helperManifest(t, "exit", 5, 30), nil, quietLogger(), log.record)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for log.count(models.PluginStatusCrashed) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	began := time.Now()
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("Stop took %v during a 30s backoff", took)
	}
	spawned := log.count(models.PluginStatusSpawned)
	time.Sleep(300 * time.Millisecond)
	if log.count(models.PluginStatusSpawned) != spawned {
		t.Fatal("a process was spawned after Stop returned")
	}
}

func TestSupervisorGivesUpAfterMaxRestarts(t *testing.T) {
	var log statusLog
	s := NewSupervisor("helper", helperManifest(t, "exit", 1, 1), nil, quietLogger(), log.record)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, 20*time.Second)
	if got := log.count(models.PluginStatusSpawned); got != 2 {
		t.Fatalf("spawned %d times, want 2 (first run + 1 restart)", got)
	}
	if got := log.last(); got != models.PluginStatusCrashed {
		t.Fatalf("final status %q, want crashed", got)
	}
}

func TestSupervisorIsSingleUse(t *testing.T) {
	s := NewSupervisor("helper", helperManifest(t, "sleep", 1, 1), nil, quietLogger(), nil)
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	if err := s.Start(); err == nil {
		t.Fatal("Start after Stop should fail; a stopped supervisor is done for good")
	}
}
