package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// channelFormApp is an app with the Create Channel form open on a server
// that has Tak (many settings) and Mynah with two instances.
func channelFormApp(t *testing.T, height int) (*App, *Connection, []*models.Channel) {
	t.Helper()
	a := newLayoutTestApp(t, 160, height)
	conn := &Connection{connected: true, send: make(chan *protocol.Message, 64)}
	server := &models.Server{ID: uuid.New()}
	channels := []*models.Channel{
		{ID: uuid.New(), ServerID: server.ID, Name: "general", Type: models.ChannelTypeText},
		{ID: uuid.New(), ServerID: server.ID, Name: "random", Type: models.ChannelTypeText},
	}
	field := func(key, typ string, opts ...string) protocol.PluginField {
		return protocol.PluginField{Key: key, Label: strings.ToUpper(key[:1]) + key[1:], Type: typ, Options: opts, Default: firstOr(opts)}
	}
	takFields := []protocol.PluginField{
		field("seating", "select", "seats", "challenge", "private"),
		field("spectators", "boolean"), field("computer", "boolean"),
		field("strength", "select", "easy", "normal", "hard"),
		field("size", "select", "3", "4", "5", "6", "7", "8"),
		field("komi", "select", "0", "0.5", "1"),
		field("timer", "number"), field("notes", "text"),
	}
	mynahFields := []protocol.PluginField{field("rate", "number"), field("listen", "channel_multi_select")}
	sc := &ServerConnection{ServerID: server.ID, Connection: conn, Channels: map[uuid.UUID][]*models.Channel{server.ID: channels}}
	sc.PluginChannelKinds = []protocol.PluginChannelKindInfo{
		{PluginID: "concord-tak", Kind: "table", DisplayName: "Tak Table", CreateFields: takFields},
		{PluginID: "mynah", Kind: "mynah", DisplayName: "Mynah", InstanceName: "Burt", AllowsInstances: true, CreateFields: mynahFields},
		{PluginID: "mynah-alice", Kind: "mynah", DisplayName: "Mynah", InstanceName: "Alice", BaseID: "mynah", AllowsInstances: true, CreateFields: mynahFields},
	}
	a.activeConn = sc
	a.currentServer = server
	name := textinput.New()
	name.SetValue("lobby")
	a.serverManagementState = &ServerManagementState{
		SelectedCategory: 0,
		FocusOnForm:      true,
		ChannelFormOpen:  true,
		ChannelFormState: &ChannelFormState{Mode: "create", NameTextInput: name, MaxUsersInput: textinput.New()},
	}
	return a, conn, channels
}

func firstOr(opts []string) string {
	if len(opts) > 0 {
		return opts[0]
	}
	return ""
}

func formKey(a *App, k string) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	a.handleChannelFormKey(msg)
}

// selectKind moves the Type field to the plugin kind named name.
func selectKind(t *testing.T, a *App, name string) {
	t.Helper()
	state := a.serverManagementState.ChannelFormState
	state.FocusField = 1
	for i := 0; i < 10; i++ {
		formKey(a, "right")
		if state.TypeIndex >= 3 && a.pluginKindOptions()[state.TypeIndex-3].DisplayName == name {
			return
		}
	}
	t.Fatalf("type %q not offered; options %+v", name, a.pluginKindOptions())
}

func TestTypeListShowsThisServersPluginsOnceEach(t *testing.T) {
	a, _, _ := channelFormApp(t, 45)
	// Another server's plugin must not be offered here.
	a.pluginChannelKinds = map[string]protocol.PluginChannelKindInfo{
		"elsewhere:x": {PluginID: "elsewhere", Kind: "x", DisplayName: "Other Server Plugin"},
	}
	var names []string
	for _, k := range a.pluginKindOptions() {
		names = append(names, k.DisplayName)
	}
	if got := strings.Join(names, ","); got != "Tak Table,Mynah" {
		t.Fatalf("type list %q, want Tak Table and Mynah once each", got)
	}
	view := ansi.Strip(a.renderChannelFormPage(160, 45, a.serverManagementState))
	if strings.Contains(view, "Other Server") || strings.Count(view, "Mynah") != 1 || strings.Contains(view, "Plugin channel\n") {
		t.Fatalf("form view:\n%s", view)
	}
}

