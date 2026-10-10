package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"

	"github.com/JMThomas00/Concord/sdk/codesign"
	"github.com/concord-chat/concord/internal/protocol"
)

// Plugin client code (To Do item D2): WebAssembly a plugin ships in its
// client/ folder, run inside this client next to the plugin's pane. It's
// sandboxed by wazero: no files, no network, no other programs, a memory
// cap, and a time budget per event. It can only use the capabilities its
// manifest lists and the member agreed to (plugin_code_consent.go), and
// only code signed by the plugin's publisher key runs.
//
// The interface is module "concord", in JSON, built so any language that
// compiles to wasip1 can use it (sdk/client is the Go side; sdk/PROTOCOL.md
// documents it):
//
//	next_event(buf, cap) -> n   blocks for the next event; n > cap: ask again
//	                            with n bytes of room; n < 0: stop.
//	call(req, len) -> n         a host call {"fn": ...}; n < 0 an error code,
//	                            else the length of its result.
//	result(buf, cap) -> n       copies the last call's result.
//
// One instance runs per open pane, on its own goroutine; everything it
// wants from the UI arrives as a codeMsg on codeHost.out.

const (
	codeMemoryPages  = 4096 // 64 KiB each: 256 MB
	codeEventBudget  = 2 * time.Second
	codeStartBudget  = 5 * time.Second
	codeStopGrace    = time.Second
	codeMaxTimers    = 16
	codeMinTimer     = 16 * time.Millisecond
	codeMaxTimer     = time.Hour
	codeMaxFrame     = 256 << 10
	codeStorageLimit = 1 << 20
	codeMaxKey       = 256
	codeLogLimit     = 64 << 10
	codeEventQueue   = 256
)

// Call results: errors are negative.
const (
	codeErrBadRequest = -1
	codeErrNotAllowed = -2
	codeErrUnknown    = -3
	codeErrLimit      = -4
	codeErrNotFound   = -5
)

// codeCapability is the capability each host function needs ("" = none).
var codeCapability = map[string]string{
	"log": "", "frame": "pane", "clear_frame": "pane", "forward_keys": "pane", "timer": "pane",
	"claim_keys": "pane", "play_sound": "sound", "storage_get": "storage", "storage_set": "storage", "send_server": "server",
}

// codeHost owns the WebAssembly runtime and every running instance.
type codeHost struct {
	dir string // ~/.concord, for the compile cache and storage ("" = neither)
	out chan tea.Msg

	rts [2]codeRuntime // [0] the compiler, [1] the interpreter
	// interpOnly means everything uses the interpreter: the default, and
	// always once the compiler has crashed (see newCodeHost).
	interpOnly atomic.Bool

	mu       sync.Mutex
	compiled map[string]wazero.CompiledModule // by the module's SHA-256
	runners  map[string]*codeRunner           // by module instance name
	next     int
}

func newCodeHost(dir string) *codeHost {
	h := &codeHost{dir: dir, out: make(chan tea.Msg, codeEventQueue), compiled: map[string]wazero.CompiledModule{}, runners: map[string]*codeRunner{}}
	// The interpreter is the default: fast enough for pane code, and wazero's
	// compiler crashed at random on Jordan's i9-14900K (2026-10-01), a sign
	// it might also miscompile there, which sandboxed code can't afford.
	// CONCORD_WASM_COMPILER=1 opts in to the compiler, which still falls back
	// to the interpreter if it crashes.
	h.interpOnly.Store(os.Getenv("CONCORD_WASM_COMPILER") == "")
	return h
}

// codeMsg is anything client code asks of the UI.
type codeMsg interface{ codeRunnerOf() *codeRunner }

type (
	codeFrameMsg   struct{ r *codeRunner }
	codeForwardMsg struct {
		r       *codeRunner
		forward bool
	}
	codeSoundMsg struct {
		r      *codeRunner
		asset  string
		volume float64
	}
	codeClaimMsg struct {
		r    *codeRunner
		keys []string
	}
	codeSendMsg struct {
		r    *codeRunner
		data json.RawMessage
	}
	codeExitMsg struct {
		r   *codeRunner
		err error
	}
)

func (m codeFrameMsg) codeRunnerOf() *codeRunner   { return m.r }
func (m codeForwardMsg) codeRunnerOf() *codeRunner { return m.r }
func (m codeSoundMsg) codeRunnerOf() *codeRunner   { return m.r }
func (m codeSendMsg) codeRunnerOf() *codeRunner    { return m.r }
func (m codeClaimMsg) codeRunnerOf() *codeRunner   { return m.r }
func (m codeExitMsg) codeRunnerOf() *codeRunner    { return m.r }

