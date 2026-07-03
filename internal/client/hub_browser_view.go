package client

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

// hubBrowserMode tracks which sub-view is active inside the browser.
type hubBrowserMode int

const (
	hubModeList      hubBrowserMode = iota // main server list
	hubModeDetail                           // detail overlay for selected server
	hubModeAddHub                           // "add hub URL" text prompt
)

// HubBrowserState is all state for the Hub Browser full-screen overlay.
// Embedded by value in App; zeroed when the browser closes.
type HubBrowserState struct {
	mode hubBrowserMode

	// View to restore when the browser closes (the browser is reachable
	// from several views via Ctrl+G).
	returnView View

	// Hub tabs
	hubURLs     []string
	hubNames    []string // display names (may be empty until health check completes)
	selectedHub int
	hubClients  map[string]*GrapevineHTTPClient

	// Server list
	allServers []HubServerEntry
	filtered   []HubServerEntry
	cursor     int
	loading    bool
	err        string

	// Category tabs (derived from allServers)
	categories  []string // ["All", "Gaming", ...]
	selectedCat int

	// Search input
	searchInput    textinput.Model
	searchFocused  bool

	// Sort order for the server list (cycled with S)
	sortMode int

	// Per-hub reachability from background probes: +1 ok, -1 bad, 0 unknown
	hubHealth map[string]int

	// Detail view
	detailServer *HubServerEntry

	// Add hub flow
	addHubInput textinput.Model
	addHubErr   string
	peerCursor  int // highlighted discovered peer (-1 = none); ↑/↓ selects

	// Join in progress
	joining bool
	joinErr string

	// Peer hubs discovered from GET /v1/hubs (not yet in hubURLs)
	discoveredPeers []HubEntry

	// Layout
	width, height int
}

// maxDiscoveredPeers caps how many discovered peer hubs the Add Hub dialog offers.
const maxDiscoveredPeers = 5

// Server-list sort orders, cycled with S. Grouping (local first, then per
// federated hub) always applies; the sort orders rows within each group.
const (
	hubSortName    = iota // A→Z (default)
	hubSortOnline         // most users online first
	hubSortMembers        // most members first
	hubSortCount          // number of modes (for cycling)
)

func hubSortLabel(mode int) string {
	switch mode {
	case hubSortOnline:
		return "online"
	case hubSortMembers:
		return "members"
	default:
		return "name"
	}
}

// defaultHubURL is the built-in fallback hub, used when the user's hub list is
// empty. It is a fallback, not a pinned entry: users may remove it (x) as long
// as another hub remains, e.g. to use only a private hub.
const defaultHubURL = "http://grapevine.concord.chat"

// hubListRow is one row in the rendered server list — either a section header or a server entry.
type hubListRow struct {
	serverIdx  int    // index into filtered; -1 for section headers
	headerText string // non-empty for section headers
}

// buildHubDisplayList produces an ordered list of display rows with federation section headers
// interleaved between groups. filtered must already be sorted (local first, then by FromHub).
func buildHubDisplayList(filtered []HubServerEntry) []hubListRow {
	var rows []hubListRow
	prevHub := "\x00" // sentinel so first entry always triggers a group check

	hasFederated := false
	for _, sv := range filtered {
		if sv.FromHub != "" {
			hasFederated = true
			break
		}
	}

	for i, sv := range filtered {
		if sv.FromHub != prevHub {
			if sv.FromHub == "" && hasFederated {
				rows = append(rows, hubListRow{serverIdx: -1, headerText: "Local"})
			} else if sv.FromHub != "" {
				rows = append(rows, hubListRow{serverIdx: -1, headerText: "via " + sv.FromHub})
			}
			prevHub = sv.FromHub
		}
		rows = append(rows, hubListRow{serverIdx: i})
	}
	return rows
}

// newHubBrowserState initialises a fresh HubBrowserState.
func newHubBrowserState(hubURLs []string, w, h int) HubBrowserState {
	si := textinput.New()
	si.Placeholder = "search servers..."
	si.CharLimit = 60
	si.Width = 30

	ai := textinput.New()
	ai.Placeholder = "http://hub.example.com:7777"
	ai.CharLimit = 200
	ai.Width = 50

	urls := make([]string, len(hubURLs))
	copy(urls, hubURLs)

	names := make([]string, len(hubURLs))
	for i, u := range hubURLs {
		names[i] = hubDisplayName(u)
	}

	clients := make(map[string]*GrapevineHTTPClient, len(hubURLs))
	for _, u := range hubURLs {
		clients[u] = newGrapevineHTTPClient(u)
	}

	return HubBrowserState{
		mode:        hubModeList,
		hubURLs:     urls,
		hubNames:    names,
		selectedHub: 0,
		hubClients:  clients,
		categories:  []string{"All"},
		searchInput: si,
		addHubInput: ai,
		peerCursor:  -1,
		hubHealth:   map[string]int{},
		width:       w,
		height:      h,
	}
}

