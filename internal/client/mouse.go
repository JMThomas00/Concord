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
	// Settings' and Server Management's slide-in/out animations
	// (clipPanelLeft/clipPanelRight) do column-based ANSI-aware string
	// truncation *after* zone marks are already embedded in the rendered
	// string -- truncating mid-slide can cut a marker's start/end apart,
	// corrupting that frame's registered click bounds. Ignore clicks until
	// each settles. One check covers both views since each flag is only
	// ever true while its own view is active.
	if a.settingsAnimating || a.srvMgmtAnimating {
		return nil
	}

	if a.linkBrowserState != nil {
		return a.handleLinkBrowserMouse(msg)
	}

	// Same overlay-priority position View() itself uses (link browser, then
	// help modal, then member context menu) -- without this branch, a click
	// meant for the popup falls through to handleMainViewMouse and gets
	// misread as a click on the member list underneath it.
	if a.memberContextMenu != nil {
		return a.handleMemberContextMenuMouse(msg)
	}

	if a.view == ViewMain {
		return a.handleMainViewMouse(msg)
	}
	if a.view == ViewSettings {
		return a.handleSettingsMouse(msg)
	}
	if a.view == ViewServerManagement {
		return a.handleServerManagementMouse(msg)
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

	if node, ok := a.resolveClickedChannelRow(msg); ok {
		a.setFocus(FocusChannelList)
		if node.IsCategory {
			a.toggleCategoryCollapsed(node.Channel.ID)
		} else {
			a.selectChannelByID(node.Channel.ID)
		}
		return nil
	}

	if idx, ok := a.resolveClickedServerRow(msg); ok {
		a.setFocus(FocusServerIcons)
		a.switchToClientServer(idx)
		return nil
	}

	if idx, ok := a.resolveClickedMemberRow(msg); ok {
		a.setFocus(FocusUserList)
		a.selectedMemberIndex = idx
		return a.openMemberContextMenu()
	}

	if cmd, ok := a.resolveClickedStatusBarHint(msg); ok {
		return cmd
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

// resolveClickedStatusBarHint handles the four clickable status-bar
// segments renderStatusBar zone-marks (Area 5 of "Concord - Mouse Support
// Plan"). "Tab: Navigate" and the conditionally-shown "Enter: Join/Leave
// voice" are deliberately left unmarked/unclickable, per that plan's scope.
// The status bar only ever renders as part of ViewMain, so each segment's
// action is called directly rather than routed through a synthesized key.
func (a *App) resolveClickedStatusBarHint(msg tea.MouseMsg) (tea.Cmd, bool) {
	if zoneInBounds("statusbar-settings", msg) {
		return a.openSettings(ViewMain), true
	}
	if zoneInBounds("statusbar-server-settings", msg) {
		if a.currentUserRoleLevel() >= roleLevelAdmin {
			return a.openServerManagement(ViewMain, 0), true
		}
		return nil, true
	}
	if zoneInBounds("statusbar-help", msg) {
		result, err := a.commandHandler.Execute(&Command{Name: "help"})
		if err == nil {
			a.openHelpModal(result)
		}
		return nil, true
	}
	if zoneInBounds("statusbar-quit", msg) {
		return tea.Quit, true
	}
	return nil, false
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
// returns the tree node whose zone contains the click -- the caller
// branches on node.IsCategory to decide between selecting a channel and
// toggling a category's collapsed state (see handleMainViewMouse).
func (a *App) resolveClickedChannelRow(msg tea.MouseMsg) (node *ChannelTreeNode, ok bool) {
	if a.channelTree == nil {
		return nil, false
	}
	for _, n := range a.channelTree.FlatList {
		if zoneInBounds("channel-row:"+n.Channel.ID.String(), msg) {
			return n, true
		}
	}
	return nil, false
}

// toggleCategoryCollapsed flips a category's collapsed state directly by
// ID, rebuilding the flat list and persisting the change -- unlike the
// keyboard path's handleCollapseCategory/handleExpandCategory (which infer
// their target from a.currentChannel), a click already names the exact
// category clicked, so there's no need to route through that indirection.
func (a *App) toggleCategoryCollapsed(categoryID uuid.UUID) {
	if a.channelTree == nil {
		return
	}
	a.collapsedCategories[categoryID] = !a.collapsedCategories[categoryID]
	a.channelTree.RebuildFlatList(a.collapsedCategories)
	a.saveCollapsedState()
}

// resolveClickedServerRow checks every server row's zone (rendered by
// renderServerIcons/renderServerIconsCollapsed, views.go) and returns the
// index of the one whose zone contains the click. That index feeds directly
// into switchToClientServer(index), the same function every keyboard path
// (arrow nav, Enter, Ctrl+Shift+S) already converges on.
//
// Known, accepted coupling, not something to "fix" here: the render loop
// indexes a.configMgr.GetClientServers() (re-read from disk every frame)
// while switchToClientServer indexes the in-memory a.clientServers field.
// They stay in sync today because nothing else mutates one without the
// other, not because of an enforced shared source of truth.
func (a *App) resolveClickedServerRow(msg tea.MouseMsg) (index int, ok bool) {
	if a.configMgr == nil {
		return 0, false
	}
	servers := a.configMgr.GetClientServers()
	for i := range servers {
		if zoneInBounds(fmt.Sprintf("server-row:%d", i), msg) {
			return i, true
		}
	}
	return 0, false
}

// resolveClickedMemberRow checks every member row's zone (rendered by
// renderUserList/renderUserListCollapsed, views.go) and returns the flat
// index of the one whose zone contains the click. buildFlatMemberList()'s
// own doc comment guarantees its order matches renderUserList's three
// render loops exactly, so trusting the loop index here carries no risk of
// drift between render time and click time.
func (a *App) resolveClickedMemberRow(msg tea.MouseMsg) (index int, ok bool) {
	flatMembers := a.buildFlatMemberList()
	for i, m := range flatMembers {
		if m.User == nil {
			continue
		}
		if zoneInBounds("member-row:"+m.User.ID.String(), msg) {
			return i, true
		}
	}
	return 0, false
}

// handleMemberContextMenuMouse handles clicks while the member action
// popup (a.memberContextMenu) is open. A click on a visible action row does
// exactly what pressing Enter after arrow-navigating to it does -- set
// SelectedIndex, call the existing executeMemberAction() -- reusing the
// same side effect rather than duplicating it.
func (a *App) handleMemberContextMenuMouse(msg tea.MouseMsg) tea.Cmd {
	m := a.memberContextMenu
	if m == nil {
		return nil
	}
	// The popup's pop-in animation re-renders its content at a narrower
	// width every frame (see renderMemberContextMenuOverlay's width-based
	// clip), which can slice a zone marker's start/end apart mid-animation
	// and corrupt that frame's registered bounds -- unlike Settings/Server
	// Management's slide animations, this one has no DisablePanelAnimations
	// opt-out and runs on every single open. Ignore clicks until it settles.
	if m.AnimFrame < contextMenuMaxFrames {
		return nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	// Volume-slider mode has no discrete rows -- a live bar adjusted by
	// Left/Right only. Out of scope for mouse support, same as the
	// no-drag policy established elsewhere (see the Mouse Support Plan).
	if m.VolumeSlider != nil {
		return nil
	}
	for i := range m.Actions {
		if zoneInBounds(fmt.Sprintf("member-action-row:%d", i), msg) {
			m.SelectedIndex = i
			return a.executeMemberAction()
		}
	}
	return nil
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
