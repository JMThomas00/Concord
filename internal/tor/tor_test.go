package tor

import (
	"bufio"
	"context"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClientKeys(t *testing.T) {
	k, err := NewClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k.PrivateText()) != 52 || len(k.PublicText()) != 52 {
		t.Fatalf("keys are 52 base32 characters: %q %q", k.PrivateText(), k.PublicText())
	}
	back, err := ParseClientKey(" " + strings.ToLower(k.PrivateText()) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if back.PublicText() != k.PublicText() {
		t.Fatal("a parsed key has a different public key")
	}
	if got := k.ServiceAuthLine(); got != "descriptor:x25519:"+k.PublicText() {
		t.Fatalf("service line %q", got)
	}
	onion := strings.Repeat("a", 56) + ".onion"
	if got := k.ClientAuthLine(onion); got != strings.Repeat("a", 56)+":descriptor:x25519:"+k.PrivateText() {
		t.Fatalf("client line %q", got)
	}
	if _, err := ParseClientKey("not a key"); err == nil {
		t.Fatal("rubbish parsed as a key")
	}
}

func TestIsOnion(t *testing.T) {
	good := strings.Repeat("a", 54) + "id.onion"
	for host, want := range map[string]bool{
		good:                               true,
		strings.ToUpper(good):              true,
		"example.com":                      false,
		strings.Repeat("a", 56):            false,
		strings.Repeat("a", 16) + ".onion": false, // v2, gone
		strings.Repeat("1", 56) + ".onion": false, // not base32
	} {
		if IsOnion(host) != want {
			t.Errorf("IsOnion(%q) = %v", host, !want)
		}
	}
}

func TestTorrc(t *testing.T) {
	o := Options{Dir: `/data/my tor`, Lines: []string{"SocksPort 0", "HiddenServiceDir " + Quote(`C:\x "y"`)}}
	rc := o.torrc()
	for _, want := range []string{
		`DataDirectory "/data/my tor/state"`,
		"ControlPort auto",
		"CookieAuthentication 1",
		"__OwningControllerProcess ",
		"SocksPort 0",
		`HiddenServiceDir "C:\\x \"y\""`,
	} {
		if !strings.Contains(rc, want) {
			t.Errorf("torrc lacks %q:\n%s", want, rc)
		}
	}
}

func TestLogLines(t *testing.T) {
	p := &Process{ready: make(chan struct{})}
	p.noteLine("Nov 05 17:00:00.000 [notice] Bootstrapped 45% (requesting_descriptors): Asking for relay descriptors")
	if st := p.Status(); st.Bootstrapped != 45 || st.Phase != "Asking for relay descriptors" {
		t.Fatalf("status %+v", st)
	}
	p.noteLine("Nov 05 17:00:01.000 [warn] Problem bootstrapping. Stuck at 45%")
	if st := p.Status(); !strings.Contains(st.Err, "Problem bootstrapping") {
		t.Fatalf("warning not kept: %+v", st)
	}
	p.noteLine("Nov 05 17:00:02.000 [notice] Bootstrapped 100% (done): Done")
	select {
	case <-p.ready:
	default:
		t.Fatal("100% isn't ready")
	}
	if st := p.Status(); st.Err != "" {
		t.Fatalf("a connected tor still shows the old warning: %q", st.Err)
	}
}

// A control port that checks the cookie and takes one command.
func TestControl(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cookie := []byte("0123456789abcdef0123456789abcdef")
	got := make(chan []string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		var lines []string
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				break
			}
			l = strings.TrimRight(l, "\r\n")
			lines = append(lines, l)
			if strings.HasPrefix(l, "AUTHENTICATE ") && l != "AUTHENTICATE "+hex.EncodeToString(cookie) {
				c.Write([]byte("515 Authentication failed\r\n"))
				break
			}
			if l == "QUIT" {
				c.Write([]byte("250 closing connection\r\n"))
				break
			}
			c.Write([]byte("250 OK\r\n"))
		}
		got <- lines
	}()
	if err := Control(ln.Addr().String(), cookie, "SIGNAL RELOAD"); err != nil {
		t.Fatal(err)
	}
	lines := <-got
	if len(lines) < 2 || lines[1] != "SIGNAL RELOAD" {
		t.Fatalf("control port got %q", lines)
	}
}

// A stand-in tor (a shell script) is started, followed and stopped.
func TestProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in tor is a shell script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-tor")
	script := "#!/bin/sh\n" +
		"echo \"$@\" > \"" + filepath.Join(dir, "args") + "\"\n" +
		"echo 'Nov 05 17:00:00.000 [notice] Bootstrapped 10% (conn): Connecting'\n" +
		"echo 'Nov 05 17:00:00.100 [notice] Bootstrapped 100% (done): Done'\n" +
		"exec sleep 60\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := Start(Options{TorPath: fake, Dir: filepath.Join(dir, "tor"), Lines: []string{"SocksPort 0"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if st := p.Status(); !st.Running || st.Bootstrapped != 100 {
		t.Fatalf("status %+v", st)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if !strings.Contains(string(args), "-f "+filepath.Join(dir, "tor", "torrc")) {
		t.Fatalf("tor was run with %q", args)
	}
	fi, err := os.Stat(filepath.Join(dir, "tor"))
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("tor's folder must be private: %v %v", fi.Mode(), err)
	}
	done := make(chan struct{})
	go func() { p.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Stop didn't stop it")
	}
	if p.Status().Running {
		t.Fatal("still running after Stop")
	}
}