// hubDisplayName returns a short display name for a hub URL.
func hubDisplayName(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	if len(u) > 32 {
		u = u[:32] + "…"
	}
	return u
}

// currentHubURL returns the URL of the currently selected hub.
func (s *HubBrowserState) currentHubURL() string {
	if s.selectedHub < len(s.hubURLs) {
		return s.hubURLs[s.selectedHub]
	}
	return ""
}

// currentClient returns the HTTP client for the selected hub.
func (s *HubBrowserState) currentClient() *GrapevineHTTPClient {
	return s.hubClients[s.currentHubURL()]
}

// applyFilter recomputes s.filtered from s.allServers using current search/category.
func (s *HubBrowserState) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(s.searchInput.Value()))
	cat := ""
	if s.selectedCat > 0 && s.selectedCat < len(s.categories) {
		cat = s.categories[s.selectedCat]
	}

	s.filtered = s.filtered[:0]
	for _, sv := range s.allServers {
		if cat != "" && !strings.EqualFold(sv.Category, cat) {
			continue
		}
		if query != "" {
			hay := strings.ToLower(sv.Name + " " + sv.Description + " " + strings.Join(sv.Tags, " "))
			if !strings.Contains(hay, query) {
				continue
			}
		}
		s.filtered = append(s.filtered, sv)
	}

	// Sort: local servers first, then federated grouped by hub name; within
	// each group, order by the active sort mode (name / online / members).
	sort.Slice(s.filtered, func(i, j int) bool {
		a, b := s.filtered[i], s.filtered[j]
		if a.FromHub != b.FromHub {
			if a.FromHub == "" {
				return true
			}
			if b.FromHub == "" {
				return false
			}
			return a.FromHub < b.FromHub
		}
		switch s.sortMode {
		case hubSortOnline:
			if a.OnlineCount != b.OnlineCount {
				return a.OnlineCount > b.OnlineCount
			}
		case hubSortMembers:
			if a.MemberCount != b.MemberCount {
				return a.MemberCount > b.MemberCount
			}
		}
		return a.Name < b.Name
	})

	if s.cursor >= len(s.filtered) {
		s.cursor = max0(len(s.filtered) - 1)
	}
}

