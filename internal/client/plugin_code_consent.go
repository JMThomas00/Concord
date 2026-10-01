package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JMThomas00/Concord/sdk/codesign"
	"github.com/concord-chat/concord/internal/protocol"
)

// Consent for plugin client code (plugin_code.go). The first time a pane
// would run a plugin's code, the pane asks instead: who published it (their
// key's fingerprint) and what it can do. The answer is remembered per
// plugin and publisher key in ~/.concord/plugin-code.json, so code signed
// by a different key, or asking for more, asks again. Settings > Display >
// Plugin Code can turn it all off, and forget the answers.

// codeCapabilityText says what each capability lets code do, for the prompt.
var codeCapabilityText = map[string]string{
	"pane":    "draw in its pane and react to your keys",
	"images":  "show pictures in its pane",
	"sound":   "play sounds",
	"storage": "keep a little data on this computer (up to 1 MB)",
	"server":  "send messages to its server half",
}

// codeDecision is one remembered answer.
type codeDecision struct {
	PublisherKey string    `json:"publisher_key"`
	Allowed      bool      `json:"allowed"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Name         string    `json:"name,omitempty"`
	At           time.Time `json:"at"`
}

// codeDecisions is every remembered answer, by plugin ID.
type codeDecisions struct {
	path      string
	Decisions map[string]codeDecision `json:"decisions"`
}

func loadCodeDecisions(dir string) *codeDecisions {
	d := &codeDecisions{Decisions: map[string]codeDecision{}}
	if dir == "" {
		return d
	}
	d.path = filepath.Join(dir, "plugin-code.json")
	if data, err := os.ReadFile(d.path); err == nil {
		_ = json.Unmarshal(data, d)
		if d.Decisions == nil {
			d.Decisions = map[string]codeDecision{}
		}
	}
	return d
}

func (d *codeDecisions) save() {
	if d.path == "" {
		return
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil || os.MkdirAll(filepath.Dir(d.path), 0o700) != nil {
		return
	}
	tmp := d.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, d.path)
	}
}

// codeConsent is a question on screen in a pane.
type codeConsent struct {
	info        protocol.PluginClientInfo
	fingerprint string
	keyChanged  bool     // a different publisher key than the one agreed to
	newCaps     []string // capabilities not agreed to before
}

// codeVerdict is what to do about a plugin's code.
type codeVerdict int

const (
	codeNone codeVerdict = iota // no code, turned off, or declined
	codeRun                     // agreed to: run it
	codeAsk                     // ask first
)

// configDir is ~/.concord, or "" without a config manager (tests).
func (a *App) configDir() string {
	if a.configMgr != nil && a.configMgr.configFilePath != "" {
		return filepath.Dir(a.configMgr.configFilePath)
	}
	return ""
}

func (a *App) decisions() *codeDecisions {
	if a.codeDecisions == nil {
		a.codeDecisions = loadCodeDecisions(a.configDir())
	}
	return a.codeDecisions
}

// pluginCodeOff reports whether plugin code is turned off in Settings.
func (a *App) pluginCodeOff() bool {
	return a.uiConfig != nil && a.uiConfig.Display.PluginCode == "never"
}

// paneCodeInfo is the client part of the pane's plugin, if it has code.
func paneCodeInfo(p *PluginPaneState) (protocol.PluginClientInfo, bool) {
	if p == nil || p.conn == nil {
		return protocol.PluginClientInfo{}, false
	}
	p.conn.mu.RLock()
	defer p.conn.mu.RUnlock()
	for _, c := range p.conn.PluginClients {
		if c.PluginID == p.PluginID && c.WASM != "" && c.PublisherKey != "" {
			return c, true
		}
	}
	return protocol.PluginClientInfo{}, false
}

// codeVerdictFor decides whether info's code runs, is asked about, or not.
func (a *App) codeVerdictFor(info protocol.PluginClientInfo) (codeVerdict, *codeConsent) {
	if a.pluginCodeOff() || a.codeNotNow[info.PluginID] {
		return codeNone, nil
	}
	pub, err := codesign.ParsePublicKey(info.PublisherKey)
	if err != nil {
		return codeNone, nil
	}
	consent := &codeConsent{info: info, fingerprint: codesign.Fingerprint(pub)}
	d, ok := a.decisions().Decisions[info.PluginID]
	switch {
	case !ok:
		return codeAsk, consent
	case d.PublisherKey != info.PublisherKey:
		consent.keyChanged = true
		return codeAsk, consent
	case !d.Allowed:
		return codeNone, nil
	}
	agreed := map[string]bool{}
	for _, c := range d.Capabilities {
		agreed[c] = true
	}
	for _, c := range info.Capabilities {
		if !agreed[c] {
			consent.newCaps = append(consent.newCaps, c)
		}
	}
	if len(consent.newCaps) > 0 {
		return codeAsk, consent
	}
	return codeRun, nil
}

// startPaneCodeIfAny runs, or asks about, the pane's plugin code. It's
// called once the pane is entered, and does nothing after the first time.
func (a *App) startPaneCodeIfAny(p *PluginPaneState) tea.Cmd {
	if p.codeChecked {
		return nil
	}
	p.codeChecked = true
	info, ok := paneCodeInfo(p)
	if !ok {
		return nil
	}
	switch verdict, consent := a.codeVerdictFor(info); verdict {
	case codeAsk:
		p.consent = consent
	case codeRun:
		return a.startPaneCode(p, info)
	}
	return nil
}

// answerConsent records the viewer's answer: "allow", "notnow" or "never".
func (a *App) answerConsent(p *PluginPaneState, answer string) tea.Cmd {
	c := p.consent
	if c == nil {
		return nil
	}
	p.consent = nil
	switch answer {
	case "allow", "never":
		d := a.decisions()
		d.Decisions[c.info.PluginID] = codeDecision{
			PublisherKey: c.info.PublisherKey, Allowed: answer == "allow",
			Capabilities: c.info.Capabilities, Name: c.info.Name, At: time.Now().UTC(),
		}
		d.save()
		if answer == "allow" {
			return a.startPaneCode(p, c.info)
		}
	default: // not now: don't ask again this session
		if a.codeNotNow == nil {
			a.codeNotNow = map[string]bool{}
		}
		a.codeNotNow[c.info.PluginID] = true
	}
	return nil
}

// handleConsentKey answers the prompt: A allows, N is "not now", D never.
func (a *App) handleConsentKey(p *PluginPaneState, msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "a", "y":
		return a.answerConsent(p, "allow")
	case "n", "esc":
		return a.answerConsent(p, "notnow")
	case "d":
		return a.answerConsent(p, "never")
	}
	return nil
}

// renderConsent draws the question in the pane.
func (a *App) renderConsent(c *codeConsent, width, height int) string {
	t := a.theme.Colors
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Purple)).Bold(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Orange)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Comment))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Cyan)).Bold(true)

	name := c.info.Name
	if name == "" {
		name = c.info.PluginID
	}
	lines := []string{title.Render(name + " wants to run code on this computer"), ""}
	if c.keyChanged {
		lines = append(lines,
			warn.Render("Its publisher key has changed since you last agreed."),
			text.Render("Only allow this if you know the plugin changed hands."), "")
	} else if len(c.newCaps) > 0 {
		lines = append(lines, warn.Render("A new version asks for more than before."), "")
	}
	lines = append(lines,
		text.Render("It runs sandboxed: no files, no network, no other programs."),
		text.Render("It can:"))
	caps := append([]string(nil), c.info.Capabilities...)
	sort.Strings(caps)
	for _, cap := range caps {
		desc := codeCapabilityText[cap]
		if desc == "" {
			desc = cap
		}
		lines = append(lines, text.Render("  • "+desc))
	}
	lines = append(lines, "",
		dim.Render("Publisher key  "+c.fingerprint),
		dim.Render(fmt.Sprintf("Code           %s, version %s", path.Base(c.info.WASM), c.info.Version)),
		"",
		key.Render("A")+text.Render(" allow   ")+key.Render("N")+text.Render(" not now   ")+key.Render("D")+text.Render(" don't allow"),
	)
	if !a.paneFocused() {
		lines = append(lines, dim.Render("Press Tab to answer."))
	}
	return lipgloss.NewStyle().Width(width).Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// forgetCodeDecisions drops every remembered answer.
func (a *App) forgetCodeDecisions() {
	d := a.decisions()
	d.Decisions = map[string]codeDecision{}
	d.save()
	a.codeNotNow = nil
}
