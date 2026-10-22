package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/themes"
	zone "github.com/lrstanley/bubblezone"
	ansitruncate "github.com/muesli/reflow/truncate"
)

// helpMarkdown is the in-app guide: how to drive the client and the keys
// worth knowing. The wiki has everything else, so this links there rather
// than repeating it (and goes stale less). Bullet lists only: glamour
// doesn't wrap tables to the panel's width.
const helpMarkdown = `# Concord — Quick Guide

How to get around Concord, and the keys worth knowing. The full guide is the **wiki**: github.com/JMThomas00/Concord/wiki (pages named below are there).

---

## The window

From left to right: your **servers**, the current server's **channels**, the **chat**, and the **members**.

- **Tab** / **Shift+Tab** — move between them
- **[** / **]** — hide or show the server list / member list
- **Esc** — close, cancel, go back
- The mouse works too: click, scroll and drag.

Start typing anywhere in the chat and it goes to the message box.

## Servers

- **Ctrl+N** — add a server: its address, port and a name for it
- **Ctrl+G** (login screen) — browse public servers on the Grapevine
- **Settings → Manage Servers** — edit or remove servers; **B** browses the Grapevine
- **Ctrl+B** — Server Settings, for admins: channels, roles, members, messages and plugins

One profile signs you in to every server. Switching servers keeps your voice call.

## Channels

- **↑** / **↓** — move (the channel opens as you go)
- **←** / **→** — collapse or expand a group
- **Enter** — join or leave a voice channel (` + "`♪`" + `)
- **Shift+↑** / **Shift+↓** — reorder (moderators)

## Chatting

- **Enter** — send; **Ctrl+J** — a new line
- **@** — mention someone (**↑**/**↓** pick, **Enter** or **Tab** accepts)
- **Ctrl+E** — pick an emoji: type to search, arrows and **Tab** to browse, **Ctrl+T** for a skin tone, **Enter** to add it. Recently used come first. Or type a shortcode like ` + "`:grapes:`" + `, which becomes 🍇 as you type the last colon.
- **/** — a command; ` + "`/help`" + ` searches them all
- **PgUp** / **PgDn** — scroll back
- Markdown works: **bold**, *italic*, ` + "`code`" + `, lists and code blocks. Links are clickable in most terminals.

### Message navigation (Alt+M)

Pick a message with **↑**/**↓**, then:

- **R** reply, **T** reply in its thread, **E** edit, **D** delete
- **C** copy, **L** its links, **A** copy a shared file's ID
- **Enter** — expand or minimise a thread; on other messages, select text inside it
- **Esc** — leave

### Threads

A thread keeps a side conversation in one box where it started. **Alt+M**, pick a message, **T**, and type; **Esc** stops replying in it. **Alt+T** (or ` + "`/threads`" + `) lists the channel's threads.

### Whispers and files

- ` + "`/w @user <message>`" + ` — a private message that isn't stored
- ` + "`/attach <path> [caption]`" + ` — share a file, straight from your computer (stay online so people can download it)
- ` + "`/download <id>`" + ` — download one (**Alt+M**, **A** copies the ID)

## Voice

- **Enter** on a voice channel — join; **Enter** again — leave
- **Ctrl+M** — mute yourself; **Ctrl+D** — deafen yourself
- In the member list, **←** / **→** on someone in your call sets their volume

**Settings → Audio** has your microphone and speakers, noise suppression, echo cancellation and voice activity sensitivity. Voice is Opus at 48 kHz; the Codec Quality preset sets the bitrate (24, 48, 64 or 96 kbps). Trouble connecting across networks? See *Voice* and *Voice Across Networks* on the wiki.

## Members

**↑**/**↓** to pick someone, **Enter** for their menu:

- **W** whisper, **V** volume (in voice)
- With permission: **M** mute, **K** kick, **B** ban, **R**/**E** give or remove a role, **X**/**D** voice mute or deafen, **O** move to another voice channel

## Plugins and games

Some channels belong to a plugin: a game, a board, a bot. Select one and its screen opens in the chat area, and your keys go to it.

- **Esc** or **Shift+Tab** — back to the channel list (unless the plugin is using Esc; the border says)
- **Ctrl+]** — always hands your keys back

Admins install plugins in **Ctrl+B → Plugins** (**I**, then the plugin's GitHub name). See *Plugins and Games* and *Managing Plugins* on the wiki.

## Settings (Ctrl+S)

**↑**/**↓** picks a page, **Tab** goes into it, **Esc** comes back.

- **Theme** — 40+ themes, previewed live (also **Ctrl+T**, or ` + "`/theme <name>`" + `)
- **Display** — timestamps, density, panels, voice level style, plugin pictures, animations
- **Notifications** — desktop popups, sounds and per-server mutes
- **Audio** — devices, voice processing, plugin sounds
- **About** — versions; **A** opens your achievements

## The login screen

- **Ctrl+P** — profiles: switch, add, edit, change password, forget
- **Ctrl+F** — forgot your password (a code by email, or a temporary password from the admin)
- **Ctrl+U** — update, reconfigure or uninstall Concord
- **Ctrl+R** — a new banner

## Everywhere

- **Ctrl+S** — Settings
- **Ctrl+X** — dismiss the bottom notification card
- **Ctrl+Q** — quit

---

## More on the wiki

- *Shortcuts and Commands* — every key and slash command
- *Troubleshooting* — sign-in, voice, pictures and colours
- *Running a Server* — host your own, with the installer or Docker
- *Plugin Quick Start* — build a bot or a game

*Concord is open source: github.com/JMThomas00/Concord*
`

