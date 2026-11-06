package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/concord-chat/concord/internal/themes"
	"github.com/concord-chat/concord/internal/tor"
)

func TestNetworkPage(t *testing.T) {
	cm := &ConfigManager{configFilePath: t.TempDir() + "/config.json"}
	theme := themes.GetDefaultTheme()
	a := &App{theme: theme, styles: theme.BuildStyles(), configMgr: cm, view: ViewSettings,
		settingsState: &SettingsState{SelectedCategory: settingsCatNetwork, FocusOnForm: true}}
	defer configureNetwork(NetworkConfig{}, "")

	page := ansi.Strip(a.renderNetworkContent(90, 30))
	for _, want := range []string{"Network", "Where needed", "Bridges", "None", "voice is off"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	about := ansi.Strip(a.renderAboutContent(90, 30))
	if lipgloss.Width(page) != lipgloss.Width(about) || lipgloss.Height(page) != lipgloss.Height(about) {
		t.Errorf("the page is %dx%d, About %dx%d", lipgloss.Width(page), lipgloss.Height(page), lipgloss.Width(about), lipgloss.Height(about))
	}
	for _, l := range strings.Split(page, "\n") {
		if ansi.StringWidth(l) > lipgloss.Width(about) {
			t.Errorf("line wider than the page: %q", l)
		}
	}

	key := func(k string) {
		var m tea.KeyMsg
		switch k {
		case "right":
			m = tea.KeyMsg{Type: tea.KeyRight}
		case "down":
			m = tea.KeyMsg{Type: tea.KeyDown}
		case "esc":
			m = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+s":
			m = tea.KeyMsg{Type: tea.KeyCtrlS}
		default:
			m = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		a.handleNetworkKey(m)
	}
	key("right") // Tor: Everything
	if a.networkConfig().Mode != NetworkTorOnly || !torOnly() {
		t.Fatal("Everything wasn't saved and applied")
	}
	key("down")
	key("right") // Snowflake
	if a.networkConfig().Bridges != tor.BridgesSnowflake {
		t.Fatalf("bridges %q", a.networkConfig().Bridges)
	}
	key("right")
	key("right") // custom: opens the editor
	if !a.netState().editing {
		t.Fatal("custom bridges didn't open the editor")
	}
	a.netState().area.SetValue("obfs4 192.0.2.1:443 AAAA cert=x iat-mode=0\n")
	key("ctrl+s")
	if n := a.networkConfig(); n.Bridges != tor.BridgesCustom || len(n.BridgeLines) != 1 {
		t.Fatalf("custom lines %+v", n)
	}
	if !strings.Contains(ansi.Strip(a.renderNetworkContent(90, 30)), "1 bridge lines") {
		t.Error("the lines row is missing")
	}
	key("esc")
	if a.settingsState.FocusOnForm {
		t.Error("Esc didn't leave the page")
	}
}
