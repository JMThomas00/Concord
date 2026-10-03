package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Updates (Ctrl+U on the login screen): checks GitHub for the newest
// Concord release and compares it with this client. Installing an update
// from here is to come with Concord's installer (To Do E/G); for now the
// page says where to download it.

const releasesURL = "https://api.github.com/repos/JMThomas00/Concord/releases/latest"

type updateState struct {
	checking bool
	latest   string // the newest release's tag, "" if none
	url      string
	date     time.Time
	err      string
}

type updateCheckedMsg struct {
	latest, url string
	date        time.Time
	err         string
}

// openUpdates shows the Updates page and starts a check.
func (a *App) openUpdates() tea.Cmd {
	a.updates = &updateState{}
	a.view = ViewUpdates
	return a.checkForUpdates()
}

func (a *App) checkForUpdates() tea.Cmd {
	a.updates.checking, a.updates.err = true, ""
	return func() tea.Msg {
		client := &http.Client{Timeout: 8 * time.Second}
		req, _ := http.NewRequest("GET", releasesURL, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return updateCheckedMsg{err: "Couldn't reach GitHub: " + err.Error()}
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return updateCheckedMsg{} // no releases published yet
		}
		if resp.StatusCode != http.StatusOK {
			return updateCheckedMsg{err: "GitHub answered " + resp.Status}
		}
		var rel struct {
			TagName     string    `json:"tag_name"`
			HTMLURL     string    `json:"html_url"`
			PublishedAt time.Time `json:"published_at"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			return updateCheckedMsg{err: "Couldn't read GitHub's answer"}
		}
		return updateCheckedMsg{latest: rel.TagName, url: rel.HTMLURL, date: rel.PublishedAt}
	}
}

func (a *App) handleUpdateChecked(m updateCheckedMsg) {
	if a.updates == nil {
		return
	}
	a.updates.checking = false
	a.updates.latest, a.updates.url, a.updates.date, a.updates.err = m.latest, m.url, m.date, m.err
}

func (a *App) handleUpdatesKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.updates = nil
		a.view = ViewLogin
		a.initLoginView()
	case "enter", "r":
		if !a.updates.checking {
			return a.checkForUpdates()
		}
	case "ctrl+q":
		return tea.Quit
	}
	return nil
}

// newerVersion reports whether release tag b is newer than version a
// ("v0.1.2" style; anything unparsable compares as not newer).
func newerVersion(a, b string) bool {
	parse := func(v string) ([3]int, bool) {
		var out [3]int
		parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 3)
		if len(parts) != 3 {
			return out, false
		}
		for i, p := range parts {
			p = strings.SplitN(p, "-", 2)[0]
			n, err := strconv.Atoi(p)
			if err != nil {
				return out, false
			}
			out[i] = n
		}
		return out, true
	}
	va, okA := parse(a)
	vb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if vb[i] != va[i] {
			return vb[i] > va[i]
		}
	}
	return false
}

func (a *App) renderUpdatesView() string {
	u := a.updates
	c := a.theme.Colors
	bold := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground)).Bold(true)
	version := a.clientVersion
	if version == "" {
		version = "dev"
	}
	var b strings.Builder
	b.WriteString(a.dim("You have  ") + bold.Render(version) + "\n")
	switch {
	case u.checking:
		b.WriteString(a.dim("Latest    checking GitHub…") + "\n")
	case u.err != "":
		b.WriteString(a.stageNotice(u.err, true))
	case u.latest == "":
		b.WriteString(a.dim("Latest    no releases published yet") + "\n")
	default:
		when := ""
		if !u.date.IsZero() {
			when = "  (" + u.date.Local().Format("Jan 2, 2006") + ")"
		}
		b.WriteString(a.dim("Latest    ") + bold.Render(u.latest) + a.dim(when) + "\n\n")
		if !newerVersion("0.0.0", version) {
			b.WriteString(a.stageNotice("This is a development build, so it can't be compared with releases.", false))
		} else if newerVersion(version, u.latest) {
			b.WriteString(a.stageNotice("A newer Concord is out. Updating from here is coming with the installer; for now, download it from:", false))
			b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Render(u.url) + "\n")
		} else {
			b.WriteString(a.stageNotice("You're up to date.", false))
		}
	}
	return a.stagePage("Updates", "Is there a newer Concord?", "newer", b.String(),
		[]keyHint{{"Enter", "Check again"}, {"Esc", "Back"}})
}

// String for logs.
func (u *updateState) String() string { return fmt.Sprintf("%+v", *u) }
