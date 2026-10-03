package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/grapes"
	"github.com/concord-chat/concord/internal/installer"
	"github.com/concord-chat/concord/internal/official"
	"github.com/concord-chat/concord/legal"
)

// quips play under the install, Concord's loading-screen voice.
var quips = []string{
	"Mashing grapes…", "Connecting the vines…", "Trellising the vineyard…", "Polishing the wine glasses…",
	"Teaching the grapes to talk…", "Counting the bunch…", "Uncorking something special…", "Pressing this year's vintage…",
	"Watering the vines…", "Warming up the voice channels…", "Folding the napkins…", "Asking the grapes nicely…",
	"Tuning the hub's grapevine…", "Sweeping the cellar…", "Labelling the bottles…", "Picking only the ripe ones…",
}

// fortunes close the summary.
var fortunes = []string{
	"A bunch is stronger than a single grape.",
	"Good conversations, like good wine, take a little time.",
	"Every vineyard starts with one vine.",
	"Your terminal just got a lot friendlier.",
	"Pour one out for the old chat apps.",
}

// --- the questions --------------------------------------------------------------

func questions(pl *installer.Plan, back *bool) *formBuilder {
	p := pl.Platform
	b := &formBuilder{}
	// Moving back (shift+tab) never stops at a check: you can always return
	// to fix an earlier answer, and every answer is checked on the way forward.
	check := func(v func(string) error) func(string) error {
		return func(s string) error {
			if *back {
				return nil
			}
			return v(s)
		}
	}
	var groups []*huh.Group
	if p.OS == "" {
		groups = append(groups, b.group(
			huh.NewSelect[string]().
				Title("Which system is this computer running?").
				Description("Concord couldn't tell by itself.").
				Options(huh.NewOption("macOS", installer.MacOS), huh.NewOption("Linux", installer.Linux), huh.NewOption("Windows", installer.Windows)).
				Value(&pl.Platform.OS),
		))
	}
	opt := func(label, desc, value string) huh.Option[string] {
		return huh.NewOption(fmt.Sprintf("%-8s %s", label, sDim.Render(desc)), value).Selected(pl.Has(value))
	}
	groups = append(groups, b.group(
		huh.NewMultiSelect[string]().
			Title("What do you want to install on this machine?").
			Description("Space picks, enter moves on. Most people only need the client.").
			Options(
				opt("Client", "chat, voice and games, right in your terminal", installer.Client),
				opt("Server", "your own community: channels, voice, plugins", installer.Server),
				opt("Hub", "a Grapevine directory, where people find servers", installer.Hub),
			).
			Value(&pl.Components).
			Validate(func(v []string) error {
				if len(v) == 0 && !*back {
					return fmt.Errorf("pick at least one (space to tick it)")
				}
				return nil
			}),
	))

	dirInput := func(c, title string, dir *string) *huh.Input {
		return huh.NewInput().Title(title).
			DescriptionFunc(func() string {
				if pl.Existing(c) {
					return "Already here: this updates it and keeps your settings."
				}
				if c == installer.Client {
					return "Just the program. Your settings live in " + filepath.Join("~", ".concord") + "."
				}
				return "Its settings, database and plugins live here too."
			}, dir).
			Value(dir).Validate(check(installer.ValidateDir))
	}
	// fresh hides a component's settings when it's already set up, except
	// when configuring again.
	fresh := func(c string) func() bool {
		return func() bool { return !pl.Has(c) || (pl.Existing(c) && !pl.Reconfigure) }
	}
	notChosen := func(c string) func() bool { return func() bool { return !pl.Has(c) } }

	// The client.
	groups = append(groups, b.group(dirInput(installer.Client, "Where should the client live?", &pl.ClientDir)).WithHideFunc(notChosen(installer.Client)))

	// The server.
	groups = append(groups,
		b.group(dirInput(installer.Server, "Where should your server live?", &pl.ServerDir)).WithHideFunc(notChosen(installer.Server)),
		b.group(
			huh.NewInput().Title("What's your server called?").Description("People see this when they join. You can change it later.").
				Value(&pl.ServerName).Validate(check(required("give it a name"))),
			huh.NewInput().Title("Which port should it listen on?").Description("8080 suits most people.").
				Value(&pl.ServerPort).Validate(check(portFree(pl, installer.Server))),
		).WithHideFunc(fresh(installer.Server)),
		b.group(
			huh.NewInput().Title("Your email, to make you its admin").
				Description("Optional. The first person to sign up becomes the owner\nanyway; the account with this email is made an admin too.").
				Placeholder("you@example.com").Value(&pl.AdminEmail).Validate(check(installer.ValidateEmail)),
		).WithHideFunc(fresh(installer.Server)),
		b.group(startQuestion(p, "server", &pl.ServerStart)).WithHideFunc(notChosen(installer.Server)),
		b.group(
			huh.NewConfirm().Title("Connect your server to the Concord hub?").
				Description("Grapevine is Concord's server directory: anyone browsing it can find and\njoin a listed server. Private ones are reached by sharing your address.").
				Affirmative("List it").Negative("Keep it private").Value(&pl.ServerOnHub),
		).WithHideFunc(fresh(installer.Server)),
		b.group(
			huh.NewInput().Title("The address people use to reach your server").
				Description("A domain name or your public IP, not localhost.").
				Placeholder("chat.example.com").Value(&pl.PublicHost).Validate(check(required("the hub needs an address to give people"))),
			huh.NewInput().Title("One line about it").Placeholder("A cozy corner for board games and bad puns").Value(&pl.Description),
		).WithHideFunc(func() bool { return fresh(installer.Server)() || !pl.ServerOnHub }),
		b.group(
			huh.NewMultiSelect[string]().Title("What's it about?").
				Description("Pick as many as fit. People search the Grapevine by these.").
				Options(tagOptions(pl.Tags)...).
				Value(&pl.Tags),
			huh.NewInput().Title("Anything else?").Description("Your own tags, separated by commas.").
				Placeholder("AI, AI Research").Value(&pl.OtherTags),
		).WithHideFunc(func() bool { return fresh(installer.Server)() || !pl.ServerOnHub }),
	)

	// The hub.
	groups = append(groups,
		b.group(dirInput(installer.Hub, "Where should your hub live?", &pl.HubDir)).WithHideFunc(notChosen(installer.Hub)),
		b.group(
			huh.NewInput().Title("What's your hub called?").Value(&pl.HubName).Validate(check(required("give it a name"))),
			huh.NewInput().Title("Which port should it listen on?").Description("7777 unless something else uses it.").
				Value(&pl.HubPort).Validate(check(portFree(pl, installer.Hub))),
		).WithHideFunc(fresh(installer.Hub)),
		b.group(startQuestion(p, "hub", &pl.HubStart)).WithHideFunc(notChosen(installer.Hub)),
		b.group(
			huh.NewConfirm().Title("Connect your hub to the official Concord hub?").
				Description("Connected hubs share their listings: your servers show\nup across the Grapevine, and theirs show up on yours.").
				Affirmative("Connect").Negative("Stand alone").Value(&pl.HubFederate),
		).WithHideFunc(fresh(installer.Hub)),
	)
	_ = groups
	return b
}

