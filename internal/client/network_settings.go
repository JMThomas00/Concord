package client

// Settings › Network (Privacy Pack 1): Tor-only mode, bridges, and how the
// client's tor is doing (tor_net.go).

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/concord-chat/concord/internal/tor"
)

const settingsCatNetwork = 6 // before Help & Guide, which is always last

// networkState is the Network page's own state.
type networkState struct {
	field   int // 0 mode, 1 bridges, 2 bridge lines (custom)
	editing bool
	area    textarea.Model
	notice  string
	isErr   bool
	ticking bool
}

type networkTickMsg struct{}

var bridgeChoices = []struct{ kind, label, about string }{
	{tor.BridgesNone, "None", "connect to Tor directly"},
	{tor.BridgesSnowflake, "Snowflake", "looks like a video call; works in most places that block Tor"},
	{tor.BridgesObfs4, "obfs4", "looks like random bytes; the built-in ones are busy and can be slow, so your own obfs4 lines work better"},
	{tor.BridgesCustom, "My own bridges", "lines from bridges.torproject.org, @GetBridgesBot on Telegram, or bridges@torproject.org"},
}

func (a *App) netState() *networkState {
	if a.network == nil {
		a.network = &networkState{}
	}
	return a.network
}

// networkConfig reads the saved network settings.
func (a *App) networkConfig() NetworkConfig {
	if a.configMgr == nil {
		return NetworkConfig{}
	}
	cfg, err := a.configMgr.LoadAppConfig()
	if err != nil || cfg == nil {
		return NetworkConfig{}
	}
	return cfg.Network
}

// saveNetworkConfig saves and applies the network settings.
func (a *App) saveNetworkConfig(n NetworkConfig) {
	if a.configMgr != nil {
		cfg, err := a.configMgr.LoadAppConfig()
		if err != nil || cfg == nil {
			cfg = &AppConfig{Version: 1}
		}
		cfg.Network = n
		_ = a.configMgr.SaveAppConfig(cfg)
	}
	configureNetwork(n, "")
}

// startNetworkTick redraws the page while it's open, for tor's progress.
func (a *App) startNetworkTick() tea.Cmd {
	ns := a.netState()
	if ns.ticking {
		return nil
	}
	ns.ticking = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return networkTickMsg{} })
}

func (a *App) handleNetworkTick() tea.Cmd {
	ns := a.netState()
	ns.ticking = false
	if a.view == ViewSettings && a.settingsState != nil && a.settingsState.SelectedCategory == settingsCatNetwork {
		return a.startNetworkTick()
	}
	return nil
}