// listen waits for the next codeMsg. Exactly one is outstanding once the
// host exists: the App re-arms it after each message.
func (h *codeHost) listen() tea.Cmd {
	return func() tea.Msg { return <-h.out }
}

// codeRuntime is one wazero runtime, started on first use.
type codeRuntime struct {
	once sync.Once
	rt   wazero.Runtime
	err  error
}

// runtime starts a wazero runtime (compiler or interpreter), WASI (without
// any filesystem) and the concord module, once each.
func (h *codeHost) runtime(interp bool) (wazero.Runtime, error) {
	slot := &h.rts[0]
	if interp {
		slot = &h.rts[1]
	}
	slot.once.Do(func() {
		ctx := context.Background()
		cfg := wazero.NewRuntimeConfig()
		if interp {
			cfg = wazero.NewRuntimeConfigInterpreter()
		}
		cfg = cfg.WithCloseOnContextDone(true).WithMemoryLimitPages(codeMemoryPages)
		if h.dir != "" && !interp {
			if cache, err := wazero.NewCompilationCacheWithDir(filepath.Join(h.dir, "plugin-cache", "wasm")); err == nil {
				cfg = cfg.WithCompilationCache(cache)
			}
		}
		rt := wazero.NewRuntimeWithConfig(ctx, cfg)
		if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
			slot.err = err
			return
		}
		_, err := rt.NewHostModuleBuilder("concord").
			NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, size uint32) int32 {
			if r := h.runner(m.Name()); r != nil {
				return r.nextEvent(ctx, m, ptr, size)
			}
			return -1
		}).Export("next_event").
			NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, size uint32) int32 {
			if r := h.runner(m.Name()); r != nil {
				return r.call(m, ptr, size)
			}
			return codeErrBadRequest
		}).Export("call").
			NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, size uint32) int32 {
			if r := h.runner(m.Name()); r != nil {
				return r.copyResult(m, ptr, size)
			}
			return 0
		}).Export("result").
			Instantiate(ctx)
		if err != nil {
			slot.err = err
			return
		}
		slot.rt = rt
	})
	return slot.rt, slot.err
}

func (h *codeHost) runner(name string) *codeRunner {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runners[name]
}

// compile compiles module, reusing an earlier compile of the same bytes. It
// uses wazero's compiler, falling back to its interpreter for good if the
// compiler crashes (it has, at random, on Jordan's i9-14900K).
func (h *codeHost) compile(sha string, module []byte) (wazero.Runtime, wazero.CompiledModule, error) {
	if !h.interpOnly.Load() {
		rt, c, err := h.compileWith(false, sha, module)
		if !errors.Is(err, errCompilerCrashed) {
			return rt, c, err
		}
		log.Printf("plugin code: %v; using the interpreter from now on", err)
		h.interpOnly.Store(true)
	}
	return h.compileWith(true, sha, module)
}

var errCompilerCrashed = errors.New("the WebAssembly compiler crashed")

// compileModule is wazero's CompileModule (a test replaces it).
var compileModule = func(rt wazero.Runtime, module []byte) (wazero.CompiledModule, error) {
	return rt.CompileModule(context.Background(), module)
}

func (h *codeHost) compileWith(interp bool, sha string, module []byte) (rt wazero.Runtime, c wazero.CompiledModule, err error) {
	key := sha
	if interp {
		key += "/interp"
	}
	h.mu.Lock()
	c = h.compiled[key]
	h.mu.Unlock()
	rt, err = h.runtime(interp)
	if err != nil || c != nil {
		return rt, c, err
	}
	defer func() {
		if p := recover(); p != nil {
			rt, c, err = nil, nil, fmt.Errorf("%w: %v", errCompilerCrashed, p)
		}
	}()
	c, err = compileModule(rt, module)
	if err != nil {
		return nil, nil, err
	}
	h.mu.Lock()
	h.compiled[key] = c
	h.mu.Unlock()
	return rt, c, nil
}

// codeSource is everything needed to load and run a plugin's code.
type codeSource struct {
	PluginID     string
	Name         string
	WASM         protocol.PluginClientFile
	Sig          protocol.PluginClientFile
	PublisherKey string
	Capabilities []string
	Files        map[string]bool // the client/ paths, for checking assets
	// load reads a verified client file (the asset store, or a test's map).
	load func(f protocol.PluginClientFile) ([]byte, error)
}

