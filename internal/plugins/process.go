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
// backoff up to a limit, and stop gracefully on shutdown.
type Supervisor struct {
	pluginID string
	manifest *Manifest
	env      map[string]string
	log      *charmlog.Logger
	onStatus StatusFunc

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
	done    chan struct{}
}

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
		done:     make(chan struct{}),
	}
}

// Start launches the plugin process and begins the crash-restart supervision
// loop in the background. It returns once the first spawn attempt has been made.
func (s *Supervisor) Start() error {
	s.mu.Lock()
	s.stopped = false
	s.mu.Unlock()

	go s.superviseLoop()
	return nil
}

func (s *Supervisor) superviseLoop() {
	defer close(s.done)

	maxRestarts := s.manifest.Process.MaxRestarts
	if maxRestarts <= 0 {
		maxRestarts = 5
	}
	backoff := time.Duration(s.manifest.Process.RestartBackoffSeconds) * time.Second
	if backoff <= 0 {
		backoff = 5 * time.Second
	}

	attempts := 0
	for {
		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		s.setStatus(models.PluginStatusStarting, "")
		err := s.runOnce()

		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			s.setStatus(models.PluginStatusStopped, "")
			return
		}

		if err == nil {
			// Clean exit while not asked to stop — treat as crashed; a
			// well-behaved plugin should run until Stop() is called.
			err = fmt.Errorf("process exited unexpectedly")
		}

		if !s.manifest.Process.RestartOnCrash || attempts >= maxRestarts {
			s.log.Error("plugin exited, not restarting", "plugin", s.pluginID, "error", err, "attempts", attempts)
			s.setStatus(models.PluginStatusCrashed, err.Error())
			return
		}

		attempts++
		s.log.Warn("plugin exited, restarting", "plugin", s.pluginID, "error", err, "attempt", attempts, "backoff", backoff)
		s.setStatus(models.PluginStatusCrashed, err.Error())
		time.Sleep(backoff)
	}
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

	cmd.Env = os.Environ()
	for k, v := range s.manifest.Process.Env {
		cmd.Env = append(cmd.Env, k+"="+expandEnv(v, s.env))
	}
	for k, v := range s.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	prefix := fmt.Sprintf("[plugin:%s] ", s.pluginID)
	cmd.Stdout = &lineWriter{log: s.log, prefix: prefix, level: "info"}
	cmd.Stderr = &lineWriter{log: s.log, prefix: prefix, level: "warn"}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start plugin process: %w", err)
	}

	s.mu.Lock()
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

// Stop signals the plugin process to exit and waits up to ctx's deadline
// before forcibly killing it.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	cmd := s.cmd
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// os.Interrupt isn't implemented on Windows — Process.Signal returns an
	// error immediately rather than delivering anything, so waiting out the
	// full ctx timeout for a graceful exit that will never happen would make
	// every disable/restart take as long as the timeout. If the signal
	// wasn't actually delivered, go straight to Kill instead of waiting.
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		<-s.done
		return nil
	}

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
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
