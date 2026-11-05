package tor

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Options says how to run tor.
type Options struct {
	TorPath string   // the program (Find)
	Dir     string   // tor's folder of its own: settings, state, control files
	Lines   []string // more settings: the onion service, a SOCKS port, bridges
	// Log receives tor's warnings and Concord's notes about it (level is
	// "info", "warn" or "error"). Optional.
	Log func(level, msg string)
}

// Status is how tor is doing.
type Status struct {
	Running      bool
	Bootstrapped int    // percent connected to the Tor network
	Phase        string // what it's doing, while not yet at 100
	Err          string // the last warning or error
	Restarts     int
}

// Process is a running tor, restarted if it stops by itself.
type Process struct {
	opts Options

	mu      sync.Mutex
	status  Status
	cmd     *exec.Cmd
	stopped bool

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	ready    chan struct{} // closed at 100%
	readyOne sync.Once
}

// Quote is a torrc value in quotes (paths with spaces or backslashes).
func Quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// torrc is the settings file Concord gives tor.
func (o Options) torrc() string {
	lines := []string{
		"# Written by Concord each time it starts tor; changes here are lost.",
		"DataDirectory " + Quote(filepath.Join(o.Dir, "state")),
		"ControlPort auto",
		"ControlPortWriteToFile " + Quote(filepath.Join(o.Dir, "control-port")),
		"CookieAuthentication 1",
		"CookieAuthFile " + Quote(filepath.Join(o.Dir, "control-cookie")),
		// tor exits by itself if Concord does, however that happens.
		"__OwningControllerProcess " + strconv.Itoa(os.Getpid()),
		"Log notice stdout",
		"AvoidDiskWrites 1",
	}
	return strings.Join(append(lines, o.Lines...), "\n") + "\n"
}

// Start writes the settings and starts tor. It doesn't wait for tor to
// connect (WaitReady does).
func Start(opts Options) (*Process, error) {
	if opts.TorPath == "" {
		return nil, ErrNotInstalled
	}
	for _, d := range []string{opts.Dir, filepath.Join(opts.Dir, "state")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		_ = os.Chmod(d, 0o700) // tor refuses folders others can read
	}
	if err := os.WriteFile(filepath.Join(opts.Dir, "torrc"), []byte(opts.torrc()), 0o600); err != nil {
		return nil, err
	}
	// An empty defaults file, so a system-wide torrc-defaults isn't read.
	if err := os.WriteFile(filepath.Join(opts.Dir, "torrc-defaults"), nil, 0o600); err != nil {
		return nil, err
	}
	p := &Process{opts: opts, stop: make(chan struct{}), done: make(chan struct{}), ready: make(chan struct{})}
	if err := p.launch(); err != nil {
		return nil, err
	}
	go p.supervise()
	return p, nil
}

func (p *Process) log(level, msg string) {
	if p.opts.Log != nil {
		p.opts.Log(level, msg)
	}
}

// launch starts one tor.
func (p *Process) launch() error {
	_ = os.Remove(filepath.Join(p.opts.Dir, "control-port"))
	cmd := exec.Command(p.opts.TorPath,
		"-f", filepath.Join(p.opts.Dir, "torrc"),
		"--defaults-torrc", filepath.Join(p.opts.Dir, "torrc-defaults"))
	hideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	p.mu.Lock()
	if p.stopped { // Stop came first: never leave a tor behind
		p.mu.Unlock()
		return errStopped
	}
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("starting tor: %w", err)
	}
	p.cmd = cmd
	p.status.Running = true
	p.status.Bootstrapped = 0
	p.status.Phase = "starting"
	p.mu.Unlock()
	go p.read(out)
	return nil
}

var errStopped = errors.New("tor was stopped")

var (
	bootstrapRe = regexp.MustCompile(`Bootstrapped (\d+)%[^:]*: (.*)$`)
	levelRe     = regexp.MustCompile(`\[(warn|err)\] (.*)$`)
)

// read follows tor's log: progress, warnings and errors.
func (p *Process) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		p.noteLine(sc.Text())
	}
}

func (p *Process) noteLine(line string) {
	if m := bootstrapRe.FindStringSubmatch(line); m != nil {
		pct, _ := strconv.Atoi(m[1])
		p.mu.Lock()
		p.status.Bootstrapped = pct
		p.status.Phase = strings.TrimSpace(m[2])
		if pct == 100 {
			p.status.Err = ""
		}
		p.mu.Unlock()
		if pct == 100 {
			p.readyOne.Do(func() { close(p.ready) })
			p.log("info", "Connected to the Tor network")
		}
		return
	}
	if m := levelRe.FindStringSubmatch(line); m != nil {
		p.mu.Lock()
		p.status.Err = strings.TrimSpace(m[2])
		p.mu.Unlock()
		level := "warn"
		if m[1] == "err" {
			level = "error"
		}
		p.log(level, "tor: "+strings.TrimSpace(m[2]))
	}
}

