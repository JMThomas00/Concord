package client

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"
)

// handleMouseMsg is the mouse-event counterpart to handleKeyPress, and
// mirrors its layering: overlay-first (anything modal that captures all
// input when open), then a view-specific handler. Only what's actually
// interactive today is handled here -- see "Concord - Mouse Support Plan"
// in the Obsidian vault for what's scoped to this pass vs. deferred.
func (a *App) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
	if a.linkBrowserState != nil {
		return a.handleLinkBrowserMouse(msg)
	}

	if a.view == ViewMain {
		return a.handleMainViewMouse(msg)
	}

	return nil
}

// handleMainViewMouse handles mouse input over the normal 4-panel main
// view. Only left-click presses do anything here; wheel scrolling is
// handled separately in Update() (unchanged from before this file existed,
// just re-pointed at the "chat-panel" zone instead of the old hand-derived
// isCursorOverChatViewport).
func (a *App) handleMainViewMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}

	// Most specific zones first: a link inside a message takes priority
	// over "clicked the chat panel", and a channel row takes priority over
	// "clicked the channel list panel".
	if link, ok := a.resolveClickedLink(msg); ok {
		return a.openURL(link)
	}

	if z := zone.Get("chat-viewport-content"); z != nil && z.InBounds(msg) {
		_, relY := z.Pos(msg)
		if idx, ok := resolveMessageAtLine(a.messageLineOffsets, a.chatViewport.YOffset, relY); ok {
			return a.selectMessageAtIndex(idx)
		}
		// Clicked inside the message area but not on any known message
		// (e.g. blank space below the last one) -- just focus the panel.
		a.setFocus(FocusChat)
		return nil
	}

	if zoneInBounds("chat-input", msg) {
		a.setFocus(FocusInput)
		return nil
	}

	if channelID, ok := a.resolveClickedChannelRow(msg); ok {
		a.setFocus(FocusChannelList)
		a.selectChannelByID(channelID)
		return nil
	}

	switch {
	case zoneInBounds("server-icons", msg):
		a.setFocus(FocusServerIcons)
	case zoneInBounds("channel-list", msg):
		a.setFocus(FocusChannelList)
	case zoneInBounds("chat-panel", msg):
		a.setFocus(FocusChat)
	case zoneInBounds("user-list", msg):
		a.setFocus(FocusUserList)
	}

	return nil
}

// zoneInBounds is a small convenience wrapper -- zone.Get returns nil for
// an ID that hasn't been marked/scanned yet (e.g. the very first frame, or
// a panel that's currently hidden), and a nil *zone.ZoneInfo isn't safe to
// call InBounds on directly from call sites that don't already guard it.
func zoneInBounds(id string, msg tea.MouseMsg) bool {
	z := zone.Get(id)
	return z != nil && z.InBounds(msg)
}

// resolveClickedLink checks every link zone for every message currently
// loaded in the active channel and returns the URL whose zone contains the
// click. It reconstructs candidate zone IDs from extractLinksFromMessage
// rather than tracking a separate "currently visible links" list, so it
// necessarily stays in sync with however renderMessageContent numbered
// them (same message, same link order, same regex).
func (a *App) resolveClickedLink(msg tea.MouseMsg) (string, bool) {
	if a.activeConn == nil || a.currentChannel == nil {
		return "", false
	}
	messages := a.activeConn.GetMessages(a.currentChannel.ID)
	for _, m := range messages {
		links := a.extractLinksFromMessage(m)
		for i, link := range links {
			if zoneInBounds(fmt.Sprintf("link:%s:%d", m.ID.String(), i), msg) {
				return link, true
			}
		}
	}
	return "", false
}

// resolveClickedChannelRow checks every channel/category row's zone and
// returns the channel ID whose zone contains the click.
func (a *App) resolveClickedChannelRow(msg tea.MouseMsg) (channelID uuid.UUID, ok bool) {
	if a.channelTree == nil {
		return channelID, false
	}
	for _, node := range a.channelTree.FlatList {
		if zoneInBounds("channel-row:"+node.Channel.ID.String(), msg) {
			return node.Channel.ID, true
		}
	}
	return channelID, false
}

// setFocus changes which panel has focus, applying the same textarea
// blur/focus side effect cycleFocus already applies when entering/leaving
// FocusInput, so a mouse-driven focus change behaves identically to a
// keyboard-driven one.
func (a *App) setFocus(target FocusArea) {
	if a.focus == target {
		return
	}
	if a.focus == FocusInput {
		a.input.Blur()
	}
	a.focus = target
	if target == FocusInput {
		a.input.Focus()
	}
}

// selectMessageAtIndex enters the same Level-1 message-navigation state the
// Alt+M keyboard shortcut does (app.go's "alt+m" case), targeting a
// specific message instead of always the newest one, and skipping the
// scroll-into-view step since a message that was just clicked is already
// on screen. Every existing keyboard handler for a selected message (edit,
// delete, reply, copy, etc.) then works unmodified for a mouse-selected one.
func (a *App) selectMessageAtIndex(idx int) tea.Cmd {
	if a.activeConn == nil || a.currentChannel == nil {
		return nil
	}
	messages := a.activeConn.GetMessages(a.currentChannel.ID)
	if idx < 0 || idx >= len(messages) {
		return nil
	}
	a.messageNavMode = true
	a.inMessageEditMode = false
	a.focus = FocusMessageNav
	a.messageNavIndex = idx
	a.messageSelectionStart = nil
	a.messageSelectionEnd = nil
	a.input.Blur()
	a.updateChatContent()
	return tea.HideCursor
}

// resolveMessageAtLine maps a click's row within the chat viewport's
// currently-rendered content (0-based, relative to the top of what's on
// screen right now -- i.e. zone.Get("chat-viewport-content").Pos(msg)'s Y)
// to the message occupying that line, using the same lineOffsets
// updateChatContent() already maintains for keyboard navigation
// (a.messageLineOffsets). viewportYOffset is a.chatViewport.YOffset at the
// time of the click. Returns ok=false if the list is empty or the line
// falls before the first message (e.g. in a leading blank/date-separator
// row).
func resolveMessageAtLine(lineOffsets []int, viewportYOffset, clickLine int) (msgIndex int, ok bool) {
	if len(lineOffsets) == 0 {
		return 0, false
	}
	absLine := viewportYOffset + clickLine
	if absLine < lineOffsets[0] {
		return 0, false
	}
	for i := len(lineOffsets) - 1; i >= 0; i-- {
		if lineOffsets[i] <= absLine {
			return i, true
		}
	}
	return 0, false
}

// handleLinkBrowserMouse handles clicks while the /links overlay is open.
// A click on a visible row does exactly what pressing that row's digit key
// does (handleLinkBrowserKey's "1".."9" case) -- resolve the row, open the
// link, close the browser -- reusing the same side effect rather than
// duplicating it.
func (a *App) handleLinkBrowserMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.linkBrowserState
	if s == nil || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	for i := range s.Links {
		if zoneInBounds(fmt.Sprintf("link-row:%d", i), msg) {
			link := s.Links[i]
			a.closeLinkBrowser()
			return a.openURL(link)
		}
	}
	return nil
}
