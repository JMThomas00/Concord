package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/concord-chat/concord/internal/plugins"
	"github.com/concord-chat/concord/internal/protocol"
)

// Plugin client parts (To Do item D): the images, sounds and code in a
// plugin's client/ folder, advertised in READY and the registry update,
// and served to signed-in members.

// PluginClientInfos describes the client part of every loaded plugin that
// has one. Instances share their base plugin's folder, so they list the
// same files.
func (h *Handlers) PluginClientInfos() []protocol.PluginClientInfo {
	var out []protocol.PluginClientInfo
	for _, m := range h.plugins.Registry().All() {
		b, err := plugins.IndexClientBundle(m.Dir)
		if err != nil {
			PluginLog.Warn("plugin client part not served", "plugin_id", m.Plugin.ID, "error", err)
			continue
		}
		if b == nil {
			continue
		}
		info := protocol.PluginClientInfo{
			PluginID: m.Plugin.ID, Version: m.Plugin.Version, Hash: b.Hash,
			WASM: strings.TrimPrefix(m.Client.WASM, plugins.ClientDir+"/"), Capabilities: m.Client.Capabilities,
			PublisherKey: m.Client.PublisherKey,
		}
		for _, f := range b.Files {
			info.Files = append(info.Files, protocol.PluginClientFile{Path: f.Path, Size: f.Size, SHA256: f.SHA256})
		}
		out = append(out, info)
	}
	return out
}

// handlePluginClientFile serves GET /api/plugins/client/{plugin_id}/{path}
// to signed-in members: only files indexed in that plugin's client/ folder.
// Clients verify each file's SHA-256 against the descriptor, so the bytes
// can be cached for good.
func (s *Server) handlePluginClientFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAPIError(w, http.StatusMethodNotAllowed, "method", "Method not allowed")
		return
	}
	if _, _, ok := s.bearerUser(w, r); !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/plugins/client/")
	pluginID, rel, ok := strings.Cut(rest, "/")
	if !ok || pluginID == "" || rel == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "No such file")
		return
	}
	m, found := s.plugins.Registry().Manifest(pluginID)
	if !found {
		writeAPIError(w, http.StatusNotFound, "not_found", "No such plugin")
		return
	}
	b, err := plugins.IndexClientBundle(m.Dir)
	if err != nil || b == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "This plugin has no client part")
		return
	}
	p := plugins.ClientFilePath(m.Dir, b, rel)
	file, _ := b.File(rel)
	if p == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "No such file")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "No such file")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}
	w.Header().Set("Content-Type", plugins.ClientContentType(rel))
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}
