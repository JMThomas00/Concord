package client

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The login stage: every page before the main window (profiles, setup,
// add server, codes, terms) shares the login screen's layout. The grapes
// and banner stay where they are and only the area under the banner
// changes, so moving between pages feels like one place. Pages use the
// concord-site's typography: a small tracked label, a headline with one
// purple word, then the page.

// stageRows is the height every stage page reserves under the banner (the
// login form's), with the key hints on its last row, so the grapes and
// banner don't move between pages. A taller page takes what it needs.
const stageRows = 12

// stageWidth is the stage's text width.
const stageWidth = 60

// tracked spaces a label's letters out: "P R O F I L E S".
func tracked(s string) string {
	rs := []rune(strings.ToUpper(s))
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return strings.Join(out, " ")
}

func (a *App) stageLabel(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render(tracked(s))
}

// stageHeadline renders a headline with its accent word in purple.
func (a *App) stageHeadline(text, accent string) string {
	plain := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Bold(true)
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	i := strings.Index(text, accent)
	if accent == "" || i < 0 {
		return plain.Render(text)
	}
	return plain.Render(text[:i]) + purple.Render(accent) + plain.Render(text[i+len(accent):])
}

// stagePage lays a page out on the stage: label, headline, body, and the
// key hints pinned to the bottom of the reserved rows.
func (a *App) stagePage(label, headline, accent, body string, hints []keyHint) string {
	return a.stagePageWith(label, "", headline, accent, body, hints)
}

// stagePageWith is stagePage with something beside the label (a progress
// line), which saves the page a row.
func (a *App) stagePageWith(label, beside, headline, accent, body string, hints []keyHint) string {
	var top strings.Builder
	if label != "" {
		top.WriteString(a.stageLabel(label))
		if beside != "" {
			top.WriteString("   " + beside)
		}
		top.WriteString("\n")
	}
	if headline != "" {
		top.WriteString(a.stageHeadline(headline, accent) + "\n")
	}
	top.WriteString("\n" + strings.TrimRight(body, "\n"))
	hintBlock := a.renderKeyHintsAligned(hints, stageWidth, lipgloss.Left)
	content := top.String()
	used := lipgloss.Height(content) + 1 + lipgloss.Height(hintBlock)
	gap := max(1, stageRows-used+1)
	block := content + strings.Repeat("\n", gap+1) + hintBlock
	// The grapes line up as on the login screen (two rows of hints) whatever
	// this page's hints take, so they don't move from page to page.
	a.formHintRows = 2
	a.stageStable = lipgloss.Height(block)
	return a.layoutBannerScreen(a.renderBanner(), block, a.stageStable)
}

// stageNotice is a page's message line: green, or red for a problem.
func (a *App) stageNotice(msg string, isErr bool) string {
	if msg == "" {
		return ""
	}
	color, prefix := a.theme.Colors.Green, "✓ "
	if isErr {
		color, prefix = a.theme.Colors.Red, "⚠ "
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Width(stageWidth).Render(prefix+msg) + "\n"
}

// stageField is a labelled input on one row: "Email   ▏value".
func (a *App) stageField(label string, focused bool, view string) string {
	mark := "  "
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Width(12)
	if focused {
		mark = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true).Render("▸ ")
		style = style.Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	}
	return mark + style.Render(label) + view
}

// stageSteps is a progress line for pages done one question at a time:
// ●───●───○───○ (done and current filled).
func (a *App) stageSteps(current, total int) string {
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	off := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	var b strings.Builder
	for i := 0; i < total; i++ {
		if i > 0 {
			if i <= current {
				b.WriteString(on.Render("───"))
			} else {
				b.WriteString(off.Render("───"))
			}
		}
		if i <= current {
			b.WriteString(on.Render("●"))
		} else {
			b.WriteString(off.Render("○"))
		}
	}
	return b.String() + off.Render(fmt.Sprintf("   %d of %d", current+1, total))
}

// passwordStrength scores a password from 0 to 8: one grape each.
func passwordStrength(pw string) int {
	if pw == "" {
		return 0
	}
	score := 0
	n := len([]rune(pw))
	for _, min := range []int{1, 8, 12, 16} {
		if n >= min {
			score++
		}
	}
	var lower, upper, digit, other bool
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			other = true
		}
	}
	for _, has := range []bool{lower && upper, digit, other} {
		if has {
			score++
		}
	}
	distinct := map[rune]bool{}
	for _, r := range pw {
		distinct[r] = true
	}
	if len(distinct) >= 6 {
		score++
	}
	if n < 8 {
		score = min(score, 2) // too short to be accepted at all
	}
	return min(score, 8)
}