// presetTags are the Grapevine's common tags; people add their own too.
var presetTags = []string{"Gaming", "Technology", "Programming", "Art", "Music", "Tabletop", "Anime", "Education", "Science", "Community", "Friends", "Chill"}

func tagOptions(chosen []string) []huh.Option[string] {
	opts := make([]huh.Option[string], len(presetTags))
	for i, t := range presetTags {
		opts[i] = huh.NewOption(t, t).Selected(contains(chosen, t))
	}
	return opts
}

func required(msg string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s", msg)
		}
		return nil
	}
}

// portFree checks a port is a port, differs from the other component's,
// and nothing on this computer is already using it.
func portFree(pl *installer.Plan, component string) func(string) error {
	other, otherPort := installer.Hub, &pl.HubPort
	if component == installer.Hub {
		other, otherPort = installer.Server, &pl.ServerPort
	}
	return func(s string) error {
		if err := installer.ValidatePort(s); err != nil {
			return err
		}
		if pl.Has(other) && strings.TrimSpace(s) == strings.TrimSpace(*otherPort) {
			return fmt.Errorf("the %s already uses that port", other)
		}
		if !pl.PortUnchanged(component, s) && !installer.PortFree(strings.TrimSpace(s)) {
			return fmt.Errorf("something on this computer already uses port %s", strings.TrimSpace(s))
		}
		return nil
	}
}