// handleNetworkKey takes the page's keys while it has focus.
func (a *App) handleNetworkKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	ns := a.netState()
	n := a.networkConfig()
	if ns.editing {
		switch msg.String() {
		case "esc":
			ns.editing = false
			ns.notice = ""
			return true, nil
		case "ctrl+s":
			lines := tor.ParseBridgeLines(ns.area.Value())
			if len(lines) == 0 {
				ns.notice, ns.isErr = "Paste at least one bridge line", true
				return true, nil
			}
			n.BridgeLines, n.Bridges = lines, tor.BridgesCustom
			a.saveNetworkConfig(n)
			ns.editing = false
			ns.notice, ns.isErr = fmt.Sprintf("Saved %d bridge lines", len(lines)), false
			return true, nil
		}
		var cmd tea.Cmd
		ns.area, cmd = ns.area.Update(msg)
		return true, cmd
	}
	rows := 2
	if n.Bridges == tor.BridgesCustom {
		rows = 3
	}
	switch msg.String() {
	case "esc":
		a.settingsState.FocusOnForm = false
		ns.notice = ""
		return true, nil
	case "up", "k":
		ns.field = max(0, ns.field-1)
	case "down", "j":
		ns.field = min(rows-1, ns.field+1)
	case "left", "right", "h", "l", "enter", " ":
		back := msg.String() == "left" || msg.String() == "h"
		switch ns.field {
		case 0:
			if n.Mode == NetworkTorOnly {
				n.Mode = NetworkNormal
				ns.notice = "Tor only where needed: .onion servers and servers set to Tor (O in Manage Servers)"
			} else {
				n.Mode = NetworkTorOnly
				ns.notice = "Everything through Tor: voice is off and files go through the server. Servers already connected switch when they reconnect"
			}
			ns.isErr = false
			a.saveNetworkConfig(n)
		case 1:
			i := 0
			for j, c := range bridgeChoices {
				if c.kind == n.Bridges {
					i = j
				}
			}
			if back {
				i = (i + len(bridgeChoices) - 1) % len(bridgeChoices)
			} else {
				i = (i + 1) % len(bridgeChoices)
			}
			n.Bridges = bridgeChoices[i].kind
			a.saveNetworkConfig(n)
			ns.notice, ns.isErr = "", false
			if n.Bridges == tor.BridgesCustom && len(n.BridgeLines) == 0 {
				a.openBridgeEditor(n)
			}
		case 2:
			a.openBridgeEditor(n)
		}
	case "t", "T": // try: start tor now and watch it connect
		ns.notice, ns.isErr = "Connecting to Tor…", false
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), torWait)
			defer cancel()
			_, _ = clientNet.ensureTor(ctx)
		}()
		return true, a.startNetworkTick()
	default:
		return false, nil
	}
	return true, nil
}

func (a *App) openBridgeEditor(n NetworkConfig) {
	ns := a.netState()
	ns.area = textarea.New()
	ns.area.Placeholder = "obfs4 192.0.2.1:443 FINGERPRINT cert=… iat-mode=0"
	ns.area.SetWidth(70)
	ns.area.SetHeight(6)
	ns.area.CharLimit = 0
	ns.area.SetValue(strings.Join(n.BridgeLines, "\n"))
	ns.area.Focus()
	ns.editing = true
	ns.notice, ns.isErr = "Paste bridge lines, one per line · Ctrl+S save · Esc cancel", false
}