// rebuildCategories derives the distinct category list from allServers.
func (s *HubBrowserState) rebuildCategories() {
	seen := map[string]bool{}
	for _, sv := range s.allServers {
		if sv.Category != "" {
			seen[sv.Category] = true
		}
	}
	cats := make([]string, 0, len(seen))
	for c := range seen {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	s.categories = append([]string{"All"}, cats...)

	if s.selectedCat >= len(s.categories) {
		s.selectedCat = 0
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// ── Bubbletea commands ────────────────────────────────────────────────────────

func fetchHubServers(client *GrapevineHTTPClient, hubURL string) tea.Cmd {
	return func() tea.Msg {
		servers, err := client.ListServers("", "")
		if err != nil {
			return hubLoadErrorMsg{err: err.Error(), hubURL: hubURL}
		}
		return hubServersLoadedMsg{servers: servers, hubURL: hubURL}
	}
}

func requestJoinServer(client *GrapevineHTTPClient, serverID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.RequestJoin(serverID)
		if err != nil {
			return hubJoinErrorMsg{err: err.Error()}
		}
		return hubJoinResponseMsg{resp: resp}
	}
}

func redeemJoinToken(resp *HubJoinResponse) tea.Cmd {
	return func() tea.Msg {
		if err := RedeemJoinToken(resp.Host, resp.Port, resp.JoinToken); err != nil {
			return hubJoinErrorMsg{err: err.Error()}
		}
		return hubJoinVerifiedMsg{resp: resp}
	}
}

func fetchHubPeers(client *GrapevineHTTPClient, hubURL string) tea.Cmd {
	return func() tea.Msg {
		peers, err := client.ListHubs()
		if err != nil {
			return hubPeersLoadErrMsg{err: err.Error(), hubURL: hubURL}
		}
		return hubPeersLoadedMsg{peers: peers, hubURL: hubURL}
	}
}

func checkHubHealth(rawURL string) tea.Cmd {
	return func() tea.Msg {
		client := newGrapevineHTTPClient(rawURL)
		name, err := client.CheckHealth()
		if err != nil {
			return hubHealthCheckErrMsg{hubURL: rawURL, err: err.Error()}
		}
		return hubHealthCheckMsg{hubURL: rawURL, hubName: name}
	}
}

// ── App: open / close ────────────────────────────────────────────────────────

// openHubBrowser initialises and opens the Hub Browser overlay.
func (a *App) openHubBrowser() tea.Cmd {
	hubURLs := a.uiConfig.HubURLs
	if len(hubURLs) == 0 {
		hubURLs = []string{defaultHubURL}
	}
	a.hubBrowser = newHubBrowserState(hubURLs, a.width, a.height)
	a.hubBrowser.returnView = a.view
	a.showHubBrowser = true
	a.hubBrowser.loading = true

	client := a.hubBrowser.currentClient()
	hubURL := a.hubBrowser.currentHubURL()

	// Probe every tab so unreachable hubs are marked before they're opened.
	cmds := []tea.Cmd{fetchHubServers(client, hubURL)}
	for _, u := range a.hubBrowser.hubURLs {
		cmds = append(cmds, probeHubTab(u))
	}
	return tea.Batch(cmds...)
}

// probeHubTab checks a hub's health endpoint for the tab indicator.
func probeHubTab(hubURL string) tea.Cmd {
	return func() tea.Msg {
		_, err := newGrapevineHTTPClient(hubURL).CheckHealth()
		return hubTabHealthMsg{hubURL: hubURL, ok: err == nil}
	}
}

// isHubServerJoined reports whether a hub listing is already in the user's
// configured server list (matched by the Grapevine listing ID recorded when
// a server is joined through a hub).
func (a *App) isHubServerJoined(hubServerID string) bool {
	if hubServerID == "" {
		return false
	}
	for _, cs := range a.clientServers {
		if cs.HubServerID == hubServerID {
			return true
		}
	}
	return false
}

// closeHubBrowser resets hub browser state and returns to the originating view.
func (a *App) closeHubBrowser() {
	a.showHubBrowser = false
	a.view = a.hubBrowser.returnView
	a.hubBrowser = HubBrowserState{}
}

// ── App: key handling ─────────────────────────────────────────────────────────

func (a *App) handleHubBrowserKey(msg tea.KeyMsg) tea.Cmd {
	s := &a.hubBrowser

	switch s.mode {
	case hubModeAddHub:
		return a.handleHubBrowserAddHubKey(msg)
	case hubModeDetail:
		return a.handleHubBrowserDetailKey(msg)
	default:
		return a.handleHubBrowserListKey(msg)
	}
}

func (a *App) handleHubBrowserListKey(msg tea.KeyMsg) tea.Cmd {
	s := &a.hubBrowser

	// If search input is focused, route typing to it
	if s.searchFocused {
		switch msg.String() {
		case "esc":
			s.searchInput.Blur()
			s.searchFocused = false
			s.applyFilter()
			return nil
		case "enter":
			s.searchInput.Blur()
			s.searchFocused = false
			s.applyFilter()
			return nil
		default:
			var cmd tea.Cmd
			s.searchInput, cmd = s.searchInput.Update(msg)
			s.applyFilter()
			return cmd
		}
	}

	switch msg.String() {
	case "esc":
		a.closeHubBrowser()
		return nil

	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}

	case "down", "j":
		if s.cursor < len(s.filtered)-1 {
			s.cursor++
		}

	case "enter":
		if len(s.filtered) > 0 {
			sv := s.filtered[s.cursor]
			s.detailServer = &sv
			s.mode = hubModeDetail
			s.joinErr = ""
		}

	case "a", "A":
		// Join the highlighted server directly (opens details for the
		// join progress/error UI, then starts the join immediately).
		if len(s.filtered) > 0 && !s.joining {
			sv := s.filtered[s.cursor]
			s.detailServer = &sv
			s.mode = hubModeDetail
			s.joining = true
			s.joinErr = ""
			return requestJoinServer(s.currentClient(), sv.ID)
		}

	case "s", "S":
		s.sortMode = (s.sortMode + 1) % hubSortCount
		s.applyFilter()

	case "/":
		s.searchFocused = true
		s.searchInput.Focus()

	case "tab":
		s.selectedCat = (s.selectedCat + 1) % len(s.categories)
		s.applyFilter()

	case "shift+tab":
		s.selectedCat = (s.selectedCat - 1 + len(s.categories)) % len(s.categories)
		s.applyFilter()

	case "h", "H":
		// Previous hub tab
		if s.selectedHub > 0 {
			s.selectedHub--
			return a.refreshCurrentHub()
		}

	case "l", "L":
		// Next hub tab
		if s.selectedHub < len(s.hubURLs)-1 {
			s.selectedHub++
			return a.refreshCurrentHub()
		}

	case "r", "R":
		return a.refreshCurrentHub()

	case "+":
		s.mode = hubModeAddHub
		s.addHubInput.Reset()
		s.addHubInput.Focus()
		s.addHubErr = ""
		s.peerCursor = -1

	case "x", "X":
		return a.removeCurrentHub()
	}

	return nil
}

// removeCurrentHub drops the selected hub tab and persists the change.
// Removing the last hub falls back to the built-in default hub.
func (a *App) removeCurrentHub() tea.Cmd {
	s := &a.hubBrowser
	if s.selectedHub >= len(s.hubURLs) {
		return nil
	}
	delete(s.hubClients, s.hubURLs[s.selectedHub])
	s.hubURLs = append(s.hubURLs[:s.selectedHub], s.hubURLs[s.selectedHub+1:]...)
	s.hubNames = append(s.hubNames[:s.selectedHub], s.hubNames[s.selectedHub+1:]...)

	if len(s.hubURLs) == 0 {
		s.hubURLs = []string{defaultHubURL}
		s.hubNames = []string{hubDisplayName(defaultHubURL)}
	}
	if s.selectedHub >= len(s.hubURLs) {
		s.selectedHub = len(s.hubURLs) - 1
	}
	if s.hubClients[s.currentHubURL()] == nil {
		s.hubClients[s.currentHubURL()] = newGrapevineHTTPClient(s.currentHubURL())
	}

	a.persistHubURLs()
	return a.refreshCurrentHub()
}

// persistHubURLs writes the current hub tab list to the client config.
func (a *App) persistHubURLs() {
	s := &a.hubBrowser
	a.uiConfig.HubURLs = s.hubURLs
	if ac, err := a.configMgr.LoadAppConfig(); err == nil {
		ac.UI.HubURLs = s.hubURLs
		_ = a.configMgr.SaveAppConfig(ac)
	}
}

func (a *App) handleHubBrowserDetailKey(msg tea.KeyMsg) tea.Cmd {
	s := &a.hubBrowser

	switch msg.String() {
	case "esc":
		s.mode = hubModeList
		s.detailServer = nil
		s.joinErr = ""

	case "a", "A", "enter":
		if s.detailServer != nil && !s.joining {
			s.joining = true
			s.joinErr = ""
			client := s.currentClient()
			serverID := s.detailServer.ID
			return requestJoinServer(client, serverID)
		}
	}
	return nil
}

func (a *App) handleHubBrowserAddHubKey(msg tea.KeyMsg) tea.Cmd {
	s := &a.hubBrowser

	switch msg.String() {
	case "esc":
		s.mode = hubModeList
		s.addHubInput.Blur()

	case "enter":
		rawURL := strings.TrimSpace(s.addHubInput.Value())
		if rawURL == "" {
			s.addHubErr = "URL cannot be empty"
			return nil
		}
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			rawURL = "http://" + rawURL
		}
		return checkHubHealth(rawURL)

	// ↑/↓ select a discovered peer hub, filling the URL input; Enter adds it.
	// Deliberately NOT digit shortcuts: digits must type into the input —
	// hub URLs are full of them (ports, IP addresses).
	case "up", "down":
		n := len(s.discoveredPeers)
		if n > maxDiscoveredPeers {
			n = maxDiscoveredPeers
		}
		if n == 0 {
			return nil
		}
		if msg.String() == "down" {
			s.peerCursor = (s.peerCursor + 1) % n
		} else {
			s.peerCursor = (s.peerCursor - 1 + n) % n
		}
		rawURL := s.discoveredPeers[s.peerCursor].URL
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			rawURL = "http://" + rawURL
		}
		s.addHubInput.SetValue(rawURL)
		s.addHubInput.CursorEnd()

	default:
		s.peerCursor = -1 // editing by hand drops the peer selection
		var cmd tea.Cmd
		s.addHubInput, cmd = s.addHubInput.Update(msg)
		return cmd
	}
	return nil
}