// codeRunner is one running instance of a plugin's client code.
type codeRunner struct {
	host      *codeHost
	name      string
	src       codeSource
	caps      map[string]bool
	channelID uuid.UUID
	storage   *codeStorage

	events   chan []byte
	stopCh   chan struct{}
	stopOnce sync.Once
	ctx      context.Context
	cancel   context.CancelFunc
	failure  atomic.Value // string: why Concord stopped it

	// Used only on the runner's goroutine (inside host calls).
	pending []byte
	result  []byte
	logged  int

	watchMu sync.Mutex
	watch   *time.Timer

	timersMu sync.Mutex
	timers   map[string]*time.Timer

	frameMu     sync.Mutex
	frame       *codeFrame
	frameQueued bool
}

// codeFrame is what the code last drew; nil text means "show the server's".
type codeFrame struct {
	text   *string
	images []protocol.PaneImage
}

// start begins running src's code for a pane on channelID, with start as
// its first event. It returns at once; the code runs on its own goroutine
// and reports back through h.out.
func (h *codeHost) start(src codeSource, channelID uuid.UUID, start map[string]any) *codeRunner {
	h.mu.Lock()
	h.next++
	name := fmt.Sprintf("plugin-%d", h.next)
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	r := &codeRunner{
		host: h, name: name, src: src, caps: map[string]bool{}, channelID: channelID,
		events: make(chan []byte, codeEventQueue), stopCh: make(chan struct{}),
		ctx: ctx, cancel: cancel, timers: map[string]*time.Timer{},
	}
	for _, c := range src.Capabilities {
		r.caps[c] = true
	}
	if h.dir != "" && r.caps["storage"] {
		r.storage = &codeStorage{path: filepath.Join(h.dir, "plugin-data", storageName(src.PluginID, src.PublisherKey))}
	} else {
		r.storage = &codeStorage{}
	}
	start["type"] = "start"
	start["plugin_id"] = src.PluginID
	start["channel_id"] = channelID.String()
	start["capabilities"] = src.Capabilities
	r.push(start)
	go r.run()
	return r
}

// storageName keeps each publisher's data apart, even under one plugin ID.
func storageName(pluginID, publisherKey string) string {
	sum := sha256.Sum256([]byte(publisherKey))
	safe := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, pluginID)
	return safe + "-" + hex.EncodeToString(sum[:6]) + ".json"
}

func (r *codeRunner) run() {
	err := r.runModule()
	if reason, _ := r.failure.Load().(string); reason != "" {
		err = errors.New(reason)
	}
	r.stopTimers()
	r.host.mu.Lock()
	delete(r.host.runners, r.name)
	r.host.mu.Unlock()
	r.cancel()
	select {
	case r.host.out <- codeExitMsg{r: r, err: err}:
	case <-time.After(5 * time.Second): // the UI is gone
	}
}

func (r *codeRunner) runModule() error {
	select {
	case <-r.stopCh:
		return nil // stopped before it started
	default:
	}
	module, err := r.src.load(r.src.WASM)
	if err != nil {
		return fmt.Errorf("couldn't get its code: %w", err)
	}
	sig, err := r.src.load(r.src.Sig)
	if err != nil {
		return fmt.Errorf("couldn't get its signature: %w", err)
	}
	if err := codesign.Verify(r.src.PublisherKey, module, sig); err != nil {
		return err
	}
	rt, compiled, err := r.host.compile(r.src.WASM.SHA256, module)
	if err != nil {
		return fmt.Errorf("its code doesn't load: %w", err)
	}
	r.host.mu.Lock()
	r.host.runners[r.name] = r
	r.host.mu.Unlock()

	logw := &codeLog{r: r}
	cfg := wazero.NewModuleConfig().WithName(r.name).
		WithStdout(logw).WithStderr(logw).WithRandSource(rand.Reader).
		WithSysWalltime().WithSysNanotime().WithSysNanosleep().
		WithArgs(r.src.PluginID).WithStartFunctions("_start")
	r.watchStart(codeStartBudget)
	mod, err := rt.InstantiateModule(r.ctx, compiled, cfg)
	r.watchStop()
	if mod != nil {
		_ = mod.Close(context.Background())
	}
	var exit *sys.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 0 {
		return nil
	}
	select {
	case <-r.stopCh:
		return nil // stopped on purpose; however it ended is fine
	default:
	}
	return err
}

// stop asks the code to finish (next_event returns -1), and cuts it off
// if it hasn't within codeStopGrace.
func (r *codeRunner) stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
		time.AfterFunc(codeStopGrace, r.cancel)
	})
}

// fail stops the code at once, recording why.
func (r *codeRunner) fail(reason string) {
	r.failure.CompareAndSwap(nil, reason)
	r.stopOnce.Do(func() { close(r.stopCh) })
	r.cancel()
}

