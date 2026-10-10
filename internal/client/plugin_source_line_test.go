package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/protocol"
	zone "github.com/lrstanley/bubblezone"
)

func renderPluginsPlain(t *testing.T, s *ServerManagementState) string {
	t.Helper()
	a := newLayoutTestApp(t, 140, 40)
	return ansi.Strip(zone.Scan(a.renderPluginsCategory(120, 36, s)))
}

// The Settings > Plugins "Source:" reference line: shown only for the
// selected row, and only when the manifest declared a source_url.
func TestPluginsCategoryShowsSourceURLOnlyForSelectedRow(t *testing.T) {
	const url = "https://example.com/hello-smoke.zip"
	plugins := []protocol.PluginInfo{
		{ID: "hello-smoke", Name: "Hello Smoke", Version: "0.0.1", Enabled: true, Status: "running", SourceURL: url},
		{ID: "other", Name: "Other", Version: "1.0.0", Enabled: true, Status: "running"},
	}

	selected := renderPluginsPlain(t, &ServerManagementState{PluginList: plugins, FocusOnForm: true, SelectedPlugin: 0})
	if !strings.Contains(selected, "Source: "+url) {
		t.Errorf("expected the selected plugin's Source line, got:\n%s", selected)
	}

	other := renderPluginsPlain(t, &ServerManagementState{PluginList: plugins, FocusOnForm: true, SelectedPlugin: 1})
	if strings.Contains(other, "Source:") {
		t.Errorf("Source line should not show when a plugin without a source_url is selected, got:\n%s", other)
	}

	unfocused := renderPluginsPlain(t, &ServerManagementState{PluginList: plugins, FocusOnForm: false, SelectedPlugin: 0})
	if strings.Contains(unfocused, "Source:") {
		t.Errorf("Source line should not show while the plugin list isn't focused, got:\n%s", unfocused)
	}
}