// passwordGrapes shows a password's strength as a bunch of eight grapes
// lighting up (five over three), with a word for it.
func (a *App) passwordGrapes(pw string) string {
	c := a.theme.Colors
	score := passwordStrength(pw)
	words := []string{"empty", "too short", "too short", "weak", "fair", "good", "strong", "very strong", "perfectly ripe"}
	colour := c.Red
	switch {
	case score >= 7:
		colour = c.Green
	case score >= 5:
		colour = c.Purple
	case score >= 3:
		colour = c.Yellow
	}
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(colour)).Bold(true)
	off := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	grape := func(i int) string {
		if i < score {
			return on.Render("●")
		}
		return off.Render("○")
	}
	rows := [][]int{{0, 1, 2, 3, 4}, {5, 6, 7}}
	var out []string
	for r, idx := range rows {
		line := strings.Repeat(" ", r)
		for _, i := range idx {
			line += grape(i) + " "
		}
		if r == 0 {
			line += strings.Repeat(" ", 3) + on.Render(words[score])
		}
		out = append(out, "  "+line)
	}
	return strings.Join(out, "\n")
}

// --- reactions ---------------------------------------------------------------

// The stage reacts: when a page shows a problem the form shakes and the
// grapes flush red; on success the leaf glows green.

const (
	shakeDur = 380 * time.Millisecond
	reactDur = 900 * time.Millisecond
)

type grapeReaction struct {
	success bool
	at      time.Time
}

// stageError is the problem the page on screen is showing, if any.
func (a *App) stageError() string {
	switch a.view {
	case ViewLogin, ViewRegister:
		return a.loginError
	case ViewIdentitySetup:
		return a.identityError
	case ViewAddServer:
		return a.addServerError
	case ViewAccountCode:
		if st := a.codeState; st != nil && st.IsErr {
			return st.Notice
		}
	case ViewProfiles:
		if s := a.profilesState; s != nil && s.NoticeErr {
			return s.Notice
		}
	}
	return ""
}

// noticeReactions starts the shake when a new problem appears. Called after
// every Update.
func (a *App) noticeReactions(now time.Time) {
	err := a.stageError()
	if err != "" && err != a.fx.lastErr && a.surprise() != surpriseOff {
		a.fx.shakeAt = now
		a.grapeReact = &grapeReaction{at: now}
	}
	a.fx.lastErr = err
}

// celebrate glows the leaf for a success.
func (a *App) celebrate() {
	a.grapeReact = &grapeReaction{success: true, at: time.Now()}
}

func (a *App) shaking(now time.Time) bool {
	return !a.fx.shakeAt.IsZero() && now.Sub(a.fx.shakeAt) < shakeDur
}

// shake moves the form's rows side to side, settling.
func shake(g *fxGrid, top, bottom, left int, p float64) {
	off := int(math.Round(math.Sin(p*math.Pi*5) * 3 * (1 - p)))
	if off == 0 {
		return
	}
	for r := max(0, top); r <= bottom && r < g.h; r++ {
		row := g.rows[r]
		moved := make([]fxCell, len(row))
		copy(moved, row)
		for c := max(0, left); c < g.w; c++ {
			moved[c] = blankCell
		}
		out := &fxGrid{w: g.w, h: 1, rows: [][]fxCell{moved}}
		for c := max(0, left); c < g.w; c++ {
			putCell(out, 0, c+off, row[c])
		}
		g.rows[r] = moved
	}
}

// reactionTint is how far the grapes are tinted now (0 to 1).
func (a *App) reactionTint(now time.Time) (float64, bool) {
	r := a.grapeReact
	if r == nil {
		return 0, false
	}
	age := now.Sub(r.at)
	if age >= reactDur {
		a.grapeReact = nil
		return 0, false
	}
	return 1 - easeOutCubic(float64(age)/float64(reactDur)), r.success
}