// push queues an event for the code. Code that lets hundreds pile up isn't
// keeping up, and is stopped.
func (r *codeRunner) push(ev map[string]any) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return
	}
	select {
	case <-r.stopCh:
	case r.events <- raw:
	default:
		r.fail("it stopped keeping up with events")
	}
}

func (r *codeRunner) watchStart(d time.Duration) {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()
	if r.watch != nil {
		r.watch.Stop()
	}
	r.watch = time.AfterFunc(d, func() { r.fail(fmt.Sprintf("it took longer than %v to answer", d)) })
}

func (r *codeRunner) watchStop() {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()
	if r.watch != nil {
		r.watch.Stop()
		r.watch = nil
	}
}

// nextEvent is the next_event import.
func (r *codeRunner) nextEvent(ctx context.Context, m api.Module, ptr, size uint32) int32 {
	r.watchStop()
	if r.pending == nil {
		select {
		case ev := <-r.events:
			r.pending = ev
		case <-r.stopCh:
			return -1
		case <-ctx.Done():
			return -1
		}
	}
	n := len(r.pending)
	r.watchStart(codeEventBudget)
	if uint32(n) > size {
		return int32(n)
	}
	if !m.Memory().Write(ptr, r.pending) {
		r.fail("it gave a bad buffer")
		return -1
	}
	r.pending = nil
	return int32(n)
}

// call is the call import.
func (r *codeRunner) call(m api.Module, ptr, size uint32) int32 {
	raw, ok := m.Memory().Read(ptr, size)
	if !ok {
		return codeErrBadRequest
	}
	result, code := r.handle(raw)
	if code < 0 {
		return code
	}
	r.result = result
	return int32(len(result))
}

// copyResult is the result import.
func (r *codeRunner) copyResult(m api.Module, ptr, size uint32) int32 {
	n := len(r.result)
	if uint32(n) > size {
		return int32(n)
	}
	if n > 0 && !m.Memory().Write(ptr, r.result) {
		return codeErrBadRequest
	}
	r.result = nil
	return int32(n)
}

// codeRequest is every host call's JSON.
type codeRequest struct {
	Fn      string               `json:"fn"`
	Text    string               `json:"text"`
	Images  []protocol.PaneImage `json:"images"`
	Forward bool                 `json:"forward"`
	ID      string               `json:"id"`
	MS      int64                `json:"ms"`
	Asset   string               `json:"asset"`
	Volume  float64              `json:"volume"`
	Key     string               `json:"key"`
	Value   *string              `json:"value"`
	Data    json.RawMessage      `json:"data"`
	Keys    []string             `json:"keys"`
}

// handle runs one host call.
func (r *codeRunner) handle(raw []byte) ([]byte, int32) {
	var req codeRequest
	if json.Unmarshal(raw, &req) != nil {
		return nil, codeErrBadRequest
	}
	need, known := codeCapability[req.Fn]
	if !known {
		return nil, codeErrUnknown
	}
	if need != "" && !r.caps[need] {
		return nil, codeErrNotAllowed
	}
	switch req.Fn {
	case "log":
		r.log(req.Text)
	case "frame":
		if len(req.Text) > codeMaxFrame {
			return nil, codeErrLimit
		}
		if len(req.Images) > 0 && !r.caps["images"] {
			return nil, codeErrNotAllowed
		}
		for _, im := range req.Images {
			if !r.src.Files[im.Asset] {
				return nil, codeErrNotFound
			}
		}
		text := req.Text
		r.setFrame(&codeFrame{text: &text, images: req.Images})
	case "clear_frame":
		r.setFrame(&codeFrame{})
	case "forward_keys":
		r.emit(codeForwardMsg{r: r, forward: req.Forward})
	case "claim_keys":
		for _, k := range req.Keys {
			if !slices.Contains(protocol.PaneNavigationKeys, k) {
				return nil, codeErrBadRequest
			}
		}
		r.emit(codeClaimMsg{r: r, keys: append([]string(nil), req.Keys...)})
	case "timer":
		return nil, r.setTimer(req.ID, time.Duration(req.MS)*time.Millisecond)
	case "play_sound":
		if !r.src.Files[req.Asset] || !isSoundAsset(req.Asset) {
			return nil, codeErrNotFound
		}
		r.emit(codeSoundMsg{r: r, asset: req.Asset, volume: req.Volume})
	case "storage_get":
		v, ok, err := r.storage.get(req.Key)
		if err != nil {
			return nil, codeErrBadRequest
		}
		if !ok {
			return []byte(`{"value":null}`), 0
		}
		out, _ := json.Marshal(map[string]string{"value": v})
		return out, 0
	case "storage_set":
		return nil, r.storage.set(req.Key, req.Value)
	case "send_server":
		if len(req.Data) == 0 {
			return nil, codeErrBadRequest
		}
		if len(req.Data) > protocol.MaxClientMessageBytes {
			return nil, codeErrLimit
		}
		r.emit(codeSendMsg{r: r, data: append(json.RawMessage(nil), req.Data...)})
	}
	return nil, 0
}