func TestConfigureHoldsAPluginsSettingsAndInstanceChoice(t *testing.T) {
	a, conn, channels := channelFormApp(t, 45)
	state := a.serverManagementState.ChannelFormState
	selectKind(t, a, "Mynah")

	// The plugin's fields aren't on the form itself: there's a Configure row.
	view := ansi.Strip(a.renderChannelFormPage(160, 45, a.serverManagementState))
	if !strings.Contains(view, "[Configure…]") || strings.Contains(view, "▸ Listen:") {
		t.Fatalf("form view:\n%s", view)
	}
	formKey(a, "tab") // → Configure
	formKey(a, "enter")
	if !state.Configuring {
		t.Fatal("Enter on Configure didn't open the Configure page")
	}
	view = ansi.Strip(a.renderChannelFormPage(160, 45, a.serverManagementState))
	if !strings.Contains(view, "Which Mynah") || !strings.Contains(view, "Burt") || !strings.Contains(view, "Listen:") {
		t.Fatalf("configure view:\n%s", view)
	}

	formKey(a, "right") // instance: Burt → Alice
	if state.PluginID != "mynah-alice" {
		t.Fatalf("instance choice set PluginID %q", state.PluginID)
	}
	formKey(a, "down") // → rate
	formKey(a, "5")
	formKey(a, "down") // → listen (channel_multi_select)
	formKey(a, "enter")
	if state.Picker == nil {
		t.Fatal("Enter on a channel list didn't open the picker")
	}
	formKey(a, " ")    // check #general
	formKey(a, "down") // → #random
	formKey(a, "down") // → Done
	formKey(a, "enter")
	if state.Picker != nil || state.PluginValues[1] != channels[0].ID.String() {
		t.Fatalf("picker left value %q", state.PluginValues[1])
	}
	formKey(a, "esc") // back to the form
	if state.Configuring {
		t.Fatal("Esc didn't leave the Configure page")
	}
	view = ansi.Strip(a.renderChannelFormPage(160, 45, a.serverManagementState))
	if !strings.Contains(view, "Alice") || !strings.Contains(view, "#general") {
		t.Fatalf("the Configure row should summarize the choices:\n%s", view)
	}

	a.handleChannelFormSubmit()
	var req protocol.ChannelCreateRequest
	lastSent(t, conn, protocol.OpChannelCreate, &req)
	if req.PluginID != "mynah-alice" || req.PluginChannelKind != "mynah" || req.PluginConfig["rate"] != "5" || req.PluginConfig["listen"] != channels[0].ID.String() {
		t.Fatalf("create request %+v", req)
	}
}

// However many plugin types and settings there are, the form never draws
// taller than its space (it used to push the category list off screen).
func TestChannelFormNeverOutgrowsItsSpace(t *testing.T) {
	for _, h := range []int{24, 30, 45} {
		a, _, _ := channelFormApp(t, h)
		state := a.serverManagementState.ChannelFormState
		selectKind(t, a, "Tak Table")
		for _, configuring := range []bool{false, true} {
			state.Configuring = configuring
			for focus := 0; focus < 12; focus++ {
				state.FocusField, state.ConfigFocus = focus%5, focus
				out := a.renderChannelFormPage(160, h, a.serverManagementState)
				if got := strings.Count(out, "\n") + 1; got > h { // the page is h-2 rows plus its border
					t.Fatalf("height %d, configuring=%v, focus %d: drew %d lines", h, configuring, focus, got)
				}
			}
		}
		// The focused field stays on screen when scrolled.
		state.Configuring, state.ConfigFocus = true, 7
		if out := ansi.Strip(a.renderChannelFormPage(160, h, a.serverManagementState)); !strings.Contains(out, "Notes:") {
			t.Fatalf("height %d: focused field scrolled out of view:\n%s", h, out)
		}
	}
}
