package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"

	"github.com/JMThomas00/Concord/sdk/codesign"
	"github.com/concord-chat/concord/internal/protocol"
)

// The client-code tests run a real module: testdata/codeguest, built with
// the SDK for wasip1 once per test run.
var guestBuild struct {
	once sync.Once
	wasm []byte
	err  error
}

func guestModule(t *testing.T) []byte {
	t.Helper()
	guestBuild.once.Do(func() {
		dir, err := os.MkdirTemp("", "codeguest")
		if err != nil {
			guestBuild.err = err
			return
		}
		out := filepath.Join(dir, "guest.wasm")
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", out, ".")
		cmd.Dir = filepath.Join("testdata", "codeguest")
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
		if msg, err := cmd.CombinedOutput(); err != nil {
			guestBuild.err = fmt.Errorf("%v: %s", err, msg)
			return
		}
		guestBuild.wasm, guestBuild.err = os.ReadFile(out)
	})
	if guestBuild.err != nil {
		t.Fatalf("building the guest module: %v", guestBuild.err)
	}
	return guestBuild.wasm
}

func fileFor(p string, data []byte) protocol.PluginClientFile {
	sum := sha256.Sum256(data)
	return protocol.PluginClientFile{Path: p, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

// guestSource is the guest module, signed, with the given capabilities.
func guestSource(t *testing.T, caps ...string) codeSource {
	t.Helper()
	module := guestModule(t)
	pub, privFile, _ := codesign.GenerateKey()
	priv, _ := codesign.ParsePrivateKey(privFile)
	sig := codesign.Sign(priv, module)
	files := map[string][]byte{"plugin.wasm": module, "plugin.wasm.sig": sig}
	return codeSource{
		PluginID: "guest", Name: "Guest", PublisherKey: pub, Capabilities: caps,
		WASM: fileFor("plugin.wasm", module), Sig: fileFor("plugin.wasm.sig", sig),
		Files: map[string]bool{"plugin.wasm": true, "plugin.wasm.sig": true, "sounds/a.wav": true, "pics/x.png": true},
		load: func(f protocol.PluginClientFile) ([]byte, error) {
			if data, ok := files[f.Path]; ok {
				return data, nil
			}
			return nil, os.ErrNotExist
		},
	}
}

// nextCode waits for the runner's next message of type T, applying frames
// as the App would, and returns it.
func nextCode[T codeMsg](t *testing.T, h *codeHost) T {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case m := <-h.out:
			if want, ok := m.(T); ok {
				return want
			}
			if f, ok := m.(codeFrameMsg); ok {
				f.r.takeFrame()
			}
		case <-deadline:
			var zero T
			t.Fatalf("no %T from the client code", zero)
			return zero
		}
	}
}

// frameText waits for the code to draw a frame containing want.
func frameText(t *testing.T, h *codeHost, want string) string {
	t.Helper()
	deadline := time.After(20 * time.Second)
	last := ""
	for {
		select {
		case m := <-h.out:
			if f, ok := m.(codeFrameMsg); ok {
				fr := f.r.takeFrame()
				if fr != nil && fr.text != nil {
					last = *fr.text
					if strings.Contains(last, want) {
						return last
					}
				}
			}
			if e, ok := m.(codeExitMsg); ok {
				t.Fatalf("code exited waiting for %q: %v", want, e.err)
			}
		case <-deadline:
			t.Fatalf("no frame with %q (last %q)", want, last)
			return ""
		}
	}
}

func key(r *codeRunner, k string) { r.push(map[string]any{"type": "key", "key": k}) }

