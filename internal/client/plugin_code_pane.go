package client

import (
	"encoding/json"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/concord-chat/concord/internal/protocol"
)

// How a pane runs its plugin's client code (plugin_code.go): started once
// the pane is entered (and agreed to), fed the viewer's keys, size, theme
// and the server half's frames and messages, and stopped when the pane
// closes. While the code has drawn a frame, the pane shows that instead of
// the server's.

// codeHostFor is the App's runtime, created on first use. The returned
// command starts listening for the code's messages; it's non-nil only the
// first time.
func (a *App) codeHostFor() (*codeHost, tea.Cmd) {
	if a.codeHost != nil {
		return a.codeHost, nil
	}
	a.codeHost = newCodeHost(a.configDir())
	return a.codeHost, a.codeHost.listen()
}

// startPaneCode runs info's code in pane p.
func (a *App) startPaneCode(p *PluginPaneState, info protocol.PluginClientInfo) tea.Cmd {
	src, ok := a.codeSourceFor(p, info)
	if !ok {
		log.Printf("plugin %s: its code or signature isn't in what the server advertised", info.PluginID)
		return nil
	}
	h, listen := a.codeHostFor()
	w, ht := a.paneSize()
	theme := a.paneTheme()
	viewer := ""
	if p.conn != nil {
		p.conn.mu.RLock()
		if p.conn.User != nil {
			viewer = p.conn.User.ID.String()
		}
		p.conn.mu.RUnlock()
	}
	p.code = h.start(src, p.ChannelID, map[string]any{"width": w, "height": ht, "theme": theme, "viewer_id": viewer})
	p.codeW, p.codeH, p.codeTheme = w, ht, theme.Name
	return listen
}

// codeSourceFor gathers what the runner needs from the server's
// advertisement; the files come through the asset store (fetched, checked
// against their hashes, cached).
func (a *App) codeSourceFor(p *PluginPaneState, info protocol.PluginClientInfo) (codeSource, bool) {
	src := codeSource{
		PluginID: info.PluginID, Name: info.Name, PublisherKey: info.PublisherKey,
		Capabilities: info.Capabilities, Files: map[string]bool{},
	}
	var haveWASM, haveSig bool
	for _, f := range info.Files {
		src.Files[f.Path] = true
		switch f.Path {
		case info.WASM:
			src.WASM, haveWASM = f, true
		case info.WASM + ".sig":
			src.Sig, haveSig = f, true
		}
	}
	store, sc := a.assets(), p.conn
	src.load = func(f protocol.PluginClientFile) ([]byte, error) { return store.bytes(sc, info.PluginID, f) }
	return src, haveWASM && haveSig
}

// stopPaneCode ends the pane's code, if it's running.
func (p *PluginPaneState) stopPaneCode() {
	if p != nil && p.code != nil {
		p.code.stop()
		p.code = nil
		p.local = nil
		p.keysLocal = false
		p.localKeys = nil
	}
}

// paneCode is the pane's running code if it has capability cap.
func (a *App) paneCode(cap string) *codeRunner {
	if p := a.pluginPane; p != nil && p.code != nil && p.code.caps[cap] {
		return p.code
	}
	return nil
}

// syncPaneCode tells the code about a new size or theme.
func (a *App) syncPaneCode(w, h int, theme *protocol.PaneTheme) {
	p := a.pluginPane
	if p == nil || p.code == nil || (w == p.codeW && h == p.codeH && theme.Name == p.codeTheme) {
		return
	}
	p.codeW, p.codeH, p.codeTheme = w, h, theme.Name
	p.code.push(map[string]any{"type": "resize", "width": w, "height": h, "theme": theme})
}

// handleCodeMsg acts on what a pane's code asked for. Messages from code
// that's no longer the pane's (it was stopped, or the pane changed) are
// dropped, except that its exit is reported.
func (a *App) handleCodeMsg(m codeMsg) tea.Cmd {
	r := m.codeRunnerOf()
	p := a.pluginPane
	current := p != nil && p.code == r
	switch m := m.(type) {
	case codeExitMsg:
		if current {
			p.code, p.local, p.keysLocal, p.localKeys = nil, nil, false, nil
		}
		if m.err != nil {
			name := r.src.Name
			if name == "" {
				name = r.src.PluginID
			}
			a.statusMessage = name + "'s code stopped: " + m.err.Error()
			log.Printf("plugin %s code stopped: %v", r.src.PluginID, m.err)
		}
	case codeFrameMsg:
		f := r.takeFrame()
		if !current || f == nil {
			return nil
		}
		if f.text == nil {
			p.local = nil
			return nil
		}
		p.local = &codeFrame{text: ptr(sanitizePaneFrame(*f.text)), images: f.images}
		return a.fetchPaneAssets(p.conn, p.PluginID, f.images)
	case codeForwardMsg:
		if current {
			p.keysLocal = !m.forward
		}
	case codeClaimMsg:
		if current {
			p.localKeys = m.keys
		}
	case codeSoundMsg:
		if current {
			a.playPluginSound(p.conn, p.PluginID, protocol.PluginPlaySoundPayload{ChannelID: p.ChannelID, Asset: m.asset, Volume: m.volume})
		}
	case codeSendMsg:
		if current && p.entered {
			payload, err := json.Marshal(protocol.PluginClientMessagePayload{ChannelID: p.ChannelID, Data: m.data})
			if err == nil {
				a.sendPluginPane(p, protocol.OpPluginEvent, protocol.PluginEventPayload{
					PluginID: p.PluginID, Kind: protocol.PluginEventClientMessage, Payload: payload,
				})
			}
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// bytes returns a client file's contents: from the disk cache, or fetched
// from the server, and checked against its advertised hash either way.
func (s *pluginAssets) bytes(sc *ServerConnection, pluginID string, f protocol.PluginClientFile) ([]byte, error) {
	if data, err := s.cached(f.SHA256); err == nil {
		return data, nil
	}
	if sc == nil {
		return nil, fmt.Errorf("%s: no server to fetch it from", f.Path)
	}
	sc.mu.RLock()
	if sc.ServerInfo == nil {
		sc.mu.RUnlock()
		return nil, fmt.Errorf("%s: no server to fetch it from", f.Path)
	}
	addr, token := sc.ServerInfo.GetHTTPURL(), sc.Token
	sc.mu.RUnlock()
	data, err := download(addr, token, pluginID, f)
	if err != nil {
		return nil, err
	}
	s.store(f.SHA256, data)
	return data, nil
}

// codeKeyEvent is a key as client code sees it.
func codeKeyEvent(msg tea.KeyMsg) map[string]any {
	ev := map[string]any{"type": "key", "key": msg.String()}
	if len(msg.Runes) > 0 && msg.Type == tea.KeyRunes {
		ev["runes"] = string(msg.Runes)
	}
	return ev
}