func (a *App) refreshCurrentHub() tea.Cmd {
	s := &a.hubBrowser
	s.loading = true
	s.err = ""
	s.allServers = nil
	s.filtered = nil
	s.cursor = 0
	client := s.currentClient()
	hubURL := s.currentHubURL()
	return fetchHubServers(client, hubURL)
}

// ── App: message handling ─────────────────────────────────────────────────────

// handleHubMsg processes messages that belong to the hub browser. Returns nil, nil if unhandled.
func (a *App) handleHubMsg(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !a.showHubBrowser {
		return false, nil
	}
	s := &a.hubBrowser

	switch m := msg.(type) {
	case hubServersLoadedMsg:
		s.hubHealth[m.hubURL] = 1
		if m.hubURL != s.currentHubURL() {
			return true, nil // stale response from a previous hub selection
		}
		s.loading = false
		s.err = ""
		s.allServers = m.servers
		s.filtered = make([]HubServerEntry, 0, len(m.servers))
		s.rebuildCategories()
		s.applyFilter()
		// Also fetch peer hub listing so we can show discovered hubs in the add-hub overlay.
		return true, fetchHubPeers(s.currentClient(), m.hubURL)

	case hubLoadErrorMsg:
		s.hubHealth[m.hubURL] = -1
		if m.hubURL != s.currentHubURL() {
			return true, nil
		}
		s.loading = false
		s.err = m.err
		return true, nil

	case hubTabHealthMsg:
		if m.ok {
			s.hubHealth[m.hubURL] = 1
		} else {
			s.hubHealth[m.hubURL] = -1
		}
		return true, nil

	case hubJoinResponseMsg:
		// The hub revealed the address; now redeem the join token with the
		// server itself before trusting it. s.joining stays true meanwhile.
		return true, redeemJoinToken(m.resp)

	case hubJoinVerifiedMsg:
		s.joining = false
		// Build ClientServerInfo from join response
		info := &ClientServerInfo{
			Name:        m.resp.DisplayName,
			Address:     m.resp.Host,
			Port:        m.resp.Port,
			HubServerID: m.resp.ServerID,
		}
		// Try to add; on "already exists", record the hub listing ID on the
		// existing entry so the browser's ✓ joined badge still applies.
		if err := a.configMgr.AddServer(info); err != nil {
			for _, cs := range a.clientServers {
				if cs.Address == info.Address && cs.Port == info.Port && cs.HubServerID == "" {
					cs.HubServerID = m.resp.ServerID
					_ = a.configMgr.UpdateServer(cs)
					break
				}
			}
		}
		// Reload client server list
		if servers := a.configMgr.GetClientServers(); len(servers) > 0 {
			a.clientServers = servers
			// Find the newly added server
			for i, cs := range a.clientServers {
				if cs.Address == info.Address && cs.Port == info.Port {
					a.serverIndex = i
					a.currentClientServer = cs
					break
				}
			}
		}
		a.showHubBrowser = false
		a.hubBrowser = HubBrowserState{}
		a.view = ViewLogin
		a.initLoginView()
		return true, nil

	case hubJoinErrorMsg:
		s.joining = false
		s.joinErr = m.err
		return true, nil

	case hubHealthCheckMsg:
		// Valid hub — add it
		rawURL := m.hubURL
		for _, existing := range s.hubURLs {
			if existing == rawURL {
				s.addHubErr = "Hub already in your list"
				s.mode = hubModeAddHub
				return true, nil
			}
		}
		s.hubURLs = append(s.hubURLs, rawURL)
		name := m.hubName
		if name == "" {
			name = hubDisplayName(rawURL)
		}
		s.hubNames = append(s.hubNames, name)
		s.hubClients[rawURL] = newGrapevineHTTPClient(rawURL)
		s.hubHealth[rawURL] = 1
		s.selectedHub = len(s.hubURLs) - 1
		s.mode = hubModeList
		s.addHubInput.Blur()

		a.persistHubURLs()
		return true, a.refreshCurrentHub()

	case hubHealthCheckErrMsg:
		s.addHubErr = fmt.Sprintf("Not a Grapevine hub: %s", m.err)
		s.mode = hubModeAddHub
		return true, nil

	case hubPeersLoadedMsg:
		if m.hubURL != s.currentHubURL() {
			return true, nil
		}
		s.discoveredPeers = s.discoveredPeers[:0]
		for _, p := range m.peers {
			alreadyAdded := false
			for _, existing := range s.hubURLs {
				if existing == p.URL {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded && p.URL != "" {
				s.discoveredPeers = append(s.discoveredPeers, p)
			}
		}
		return true, nil

	case hubPeersLoadErrMsg:
		// Peer listing is best-effort — silently ignore errors.
		return true, nil
	}

	return false, nil
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func (a *App) renderHubBrowserView() string {
	s := &a.hubBrowser
	w, h := a.width, a.height

	theme := a.theme
	accent := lipgloss.Color(theme.Colors.Purple)
	fg := lipgloss.Color(theme.Colors.Foreground)
	dim := lipgloss.Color(theme.Colors.Comment)
	green := lipgloss.Color(theme.Colors.Green)
	red := lipgloss.Color(theme.Colors.Red)
	bg := lipgloss.Color(theme.Colors.Background)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Background(bg).
		Width(w - 2).
		Height(h - 2)

	// ── Header ────────────────────────────────────────────────────────────────
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)
	title := titleStyle.Render("Grapevine Hub Browser")

	// Hub tabs (✗ marks hubs whose background health probe failed)
	var hubTabParts []string
	for i, name := range s.hubNames {
		label := "[" + name + "]"
		if s.hubHealth[s.hubURLs[i]] == -1 {
			label = "[✗ " + name + "]"
		}
		switch {
		case i == s.selectedHub:
			hubTabParts = append(hubTabParts, lipgloss.NewStyle().
				Bold(true).Foreground(accent).Render(label))
		case s.hubHealth[s.hubURLs[i]] == -1:
			hubTabParts = append(hubTabParts, lipgloss.NewStyle().
				Foreground(red).Render(label))
		default:
			hubTabParts = append(hubTabParts, lipgloss.NewStyle().
				Foreground(dim).Render(label))
		}
	}
	hubTabStr := strings.Join(hubTabParts, " ")
	addHubBtn := lipgloss.NewStyle().Foreground(dim).Render("[+]")

	headerRight := hubTabStr + "  " + addHubBtn + "  " +
		lipgloss.NewStyle().Foreground(dim).Render("[r] Refresh")

	innerW := w - 6 // accounting for border+padding
	rightPad := innerW - lipgloss.Width(title) - lipgloss.Width(headerRight)
	if rightPad < 1 {
		rightPad = 1
	}
	header := title + strings.Repeat(" ", rightPad) + headerRight

	sep := lipgloss.NewStyle().Foreground(dim).Render(strings.Repeat("─", innerW))

	// ── Category tabs ─────────────────────────────────────────────────────────
	var catParts []string
	for i, cat := range s.categories {
		if i == s.selectedCat {
			catParts = append(catParts, lipgloss.NewStyle().
				Bold(true).Foreground(accent).Render(cat))
		} else {
			catParts = append(catParts, lipgloss.NewStyle().
				Foreground(dim).Render(cat))
		}
	}
	catLine := lipgloss.NewStyle().Foreground(dim).Render("Category: ") +
		strings.Join(catParts, lipgloss.NewStyle().Foreground(dim).Render(" │ "))

	// ── Search bar ────────────────────────────────────────────────────────────
	searchLabel := lipgloss.NewStyle().Foreground(dim).Render("Search: ")
	searchBox := s.searchInput.View()
	if !s.searchFocused {
		searchBox = lipgloss.NewStyle().Foreground(fg).Render(
			"[" + s.searchInput.Value() + strings.Repeat(" ", s.searchInput.Width-len(s.searchInput.Value())) + "]")
	}

	// Status indicator (loading / error / count)
	var statusStr string
	if s.loading {
		statusStr = lipgloss.NewStyle().Foreground(dim).Render("loading...")
	} else if s.err != "" {
		statusStr = lipgloss.NewStyle().Foreground(red).Render("Error: " + truncate(s.err, 40))
	} else {
		cnt := fmt.Sprintf("%d server", len(s.filtered))
		if len(s.filtered) != 1 {
			cnt += "s"
		}
		statusStr = lipgloss.NewStyle().Foreground(dim).Render(cnt)
	}

	sortStr := lipgloss.NewStyle().Foreground(dim).Render("sort: " + hubSortLabel(s.sortMode))
	searchLine := searchLabel + searchBox + "   " + statusStr + "   " + sortStr

	// ── Server list ────────────────────────────────────────────────────────────
	// Online/Members/Status are fixed; Server and Category flex to fill the
	// row (minimums 30/12, which the header+separators put at 71 columns).
	colW := []int{30, 12, 7, 8, 10} // Name, Category, Online, Members, Status
	if extra := innerW - 71; extra > 0 {
		catExtra := extra * 2 / 5
		if catExtra > 20 {
			catExtra = 20 // categories are short; give the rest to names
		}
		colW[1] += catExtra
		colW[0] += extra - catExtra
	}
	colHeaders := []string{"Server", "Category", "Online", "Members", "Status"}
	var colHeaderParts []string
	for i, h2 := range colHeaders {
		colHeaderParts = append(colHeaderParts, pad(h2, colW[i]))
	}
	tableHeader := lipgloss.NewStyle().Foreground(dim).Bold(true).
		Render(strings.Join(colHeaderParts, " "))
	tableSep := lipgloss.NewStyle().Foreground(dim).Render(
		strings.Repeat("─", innerW))

	// Calculate how many rows fit
	// Used lines so far: title(1) sep(1) catLine(1) searchLine(1) tableHeader(1) tableSep(1) = 6
	// plus instructions at bottom (1) + border padding (2) = 9
	listH := h - 2 - 2 - 6 - 1 // border(2) + padding(2) + fixed rows(6) + footer(1)
	if listH < 1 {
		listH = 1
	}

	// Build display list (interleaves federation section headers between groups).
	displayList := buildHubDisplayList(s.filtered)

	// Find the display row of the cursor so scrolling is correct.
	cursorDisplayRow := s.cursor
	for i, dr := range displayList {
		if dr.serverIdx == s.cursor {
			cursorDisplayRow = i
			break
		}
	}

	start := 0
	if cursorDisplayRow >= listH {
		start = cursorDisplayRow - listH + 1
	}
	end := start + listH
	if end > len(displayList) {
		end = len(displayList)
	}

	var rows []string
	if s.loading {
		rows = append(rows, lipgloss.NewStyle().Foreground(dim).Italic(true).
			Render("  Fetching server list..."))
	} else if len(s.filtered) == 0 && s.err == "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(dim).Italic(true).
			Render("  No servers found."))
	} else {
		for _, dr := range displayList[start:end] {
			if dr.serverIdx == -1 {
				// Federation section header
				headerLabel := dr.headerText
				headerLine := lipgloss.NewStyle().Foreground(dim).Render(
					"─── " + headerLabel + " " + strings.Repeat("─", max0(innerW-len(headerLabel)-5)))
				rows = append(rows, headerLine)
				continue
			}

			sv := s.filtered[dr.serverIdx]
			selected := dr.serverIdx == s.cursor

			cursor := "  "
			nameStyle := lipgloss.NewStyle().Foreground(fg)
			if selected {
				cursor = lipgloss.NewStyle().Foreground(accent).Render("▶ ")
				nameStyle = nameStyle.Bold(true).Foreground(accent)
			}

			var statusIcon, statusColor string
			if sv.IsOnline {
				statusIcon = "●"
				statusColor = string(green)
			} else {
				statusIcon = "○"
				statusColor = string(dim)
			}
			statusStr2 := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).
				Render(statusIcon + " " + func() string {
					if sv.IsOnline {
						return "online"
					}
					return "offline"
				}())

			nameCell := sv.Name
			if a.isHubServerJoined(sv.ID) {
				nameCell += " ✓"
			}
			nameCell = truncate(nameCell, colW[0]-2)
			catCell := truncate(sv.Category, colW[1])
			onlineCell := fmt.Sprintf("%d", sv.OnlineCount)
			memberCell := fmt.Sprintf("%d", sv.MemberCount)

			row := cursor +
				nameStyle.Render(pad(nameCell, colW[0]-2)) + "  " +
				lipgloss.NewStyle().Foreground(dim).Render(pad(catCell, colW[1])) + " " +
				lipgloss.NewStyle().Foreground(fg).Render(pad(onlineCell, colW[2])) + " " +
				lipgloss.NewStyle().Foreground(fg).Render(pad(memberCell, colW[3])) + " " +
				statusStr2

			rows = append(rows, row)
		}
	}

	// Fill remaining rows with empty lines
	for len(rows) < listH {
		rows = append(rows, "")
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	footer := lipgloss.NewStyle().Foreground(dim).Render(
		"↑/↓ Navigate  Enter Details  A Join  / Search  S Sort  Tab Category  H/L Hubs  +/X Add/Remove Hub  Esc Back")

	// ── Assemble ──────────────────────────────────────────────────────────────
	var body strings.Builder
	body.WriteString(header + "\n")
	body.WriteString(sep + "\n")
	body.WriteString(catLine + "\n")
	body.WriteString(searchLine + "\n")
	body.WriteString(sep + "\n")
	body.WriteString(tableHeader + "\n")
	body.WriteString(tableSep + "\n")
	for _, row := range rows {
		body.WriteString(row + "\n")
	}
	body.WriteString(sep + "\n")
	body.WriteString(footer)

	result := border.Padding(1, 2).Render(body.String())

	// ── Detail overlay ────────────────────────────────────────────────────────
	if s.mode == hubModeDetail && s.detailServer != nil {
		result = a.renderHubDetailOverlay(result)
	}

	// ── Add Hub overlay ───────────────────────────────────────────────────────
	if s.mode == hubModeAddHub {
		result = a.renderAddHubOverlay(result)
	}

	return result
}

