package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// pluginPageApp is the Server Settings → Plugins list on a server with Mynah
// (instances Burt and Alice), Tak, and an old separate "Mynah" persona
// install, Carl.
func pluginPageApp(t *testing.T) (*App, *Connection) {
	t.Helper()
	a, conn, _ := channelFormApp(t, 45)
	server := a.currentServer
	sc := a.activeConn
	sc.Channels[server.ID] = append(sc.Channels[server.ID],
		&models.Channel{ID: uuid.New(), ServerID: server.ID, Name: "ask-burt", Type: models.ChannelTypePlugin, PluginID: "mynah"},
		&models.Channel{ID: uuid.New(), ServerID: server.ID, Name: "ask-alice", Type: models.ChannelTypePlugin, PluginID: "mynah-alice"},
		&models.Channel{ID: uuid.New(), ServerID: server.ID, Name: "tak", Type: models.ChannelTypePlugin, PluginID: "concord-tak"},
	)
	all := []protocol.PluginInfo{
		{ID: "mynah", Name: "Burt", Product: "Mynah", Version: "0.3.0", AllowsInstances: true, Enabled: true, Status: "running"},
		{ID: "mynah-alice", Name: "Alice", Product: "Mynah", Version: "0.3.0", BaseID: "mynah", AllowsInstances: true, Enabled: true, Status: "running"},
		{ID: "concord-tak", Name: "Tak", Version: "0.1.0", Enabled: true, Status: "running"},
		{ID: "carl", Name: "Carl", Product: "Mynah", Version: "0.2.0", Enabled: true, Status: "running"},
	}
	s := a.serverManagementState
	s.ChannelFormOpen, s.ChannelFormState = false, nil
	s.SelectedCategory = 4
	s.AllPlugins, s.PluginList = all, installedPlugins(all)
	return a, conn
}

func pageKey(a *App, k string) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	a.handlePluginPageKey(msg)
}

func TestPluginListHidesInstancesAndOffersAdoption(t *testing.T) {
	a, conn := pluginPageApp(t)
	s := a.serverManagementState
	var ids []string
	for _, p := range s.PluginList {
		ids = append(ids, p.ID)
	}
	if got := strings.Join(ids, ","); got != "mynah,concord-tak,carl" {
		t.Fatalf("plugin list %q: instances belong on their plugin's page", got)
	}
	if n := s.instanceCount(s.PluginList[0]); n != 2 {
		t.Fatalf("Mynah has %d instances, want 2", n)
	}
	if s.legacyInstanceTarget(s.PluginList[1]) != nil {
		t.Fatal("Tak isn't a persona of anything")
	}

	s.SelectedPlugin = 2 // Carl
	if !a.handlePluginListKey("m") {
		t.Fatal("M not handled")
	}
	var req protocol.PluginManageRequest
	lastSent(t, conn, protocol.OpPluginManage, &req)
	if req.Action != protocol.PluginActionAdoptInstance || req.PluginID != "carl" || req.TargetID != "mynah" {
		t.Fatalf("adopt request %+v", req)
	}
}

func TestPluginPageManagesInstancesAndListsTheirChannels(t *testing.T) {
	a, conn := pluginPageApp(t)
	s := a.serverManagementState
	s.SelectedPlugin = 0
	a.openPluginPage()

	view := ansi.Strip(a.renderPluginPage(160, 45, s))
	for _, want := range []string{"Mynah v0.3.0", "Instances", "Burt", "Alice", "+ Add instance", "#ask-burt", "#ask-alice", "(Alice)", "+ New channel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("plugin page lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "#tak") {
		t.Fatalf("another plugin's channel on Mynah's page:\n%s", view)
	}

	// Enter on an instance opens its settings.
	pageKey(a, "down") // Alice
	pageKey(a, "enter")
	if s.PluginConfigState == nil || s.PluginConfigState.PluginID != "mynah-alice" {
		t.Fatalf("Enter on Alice opened %+v", s.PluginConfigState)
	}
	s.PluginConfigState = nil

	// Rename Alice.
	pageKey(a, "n")
	s.PluginPage.NameInput.SetValue("Alicia")
	pageKey(a, "enter")
	var req protocol.PluginManageRequest
	lastSent(t, conn, protocol.OpPluginManage, &req)
	if req.Action != protocol.PluginActionRenameInstance || req.PluginID != "mynah-alice" || req.Name != "Alicia" {
		t.Fatalf("rename request %+v", req)
	}

	// Removing takes two presses, and the plugin itself can't be removed.
	pageKey(a, "x")
	if s.PluginPage.Confirm != "mynah-alice" {
		t.Fatal("first X should ask to confirm")
	}
	pageKey(a, "x")
	lastSent(t, conn, protocol.OpPluginManage, &req)
	if req.Action != protocol.PluginActionRemoveInstance || req.PluginID != "mynah-alice" {
		t.Fatalf("remove request %+v", req)
	}
	pageKey(a, "up") // Burt
	pageKey(a, "x")
	pageKey(a, "x")
	if s.PluginPage.Confirm != "" || !s.PluginNoticeError {
		t.Fatal("X on the base plugin should only explain itself")
	}

	// Add an instance.
	pageKey(a, "down")
	pageKey(a, "down") // + Add instance
	pageKey(a, "enter")
	if s.PluginPage.Naming != "add" {
		t.Fatal("Enter on + Add instance should ask for a name")
	}
	s.PluginPage.NameInput.SetValue("Dora")
	pageKey(a, "enter")
	lastSent(t, conn, protocol.OpPluginManage, &req)
	if req.Action != protocol.PluginActionAddInstance || req.PluginID != "mynah" || req.Name != "Dora" {
		t.Fatalf("add request %+v", req)
	}

	pageKey(a, "esc")
	if s.PluginPage != nil {
		t.Fatal("Esc should close the page")
	}
}