// renderHelpContent renders the full user guide using glamour markdown rendering.
// The right edge of the content area carries a scrollbar showing position in the document.
func (a *App) renderHelpContent(width, height int) string {
	s := a.settingsState

	dimStyle   := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Yellow)).Bold(true)
	thumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Yellow))
	trackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))

	layout := calculateSettingsLayout(width, height, 2, 0)

	// Reserve 2 chars on the right of each middle line for the scrollbar (" █" / " │").
	contentWidth := layout.interiorWidth - 2

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(titleStyle.Render("Help & User Guide"))
	top.writeLine(dimStyle.Render(fmt.Sprintf("Complete reference for Concord v%s", a.clientVersion)))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE — glamour-rendered markdown with inline scrollbar ──
	// Cache rendered lines; invalidate when content width OR the active
	// theme changes -- the style is theme-derived now (buildThemedGlamourStyle),
	// so a theme switch must re-render, not just a resize.
	//
	// The width check is additionally gated on !a.helpResizing: renderHelpMarkdown
	// does a full goldmark+chroma pass over the whole document (~40ms
	// measured on a high-end desktop CPU), and a live window-drag resize
	// fires many WindowSizeMsg ticks in quick succession -- recomputing on
	// every single one of them (real bug, live-reported 2026-09-11) blocked
	// the render loop repeatedly, seen as very slow redraws while widening
	// and garbled/torn frames while narrowing. While a resize is still in
	// progress this deliberately keeps showing the last-computed (possibly
	// stale-width) lines; the debounce timer in Update() clears helpResizing
	// once the resize actually settles, and the next render then does the
	// one real recompute at the final width. A theme switch isn't part of a
	// resize drag, so it still always recomputes immediately regardless.
	if s != nil && (s.HelpRenderedLines == nil || s.HelpRenderTheme != a.theme.Meta.Name || (!a.helpResizing && s.HelpRenderWidth != contentWidth)) {
		s.HelpRenderedLines = renderHelpMarkdown(contentWidth, a.theme)
		s.HelpRenderWidth = contentWidth
		s.HelpRenderTheme = a.theme.Meta.Name
	}

	var allLines []string
	if s != nil && s.HelpRenderedLines != nil {
		allLines = s.HelpRenderedLines
	}

	totalLines  := len(allLines)
	trackHeight := layout.middleLines

	// Clamp scroll offset.
	offset := 0
	if s != nil {
		offset = s.HelpScrollOffset
		maxOffset := totalLines - trackHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if offset > maxOffset {
			offset = maxOffset
			s.HelpScrollOffset = offset
		}
		if offset < 0 {
			offset = 0
			s.HelpScrollOffset = 0
		}
	}

	end := offset + trackHeight
	if end > totalLines {
		end = totalLines
	}
	window := allLines[offset:end]

	// Compute scrollbar thumb position and size.
	thumbPos, thumbSize := helpScrollbarThumb(offset, totalLines, trackHeight)

	// Build each middle line as [content padded to contentWidth] + [2-char scrollbar].
	// The scrollbar column is zone-marked as one continuous zone spanning
	// every row (writeZoneMarkedLines's join-mark-split technique, adapted
	// here since this builds a plain strings.Builder rather than a
	// settingsSectionBuilder) so a click/drag anywhere in the column can
	// resolve its row via z.Pos(msg), letting handleHelpScrollbarMouse
	// (mouse.go) map that row to a scroll offset -- click-to-jump and
	// click-and-drag both fall out of the same row->offset math.
	scrollGlyphs := make([]string, trackHeight)
	for i := 0; i < trackHeight; i++ {
		if totalLines > trackHeight {
			if i >= thumbPos && i < thumbPos+thumbSize {
				scrollGlyphs[i] = thumbStyle.Render(" █")
			} else {
				scrollGlyphs[i] = trackStyle.Render(" │")
			}
		} else {
			scrollGlyphs[i] = "  "
		}
	}
	markedScrollbar := zone.Mark("help-scrollbar", strings.Join(scrollGlyphs, "\n"))
	markedGlyphs := strings.SplitN(markedScrollbar, "\n", trackHeight)

	var middleBuf strings.Builder
	for i := 0; i < trackHeight; i++ {
		// Content — pad/clip to contentWidth so the scrollbar column stays aligned.
		contentLine := ""
		if i < len(window) {
			contentLine = window[i]
		}
		// Guards against the debounced resize cache briefly showing a line
		// rendered at a wider, stale width than the current, now-narrower
		// contentWidth. lipgloss.Style.Width alone only PADS short content
		// -- given content already wider than the target, it word-WRAPS it
		// into multiple lines rather than truncating, which would throw off
		// this whole fixed-row-per-line loop (each row assumes exactly one
		// physical line in, one out). ansitruncate.String actually cuts a
		// single line down to a printable-cell width, ANSI-escape-aware, so
		// wide stale content collapses back to one row instead of several.
		contentLine = ansitruncate.String(contentLine, uint(contentWidth))
		line := lipgloss.NewStyle().Width(contentWidth).Render(contentLine)

		middleBuf.WriteString(line + markedGlyphs[i])
		if i < trackHeight-1 {
			middleBuf.WriteString("\n")
		}
	}

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	focused := s != nil && s.FocusOnForm
	if focused {
		bottom.writeLine(dimStyle.Render("↑↓ / PgUp PgDn / scroll wheel · Tab back to menu · Esc close"))
	} else {
		bottom.writeLine(dimStyle.Render("Tab for keyboard scroll · scroll wheel anywhere · Esc close"))
	}
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middleBuf.String(), bottom.String())
	borderColor := lipgloss.Color(a.theme.Colors.Comment)
	if focused {
		borderColor = lipgloss.Color(a.theme.Colors.Yellow)
	}
	// Border() adds 2 lines on top of Height(N) -- see the matching comment
	// in renderServerIconsCollapsed (views.go). Found again here 2026-09-07
	// wiring mouse support to Help & Guide. The whole pane is wrapped in one
	// zone so a click anywhere focuses it (no per-line selection needed --
	// this is read-only content; wheel-scroll already works regardless of
	// focus, this just enables keyboard scroll without pressing Tab first).
	return zone.Mark("settings-help-panel", lipgloss.NewStyle().
		Width(width).Height(height-2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).Render(content))
}

// helpScrollbarThumb computes the scrollbar thumb start row and height in track coordinates.
func helpScrollbarThumb(offset, totalLines, trackHeight int) (thumbPos, thumbSize int) {
	if totalLines <= trackHeight {
		return 0, trackHeight
	}
	thumbSize = trackHeight * trackHeight / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxThumbPos := trackHeight - thumbSize
	maxOffset   := totalLines - trackHeight
	if maxOffset > 0 {
		thumbPos = offset * maxThumbPos / maxOffset
	}
	return
}

// renderHelpMarkdown renders the help markdown document via glamour and returns
// it as a slice of lines ready for the scroll-window display.
func renderHelpMarkdown(width int, theme *themes.Theme) []string {
	wrapWidth := width - 2
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(buildThemedGlamourStyle(theme)),
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return []string{"[glamour init error: " + err.Error() + "]"}
	}

	rendered, err := r.Render(helpMarkdown)
	if err != nil {
		return []string{"[glamour render error: " + err.Error() + "]"}
	}

	// Split into individual lines; preserve empty lines for spacing.
	lines := strings.Split(rendered, "\n")
	// Trim trailing empty lines that glamour adds.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