func startQuestion(p installer.Platform, what string, value *string) *huh.Select[string] {
	var opts []huh.Option[string]
	if p.CanStartAtBoot() {
		opts = append(opts, huh.NewOption("Always: in the background from when the computer starts", installer.StartAtBoot))
	}
	if p.CanStartAtLogin() {
		opts = append(opts, huh.NewOption("While I'm signed in", installer.StartAtLogin))
	}
	opts = append(opts, huh.NewOption("Only when I start it myself", installer.StartNever))
	desc := "Always keeps it up for everyone, even before anyone signs in."
	switch {
	case p.OS == installer.Linux && !p.Systemd:
		desc = "This system has no systemd (WSL?), so it runs when you start it."
	case p.OS == installer.Windows && !p.Admin:
		desc += "\nWindows will ask for permission once."
	case !p.Admin:
		desc += "\nThat needs your password once."
	}
	return huh.NewSelect[string]().Title("When should your " + what + " run?").Description(desc).Options(opts...).Value(value)
}

// --- the terms ----------------------------------------------------------------------

func renderTerms(width int) string {
	text := strings.ReplaceAll(legal.ServerTerms, "\r", "") // the file has Windows line endings
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dracula"), glamour.WithWordWrap(width))
	if err != nil {
		return text
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.Trim(out, "\n")
}

// termsHeight is how tall the terms' box can be inside the page.
func (m *model) termsHeight() int { return max(5, min(m.h-14, 30)) }

// --- layout -----------------------------------------------------------------------------

// The grapes sit where the welcome put them, the content beside them; on
// a small screen the grapes step aside and the content takes the width.
const (
	introGap   = 4                           // between the grapes and the banner
	introWidth = grapes.Size + introGap + 60 // grapes, gap, banner
	introRows  = grapes.Rows + 3             // and the hint under them
)

func (m *model) showSide() bool { return m.w >= introWidth+5 && m.h >= introRows }

// grapesAt is the grapes' top-left corner, the same on every page.
func (m *model) grapesAt() (x, y int) {
	return max(0, (m.w-introWidth)/2), max(0, (m.h-introRows)/2)
}

// contentX is where the content column starts, beside the grapes.
func (m *model) contentX() int {
	x, _ := m.grapesAt()
	return x + grapes.Size + introGap
}

func (m *model) contentWidth() int {
	if m.showSide() {
		return min(76, m.w-m.contentX()-2)
	}
	return max(30, min(80, m.w-4))
}

func (m *model) View() string {
	switch m.st {
	case stIntro:
		return m.viewIntro()
	case stParty:
		return m.viewParty()
	}
	var body string
	switch m.st {
	case stForm:
		body = m.viewForm()
	case stTerms:
		body = m.viewTerms()
	case stReview:
		body = m.viewReview()
	case stInstall, stFailed:
		body = m.viewInstall()
	case stAfter:
		body = m.viewAfter()
	case stConfirm:
		body = m.viewConfirm()
	}
	content := lipgloss.NewStyle().Width(m.contentWidth()).Render(m.header() + "\n\n" + body)
	top := max(1, (m.h-lipgloss.Height(content))/2) // centred on the screen
	if !m.showSide() {
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Top, lipgloss.NewStyle().PaddingTop(min(top, 2)).Render(content))
	}
	gx, gy := m.grapesAt()
	side := lipgloss.NewStyle().PaddingLeft(gx).PaddingTop(gy).Width(m.contentX()).
		Render(grapes.Render(grapes.Frame(m.light), grapeStyles))
	page := lipgloss.JoinHorizontal(lipgloss.Top, side, lipgloss.NewStyle().PaddingTop(top).Render(content))
	return lipgloss.Place(m.w, m.h, lipgloss.Left, lipgloss.Top, page)
}

