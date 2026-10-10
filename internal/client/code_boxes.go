package client

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The code screen's six boxes: each letter flips in as it's typed, a code
// the server accepts ripples green before the screen closes, and the wait
// before another code can be sent drains as a bar.

const (
	codeLen        = 6
	codeFlip       = 160 * time.Millisecond
	codeAcceptHold = 700 * time.Millisecond
	resendCooldown = 60 * time.Second // the server's (internal/server/accounts.go)
)

// codeAcceptedMsg closes the code screen once its ripple has played.
type codeAcceptedMsg struct{ st *AccountCodeState }

// acceptCode plays the ripple, then closes the screen.
func (a *App) acceptCode(st *AccountCodeState) tea.Cmd {
	st.Accepted = time.Now()
	a.celebrate()
	return tea.Tick(codeAcceptHold, func(time.Time) tea.Msg { return codeAcceptedMsg{st} })
}

// codeChars is what's been typed, without spaces or dashes, uppercased.
func codeChars(v string) []rune {
	var out []rune
	for _, r := range strings.ToUpper(v) {
		if r != ' ' && r != '-' {
			out = append(out, r)
		}
	}
	return out
}

// usesBoxes reports whether the code screen shows its code as boxes (a
// temporary password from an admin is typed in a plain field).
func (st *AccountCodeState) usesBoxes() bool {
	return !st.TempPassword && !st.Fixing && !(st.Mode == codeModeForgot && st.Step == 0)
}

// trackCodeTyping notes when each letter was typed, for its flip. Called
// after every Update.
func (a *App) trackCodeTyping(now time.Time) {
	st := a.codeState
	if st == nil || a.view != ViewAccountCode {
		return
	}
	n := len(codeChars(st.Code.Value()))
	if n < len(st.typedAt) {
		st.typedAt = st.typedAt[:n]
	}
	for len(st.typedAt) < n {
		st.typedAt = append(st.typedAt, now)
	}
}

// codeAnimating reports whether the code screen has something moving.
func (a *App) codeAnimating(now time.Time) bool {
	st := a.codeState
	if st == nil || a.view != ViewAccountCode {
		return false
	}
	if !st.Accepted.IsZero() || (!st.SentAt.IsZero() && now.Sub(st.SentAt) < resendCooldown) {
		return true
	}
	for _, t := range st.typedAt {
		if now.Sub(t) < codeFlip {
			return true
		}
	}
	return false
}

// renderCodeBoxes draws the six boxes for what's been typed.
func (a *App) renderCodeBoxes(st *AccountCodeState, now time.Time) string {
	c := a.theme.Colors
	chars := codeChars(st.Code.Value())
	boxes := make([]string, codeLen)
	for i := range boxes {
		border, fg := c.Comment, c.Foreground
		text := " "
		switch {
		case i < len(chars):
			text = string(chars[i])
			border = c.Purple
			if i < len(st.typedAt) {
				// The flip: the letter's card turns over as it lands.
				switch age := now.Sub(st.typedAt[i]); {
				case age < codeFlip/2:
					text, fg = "▄", c.Purple
				case age < codeFlip:
					text, fg = "▀", c.Purple
				}
			}
		case i == len(chars) && !st.Busy && st.Focus == 0:
			text, fg = "_", c.Purple // where the next letter goes
			if now.UnixMilli()/500%2 == 0 {
				text = " "
			}
		}
		if !st.Accepted.IsZero() && now.Sub(st.Accepted) >= time.Duration(i)*60*time.Millisecond {
			border, fg = c.Green, c.Green // the ripple
		} else if st.IsErr && st.Notice != "" && len(chars) == codeLen {
			border = c.Red
		}
		boxes[i] = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(border)).
			Foreground(lipgloss.Color(fg)).Bold(true).Padding(0, 1).Render(text)
	}
	dash := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Render("\n ─ \n")
	parts := append(append(append([]string{}, boxes[:3]...), dash), boxes[3:]...)
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// renderResendBar shows the wait before another code can be sent, as a
// draining bar, then the key to send one.
func (a *App) renderResendBar(st *AccountCodeState, now time.Time) string {
	c := a.theme.Colors
	left := resendCooldown - now.Sub(st.SentAt)
	if st.SentAt.IsZero() || left <= 0 {
		return a.dim("Not there? Check spam, or ") + lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true).Render("Ctrl+R") + a.dim(" for a new code.")
	}
	const w = 24
	full := int(float64(w) * float64(left) / float64(resendCooldown))
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Render(strings.Repeat("▰", full)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Render(strings.Repeat("▱", w-full))
	return bar + a.dim(fmt.Sprintf("  a new code in %ds", int(left.Seconds()+0.999)))
}