// renderHubDetailOverlay draws the detail panel centered over the base view.
func (a *App) renderHubDetailOverlay(base string) string {
	s := &a.hubBrowser
	sv := s.detailServer

	theme := a.theme
	accent := lipgloss.Color(theme.Colors.Purple)
	fg := lipgloss.Color(theme.Colors.Foreground)
	dim := lipgloss.Color(theme.Colors.Comment)
	green := lipgloss.Color(theme.Colors.Green)
	red := lipgloss.Color(theme.Colors.Red)
	bg := lipgloss.Color(theme.Colors.Background)

	dw := 52
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)
	sep := lipgloss.NewStyle().Foreground(dim).Render(strings.Repeat("─", dw-4))

	var onlineStr string
	if sv.IsOnline {
		onlineStr = lipgloss.NewStyle().Foreground(green).Render("● Online") +
			lipgloss.NewStyle().Foreground(dim).
				Render(fmt.Sprintf("  (%d online / %d members)", sv.OnlineCount, sv.MemberCount))
	} else {
		onlineStr = lipgloss.NewStyle().Foreground(lipgloss.Color(string(red))).Render("○ Offline")
	}

	fromLine := ""
	if sv.FromHub != "" {
		fromLine = "\n" + lipgloss.NewStyle().Foreground(dim).Render("From:       ") +
			lipgloss.NewStyle().Foreground(fg).Render(sv.FromHub)
	}

	tagStr := ""
	if len(sv.Tags) > 0 {
		tags := make([]string, len(sv.Tags))
		for i, t := range sv.Tags {
			tags[i] = lipgloss.NewStyle().Foreground(dim).Render(t)
		}
		tagStr = "\n" + lipgloss.NewStyle().Foreground(dim).Render("Tags:       ") +
			strings.Join(tags, lipgloss.NewStyle().Foreground(dim).Render(" • "))
	}

	desc := sv.Description
	if desc == "" {
		desc = lipgloss.NewStyle().Italic(true).Foreground(dim).Render("(no description)")
	} else {
		desc = wrapText(desc, dw-6)
	}

	var joinStatus string
	if s.joining {
		joinStatus = "\n" + lipgloss.NewStyle().Foreground(dim).Render("Requesting connection...")
	} else if s.joinErr != "" {
		joinStatus = "\n" + lipgloss.NewStyle().Foreground(red).Render("Error: "+s.joinErr)
	}

	content := titleStyle.Render(truncate(sv.Name, dw-6)) + "\n" +
		sep + "\n" +
		lipgloss.NewStyle().Foreground(dim).Render("Category:   ") +
		lipgloss.NewStyle().Foreground(fg).Render(sv.Category) + "\n" +
		lipgloss.NewStyle().Foreground(dim).Render("Status:     ") + onlineStr + "\n" +
		lipgloss.NewStyle().Foreground(dim).Render("Last seen:  ") +
		lipgloss.NewStyle().Foreground(fg).Render(sv.LastSeen) +
		fromLine + tagStr + "\n\n" +
		desc +
		joinStatus + "\n\n" +
		lipgloss.NewStyle().Foreground(dim).Render("[A] Add to My Servers   [Esc] Back")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Background(bg).
		Padding(1, 2).
		Width(dw).
		Render(content)

	return overlayCenter(base, box, a.width, a.height)
}