// header is the title and where you are in the journey.
func (m *model) header() string {
	var name strings.Builder
	for i, r := range "CONCORD" {
		name.WriteString(fg(mix(cPurple, cPink, float64(i)/6)).Bold(true).Render(string(r)) + " ")
	}
	stages := []string{"Choose", "Review", "Install", "Enjoy"}
	at := map[stage]int{stForm: 0, stTerms: 0, stReview: 1, stInstall: 2, stFailed: 2, stAfter: 3}[m.st]
	var trail []string
	for i, s := range stages {
		switch {
		case i == at:
			trail = append(trail, sAccent.Bold(true).Render(s))
		case i < at:
			trail = append(trail, sGood.Render(s))
		default:
			trail = append(trail, sDim.Render(s))
		}
	}
	dry := ""
	if m.plan.DryRun {
		dry = sYellow.Render("  (dry run: nothing will change)")
	}
	return "🍇 " + name.String() + sDim.Render("installer") + dry + "\n" + strings.Join(trail, sDim.Render(" ─ "))
}

// --- the intro ------------------------------------------------------------------------------

func (m *model) viewIntro() string {
	t := time.Since(m.stageAt).Seconds()
	logo := grapes.Render(grapes.Frame(m.light), grapeStyles)
	reveal := int(math.Max(0, (t-.7)/1.0) * float64(len([]rune(banner[0]))))
	tagline := typeOut("Chat that lives in your terminal.", t-1.8, 28)
	tagline2 := typeOut("Let's grow some grapes.", t-2.6, 28)
	words := renderBanner(min(reveal, 999), t*.15) + "\n\n" + sText.Render(tagline) + "\n" + sAccent.Render(tagline2)
	hint := ""
	if t > 1.2 {
		hint = sDim.Render("press any key")
	}
	if !m.showSide() {
		art := lipgloss.JoinVertical(lipgloss.Center, logo, "", sTitle.Render("C O N C O R D"), sText.Render(tagline))
		return center(m.w, m.h, lipgloss.JoinVertical(lipgloss.Center, art, "", hint))
	}
	// Placed exactly where every later page keeps the grapes.
	gx, gy := m.grapesAt()
	words = lipgloss.NewStyle().PaddingTop((grapes.Rows - lipgloss.Height(words)) / 2).Render(words)
	art := lipgloss.JoinHorizontal(lipgloss.Top, logo, strings.Repeat(" ", introGap), words)
	art += "\n\n" + lipgloss.PlaceHorizontal(introWidth, lipgloss.Center, hint)
	page := lipgloss.NewStyle().PaddingLeft(gx).PaddingTop(gy).Render(art)
	return lipgloss.Place(m.w, m.h, lipgloss.Left, lipgloss.Top, page)
}

func typeOut(s string, t float64, perSec float64) string {
	if t <= 0 {
		return ""
	}
	n := min(len([]rune(s)), int(t*perSec))
	return string([]rune(s)[:n])
}

// --- each stage --------------------------------------------------------------------------------

func (m *model) viewForm() string {
	intro := sText.Render("Welcome! ") + sDim.Render("This sets up Concord on "+m.plan.Platform.Name()+". A few questions, then I'll do the rest.")
	if m.notice != "" {
		intro = sWarm.Render(m.notice)
	}
	return intro + "\n\n" + m.form.View()
}

func (m *model) viewTerms() string {
	pct := int(m.terms.ScrollPercent() * 100)
	accept, decline := "  Accept  ", "  Decline  "
	on := lipgloss.NewStyle().Foreground(lipgloss.Color("#282A36")).Background(lipgloss.Color(cPurple)).Bold(true)
	off := lipgloss.NewStyle().Foreground(lipgloss.Color(cFg)).Background(lipgloss.Color(cDim))
	if m.termsOK {
		accept, decline = on.Render(accept), off.Render(decline)
	} else {
		accept, decline = off.Render(accept), on.Render(decline)
	}
	return sTitle.Render("One thing first: the Server Terms") + "\n" +
		sDim.Render("Running a Concord server means agreeing to these.") + "\n\n" +
		lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(cPurple)).Padding(0, 1).
			Render(m.terms.View()) + "\n" +
		accept + " " + decline + sDim.Render(fmt.Sprintf("   %d%% read", pct)) + "\n" +
		helpLine("↑/↓", "read", "←/→", "choose", "enter", "confirm")
}

