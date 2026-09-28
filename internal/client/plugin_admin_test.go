package client

import (
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// pluginAdminApp is an app on Server Settings > Plugins with one plugin
// listed, whose outgoing messages land in the returned connection's queue.
func pluginAdminApp(t *testing.T) (*App, *Connection) {
	t.Helper()
	a := newLayoutTestApp(t, 160, 45)
	conn := &Connection{connected: true, send: make(chan *protocol.Message, 64)}
	a.activeConn = &ServerConnection{ServerID: uuid.New(), Connection: conn}
	a.currentServer = &models.Server{ID: uuid.New()}
	a.serverManagementState = &ServerManagementState{
		SelectedCategory: 4,
		FocusOnForm:      true,
		PluginList: []protocol.PluginInfo{{
			ID: "Mynah", Name: "Mynah", Enabled: true,
			ConfigFields: []protocol.PluginField{
				{Key: "api_key", Label: "API Key", Type: "secret"},
				{Key: "model", Label: "Model", Type: "text"},
			},
			ConfigValues: map[string]string{"model": "gpt"},
			SecretsSet:   []string{"api_key"},
		}},
	}
	return a, conn
}

func lastSent(t *testing.T, conn *Connection, op protocol.OpCode, into interface{}) {
	t.Helper()
	var found *protocol.Message
	for _, m := range sent(conn) {
		if m.Op == op {
			found = m
		}
	}
	if found == nil {
		t.Fatalf("no op %d was sent", op)
	}
	if err := json.Unmarshal(found.Data, into); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallNeedsASecondPress(t *testing.T) {
	a, conn := pluginAdminApp(t)
	a.handlePluginListKey("x")
	if msgs := sent(conn); len(msgs) != 0 {
		t.Fatalf("the first X sent %d messages; it should only ask for confirmation", len(msgs))
	}
	a.handlePluginListKey("x")
	var req protocol.PluginManageRequest
	lastSent(t, conn, protocol.OpPluginManage, &req)
	if req.Action != protocol.PluginActionUninstall || req.PluginID != "Mynah" || req.ServerID != a.currentServer.ID {
		t.Fatalf("uninstall request = %+v", req)
	}

	// Anything else in between cancels the pending confirmation.
	a.handlePluginListKey("x")
	a.handlePluginListKey("s")
	sent(conn)
	a.handlePluginListKey("x")
	for _, m := range sent(conn) {
		if m.Op == protocol.OpPluginManage {
			t.Fatal("an X after another action uninstalled without confirming")
		}
	}
}

func TestSettingsFormWaitsForTheServerAndShowsFieldErrors(t *testing.T) {
	a, conn := pluginAdminApp(t)
	a.handleOpenPluginConfigAction()
	form := a.serverManagementState.PluginConfigState
	if form.TextInputs[0].Value() != "" || form.TextInputs[0].Placeholder == "" {
		t.Fatal("a secret field must start empty, with a placeholder saying it's set")
	}

	a.handleSavePluginConfig()
	var req protocol.PluginConfigSetRequest
	lastSent(t, conn, protocol.OpPluginConfigSet, &req)
	if _, ok := req.Config["api_key"]; ok {
		t.Fatal("an untouched secret was sent (it would overwrite the stored key with nothing)")
	}
	if a.serverManagementState.PluginConfigState == nil || !form.Saving {
		t.Fatal("the form should stay open while the server decides")
	}

	a.applyPluginManageResult(protocol.PluginManageResult{Action: "configure", PluginID: "Mynah", OK: false,
		Message: "Some settings need fixing.", FieldErrors: map[string]string{"model": "required"}})
	if a.serverManagementState.PluginConfigState == nil || form.FieldErrors["model"] != "required" || form.Saving {
		t.Fatalf("rejected save: form %+v", form)
	}

	form.TextInputs[0].SetValue("sk-new")
	a.handleSavePluginConfig()
	lastSent(t, conn, protocol.OpPluginConfigSet, &req)
	if req.Config["api_key"] != "sk-new" {
		t.Fatalf("a typed secret wasn't sent: %v", req.Config)
	}
	a.applyPluginManageResult(protocol.PluginManageResult{Action: "configure", PluginID: "Mynah", OK: true, Message: "Saved."})
	if a.serverManagementState.PluginConfigState != nil {
		t.Fatal("the form should close once the server accepts the save")
	}
}

func TestInstallFormSendsManageRequest(t *testing.T) {
	a, conn := pluginAdminApp(t)
	a.handlePluginListKey("i")
	form := a.serverManagementState.PluginConfigState
	if form == nil || form.Mode != protocol.PluginActionInstall {
		t.Fatal("I should open the install form")
	}
	if len(form.Fields) != 1 {
		t.Fatalf("install should ask for one thing (where to get it), got %d fields", len(form.Fields))
	}
	form.TextInputs[0].SetValue(" jordan/concord-chess ")
	form.FocusField = len(form.Fields) // the Install button
	a.handlePluginConfigKey(tea.KeyMsg{Type: tea.KeyEnter})

	var req protocol.PluginManageRequest
	lastSent(t, conn, protocol.OpPluginManage, &req)
	if req.Action != "install" || req.PluginID != "" || req.SourceURL != "jordan/concord-chess" || req.SHA256 != "" {
		t.Fatalf("install request = %+v", req)
	}
	a.applyPluginManageResult(protocol.PluginManageResult{Action: "install", PluginID: "concord-chess", OK: true, Message: "Installed concord-chess."})
	if a.serverManagementState.PluginConfigState != nil || a.serverManagementState.PluginNotice != "Installed concord-chess." {
		t.Fatal("a successful install should close the form and say so")
	}
}

// A live registry update from one server replaces that server's plugin
// channel kinds without disturbing another server's.
func TestRegistryUpdateIsPerServer(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)
	one, _ := a.connMgr.AddServer(&ClientServerInfo{ID: uuid.New()})
	two, _ := a.connMgr.AddServer(&ClientServerInfo{ID: uuid.New()})
	a.setPluginChannelKinds(one, []protocol.PluginChannelKindInfo{{PluginID: "Tukan", Kind: "board"}})
	a.setPluginChannelKinds(two, []protocol.PluginChannelKindInfo{{PluginID: "Chess", Kind: "table"}})

	a.setPluginChannelKinds(two, nil) // chess uninstalled on server two
	if _, ok := a.pluginChannelKinds["Tukan:board"]; !ok {
		t.Error("server one's kind vanished when server two changed")
	}
	if _, ok := a.pluginChannelKinds["Chess:table"]; ok {
		t.Error("an uninstalled plugin's kind is still offered")
	}
}

// Update is prefilled with where the plugin came from, so it's U, Enter.
func TestUpdateFormIsPrefilledWithThePluginsSource(t *testing.T) {
	a, conn := pluginAdminApp(t)
	a.serverManagementState.PluginList[0].SourceURL = "https://github.com/jordan/mynah"
	a.handlePluginListKey("u")
	form := a.serverManagementState.PluginConfigState
	if form == nil || form.Mode != "update" || form.TextInputs[0].Value() != "https://github.com/jordan/mynah" {
		t.Fatalf("update form = %+v", form)
	}
	form.FocusField = len(form.Fields)
	a.handlePluginConfigKey(tea.KeyMsg{Type: tea.KeyEnter})
	var req protocol.PluginManageRequest
	lastSent(t, conn, protocol.OpPluginManage, &req)
	if req.Action != "update" || req.PluginID != "Mynah" || req.SourceURL != "https://github.com/jordan/mynah" {
		t.Fatalf("update request = %+v", req)
	}
}