// renderAddHubOverlay draws the "add hub URL" prompt centered over base.
func (a *App) renderAddHubOverlay(base string) string {
	s := &a.hubBrowser
	theme := a.theme
	accent := lipgloss.Color(theme.Colors.Purple)
	fg := lipgloss.Color(theme.Colors.Foreground)
	dim := lipgloss.Color(theme.Colors.Comment)
	red := lipgloss.Color(theme.Colors.Red)
	bg := lipgloss.Color(theme.Colors.Background)

	pw := 62
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("Add Hub")

	var errLine string
	if s.addHubErr != "" {
		errLine = "\n" + lipgloss.NewStyle().Foreground(red).Render(s.addHubErr)
	}

	// Show discovered peer hubs from the current hub's GET /v1/hubs response.
	var discoveredSection string
	hints := "[Enter] Confirm   [Esc] Cancel"
	if len(s.discoveredPeers) > 0 {
		var peerLines []string
		peers := s.discoveredPeers
		if len(peers) > maxDiscoveredPeers {
			peers = peers[:maxDiscoveredPeers]
		}
		for i, p := range peers {
			name := p.Name
			if name == "" {
				name = hubDisplayName(p.URL)
			}
			line := truncate(name+" — "+p.URL, pw-8)
			if i == s.peerCursor {
				peerLines = append(peerLines, lipgloss.NewStyle().Foreground(accent).Bold(true).Render("  ▶ "+line))
			} else {
				peerLines = append(peerLines, lipgloss.NewStyle().Foreground(fg).Render("    "+line))
			}
		}
		discoveredSection = "\n\n" +
			lipgloss.NewStyle().Foreground(dim).Render("Discovered peer hubs (↑/↓ to select):") +
			"\n" + strings.Join(peerLines, "\n")
		hints = "[↑/↓] Pick Hub   " + hints
	}

	content := title + "\n\n" +
		lipgloss.NewStyle().Foreground(dim).Render("Enter a Grapevine hub URL:") + "\n" +
		s.addHubInput.View() +
		errLine +
		discoveredSection + "\n\n" +
		lipgloss.NewStyle().Foreground(dim).Render(hints)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Background(bg).
		Padding(1, 2).
		Width(pw).
		Render(content)

	return overlayCenter(base, box, a.width, a.height)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}

func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) <= width {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = w
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}