// supervise restarts tor when it stops by itself: after 2 s, doubling to a
// minute, back to 2 s after a run of a minute or more.
func (p *Process) supervise() {
	defer close(p.done)
	backoff := 2 * time.Second
	for {
		p.mu.Lock()
		cmd := p.cmd
		p.mu.Unlock()
		started := time.Now()
		err := cmd.Wait()
		p.mu.Lock()
		p.status.Running = false
		p.status.Phase = "stopped"
		p.mu.Unlock()
		select {
		case <-p.stop:
			return
		default:
		}
		if time.Since(started) >= time.Minute {
			backoff = 2 * time.Second
		}
		msg := "tor stopped"
		if err != nil {
			msg += ": " + err.Error()
		}
		p.log("warn", msg+"; starting it again in "+backoff.String())
		p.mu.Lock()
		if p.status.Err == "" {
			p.status.Err = msg
		}
		p.status.Restarts++
		p.mu.Unlock()
		for {
			select {
			case <-p.stop:
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > time.Minute {
				backoff = time.Minute
			}
			err := p.launch()
			if err == nil {
				break
			}
			if errors.Is(err, errStopped) {
				return
			}
			p.log("error", err.Error())
			p.mu.Lock()
			p.status.Err = err.Error()
			p.mu.Unlock()
		}
	}
}

// Status is how tor is doing now.
func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// WaitReady waits until tor is connected to the Tor network.
func (p *Process) WaitReady(ctx context.Context) error {
	select {
	case <-p.ready:
		return nil
	case <-p.done:
		return errors.New("tor stopped")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop stops tor and waits for it to exit.
func (p *Process) Stop() {
	p.stopOnce.Do(func() {
		close(p.stop)
		p.mu.Lock()
		p.stopped = true
		cmd := p.cmd
		p.mu.Unlock()
		if cmd != nil && cmd.Process != nil {
			terminate(cmd.Process)
		}
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-p.done
		}
	})
}

// Reload has tor read its settings again (and an onion service's
// authorized clients), without dropping connections.
func (p *Process) Reload() error {
	return p.control("SIGNAL RELOAD")
}

// control sends one command over tor's control port.
func (p *Process) control(command string) error {
	portFile, err := os.ReadFile(filepath.Join(p.opts.Dir, "control-port"))
	if err != nil {
		return fmt.Errorf("tor's control port isn't open yet: %w", err)
	}
	addr := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(portFile)), "PORT="))
	cookie, err := os.ReadFile(filepath.Join(p.opts.Dir, "control-cookie"))
	if err != nil {
		return err
	}
	return Control(addr, cookie, command)
}

// Control authenticates to a control port with a cookie and sends one
// command, expecting 250.
func Control(addr string, cookie []byte, command string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	ask := func(line string) error {
		if _, err := io.WriteString(conn, line+"\r\n"); err != nil {
			return err
		}
		for {
			reply, err := r.ReadString('\n')
			if err != nil {
				return err
			}
			reply = strings.TrimRight(reply, "\r\n")
			if len(reply) < 4 {
				return fmt.Errorf("tor answered %q", reply)
			}
			if reply[3] == '-' || reply[3] == '+' {
				continue // more lines follow
			}
			if !strings.HasPrefix(reply, "250") {
				return fmt.Errorf("tor answered %q", reply)
			}
			return nil
		}
	}
	if err := ask("AUTHENTICATE " + hex.EncodeToString(cookie)); err != nil {
		return err
	}
	if err := ask(command); err != nil {
		return err
	}
	_ = ask("QUIT")
	return nil
}

// AddClientAuth gives a running tor the access key for an onion address,
// so it can reach it straight away (the ClientOnionAuthDir file is what a
// tor started later reads).
func (p *Process) AddClientAuth(onion string, k ClientKey) error {
	host := strings.TrimSuffix(strings.ToLower(onion), ".onion")
	return p.control("ONION_CLIENT_AUTH_ADD " + host + " x25519:" + base64.StdEncoding.EncodeToString(k.Private))
}

// SocksAddr reads a SocksPort setting's address back from the options
// ("" if there's none).
func (o Options) SocksAddr() string {
	for _, l := range o.Lines {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "SocksPort" && f[1] != "0" {
			return f[1]
		}
	}
	return ""
}
