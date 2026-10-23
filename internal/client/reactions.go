package client

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Reactions: Alt+M to a message, + opens the emoji picker on it, and the
// emoji picked is added (or, if it's already yours, taken off). They show
// as chips under the message ("👍 3  🍇 1", yours highlighted); the picker
// says who reacted with what.

// reactionsVersion is the first server version that takes reactions
// (OpReaction). A server built from source says "dev".
const reactionsVersion = "0.1.7"

// takesReactions reports whether sc's server takes reactions.
func takesReactions(sc *ServerConnection) bool {
	if sc == nil {
		return false
	}
	v := strings.TrimPrefix(strings.TrimSpace(sc.ServerVersion), "v")
	if v == "dev" {
		return true
	}
	return versionAtLeast(v, reactionsVersion)
}

// versionAtLeast compares dotted versions ("0.1.10" >= "0.1.7"); anything
// unparsable is too old.
func versionAtLeast(v, min string) bool {
	vs, ms := strings.Split(v, "."), strings.Split(min, ".")
	for i := range ms {
		if i >= len(vs) {
			return false
		}
		a, err1 := strconv.Atoi(strings.SplitN(vs[i], "-", 2)[0])
		b, err2 := strconv.Atoi(ms[i])
		if err1 != nil || err2 != nil {
			return false
		}
		if a != b {
			return a > b
		}
	}
	return true
}

// reactToSelected opens the picker to react to the message selected in
// message navigation.
func (a *App) reactToSelected() {
	msgs := a.visibleMessages()
	if a.messageNavIndex < 0 || a.messageNavIndex >= len(msgs) {
		return
	}
	m := msgs[a.messageNavIndex]
	switch {
	case m.IsSystem || m.IsDeleted || m.Message == nil:
		a.statusMessage = "You can't react to that message"
	case !takesReactions(a.activeConn):
		a.statusMessage = "This server doesn't take reactions yet: it needs Concord v" + reactionsVersion + " or later"
	default:
		a.openEmojiPicker()
		a.emojiPicker.ReactTo = m
	}
}

// myReaction reports whether this user has reacted to m with emoji.
func (a *App) myReaction(m *MessageDisplay, emoji string) bool {
	if a.activeConn == nil || a.activeConn.User == nil {
		return false
	}
	for _, r := range m.Reactions {
		if r.Emoji == emoji {
			for _, id := range r.UserIDs {
				if id == a.activeConn.User.ID {
					return true
				}
			}
		}
	}
	return false
}

// sendReaction adds emoji to m, or takes it off if it's already yours.
// The chips change when the server says so (MESSAGE_REACTION_ADD/_REMOVE).
func (a *App) sendReaction(m *MessageDisplay, emoji string) {
	if a.activeConn == nil || a.activeConn.Connection == nil {
		a.statusMessage = "Not connected"
		return
	}
	msg, err := protocol.NewMessage(protocol.OpReaction, &protocol.ReactionRequest{
		ChannelID: m.ChannelID, MessageID: m.ID, Emoji: emoji, Remove: a.myReaction(m, emoji),
	})
	if err == nil {
		err = a.activeConn.Connection.Send(msg)
	}
	if err != nil {
		a.statusMessage = "Couldn't send the reaction: " + err.Error()
	}
}

// handleReactionEvent applies a MESSAGE_REACTION_ADD or _REMOVE to the
// message, wherever it is: the channel's history or a thread's replies.
func (a *App) handleReactionEvent(sc *ServerConnection, data json.RawMessage, add bool) {
	var p protocol.ReactionPayload
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("Failed to parse reaction payload: %v", err)
		return
	}
	sc.mu.Lock()
	var target *MessageDisplay
	for _, m := range sc.Messages[p.ChannelID] {
		if m.ID == p.MessageID {
			target = m
			break
		}
	}
	for _, ts := range sc.Threads {
		if target != nil {
			break
		}
		for _, m := range ts.replies {
			if m.ID == p.MessageID {
				target = m
				break
			}
		}
	}
	if target != nil {
		target.Reactions = applyReaction(target.Reactions, p.Emoji, p.UserID, add)
	}
	sc.mu.Unlock()
	if target != nil && a.activeConn == sc && a.currentChannel != nil && a.currentChannel.ID == p.ChannelID {
		a.updateChatContent()
	}
}