// emit hands a message to the UI, unless the code is stopping.
func (r *codeRunner) emit(m tea.Msg) {
	select {
	case r.host.out <- m:
	case <-r.stopCh:
	case <-r.ctx.Done():
	}
}

// setFrame records what the code drew. Frames are coalesced: the UI takes
// the latest when it gets to it, so drawing faster than the screen
// refreshes costs nothing.
func (r *codeRunner) setFrame(f *codeFrame) {
	r.frameMu.Lock()
	r.frame = f
	queue := !r.frameQueued
	r.frameQueued = true
	r.frameMu.Unlock()
	if queue {
		r.emit(codeFrameMsg{r: r})
	}
}

// takeFrame returns the latest frame (on the UI goroutine).
func (r *codeRunner) takeFrame() *codeFrame {
	r.frameMu.Lock()
	defer r.frameMu.Unlock()
	r.frameQueued = false
	f := r.frame
	r.frame = nil
	return f
}

func (r *codeRunner) setTimer(id string, d time.Duration) int32 {
	if id == "" || len(id) > codeMaxKey {
		return codeErrBadRequest
	}
	d = min(max(d, codeMinTimer), codeMaxTimer)
	r.timersMu.Lock()
	defer r.timersMu.Unlock()
	if old := r.timers[id]; old != nil {
		old.Stop()
	} else if len(r.timers) >= codeMaxTimers {
		return codeErrLimit
	}
	r.timers[id] = time.AfterFunc(d, func() {
		r.timersMu.Lock()
		delete(r.timers, id)
		r.timersMu.Unlock()
		r.push(map[string]any{"type": "timer", "id": id})
	})
	return 0
}

func (r *codeRunner) stopTimers() {
	r.timersMu.Lock()
	defer r.timersMu.Unlock()
	for id, t := range r.timers {
		t.Stop()
		delete(r.timers, id)
	}
}

// log writes the code's logging to the client log, up to codeLogLimit.
func (r *codeRunner) log(text string) {
	if r.logged >= codeLogLimit {
		return
	}
	r.logged += len(text)
	if len(text) > 1000 {
		text = text[:1000] + "…"
	}
	log.Printf("plugin %s code: %s", r.src.PluginID, strings.TrimRight(text, "\n"))
}

// codeLog takes the code's stdout and stderr.
type codeLog struct{ r *codeRunner }

func (w *codeLog) Write(p []byte) (int, error) {
	w.r.log(string(p))
	return len(p), nil
}

// codeStorage is a plugin's small key/value store on this computer, one
// JSON file per plugin and publisher.
type codeStorage struct {
	path   string // "" = in memory only
	loaded bool
	values map[string]string
	size   int
}

func (s *codeStorage) load() {
	if s.loaded {
		return
	}
	s.loaded, s.values = true, map[string]string{}
	if s.path == "" {
		return
	}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &s.values)
	}
	for k, v := range s.values {
		s.size += len(k) + len(v)
	}
}

func (s *codeStorage) get(key string) (string, bool, error) {
	if key == "" || len(key) > codeMaxKey {
		return "", false, errors.New("bad key")
	}
	s.load()
	v, ok := s.values[key]
	return v, ok, nil
}

func (s *codeStorage) set(key string, value *string) int32 {
	if key == "" || len(key) > codeMaxKey {
		return codeErrBadRequest
	}
	s.load()
	old, had := s.values[key]
	size := s.size
	if had {
		size -= len(key) + len(old)
	}
	if value == nil {
		delete(s.values, key)
	} else {
		if size+len(key)+len(*value) > codeStorageLimit {
			return codeErrLimit
		}
		s.values[key] = *value
		size += len(key) + len(*value)
	}
	s.size = size
	if s.path != "" {
		data, _ := json.Marshal(s.values)
		if os.MkdirAll(filepath.Dir(s.path), 0o700) == nil {
			tmp := s.path + ".tmp"
			if os.WriteFile(tmp, data, 0o600) == nil {
				_ = os.Rename(tmp, s.path)
			}
		}
	}
	return 0
}

// isSoundAsset reports whether a client file is a sound.
func isSoundAsset(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".wav", ".ogg", ".opus":
		return true
	}
	return false
}
