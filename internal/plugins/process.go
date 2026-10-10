package plugins

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	charmlog "github.com/charmbracelet/log"
	"github.com/concord-chat/concord/internal/models"
)

// StatusFunc is called whenever a supervised plugin process's status changes,
// so the caller (Manager) can persist it without process.go depending on the
// database package.
type StatusFunc func(status, lastError string)

// Supervisor spawns and supervises one plugin's OS process: start, pipe its
// stdout/stderr into Concord's logger, restart on unexpected exit with
// exponential backoff up to a limit, and stop on request.
//
// A Supervisor runs exactly once: Start, then Stop. Restarting a plugin
// (enable after disable, update, admin restart) builds a new Supervisor with
// a fresh token, so no state from a previous run can leak into the next.
// Stop is safe at any point -- before the first spawn, mid-spawn, during a
// backoff sleep, or while the process runs -- and returns only once the
// supervision loop has fully exited, so no process can be started after it.
type Supervisor struct {
	pluginID string
	manifest *Manifest
	env      map[string]string
	log      *charmlog.Logger
	onStatus StatusFunc

	mu      sync.Mutex
	cmd     *exec.Cmd
	started bool
	stopped bool
	stopCh  chan struct{} // closed by Stop; wakes a backoff sleep
	done    chan struct{} // closed when superviseLoop exits
}

// Backoff between crash restarts: starts at [process].restart_backoff_seconds
// (default 2s), doubles per consecutive crash up to maxBackoff, and resets
// once a run stays up for healthyRun. max_restarts counts consecutive crashes.
const (
	defaultBackoff = 2 * time.Second
	maxBackoff     = 60 * time.Second
	healthyRun     = 60 * time.Second
)

// errStopped is runOnce's result when Stop won the race with a spawn.
var errStopped = fmt.Errorf("supervisor stopped")

// NewSupervisor builds a Supervisor for one plugin. env is merged over the
// manifest's own [process.env] map and over the plugin's declared entrypoint
// args (which may reference ${VAR} placeholders).
func NewSupervisor(pluginID string, manifest *Manifest, env map[string]string, log *charmlog.Logger, onStatus StatusFunc) *Supervisor {
	return &Supervisor{
		pluginID: pluginID,
		manifest: manifest,
		env:      env,
		log:      log,
		onStatus: onStatus,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the supervision loop in the background. Calling it twice,
// or after Stop, is an error.
func (s *Supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return fmt.Errorf("plugin %q supervisor already used; build a new one", s.pluginID)
	}
	s.started = true
	go s.superviseLoop()
	return nil
}

// Done is closed once the supervision loop has exited -- after Stop, or
// after the plugin crashed more times than it's allowed to restart.
func (s *Supervisor) Done() <-chan struct{} { return s.done }

func (s *Supervisor) superviseLoop() {
	defer close(s.done)

	maxRestarts := s.manifest.Process.MaxRestarts
	if maxRestarts <= 0 {
		maxRestarts = 5
	}
	base := time.Duration(s.manifest.Process.RestartBackoffSeconds) * time.Second
	if base <= 0 {
		base = defaultBackoff
	}
	backoff := base

	crashes := 0
	for {
		s.setStatus(models.PluginStatusStarting, "")
		began := time.Now()
		err := s.runOnce()
		if s.isStopped() {
			s.setStatus(models.PluginStatusStopped, "")
			return
		}
		if err == nil {
			// Clean exit while not asked to stop — treat as crashed; a
			// well-behaved plugin should run until Stop() is called.
			err = fmt.Errorf("process exited unexpectedly")
		}
		if time.Since(began) >= healthyRun {
			crashes, backoff = 0, base // it had been fine; start counting afresh
		}

		if !s.manifest.Process.RestartOnCrash || crashes >= maxRestarts {
			s.log.Error("plugin exited, not restarting", "plugin", s.pluginID, "error", err, "crashes", crashes)
			s.setStatus(models.PluginStatusCrashed, err.Error())
			return
		}

		crashes++
		s.log.Warn("plugin exited, restarting", "plugin", s.pluginID, "error", err, "attempt", crashes, "backoff", backoff)
		s.setStatus(models.PluginStatusCrashed, err.Error())
		select {
		case <-time.After(backoff):
		case <-s.stopCh:
			s.setStatus(models.PluginStatusStopped, "")
			return
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (s *Supervisor) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Supervisor) setStatus(status, lastError string) {
	if s.onStatus != nil {
		s.onStatus(status, lastError)
	}
}

// runOnce spawns the process once and blocks until it exits.
func (s *Supervisor) runOnce() error {
	ep, err := s.manifest.Entrypoint()
	if err != nil {
		return err
	}

	binPath := ep.Bin
	if !isAbsPath(binPath) {
		binPath = joinPath(s.manifest.Dir, binPath)
	}

	args := make([]string, len(ep.Args))
	for i, a := range ep.Args {
		args[i] = expandEnv(a, s.env)
	}

	cmd := exec.Command(binPath, args...)
	workDir := s.manifest.Process.WorkingDir
	if workDir == "" {
		workDir = "."
	}
	if !isAbsPath(workDir) {
		workDir = joinPath(s.manifest.Dir, workDir)
	}
	cmd.Dir = workDir

	cmd.Env = pluginBaseEnv(os.Environ())
	for k, v := range s.manifest.Process.Env {
		cmd.Env = append(cmd.Env, k+"="+expandEnv(v, s.env))
	}
	for k, v := range s.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	prefix := fmt.Sprintf("[plugin:%s] ", s.pluginID)
	cmd.Stdout = &lineWriter{log: s.log, prefix: prefix, level: "info"}
	cmd.Stderr = &lineWriter{log: s.log, prefix: prefix, level: "warn"}

	// Checking stopped and publishing s.cmd under the same lock as Stop
	// means Stop either sees this process (and kills it) or prevents it
	// from ever starting -- never neither.
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return errStopped
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to start plugin process: %w", err)
	}
	s.cmd = cmd
	s.mu.Unlock()

	s.setStatus(models.PluginStatusSpawned, "")
	s.log.Info("plugin process started", "plugin", s.pluginID, "pid", cmd.Process.Pid)

	err = cmd.Wait()

	s.mu.Lock()
	s.cmd = nil
	s.mu.Unlock()

	return err
}

// Stop ends supervision: no further restarts, and the running process (if
// any) is asked to exit, then killed at ctx's deadline. It waits for the
// supervision loop to finish. Safe to call more than once, and on a
// Supervisor that was never started.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
	started := s.started
	cmd := s.cmd
	s.mu.Unlock()

	if !started {
		return nil
	}
	if cmd != nil && cmd.Process != nil {
		// os.Interrupt isn't implemented on Windows — Process.Signal returns
		// an error immediately rather than delivering anything, so go
		// straight to Kill instead of waiting out a graceful exit that
		// will never happen.
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			_ = cmd.Process.Kill()
		}
	}

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		cmd = s.cmd
		s.mu.Unlock()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-s.done
		return ctx.Err()
	}
}

// IsRunning reports whether a process is currently active.
func (s *Supervisor) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil
}

// lineWriter pipes a subprocess's output into Concord's structured logger,
// one log line per line of child output.
type lineWriter struct {
	log    *charmlog.Logger
	prefix string
	level  string
	buf    strings.Builder
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		s := w.buf.String()
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(s[:idx], "\r")
		if line != "" {
			if w.level == "warn" {
				w.log.Warn(w.prefix + line)
			} else {
				w.log.Info(w.prefix + line)
			}
		}
		w.buf.Reset()
		w.buf.WriteString(s[idx+1:])
	}
	return len(p), nil
}