// applyReaction adds or takes off one person's reaction, keeping each
// emoji where it first appeared and dropping one nobody has any more.
func applyReaction(list []models.Reaction, emoji string, user uuid.UUID, add bool) []models.Reaction {
	i := 0
	for i < len(list) && list[i].Emoji != emoji {
		i++
	}
	has := func(r models.Reaction) int {
		for j, id := range r.UserIDs {
			if id == user {
				return j
			}
		}
		return -1
	}
	switch {
	case add && i == len(list):
		return append(list, models.Reaction{Emoji: emoji, Count: 1, UserIDs: []uuid.UUID{user}})
	case add && has(list[i]) < 0:
		list[i].UserIDs = append(list[i].UserIDs, user)
		list[i].Count = len(list[i].UserIDs)
	case !add && i < len(list):
		if j := has(list[i]); j >= 0 {
			list[i].UserIDs = append(list[i].UserIDs[:j:j], list[i].UserIDs[j+1:]...)
			list[i].Count = len(list[i].UserIDs)
			if list[i].Count == 0 {
				return append(list[:i:i], list[i+1:]...)
			}
		}
	}
	return list
}

// formatReactionLines draws a message's reactions under its text, on its
// side, joined to it by a corner (╰ under someone else's, ╯ under yours),
// wrapped to width: each emoji and its count, the count bold in the accent
// colour where one of them is yours. Each chip is a click zone (to react
// with the same emoji, or take yours off; hovering shows who). A selected
// message (Alt+M) shares its highlight with them and lists who reacted.
// No trailing newline: the caller starts it on a line of its own.
func (a *App) formatReactionLines(m *MessageDisplay, width int, rightAlign, selected bool) string {
	if len(m.Reactions) == 0 {
		return ""
	}
	mine := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	other := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	room := max(10, width-2*messageGutterWidth-2) // (the corner)
	var rows []string
	row := ""
	for _, r := range m.Reactions {
		count := other
		if a.myReaction(m, r.Emoji) {
			count = mine
		}
		id := fmt.Sprintf("react:%s:%d", m.ID, len(a.reactionZones))
		a.reactionZones = append(a.reactionZones, reactionZone{id: id, msg: m, emoji: r.Emoji})
		chip := zone.Mark(id, r.Emoji+" "+count.Render(strconv.Itoa(r.Count)))
		switch {
		case row == "":
			row = chip
		case ansi.StringWidth(row)+2+ansi.StringWidth(chip) > room:
			rows = append(rows, row)
			row = chip
		default:
			row += "  " + chip
		}
	}
	rows = append(rows, row)
	for i := range rows {
		switch {
		case i == 0 && rightAlign:
			rows[i] += dim.Render(" ╯")
		case i == 0:
			rows[i] = dim.Render("╰ ") + rows[i]
		case rightAlign:
			rows[i] += "  "
		default:
			rows[i] = "  " + rows[i]
		}
	}
	if selected { // who, for the keyboard: hovering does it for the mouse
		rows = append(rows, dim.Render(ansi.Truncate(a.whoReacted(m), room, "…")))
	}
	style := lipgloss.NewStyle().Width(width)
	if rightAlign {
		style = style.Align(lipgloss.Right).PaddingRight(messageGutterWidth)
	} else {
		style = style.PaddingLeft(messageGutterWidth)
	}
	out := strings.Join(rows, "\n")
	if selected {
		style = style.Background(lipgloss.Color(a.theme.Colors.Selection))
		out = reassertBackgroundAfterResets(out, a.theme.Colors.Selection)
	}
	return style.Render(out)
}