func (a *App) renderNetworkContent(width, height int) string {
	layout := calculateSettingsLayout(width, height, 0, 0)
	c := a.theme.Colors
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	good := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Orange))
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	w := layout.interiorWidth
	ns := a.netState()
	n := a.networkConfig()
	focused := a.settingsState != nil && a.settingsState.FocusOnForm

	top := newSectionBuilder(layout.topLines, w)
	top.writeLine(label.Render("Network"))
	top.writeLine(dim.Render("Tor: reaching servers without showing them your address"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(w))

	middle := newSectionBuilder(layout.middleLines, w)
	field := func(i int, name, value, about string) {
		cursor := "  "
		st := normal
		if focused && ns.field == i {
			cursor, st = "▸ ", sel
		}
		middle.writeLine(cursor + label.Render(fmt.Sprintf("%-10s", name)) + " " + st.Render("‹ "+value+" ›"))
		if about != "" {
			middle.writeLine("             " + dim.Render(ansi.Truncate(about, w-13, "…")))
		}
	}
	mode, modeAbout := "Where needed", ".onion servers, and servers you set to Tor (O in Manage Servers)"
	if n.Mode == NetworkTorOnly {
		mode, modeAbout = "Everything", "every server and everything else; voice off, files through the server"
	}
	field(0, "Tor", mode, modeAbout)
	bl, blAbout := "None", ""
	for _, b := range bridgeChoices {
		if b.kind == n.Bridges {
			bl, blAbout = b.label, b.about
		}
	}
	field(1, "Bridges", bl, blAbout)
	if n.Bridges == tor.BridgesCustom {
		field(2, "Lines", fmt.Sprintf("%d bridge lines", len(n.BridgeLines)), "Enter to paste or change them")
	}
	middle.writeBlank()

	// How tor is doing.
	path, findErr := tor.Find(n.TorPath)
	running, st, err := torStatus()
	switch {
	case findErr != nil:
		middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Status")) + " " + warn.Render("tor isn't installed"))
		middle.writeLine("             " + dim.Render(ansi.Truncate(torInstallHint(), w-13, "…")))
	case err != nil:
		middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Status")) + " " + warn.Render(ansi.Truncate(err.Error(), w-13, "…")))
	case !running:
		middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Status")) + " " + dim.Render("not running (it starts when a connection needs it; T starts it now)"))
	case st.Bootstrapped >= 100:
		middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Status")) + " " + good.Render("connected to Tor"))
	default:
		line := fmt.Sprintf("connecting… %d%%", st.Bootstrapped)
		if st.Phase != "" {
			line += " · " + st.Phase
		}
		middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Status")) + " " + normal.Render(ansi.Truncate(line, w-13, "…")))
		if st.Err != "" {
			middle.writeLine("             " + warn.Render(ansi.Truncate(st.Err, w-13, "…")))
		}
	}
	if findErr == nil {
		middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Program")) + " " + dim.Render(ansi.Truncate(path, w-13, "…")))
	}
	if n.Bridges == tor.BridgesSnowflake || n.Bridges == tor.BridgesObfs4 || n.Bridges == tor.BridgesCustom {
		kind := n.Bridges
		if kind == tor.BridgesCustom && len(n.BridgeLines) > 0 {
			kind = strings.Fields(n.BridgeLines[0])[0]
		}
		if kind == tor.BridgesSnowflake || kind == tor.BridgesObfs4 {
			if p, err := tor.FindTransport(kind, path); err != nil {
				middle.writeLine("             " + warn.Render(ansi.Truncate(err.Error(), w-13, "…")))
			} else {
				middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Transport")) + " " + dim.Render(ansi.Truncate(p, w-13, "…")))
			}
		}
	}
	overTor := 0
	for _, info := range a.clientServers {
		if info.overTor() {
			overTor++
		}
	}
	middle.writeLine("  " + label.Render(fmt.Sprintf("%-10s", "Servers")) + " " + normal.Render(fmt.Sprintf("%d of %d over Tor", overTor, len(a.clientServers))))

	if ns.editing {
		middle.writeBlank()
		ns.area.SetWidth(max(20, w-4))
		for _, l := range strings.Split(ns.area.View(), "\n") {
			middle.writeLine("  " + l)
		}
	}
	if ns.notice != "" {
		middle.writeBlank()
		style := good
		if ns.isErr {
			style = warn
		}
		for _, l := range wrapWords(ns.notice, w-2) {
			middle.writeLine("  " + style.Render(l))
		}
	}
	middle.writeBlank()
	for _, l := range wrapWords("Over Tor, voice is off and files go through the server: both would otherwise connect you directly to other people and show them your address.", w-2) {
		middle.writeLine(dim.Render("  " + l))
	}
	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, w)
	bottom.writeLine(a.renderSeparator(w))
	hint := "Enter or → to change · Esc back"
	if focused {
		hint = "↑↓ choose · ←→ change · T connect now · Esc back"
	}
	bottom.writeLine(dim.Render(hint))
	bottom.pad()
	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height-2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(c.Selection)).
		Padding(0, 1).Render(content)
}

// wrapWords splits text into lines of at most width cells.
func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && ansi.StringWidth(line)+1+ansi.StringWidth(word) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// torInstallHint says how to get tor on this computer.
func torInstallHint() string {
	switch {
	case runtime.GOOS == "windows":
		return "Get the Tor Expert Bundle from torproject.org/download/tor, or run the installer's --configure"
	case runtime.GOOS == "darwin":
		return "brew install tor (or run concord-install --configure)"
	}
	return "install the tor package (apt/dnf/pacman install tor), or run concord-install --configure"
}
