package client

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/themes"
	"github.com/google/uuid"
)

// Command represents a parsed slash command
type Command struct {
	Name string
	Args []string
}

// ParseCommand parses a slash command string into a Command struct
func ParseCommand(input string) (*Command, error) {
	if !strings.HasPrefix(input, "/") {
		return nil, errors.New("not a command")
	}

	// Remove leading slash and split into parts
	parts := strings.Fields(input[1:])
	if len(parts) == 0 {
		return nil, errors.New("empty command")
	}

	return &Command{
		Name: strings.ToLower(parts[0]),
		Args: parts[1:],
	}, nil
}

// CommandHandler handles slash command execution
type CommandHandler struct {
	app *App
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(app *App) *CommandHandler {
	return &CommandHandler{app: app}
}

// Execute executes a parsed command
func (ch *CommandHandler) Execute(cmd *Command) (string, error) {
	switch cmd.Name {
	case "create-channel":
		return ch.handleCreateChannel(cmd.Args)
	case "create-category":
		return ch.handleCreateCategory(cmd.Args)
	case "delete-channel":
		return ch.handleDeleteChannel(cmd.Args)
	case "delete-category":
		return ch.handleDeleteCategory(cmd.Args)
	case "rename-channel":
		return ch.handleRenameChannel(cmd.Args)
	case "move-channel":
		return ch.handleMoveChannel(cmd.Args)
	case "help":
		return ch.handleHelp(cmd.Args)
	case "theme":
		return ch.handleTheme(cmd.Args)
	case "mute":
		// /mute with no args = mute current channel; /mute @user = server-mute
		if len(cmd.Args) > 0 && strings.HasPrefix(cmd.Args[0], "@") {
			return ch.handleMuteMember(cmd.Args, true)
		}
		return ch.handleMuteChannel(false)
	case "unmute":
		if len(cmd.Args) > 0 && strings.HasPrefix(cmd.Args[0], "@") {
			return ch.handleMuteMember(cmd.Args, false)
		}
		return ch.handleMuteChannel(true)
	case "role":
		return ch.handleRole(cmd.Args)
	case "kick":
		return ch.handleKickBan(cmd.Args, false)
	case "ban":
		return ch.handleKickBan(cmd.Args, true)
	case "whisper", "w":
		return ch.handleWhisper(cmd.Args)
	default:
		return "", fmt.Errorf("unknown command: %s", cmd.Name)
	}
}

func (ch *CommandHandler) handleCreateChannel(args []string) (string, error) {
	if len(args) < 1 {
		return "", errors.New("usage: /create-channel <name>")
	}

	if ch.app.activeConn == nil || ch.app.currentServer == nil {
		return "", errors.New("not connected to a server")
	}

	name := strings.Join(args, " ")

	// Determine category from current channel if in a category
	var categoryID *uuid.UUID
	if ch.app.currentChannel != nil && ch.app.channelTree != nil {
		node := ch.app.channelTree.NodeMap[ch.app.currentChannel.ID]
		if node != nil && node.Parent != nil && node.Parent.IsCategory {
			id := node.Parent.Channel.ID
			categoryID = &id
		}
	}

	req := &protocol.ChannelCreateRequest{
		ServerID:   ch.app.currentServer.ID,
		Name:       name,
		Type:       models.ChannelTypeText,
		CategoryID: categoryID,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelCreate, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := ch.app.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	return fmt.Sprintf("Creating channel #%s...", name), nil
}

func (ch *CommandHandler) handleCreateCategory(args []string) (string, error) {
	if len(args) < 1 {
		return "", errors.New("usage: /create-category <name>")
	}

	if ch.app.activeConn == nil || ch.app.currentServer == nil {
		return "", errors.New("not connected to a server")
	}

	name := strings.Join(args, " ")

	req := &protocol.ChannelCreateRequest{
		ServerID: ch.app.currentServer.ID,
		Name:     name,
		Type:     models.ChannelTypeCategory,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelCreate, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := ch.app.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	return fmt.Sprintf("Creating category '%s'...", strings.ToUpper(name)), nil
}

func (ch *CommandHandler) handleDeleteChannel(args []string) (string, error) {
	if ch.app.activeConn == nil || ch.app.currentServer == nil {
		return "", errors.New("not connected to a server")
	}

	if ch.app.currentChannel == nil {
		return "", errors.New("no channel selected")
	}

	req := &protocol.ChannelDeleteRequest{
		ServerID:  ch.app.currentServer.ID,
		ChannelID: ch.app.currentChannel.ID,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelDelete, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := ch.app.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	return fmt.Sprintf("Deleting channel #%s...", ch.app.currentChannel.Name), nil
}

func (ch *CommandHandler) handleDeleteCategory(args []string) (string, error) {
	if len(args) < 1 {
		return "", errors.New("usage: /delete-category <name>")
	}

	if ch.app.activeConn == nil || ch.app.currentServer == nil {
		return "", errors.New("not connected to a server")
	}

	categoryName := strings.Join(args, " ")
	log.Printf("DELETE-CATEGORY: Looking for category '%s'", categoryName)

	// Find the category by name
	if ch.app.channelTree == nil || ch.app.channelTree.Root == nil {
		return "", errors.New("no channels loaded")
	}

	var categoryID uuid.UUID
	var foundCategory *ChannelTreeNode

	// Search through root's children for the category (case-insensitive)
	for _, node := range ch.app.channelTree.Root.Children {
		if node.IsCategory && node.Channel != nil {
			log.Printf("DELETE-CATEGORY: Found category in tree: '%s'", node.Channel.Name)
			if strings.EqualFold(node.Channel.Name, categoryName) {
				categoryID = node.Channel.ID
				foundCategory = node
				break
			}
		}
	}

	if foundCategory == nil {
		return "", fmt.Errorf("category '%s' not found", categoryName)
	}

	// Check if category has any channels in it
	if len(foundCategory.Children) > 0 {
		return "", fmt.Errorf("cannot delete category '%s': it contains %d channel(s). Please move or delete them first.", categoryName, len(foundCategory.Children))
	}

	req := &protocol.ChannelDeleteRequest{
		ServerID:  ch.app.currentServer.ID,
		ChannelID: categoryID,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelDelete, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := ch.app.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	return fmt.Sprintf("Deleting category '%s'...", categoryName), nil
}

func (ch *CommandHandler) handleRenameChannel(args []string) (string, error) {
	if len(args) < 1 {
		return "", errors.New("usage: /rename-channel <new-name>")
	}

	if ch.app.activeConn == nil || ch.app.currentServer == nil {
		return "", errors.New("not connected to a server")
	}

	if ch.app.currentChannel == nil {
		return "", errors.New("no channel selected")
	}

	newName := strings.Join(args, " ")

	req := &protocol.ChannelUpdateRequest{
		ServerID:  ch.app.currentServer.ID,
		ChannelID: ch.app.currentChannel.ID,
		Name:      &newName,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := ch.app.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	return fmt.Sprintf("Renaming channel to #%s...", newName), nil
}

func (ch *CommandHandler) handleMoveChannel(args []string) (string, error) {
	if len(args) < 1 {
		return "", errors.New("usage: /move-channel <category-name>")
	}

	if ch.app.activeConn == nil || ch.app.currentServer == nil {
		return "", errors.New("not connected to a server")
	}

	if ch.app.currentChannel == nil {
		return "", errors.New("no channel selected")
	}

	categoryName := strings.ToUpper(strings.Join(args, " "))

	// Find category by name
	var categoryID *uuid.UUID
	if ch.app.channelTree != nil {
		for _, node := range ch.app.channelTree.FlatList {
			if node.IsCategory && strings.ToUpper(node.Channel.Name) == categoryName {
				id := node.Channel.ID
				categoryID = &id
				break
			}
		}
	}

	if categoryID == nil && categoryName != "NONE" {
		return "", fmt.Errorf("category '%s' not found", categoryName)
	}

	req := &protocol.ChannelUpdateRequest{
		ServerID:   ch.app.currentServer.ID,
		ChannelID:  ch.app.currentChannel.ID,
		CategoryID: categoryID,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := ch.app.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	if categoryID == nil {
		return "Moving channel to top level...", nil
	}
	return fmt.Sprintf("Moving channel to category '%s'...", categoryName), nil
}

func (ch *CommandHandler) handleHelp(args []string) (string, error) {
	help := `Available Commands:
/create-channel <name>     - Create a new text channel
/create-category <name>    - Create a new category
/delete-channel            - Delete the current channel
/delete-category <name>    - Delete an empty category
/rename-channel <name>     - Rename the current channel
/move-channel <category>   - Move current channel to a category
/theme [name]              - Open theme browser, or apply theme directly
/mute                      - Mute current channel (suppress unread badges)
/mute @user                - Server-mute a member (requires permission)
/unmute @user              - Server-unmute a member
/unmute                    - Unmute current channel
/role assign|remove @user <role> - Manage member roles
/kick @user [reason]       - Kick a member from the server
/ban @user [reason]        - Ban a member from the server
/whisper @user <message>   - Send an ephemeral DM (alias: /w)
/help                      - Show this help message`

	return help, nil
}

// resolveMember finds a MemberDisplay by @username (strips leading @).
func (ch *CommandHandler) resolveMember(mention string) *MemberDisplay {
	a := ch.app
	name := strings.TrimPrefix(strings.ToLower(mention), "@")
	sc := a.activeConn
	if sc == nil {
		return nil
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	for _, m := range sc.Members {
		if m.User != nil && strings.ToLower(m.User.Username) == name {
			return m
		}
	}
	return nil
}

// sendModMsg sends a moderation opcode message over the active WebSocket connection.
func (ch *CommandHandler) sendModMsg(op protocol.OpCode, payload interface{}) error {
	a := ch.app
	if a.activeConn == nil || a.activeConn.Connection == nil {
		return fmt.Errorf("not connected")
	}
	msg, err := protocol.NewMessage(op, payload)
	if err != nil {
		return err
	}
	return a.activeConn.Connection.Send(msg)
}

// handleWhisper handles /whisper @user message or /w @user message
func (ch *CommandHandler) handleWhisper(args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("usage: /whisper @user <message>")
	}
	md := ch.resolveMember(args[0])
	if md == nil {
		return "", fmt.Errorf("user %s not found", args[0])
	}
	content := strings.Join(args[1:], " ")
	if content == "" {
		return "", fmt.Errorf("message cannot be empty")
	}
	a := ch.app
	if a.activeConn == nil || a.activeConn.Connection == nil {
		return "", fmt.Errorf("not connected")
	}
	msg, err := protocol.NewMessage(protocol.OpWhisper, &protocol.WhisperPayload{
		TargetUserID: md.User.ID,
		Content:      content,
	})
	if err != nil {
		return "", err
	}
	return "", a.activeConn.Connection.Send(msg)
}

// handleRole handles /role assign @user rolename  or  /role remove @user rolename
func (ch *CommandHandler) handleRole(args []string) (string, error) {
	if len(args) < 3 {
		return "", fmt.Errorf("usage: /role assign|remove @user <rolename>")
	}
	subCmd := strings.ToLower(args[0])
	md := ch.resolveMember(args[1])
	if md == nil {
		return "", fmt.Errorf("user %s not found", args[1])
	}
	roleName := strings.Join(args[2:], " ")
	serverID := ch.app.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}
	switch subCmd {
	case "assign":
		err := ch.sendModMsg(protocol.OpRoleAssign, &protocol.RoleAssignRequest{
			ServerID: serverID, UserID: md.User.ID, RoleName: roleName,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Assigning role %q to %s...", roleName, md.User.Username), nil
	case "remove":
		err := ch.sendModMsg(protocol.OpRoleRemove, &protocol.RoleRemoveRequest{
			ServerID: serverID, UserID: md.User.ID, RoleName: roleName,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Removing role %q from %s...", roleName, md.User.Username), nil
	default:
		return "", fmt.Errorf("unknown subcommand %q — use assign or remove", subCmd)
	}
}

// handleKickBan handles /kick @user [reason] and /ban @user [reason]
func (ch *CommandHandler) handleKickBan(args []string, ban bool) (string, error) {
	if len(args) < 1 {
		verb := "kick"
		if ban {
			verb = "ban"
		}
		return "", fmt.Errorf("usage: /%s @user [reason]", verb)
	}
	md := ch.resolveMember(args[0])
	if md == nil {
		return "", fmt.Errorf("user %s not found", args[0])
	}
	reason := strings.Join(args[1:], " ")
	serverID := ch.app.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}
	if ban {
		err := ch.sendModMsg(protocol.OpBanMember, &protocol.BanMemberRequest{
			ServerID: serverID, UserID: md.User.ID, Reason: reason,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Banned %s.", md.User.Username), nil
	}
	err := ch.sendModMsg(protocol.OpKickMember, &protocol.KickMemberRequest{
		ServerID: serverID, UserID: md.User.ID, Reason: reason,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Kicked %s.", md.User.Username), nil
}

// handleMuteMember handles /mute @user and /unmute @user (server-side mute)
func (ch *CommandHandler) handleMuteMember(args []string, mute bool) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /mute @user")
	}
	md := ch.resolveMember(args[0])
	if md == nil {
		return "", fmt.Errorf("user %s not found", args[0])
	}
	serverID := ch.app.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}
	err := ch.sendModMsg(protocol.OpMuteMember, &protocol.MuteMemberRequest{
		ServerID: serverID, UserID: md.User.ID, Mute: mute,
	})
	if err != nil {
		return "", err
	}
	action := "Muted"
	if !mute {
		action = "Unmuted"
	}
	return fmt.Sprintf("%s %s.", action, md.User.Username), nil
}

// handleMuteChannel mutes (unmute=false) or unmutes (unmute=true) the current channel.
func (ch *CommandHandler) handleMuteChannel(unmute bool) (string, error) {
	a := ch.app
	if a.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	chID := a.currentChannel.ID
	chName := a.currentChannel.Name
	if unmute {
		delete(a.mutedChannels, chID)
		a.saveMutedChannels()
		return fmt.Sprintf("Unmuted #%s", chName), nil
	}
	a.mutedChannels[chID] = true
	// Clear any existing unreads for this channel
	if a.currentClientServer != nil {
		serverID := a.currentClientServer.ID
		if a.unreadCounts[serverID] != nil {
			delete(a.unreadCounts[serverID], chID)
		}
		if a.mentionCounts[serverID] != nil {
			delete(a.mentionCounts[serverID], chID)
		}
	}
	a.saveMutedChannels()
	return fmt.Sprintf("Muted #%s", chName), nil
}

func (ch *CommandHandler) handleTheme(args []string) (string, error) {
	if len(args) == 0 {
		// Open the interactive theme browser
		ch.app.openThemeBrowser(ViewMain)
		return "", nil
	}

	// Direct apply: /theme nord
	name := strings.ToLower(strings.Join(args, "-"))
	ch.app.applyAndSaveTheme(name)
	displayName := themes.GetThemeDisplayName(name)
	return fmt.Sprintf("Theme set to %q", displayName), nil
}