func TestClientCodeRunsSandboxed(t *testing.T) {
	h := newCodeHost(t.TempDir())
	src := guestSource(t, "pane", "images", "sound", "storage", "server")
	r := h.start(src, uuid.New(), map[string]any{"width": 40, "height": 12})
	defer r.stop()

	frameText(t, h, "hello 40x12")
	r.push(map[string]any{"type": "resize", "width": 50, "height": 9})
	frameText(t, h, "size 50x9")

	key(r, "send")
	if m := nextCode[codeSendMsg](t, h); string(m.data) != `{"from":"code"}` {
		t.Fatalf("sent %s", m.data)
	}
	key(r, "sound")
	if m := nextCode[codeSoundMsg](t, h); m.asset != "sounds/a.wav" || m.volume != 0.5 {
		t.Fatalf("sound %+v", m)
	}
	key(r, "image")
	frameText(t, h, "pic")
	key(r, "badimage")
	frameText(t, h, "badimage: concord: not found")
	key(r, "timer")
	frameText(t, h, "tick t1")
	key(r, "set")
	key(r, "get")
	frameText(t, h, `got "remembered" true <nil>`)
	r.push(map[string]any{"type": "server", "data": json.RawMessage(`{"n":1}`)})
	frameText(t, h, `server {"n":1}`)
	r.push(map[string]any{"type": "server_frame", "text": "board"})
	frameText(t, h, "saw board")
	key(r, "local")
	if m := nextCode[codeForwardMsg](t, h); m.forward {
		t.Fatal("forward_keys(false) arrived as true")
	}

	// Two hundred frames in a burst reach the UI as a handful.
	key(r, "burst")
	frameText(t, h, "burst 199")

	// Leaving the pane ends it cleanly.
	r.stop()
	if e := nextCode[codeExitMsg](t, h); e.err != nil {
		t.Fatalf("clean stop: %v", e.err)
	}
}

func TestClientCodeStorageSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	src := guestSource(t, "pane", "storage")
	h := newCodeHost(dir)
	r := h.start(src, uuid.New(), map[string]any{"width": 10, "height": 5})
	frameText(t, h, "hello")
	key(r, "set")
	key(r, "get")
	frameText(t, h, "remembered")
	r.stop()
	nextCode[codeExitMsg](t, h)

	h2 := newCodeHost(dir)
	r2 := h2.start(src, uuid.New(), map[string]any{"width": 10, "height": 5})
	defer r2.stop()
	key(r2, "get")
	frameText(t, h2, `got "remembered" true`)
}

func TestClientCodeCapabilitiesAreEnforced(t *testing.T) {
	h := newCodeHost("")
	r := h.start(guestSource(t, "pane"), uuid.New(), map[string]any{"width": 10, "height": 5})
	defer r.stop()
	frameText(t, h, "hello")
	key(r, "denied")
	frameText(t, h, "denied: concord: capability not granted")
	key(r, "image") // images need "images"
	key(r, "get")   // storage needs "storage"
	frameText(t, h, "capability not granted")
}

// Code that never comes back is stopped after the budget.
func TestClientCodeThatSpinsIsStopped(t *testing.T) {
	h := newCodeHost("")
	r := h.start(guestSource(t, "pane"), uuid.New(), map[string]any{"width": 10, "height": 5})
	frameText(t, h, "hello")
	start := time.Now()
	key(r, "spin")
	e := nextCode[codeExitMsg](t, h)
	if e.err == nil || !strings.Contains(e.err.Error(), "took longer than") {
		t.Fatalf("spinning code ended with %v", e.err)
	}
	if d := time.Since(start); d > codeEventBudget+3*time.Second {
		t.Fatalf("took %v to stop", d)
	}
}

// Code whose signature doesn't match its publisher key never runs.
func TestClientCodeMustBeSigned(t *testing.T) {
	src := guestSource(t, "pane")
	other, _, _ := codesign.GenerateKey()
	src.PublisherKey = other
	h := newCodeHost("")
	r := h.start(src, uuid.New(), map[string]any{"width": 10, "height": 5})
	defer r.stop()
	e := nextCode[codeExitMsg](t, h)
	if e.err == nil || !strings.Contains(e.err.Error(), "signature") {
		t.Fatalf("wrongly signed code: %v", e.err)
	}
}

var _ tea.Msg = codeExitMsg{}

// If wazero's compiler crashes, the code runs on the interpreter instead,
// and so does everything after it.
func TestClientCodeSurvivesACompilerCrash(t *testing.T) {
	orig := compileModule
	defer func() { compileModule = orig }()
	calls := 0
	compileModule = func(rt wazero.Runtime, module []byte) (wazero.CompiledModule, error) {
		calls++
		if calls == 1 {
			panic("BUG: definingBlk should not be nil")
		}
		return orig(rt, module)
	}
	h := newCodeHost("")
	h.interpOnly.Store(false) // as with CONCORD_WASM_COMPILER=1
	r := h.start(guestSource(t, "pane"), uuid.New(), map[string]any{"width": 10, "height": 5})
	defer r.stop()
	frameText(t, h, "hello")
	if !h.interpOnly.Load() || calls != 2 {
		t.Fatalf("interpOnly=%v after %d compiles", h.interpOnly.Load(), calls)
	}
}