func (m *model) viewReview() string {
	pl := m.plan
	var b strings.Builder
	b.WriteString(sTitle.Render("Here's the plan") + "\n\n")
	row := func(k, v string) { fmt.Fprintf(&b, "  %s %s\n", sDim.Render(fmt.Sprintf("%-14s", k)), sText.Render(v)) }
	verb := func(c string) string {
		if pl.Existing(c) {
			return sWarm.Render("update")
		}
		return sGood.Render("install")
	}
	startLabel := map[string]string{installer.StartAtBoot: "always, in the background from startup", installer.StartAtLogin: "while you are signed in", installer.StartNever: "only when you start it"}
	row("System", pl.Platform.Name())
	if pl.Has(installer.Client) {
		b.WriteString("\n" + sSection.Render("Client") + "  " + verb(installer.Client) + "\n")
		row("Folder", installer.Tilde(pl.ClientDir))
		row("Command", "concord")
	}
	if pl.Has(installer.Server) {
		b.WriteString("\n" + sSection.Render("Server") + "  " + verb(installer.Server) + "\n")
		row("Folder", installer.Tilde(pl.ServerDir))
		if !pl.Existing(installer.Server) {
			row("Name", pl.ServerName)
			row("Port", pl.ServerPort)
			if pl.AdminEmail != "" {
				row("Admin", pl.AdminEmail)
			}
			if pl.ServerOnHub {
				row("Grapevine", "listed as "+pl.PublicHost)
				if tags := pl.AllTags(); len(tags) > 0 {
					row("Tags", strings.Join(tags, ", "))
				}
			} else {
				row("Grapevine", "private")
			}
		}
		row("Runs", startLabel[pl.ServerStart])
	}
	if pl.Has(installer.Hub) {
		b.WriteString("\n" + sSection.Render("Hub") + "  " + verb(installer.Hub) + "\n")
		row("Folder", installer.Tilde(pl.HubDir))
		if !pl.Existing(installer.Hub) {
			row("Name", pl.HubName)
			row("Port", pl.HubPort)
			if pl.HubFederate {
				row("Grapevine", "connected to "+official.HubName)
			} else {
				row("Grapevine", "stands alone")
			}
		}
		row("Runs", startLabel[pl.HubStart])
	}
	b.WriteString("\n" + m.review.View())
	return b.String()
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *model) viewInstall() string {
	var b strings.Builder
	title := map[string]string{modeInstall: "Installing Concord", modeUpdate: "Updating Concord", modeConfigure: "Configuring Concord", modeUninstall: "Removing Concord"}[m.mode]
	if r := m.runner; r != nil && r.Result.Tag != "" {
		title += " " + r.Result.Tag
	}
	b.WriteString(sTitle.Render(title) + "\n\n")
	spin := spinner[int(time.Now().UnixMilli()/80)%len(spinner)]
	w := m.contentWidth()
	for i, s := range m.steps {
		switch {
		case m.st == stFailed && i == m.failAt:
			b.WriteString(sBad.Render("  ✗ ") + sText.Render(s.Title) + "\n")
		case i < m.cur:
			b.WriteString(sGood.Render("  ✓ ") + sDim.Render(s.Title) + "\n")
		case i == m.cur && m.st == stInstall:
			b.WriteString(sAccent.Render("  "+spin+" ") + sText.Bold(true).Render(s.Title) + "\n")
			if m.detail != "" {
				b.WriteString("    " + sDim.Render(truncate(m.detail, w-6)) + "\n")
			}
			if i == 0 && m.frac > 0 && m.frac < 1 {
				b.WriteString("    " + gradientBar(min(40, w-12), m.frac) + sDim.Render(fmt.Sprintf(" %3d%%", int(m.frac*100))) + "\n")
			}
		default:
			b.WriteString(sDim.Render("  · "+s.Title) + "\n")
		}
	}
	if m.st == stFailed {
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(cRed)).Padding(0, 1).Width(w - 4)
		b.WriteString("\n" + box.Render(sBad.Bold(true).Render("That didn't work.")+"\n"+sText.Render(m.failed.Error())) + "\n\n")
		b.WriteString(helpLine("R", "try again", "C", "change answers", "Q", "quit"))
		return b.String()
	}
	b.WriteString("\n" + fg(cPink).Italic(true).Render(m.quip) + "\n")
	logs := m.logs
	if len(logs) > 3 {
		logs = logs[len(logs)-3:]
	}
	for _, l := range logs {
		b.WriteString(sDim.Render("  "+truncate(l, w-4)) + "\n")
	}
	return b.String()
}