// reactionZone is one reaction chip on screen, for the mouse.
type reactionZone struct {
	id    string
	msg   *MessageDisplay
	emoji string
}

// reactionHoverMsg says the mouse moved onto (or off, at nil) a chip.
type reactionHoverMsg struct{ zone *reactionZone }

// reactionChipAt is the chip under the mouse, if any.
func (a *App) reactionChipAt(x, y int) *reactionZone {
	for i := range a.reactionZones {
		if z := zone.Get(a.reactionZones[i].id); z != nil && !z.IsZero() &&
			x >= z.StartX && x <= z.EndX && y >= z.StartY && y <= z.EndY {
			return &a.reactionZones[i]
		}
	}
	return nil
}

// reactionHover is MouseHoverFilter's part: a move onto or off a chip
// becomes a reactionHoverMsg (to show or hide who reacted); any other move
// is nothing, as before.
func (a *App) reactionHover(m tea.MouseMsg) tea.Msg {
	if a.view != ViewMain || a.emojiPicker != nil {
		return nil
	}
	z := a.reactionChipAt(m.X, m.Y)
	var was string
	if a.hoverChip != nil {
		was = a.hoverChip.id
	}
	var now string
	if z != nil {
		now = z.id
	}
	if now == was {
		return nil
	}
	return reactionHoverMsg{zone: z}
}

// clickReaction reacts with a clicked chip's emoji, or takes yours off.
func (a *App) clickReaction(msg tea.MouseMsg) bool {
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return false
	}
	z := a.reactionChipAt(msg.X, msg.Y)
	if z == nil {
		return false
	}
	if !takesReactions(a.activeConn) {
		a.statusMessage = "This server doesn't take reactions yet: it needs Concord v" + reactionsVersion + " or later"
		return true
	}
	a.sendReaction(z.msg, z.emoji)
	return true
}

// withReactionTooltip puts who reacted with the hovered chip's emoji on the
// line above it (below, at the top of the screen).
func (a *App) withReactionTooltip(frame string) string {
	h := a.hoverChip
	if h == nil || a.view != ViewMain {
		return frame
	}
	z := zone.Get(h.id)
	if z == nil || z.IsZero() {
		return frame
	}
	var names []string
	for _, r := range h.msg.Reactions {
		if r.Emoji == h.emoji {
			for _, id := range r.UserIDs {
				names = append(names, a.memberName(id))
			}
		}
	}
	if len(names) == 0 {
		return frame
	}
	tip := lipgloss.NewStyle().Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Render(ansi.Truncate(" "+h.emoji+" "+strings.Join(names, ", ")+" ", max(10, a.width-2), "…"))
	lines := strings.Split(frame, "\n")
	y := z.StartY - 1
	if y < 0 {
		y = z.EndY + 1
	}
	if y >= len(lines) {
		return frame
	}
	w := lipgloss.Width(tip)
	x := min(max(0, z.StartX), max(0, a.width-w))
	line := lines[y]
	if lw := lipgloss.Width(line); lw < x+w {
		line += strings.Repeat(" ", x+w-lw)
	}
	lines[y] = visualTake(line, x) + tip + visualSkip(line, x+w)
	return strings.Join(lines, "\n")
}

// whoReacted says who reacted to m with what: "👍 alice, bob · 🍇 carol".
func (a *App) whoReacted(m *MessageDisplay) string {
	var parts []string
	for _, r := range m.Reactions {
		var names []string
		for _, id := range r.UserIDs {
			names = append(names, a.memberName(id))
		}
		parts = append(parts, r.Emoji+" "+strings.Join(names, ", "))
	}
	return strings.Join(parts, " · ")
}

// memberName is a member's name on this server, or "someone" if they've left.
func (a *App) memberName(id uuid.UUID) string {
	sc := a.activeConn
	if sc == nil {
		return "someone"
	}
	if sc.User != nil && sc.User.ID == id {
		return "you"
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	for _, m := range sc.Members {
		if m.User != nil && m.User.ID == id {
			return m.GetDisplayName()
		}
	}
	return "someone"
}