// appWithCodePane is an App showing a pane whose plugin ships the guest
// code, with the files already in the asset cache.
func appWithCodePane(t *testing.T, src codeSource) (*App, *PluginPaneState) {
	t.Helper()
	a := settingsTestApp()
	a.pluginAssets = newPluginAssets(t.TempDir())
	var files []protocol.PluginClientFile
	for _, f := range []protocol.PluginClientFile{src.WASM, src.Sig} {
		data, _ := src.load(f)
		a.pluginAssets.store(f.SHA256, data)
		files = append(files, f)
	}
	sc := &ServerConnection{PluginClients: []protocol.PluginClientInfo{{
		PluginID: src.PluginID, Name: src.Name, Version: "1.0", WASM: "plugin.wasm",
		Capabilities: src.Capabilities, PublisherKey: src.PublisherKey, Files: files,
	}}}
	p := &PluginPaneState{ChannelID: uuid.New(), PluginID: src.PluginID, conn: sc, entered: true, renderW: 40, renderH: 12}
	a.pluginPane = p
	t.Cleanup(func() { p.stopPaneCode() })
	return a, p
}

// paneShows runs the App's code messages until the pane shows want.
func paneShows(t *testing.T, a *App, want string) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		if p := a.pluginPane; p.local != nil && p.local.text != nil && strings.Contains(*p.local.text, want) {
			return
		}
		select {
		case m := <-a.codeHost.out:
			a.handleCodeMsg(m.(codeMsg))
		case <-deadline:
			t.Fatalf("the pane never showed %q (status %q)", want, a.statusMessage)
		}
	}
}

func TestPaneAsksBeforeRunningPluginCode(t *testing.T) {
	src := guestSource(t, "pane")
	a, p := appWithCodePane(t, src)

	a.startPaneCodeIfAny(p)
	if p.consent == nil || p.code != nil {
		t.Fatal("the pane should ask first")
	}
	view := ansiStrip(a.renderPluginPaneFrame(80, 24))
	for _, want := range []string{"Guest wants to run code", "draw in its pane", "Publisher key", "A allow"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the question doesn't say %q:\n%s", want, view)
		}
	}
	a.forwardPluginPaneInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if p.code == nil || p.consent != nil || !a.decisions().Decisions["guest"].Allowed {
		t.Fatal("A should run the code and remember the answer")
	}
	paneShows(t, a, "hello 80x24") // the size the question was drawn at
	if !strings.Contains(ansiStrip(a.renderPluginPaneFrame(80, 24)), "hello 80x24") {
		t.Fatal("the code's frame isn't what the pane shows")
	}

	// Keys reach the code.
	a.forwardPluginPaneInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	p.code.push(map[string]any{"type": "key", "key": "get"})
	paneShows(t, a, "got")

	// The same plugin and key next time: no question.
	p.stopPaneCode()
	p.codeChecked = false
	a.startPaneCodeIfAny(p)
	if p.consent != nil || p.code == nil {
		t.Fatal("an agreed plugin asked again")
	}

	// A different publisher key asks again, and says so.
	p.stopPaneCode()
	p.codeChecked = false
	other, _, _ := codesign.GenerateKey()
	p.conn.PluginClients[0].PublisherKey = other
	a.startPaneCodeIfAny(p)
	if p.consent == nil || !p.consent.keyChanged {
		t.Fatal("a changed publisher key should ask again")
	}
	if !strings.Contains(ansiStrip(a.renderPluginPaneFrame(80, 24)), "publisher key has changed") {
		t.Fatal("the question doesn't mention the changed key")
	}
	a.forwardPluginPaneInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if p.consent != nil || p.code != nil || !a.codeNotNow["guest"] {
		t.Fatal("N should close the question for this session without running")
	}

	// Turned off in Settings: nothing runs or asks.
	a.codeNotNow = nil
	a.uiConfig.Display.PluginCode = "never"
	p.codeChecked = false
	a.startPaneCodeIfAny(p)
	if p.consent != nil || p.code != nil {
		t.Fatal("plugin code turned off, but it asked or ran")
	}
}

func ansiStrip(s string) string { return ansi.Strip(s) }