func (m *model) viewAfter() string {
	return sGood.Bold(true).Render("Concord is installed. 🍇") + "\n" + sDim.Render("Two quick things, and you're in.") + "\n\n" + m.after.View()
}

// viewParty is confetti bursting out from behind "ready".
func (m *model) viewParty() string {
	type cell struct {
		s string
		w int
	}
	grid := make([][]cell, m.h)
	for r := range grid {
		grid[r] = make([]cell, m.w)
		for c := range grid[r] {
			grid[r][c] = cell{" ", 1}
		}
	}
	for _, c := range m.confetti {
		x, y := int(c.x), int(c.y)
		if y < 0 || y >= m.h || x < 0 || x >= m.w-1 {
			continue
		}
		grid[y][x] = cell{fg(c.col).Render(c.ch), ansi.StringWidth(c.ch)}
		if grid[y][x].w == 2 {
			grid[y][x+1] = cell{"", 0}
		}
	}
	msg := []string{renderBanner(-1, time.Since(m.stageAt).Seconds()*.4), "", sGood.Bold(true).Render("is ready.")}
	if m.w < 64 {
		msg = []string{sTitle.Render("C O N C O R D"), sGood.Bold(true).Render("is ready.")}
	}
	block := strings.Split(lipgloss.JoinVertical(lipgloss.Center, msg...), "\n")
	top := (m.h - len(block)) / 2
	var b strings.Builder
	for r := 0; r < m.h; r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		if i := r - top; i >= 0 && i < len(block) {
			line := block[i]
			pad := max(0, (m.w-ansi.StringWidth(line))/2)
			b.WriteString(strings.Repeat(" ", pad) + line)
			continue
		}
		for c := 0; c < m.w; c++ {
			if grid[r][c].w > 0 {
				b.WriteString(grid[r][c].s)
			}
		}
	}
	return b.String()
}

// --- after it's over (printed to the terminal, so it stays) ------------------------------------

func (m *model) summary() string {
	pl, res := m.plan, m.runner.Result
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	cmd := func(s string) string { return sCode.Render(s) }
	tag := res.Tag
	if tag == "" {
		tag = "dev"
	}
	line("")
	line("🍇 " + sGood.Bold(true).Render("Concord "+tag+" is ready.") + map[bool]string{true: sYellow.Render("  (dry run: nothing was changed)")}[pl.DryRun])
	for _, c := range res.Missing {
		line(sWarm.Render(fmt.Sprintf("   This release has no %s for %s yet, so it wasn't installed.", c, pl.Platform.Name())))
	}
	if contains(res.Installed, installer.Client) {
		line("")
		line(sSection.Render("Client") + sDim.Render("  "+pl.ClientDir))
		if res.NewPath {
			line("   Open a " + sText.Bold(true).Render("new terminal") + ", then type " + cmd("concord") + " to start it.")
		} else {
			line("   Type " + cmd("concord") + " to start it.")
		}
		if pl.Platform.OS == installer.Windows {
			line("   Or pick " + sText.Bold(true).Render("Concord") + " from Windows Terminal's new-tab menu.")
		}
	}
	if contains(res.Installed, installer.Server) {
		line("")
		line(sSection.Render("Server") + sDim.Render("  "+pl.ServerDir))
		port := portOf(m, installer.Server)
		if res.Running[installer.Server] {
			line("   " + sGood.Render("Running.") + " Friends on your network connect to " + cmd(addr(port)) + ".")
		} else if pl.ServerStart == installer.StartNever {
			line("   Start it with " + cmd(pl.StartCommand(installer.Server)))
		}
		if !res.Updated[installer.Server] {
			line("   The first person to sign up becomes its owner: make it you!")
			if pl.ServerOnHub {
				line("   Listed on the Grapevine as " + cmd(pl.PublicHost) + ". Forward port " + fmt.Sprint(port) + " on your router so people can reach it.")
			}
		}
		line("   Settings: " + cmd(filepath.Join(pl.ServerDir, installer.ServerConfigFile)) + "   Log: " + cmd(pl.LogFile(installer.Server)))
		line("   Plugins go in " + cmd(filepath.Join(pl.ServerDir, "Plugins")) + ", or install them from Settings → Plugins.")
	}
	if contains(res.Installed, installer.Hub) {
		line("")
		line(sSection.Render("Hub") + sDim.Render("  "+pl.HubDir))
		port := portOf(m, installer.Hub)
		if res.Running[installer.Hub] {
			line("   " + sGood.Render("Running") + " at " + cmd("http://"+addr(port)) + ". Servers register with that address.")
		} else if pl.HubStart == installer.StartNever {
			line("   Start it with " + cmd(pl.StartCommand(installer.Hub)))
		}
		if res.HubToken != "" {
			line("   Admin token (save it somewhere safe; it isn't shown again):")
			line("   " + sYellow.Render(res.HubToken))
		}
		line("   Settings: " + cmd(filepath.Join(pl.HubDir, installer.HubConfigFile)) + "   Log: " + cmd(pl.LogFile(installer.Hub)))
	}
	if len(res.Notes) > 0 || m.addedNote != "" {
		line("")
		for _, n := range append(res.Notes, m.addedNote) {
			if n != "" {
				line(sWarm.Render("   " + n))
			}
		}
	}
	line("")
	line(sTitle.Render("Next steps"))
	n := 0
	step := func(s string) { n++; line(fmt.Sprintf("   %s %s", sAccent.Render(fmt.Sprintf("%d.", n)), s)) }
	if contains(res.Installed, installer.Client) {
		step("Open Concord (" + cmd("concord") + ") and create your profile: a name, your email and a password.")
		if m.join {
			step("You'll find " + sText.Bold(true).Render(official.ServerName) + " in your server list. Say hi!")
		} else {
			step("Add a server with " + sKey.Render("Ctrl+B") + ", or browse the Grapevine with " + sKey.Render("Ctrl+G") + ".")
		}
		if contains(res.Installed, installer.Server) {
			step("Your own server is in the list too. Sign up there first to become its owner.")
		}
	} else if contains(res.Installed, installer.Server) {
		step("Install the client on any computer, then add this server: " + cmd(addr(portOf(m, installer.Server))) + ".")
	}
	step("Run this installer again any time to update.")
	line("")
	line(sDim.Render("   " + fortunes[time.Now().UnixNano()%int64(len(fortunes))]))
	if m.openNow && !pl.DryRun {
		line("")
		line(sAccent.Render("   Opening Concord…"))
	}
	return b.String()
}

func (m *model) failureReport() string {
	return "\n" + sBad.Bold(true).Render("Concord wasn't fully installed.") + "\n" + sText.Render(m.failed.Error()) + "\n\n" +
		sDim.Render("Run the installer again to retry; it picks up where it left off. If it keeps failing, open an issue at https://github.com/"+installer.Repo+"/issues") + "\n"
}

func portOf(m *model, c string) int {
	if m.runner != nil {
		if u := m.runner.HealthURL(c); u != "" {
			var p int
			if _, err := fmt.Sscanf(u[strings.LastIndex(u, ":")+1:], "%d", &p); err == nil {
				return p
			}
		}
	}
	return 0
}

func addr(port int) string {
	host := installer.LANAddress()
	if host == "" {
		host, _ = os.Hostname()
	}
	return fmt.Sprintf("%s:%d", host, port)
}
