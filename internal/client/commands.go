package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/themes"
	"github.com/google/uuid"
	"github.com/sqweek/dialog"
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
	parts := splitArgs(input[1:])
	if len(parts) == 0 {
		return nil, errors.New("empty command")
	}

	return &Command{
		Name: strings.ToLower(parts[0]),
		Args: parts[1:],
	}, nil
}

// splitArgs tokenizes a command's argument string the way a shell would:
// whitespace-separated, except a double-quoted span is kept as one token
// with the quotes stripped. Without this, a path a user defensively quotes
// (very natural, especially pasted via Windows Explorer's "Copy as path")
// or one that genuinely contains spaces comes through as a literal
// quote-wrapped string and fails to resolve as a real filesystem path.
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuotes := false
	hasToken := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			hasToken = true
		case unicode.IsSpace(r) && !inQuotes:
			if hasToken {
				args = append(args, cur.String())
				cur.Reset()
				hasToken = false
			}
		default:
			cur.WriteRune(r)
			hasToken = true
		}
	}
	if hasToken {
		args = append(args, cur.String())
	}
	return args
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
	case "create-group":
		return ch.handleCreateCategory(cmd.Args)
	case "delete-channel":
		return ch.handleDeleteChannel(cmd.Args)
	case "delete-group":
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
		// /mute with no args = mute current channel; /mute @user [minutes] = server-mute
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
	case "roles":
		return ch.handleRoles(cmd.Args)
	case "create-role":
		return ch.handleCreateRole(cmd.Args)
	case "kick":
		return ch.handleKickBan(cmd.Args, false)
	case "ban":
		return ch.handleKickBan(cmd.Args, true)
	case "timeout":
		return ch.handleTimeout(cmd.Args)
	case "unban":
		return ch.handleUnban(cmd.Args)
	case "pin":
		return ch.handlePin(cmd.Args)
	case "unpin":
		return ch.handleUnpin(cmd.Args)
	case "whisper", "w":
		return ch.handleWhisper(cmd.Args)
	case "links":
		return ch.handleLinks(cmd.Args)
	case "status":
		return ch.handleStatus(cmd.Args)
	case "title":
		return ch.handleTitle(cmd.Args)
	case "nick":
		return ch.handleNickname(cmd.Args)
	case "lock":
		return ch.handleLockChannel(true)
	case "unlock":
		return ch.handleLockChannel(false)
	case "join-voice":
		return ch.handleJoinVoice(cmd.Args)
	case "leave-voice":
		return ch.handleLeaveVoice(cmd.Args)
	case "mute-voice":
		return ch.handleVoiceServerMute(cmd.Args, true, false)
	case "deafen-voice":
		return ch.handleVoiceServerMute(cmd.Args, true, true)
	case "unmute-voice":
		return ch.handleVoiceServerMute(cmd.Args, false, false)
	case "move-voice":
		return ch.handleMoveVoice(cmd.Args)
	case "attach":
		return ch.handleAttach(cmd.Args)
	case "download":
		return ch.handleDownload(cmd.Args)
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
		return "", errors.New("usage: /create-group <name>")
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

	return fmt.Sprintf("Creating channel group '%s'...", strings.ToUpper(name)), nil
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
		return "", errors.New("usage: /delete-group <name>")
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
		return "", fmt.Errorf("channel group '%s' not found", categoryName)
	}

	// Check if category has any channels in it
	if len(foundCategory.Children) > 0 {
		return "", fmt.Errorf("cannot delete channel group '%s': it contains %d channel(s). Please move or delete them first.", categoryName, len(foundCategory.Children))
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

	return fmt.Sprintf("Deleting channel group '%s'...", categoryName), nil
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
		return "", errors.New("usage: /move-channel <group-name>")
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
		return "", fmt.Errorf("channel group '%s' not found", categoryName)
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
	return fmt.Sprintf("Moving channel to group '%s'...", categoryName), nil
}

func (ch *CommandHandler) handleLockChannel(lock bool) (string, error) {
	if ch.app.activeConn == nil || ch.app.currentServer == nil {
		return "", errors.New("not connected to a server")
	}

	if ch.app.currentChannel == nil {
		return "", errors.New("no channel selected")
	}

	// Only allow locking text channels (not categories, voice, or DMs)
	if ch.app.currentChannel.Type != models.ChannelTypeText {
		return "", errors.New("can only lock/unlock text channels")
	}

	isLocked := lock
	req := &protocol.ChannelUpdateRequest{
		ServerID:  ch.app.currentServer.ID,
		ChannelID: ch.app.currentChannel.ID,
		IsLocked:  &isLocked,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := ch.app.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	if lock {
		return "Channel locked. Only users with Manage Messages permission can post.", nil
	}
	return "Channel unlocked. All users can post.", nil
}

func (ch *CommandHandler) handleHelp(args []string) (string, error) {
	level := ch.app.currentUserRoleLevel()

	// All users can whisper and change theme
	lines := []string{
		"Available Commands:",
		"/whisper @user <msg>       - Send an ephemeral DM (alias: /w)",
		"/links [N]                 - Show links from recent N messages (default: 20)",
		"/theme [name]              - Open theme browser, or apply theme directly",
		"/status <message>          - Set your status (use /status clear to remove)",
		"/nick <nickname>           - Set your own nickname on this server (use clear to remove)",
		"/attach <path> [caption]   - Share a local file peer-to-peer (you must stay online for others to download it)",
		"/download <attachment-id>  - Download a file someone else attached",
		"/mute                      - Mute current channel (suppress unread badges)",
		"/unmute                    - Unmute current channel",
		"/join-voice [#channel]     - Join a voice channel",
		"/leave-voice               - Leave the current voice channel",
	}

	if level >= roleLevelMod {
		lines = append(lines,
			"/create-channel <name>     - Create a new text channel",
			"/create-group <name>       - Create a new channel group",
			"/delete-channel            - Delete the current channel",
			"/delete-group <name>       - Delete an empty channel group",
			"/rename-channel <name>     - Rename the current channel",
			"/move-channel <group>      - Move current channel to a channel group",
			"/lock                      - Lock current channel (only mods/admins can post)",
			"/unlock                    - Unlock current channel (all users can post)",
			"/mute @user [minutes]      - Server-mute a member",
			"/unmute @user              - Server-unmute a member",
			"/mute-voice @user          - Server-mute a user in voice",
			"/deafen-voice @user        - Server-deafen a user in voice",
			"/unmute-voice @user        - Lift voice mute/deafen",
			"/kick @user [reason]       - Kick a member from the server",
			"/timeout @user <minutes>   - Temporarily ban a member",
			"/pin [N]                   - Pin the Nth most recent message (default: 1)",
			"/unpin [N]                 - Unpin the Nth pinned message (default: 1)",
		)
	}

	if level >= roleLevelAdmin {
		lines = append(lines,
			"/roles                     - List all available roles on this server",
			"/role assign|remove @user <role> - Manage member roles",
			"/create-role <name> [preset] - Create a new role (presets: text, moderator, admin)",
			"/title @user <title>       - Assign a custom title to a member (use clear to remove)",
			"/ban @user [reason]        - Permanently ban a member",
			"/unban @user               - Lift a ban from a member",
			"/move-voice @user <channel> - Force-move user to a voice channel",
		)
	}

	return strings.Join(lines, "\n"), nil
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
	if a.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	msg, err := protocol.NewMessage(protocol.OpWhisper, &protocol.WhisperPayload{
		TargetUserID: md.User.ID,
		ChannelID:    a.currentChannel.ID,
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
		// Check for --force flag
		force := false
		if strings.HasSuffix(roleName, " --force") {
			force = true
			roleName = strings.TrimSuffix(roleName, " --force")
		}
		err := ch.sendModMsg(protocol.OpRoleRemove, &protocol.RoleRemoveRequest{
			ServerID: serverID, UserID: md.User.ID, RoleName: roleName, Force: force,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Removing role %q from %s...", roleName, md.User.Username), nil
	default:
		return "", fmt.Errorf("unknown subcommand %q — use assign or remove", subCmd)
	}
}

// handleRoles lists all available roles on the current server
func (ch *CommandHandler) handleRoles(args []string) (string, error) {
	a := ch.app
	if a.activeConn == nil {
		return "", fmt.Errorf("not connected to a server")
	}

	serverID := a.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}

	roles, ok := a.activeConn.Roles[serverID]
	if !ok || len(roles) == 0 {
		return "No roles available on this server.", nil
	}

	var result strings.Builder
	result.WriteString("Available roles:\n")

	// Sort roles by position (highest first)
	sortedRoles := make([]*models.Role, len(roles))
	copy(sortedRoles, roles)
	// Sort by position descending
	for i := 0; i < len(sortedRoles)-1; i++ {
		for j := i + 1; j < len(sortedRoles); j++ {
			if sortedRoles[j].Position > sortedRoles[i].Position {
				sortedRoles[i], sortedRoles[j] = sortedRoles[j], sortedRoles[i]
			}
		}
	}

	for _, role := range sortedRoles {
		// Skip @everyone role
		if role.Name == "@everyone" {
			continue
		}
		result.WriteString(fmt.Sprintf("  • %s (position: %d)\n", role.Name, role.Position))
	}

	return result.String(), nil
}

// handleCreateRole handles /create-role <name> [preset]
// Presets: text (default), moderator, admin
func (ch *CommandHandler) handleCreateRole(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /create-role <name> [preset]\nPresets: text (default), moderator, admin")
	}

	a := ch.app
	if a.activeConn == nil {
		return "", fmt.Errorf("not connected to a server")
	}

	serverID := a.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}

	if a.currentChannel == nil {
		return "", fmt.Errorf("no active channel")
	}

	// Parse role name (first arg)
	roleName := args[0]
	// Remove quotes if present
	roleName = strings.Trim(roleName, "\"'")

	// Validate role name
	if len(roleName) < 1 || len(roleName) > 32 {
		return "", fmt.Errorf("role name must be 1-32 characters")
	}
	if strings.EqualFold(roleName, "@everyone") {
		return "", fmt.Errorf("cannot create role named '@everyone' (reserved)")
	}

	// Parse preset (optional second arg, default: text)
	preset := "text"
	if len(args) > 1 {
		preset = strings.ToLower(args[1])
	}

	// Map preset to permission bitfield
	var permissions uint64
	switch preset {
	case "text":
		permissions = uint64(models.PermissionsText)
	case "moderator", "mod":
		permissions = uint64(models.PermissionsModerator)
	case "admin", "administrator":
		permissions = uint64(models.PermissionsAdmin)
	default:
		return "", fmt.Errorf("unknown preset %q (valid: text, moderator, admin)", preset)
	}

	// Default color (purple)
	color := 0xBD93F9

	// Create request
	req := &protocol.CreateRoleRequest{
		ServerID:      serverID,
		ChannelID:     a.currentChannel.ID,
		Name:          roleName,
		Permissions:   permissions,
		Color:         color,
		IsHoisted:     false,
		IsMentionable: true,
	}

	// Send to server
	if err := ch.sendModMsg(protocol.OpCreateRole, req); err != nil {
		return "", fmt.Errorf("failed to create role: %w", err)
	}

	return fmt.Sprintf("Creating role %q with %s permissions...", roleName, preset), nil
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
	if ch.app.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	channelID := ch.app.currentChannel.ID
	if ban {
		err := ch.sendModMsg(protocol.OpBanMember, &protocol.BanMemberRequest{
			ServerID: serverID, ChannelID: channelID, UserID: md.User.ID, Reason: reason,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Banned %s.", md.User.Username), nil
	}
	err := ch.sendModMsg(protocol.OpKickMember, &protocol.KickMemberRequest{
		ServerID: serverID, ChannelID: channelID, UserID: md.User.ID, Reason: reason,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Kicked %s.", md.User.Username), nil
}

// handleTimeout handles /timeout @user <minutes> [reason] — temporarily bans a user
func (ch *CommandHandler) handleTimeout(args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("usage: /timeout @user <minutes> [reason]")
	}
	md := ch.resolveMember(args[0])
	if md == nil {
		return "", fmt.Errorf("user %s not found", args[0])
	}

	duration, err := strconv.Atoi(args[1])
	if err != nil || duration <= 0 {
		return "", fmt.Errorf("invalid duration %q — must be a positive integer (minutes)", args[1])
	}

	reason := ""
	if len(args) > 2 {
		reason = strings.Join(args[2:], " ")
	}

	serverID := ch.app.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}

	if ch.app.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	channelID := ch.app.currentChannel.ID

	err = ch.sendModMsg(protocol.OpTimeoutMember, &protocol.TimeoutMemberRequest{
		ServerID:  serverID,
		ChannelID: channelID,
		UserID:    md.User.ID,
		Duration:  duration,
		Reason:    reason,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Timed out %s for %d minutes.", md.User.Username, duration), nil
}

// handleUnban handles /unban username — lifts a ban by username
func (ch *CommandHandler) handleUnban(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /unban <username>")
	}
	username := strings.TrimPrefix(args[0], "@")
	if username == "" {
		return "", fmt.Errorf("usage: /unban <username>")
	}
	serverID := ch.app.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}
	if ch.app.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	channelID := ch.app.currentChannel.ID

	err := ch.sendModMsg(protocol.OpUnbanMember, &protocol.UnbanMemberRequest{
		ServerID:  serverID,
		ChannelID: channelID,
		Username:  username,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Unbanned %s.", username), nil
}

// handleMuteMember handles /mute @user [minutes] and /unmute @user (server-side mute)
func (ch *CommandHandler) handleMuteMember(args []string, mute bool) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /mute @user [minutes]")
	}
	md := ch.resolveMember(args[0])
	if md == nil {
		return "", fmt.Errorf("user %s not found", args[0])
	}
	serverID := ch.app.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}

	if ch.app.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	channelID := ch.app.currentChannel.ID

	// Parse duration if provided (for mute only)
	durationMinutes := 0
	if mute && len(args) >= 2 {
		n, err := strconv.Atoi(args[1])
		if err != nil || n <= 0 {
			return "", fmt.Errorf("invalid duration %q — must be a positive integer (minutes)", args[1])
		}
		durationMinutes = n
	}

	err := ch.sendModMsg(protocol.OpMuteMember, &protocol.MuteMemberRequest{
		ServerID:  serverID,
		ChannelID: channelID,
		UserID:    md.User.ID,
		Mute:      mute,
		Duration:  durationMinutes,
	})
	if err != nil {
		return "", err
	}

	if !mute {
		return fmt.Sprintf("Unmuted %s.", md.User.Username), nil
	}

	if durationMinutes > 0 {
		return fmt.Sprintf("Muted %s for %d minutes.", md.User.Username, durationMinutes), nil
	}
	return fmt.Sprintf("Muted %s.", md.User.Username), nil
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

// TODO: Implement when protocol support is added
// handleTimeout handles /timeout @user <minutes> [reason]
// func (ch *CommandHandler) handleTimeout(args []string) (string, error) {
// 	if len(args) < 2 {
// 		return "", fmt.Errorf("usage: /timeout @user <minutes> [reason]")
// 	}
// 	md := ch.resolveMember(args[0])
// 	if md == nil {
// 		return "", fmt.Errorf("user %s not found", args[0])
// 	}
// 	minutes, err := strconv.Atoi(args[1])
// 	if err != nil || minutes <= 0 {
// 		return "", fmt.Errorf("invalid duration %q — must be a positive integer (minutes)", args[1])
// 	}
// 	reason := strings.Join(args[2:], " ")
// 	serverID := ch.app.getActiveServerID()
// 	if serverID == uuid.Nil {
// 		return "", fmt.Errorf("not connected to a server")
// 	}
// 	if err := ch.sendModMsg(protocol.OpTimeoutMember, &protocol.TimeoutMemberRequest{
// 		ServerID:        serverID,
// 		UserID:          md.User.ID,
// 		DurationMinutes: minutes,
// 		Reason:          reason,
// 	}); err != nil {
// 		return "", err
// 	}
// 	return fmt.Sprintf("Timed out %s for %d minutes.", md.User.Username, minutes), nil
// }

// handlePin handles /pin [N] — pins the Nth most recent message (default 1).
func (ch *CommandHandler) handlePin(args []string) (string, error) {
	a := ch.app
	if a.activeConn == nil || a.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	n := 1
	if len(args) >= 1 {
		parsed, err := strconv.Atoi(args[0])
		if err != nil || parsed <= 0 {
			return "", fmt.Errorf("invalid index %q — must be a positive integer", args[0])
		}
		n = parsed
	}
	a.activeConn.mu.RLock()
	msgs := a.activeConn.Messages[a.currentChannel.ID]
	a.activeConn.mu.RUnlock()
	if len(msgs) == 0 {
		return "", fmt.Errorf("no messages in this channel")
	}
	if n > len(msgs) {
		return "", fmt.Errorf("only %d messages available", len(msgs))
	}
	target := msgs[len(msgs)-n]
	if err := ch.sendModMsg(protocol.OpPinMessage, &protocol.PinMessageRequest{
		ChannelID: a.currentChannel.ID,
		MessageID: target.Message.ID,
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Pinning message by %s...", target.AuthorName), nil
}

// handleUnpin handles /unpin [N] — unpins the Nth pinned message (default 1).
func (ch *CommandHandler) handleUnpin(args []string) (string, error) {
	a := ch.app
	if a.activeConn == nil || a.currentChannel == nil {
		return "", fmt.Errorf("no channel selected")
	}
	n := 1
	if len(args) >= 1 {
		parsed, err := strconv.Atoi(args[0])
		if err != nil || parsed <= 0 {
			return "", fmt.Errorf("invalid index %q — must be a positive integer", args[0])
		}
		n = parsed
	}
	a.activeConn.mu.RLock()
	pinned := a.activeConn.PinnedMessages[a.currentChannel.ID]
	a.activeConn.mu.RUnlock()
	if len(pinned) == 0 {
		return "", fmt.Errorf("no pinned messages in this channel")
	}
	if n > len(pinned) {
		return "", fmt.Errorf("only %d pinned messages", len(pinned))
	}
	target := pinned[n-1]
	if err := ch.sendModMsg(protocol.OpUnpinMessage, &protocol.UnpinMessageRequest{
		ChannelID: a.currentChannel.ID,
		MessageID: target.ID,
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Unpinning message #%d...", n), nil
}

func (ch *CommandHandler) handleTheme(args []string) (string, error) {
	if len(args) == 0 {
		// Open the interactive theme browser
		ch.app.openThemeBrowser(ViewMain)
		return "", nil
	}

	// Direct apply: /theme nord
	name := strings.ToLower(strings.Join(args, "-"))
	t, err := themes.GetTheme(name)
	if err != nil {
		return "", fmt.Errorf("theme %q not found — use /theme to browse available themes", name)
	}
	ch.app.applyAndSaveTheme(name)
	displayName := t.Meta.Name
	if displayName == "" {
		displayName = name
	}
	return fmt.Sprintf("Theme set to %q", displayName), nil
}

// handleLinks handles /links [N] — shows links from recent N messages (default 20)
func (ch *CommandHandler) handleLinks(args []string) (string, error) {
	a := ch.app
	if a.activeConn == nil || a.currentChannel == nil {
		return "", fmt.Errorf("not connected to a channel")
	}

	messages := a.activeConn.GetMessages(a.currentChannel.ID)

	// Collect all links from recent N messages (default: last 20)
	limit := 20
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			limit = n
		}
	}

	var allLinks []string
	startIdx := len(messages) - limit
	if startIdx < 0 {
		startIdx = 0
	}

	for i := startIdx; i < len(messages); i++ {
		links := a.extractLinksFromMessage(messages[i])
		allLinks = append(allLinks, links...)
	}

	if len(allLinks) == 0 {
		return "No links found in recent messages", nil
	}

	a.openLinkBrowser(allLinks, nil, "main")
	return "", nil
}

// handleStatus handles /status <message> or /status clear
func (ch *CommandHandler) handleStatus(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: /status <message> or /status clear")
	}

	// Check for "clear" subcommand
	if args[0] == "clear" {
		return ch.setStatus("")
	}

	// Set status message
	statusText := strings.Join(args, " ")
	if len(statusText) > 100 {
		return "", fmt.Errorf("status message too long (max 100 characters)")
	}

	return ch.setStatus(statusText)
}

// setStatus updates the user's status text
func (ch *CommandHandler) setStatus(statusText string) (string, error) {
	a := ch.app
	if a.activeConn == nil {
		return "", fmt.Errorf("not connected to server")
	}

	if a.activeConn.User == nil {
		return "", fmt.Errorf("user not authenticated")
	}

	// Send presence update
	payload := &protocol.PresenceUpdatePayload{
		Status:     a.activeConn.User.Status, // Keep current status
		StatusText: statusText,
	}

	msg, err := protocol.NewMessage(protocol.OpPresenceUpdate, payload)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := a.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send status update: %w", err)
	}

	// Update local state optimistically
	a.activeConn.User.StatusText = statusText

	if statusText == "" {
		return "Status cleared", nil
	}
	return fmt.Sprintf("Status set to: %s", statusText), nil
}

// handleNickname handles /nick <nickname> or /nick clear — always sets the
// caller's own nickname (renaming someone else has no client command; the
// server would reject it anyway since OpSetNickname requires
// PermissionManageNicknames for any UserID other than the caller's own).
// handleAttach shares a local file peer-to-peer: /attach <path> [caption].
// The server only ever stores a small manifest (name, size, hash) -- the
// bytes themselves transfer directly to whoever downloads it, later, over
// WebRTC. This client must stay online for the file to remain downloadable.
func (ch *CommandHandler) handleAttach(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /attach <local-path> [caption]")
	}

	a := ch.app
	if a.activeConn == nil || a.activeConn.User == nil {
		return "", errors.New("not connected to a server")
	}
	if a.currentChannel == nil {
		return "", errors.New("no channel selected")
	}

	path := args[0]
	caption := strings.Join(args[1:], " ")

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a file", path)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	attachmentID := uuid.New()
	attachment := models.Attachment{
		ID:          attachmentID,
		Filename:    filepath.Base(absPath),
		Size:        info.Size(),
		ContentHash: contentHash,
		SenderID:    a.activeConn.User.ID,
	}

	payload := &protocol.SendMessagePayload{
		ChannelID:   a.currentChannel.ID,
		Content:     caption,
		Nonce:       uuid.New().String(),
		Attachments: []models.Attachment{attachment},
	}
	msg, err := protocol.NewMessage(protocol.OpSendMessage, payload)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}
	if err := a.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	// Remember where the source file lives so this client can keep serving
	// it to downloaders, including across a restart.
	if a.configMgr != nil {
		if err := a.configMgr.RecordSharedFile(attachmentID, absPath, attachment.Filename); err != nil {
			log.Printf("filetransfer: failed to record shared file: %v", err)
		}
	}
	if engine, _ := a.ensureFileTransferEngine(); engine != nil {
		engine.RegisterSharedFile(attachmentID, absPath)
	}

	return fmt.Sprintf("Shared %s (%d bytes)", attachment.Filename, attachment.Size), nil
}

// handleDownload starts a peer-to-peer download of an attachment already
// visible in the current channel: /download <attachment-id>.
func (ch *CommandHandler) handleDownload(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /download <attachment-id>")
	}

	a := ch.app
	if a.activeConn == nil {
		return "", errors.New("not connected to a server")
	}
	if a.currentChannel == nil {
		return "", errors.New("no channel selected")
	}

	attachmentID, err := uuid.Parse(args[0])
	if err != nil {
		return "", fmt.Errorf("invalid attachment ID: %w", err)
	}

	var found *models.Attachment
	a.activeConn.mu.RLock()
	for _, md := range a.activeConn.Messages[a.currentChannel.ID] {
		for i := range md.Attachments {
			if md.Attachments[i].ID == attachmentID {
				att := md.Attachments[i]
				found = &att
			}
		}
	}
	a.activeConn.mu.RUnlock()
	if found == nil {
		return "", fmt.Errorf("no attachment with that ID in this channel")
	}

	engine, _ := a.ensureFileTransferEngine()
	if engine == nil {
		return "", errors.New("not connected to a server")
	}

	// Ask where to save via the OS's native Save As dialog rather than
	// silently dropping it into ~/Downloads -- this blocks the TUI while
	// open, same as any other modal file picker.
	destPath, dlgErr := dialog.File().SetStartFile(found.Filename).Title("Save " + found.Filename + " as").Save()
	switch {
	case dlgErr == nil:
		// proceed with the chosen path
	case errors.Is(dlgErr, dialog.ErrCancelled):
		return "Download cancelled", nil
	default:
		// Save dialog unavailable in this environment (e.g. no display) --
		// fall back to the default Downloads-folder behavior rather than
		// failing the command outright.
		log.Printf("filetransfer: save dialog unavailable, defaulting to downloads folder: %v", dlgErr)
		destPath = ""
	}

	engine.RequestDownload(found.ID, found.SenderID, found.Filename, found.Size, found.ContentHash, destPath)

	return fmt.Sprintf("Requesting %s from the sender...", found.Filename), nil
}

func (ch *CommandHandler) handleNickname(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("usage: /nick <nickname> or /nick clear")
	}

	a := ch.app
	if a.activeConn == nil {
		return "", fmt.Errorf("not connected to server")
	}

	serverID := a.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}

	nickname := strings.Join(args, " ")
	if strings.ToLower(nickname) == "clear" {
		nickname = ""
	}
	if len(nickname) > 32 {
		return "", fmt.Errorf("nickname too long (max 32 characters)")
	}

	req := &protocol.SetNicknameRequest{
		ServerID: serverID,
		Nickname: nickname,
	}

	msg, err := protocol.NewMessage(protocol.OpSetNickname, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := a.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	if nickname == "" {
		return "Cleared your nickname", nil
	}
	return fmt.Sprintf("Set your nickname: %s", nickname), nil
}

// handleTitle handles /title @username <title> or /title @username clear
func (ch *CommandHandler) handleTitle(args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("usage: /title @username <title> or /title @username clear")
	}

	a := ch.app
	if a.activeConn == nil {
		return "", fmt.Errorf("not connected to server")
	}

	serverID := a.getActiveServerID()
	if serverID == uuid.Nil {
		return "", fmt.Errorf("not connected to a server")
	}

	// Parse username (remove @ prefix if present)
	username := strings.TrimPrefix(args[0], "@")

	// Find user by alias in current server's members
	a.activeConn.mu.RLock()
	members := a.activeConn.Members
	a.activeConn.mu.RUnlock()

	var targetUserID uuid.UUID
	var targetUsername string
	found := false
	for _, member := range members {
		if strings.EqualFold(member.User.Username, username) {
			targetUserID = member.User.ID
			targetUsername = member.User.Username
			found = true
			break
		}
	}

	if !found {
		return "", fmt.Errorf("user '%s' not found", username)
	}

	// Get title (everything after username)
	title := strings.Join(args[1:], " ")
	if strings.ToLower(title) == "clear" {
		title = ""
	}

	if len(title) > 50 {
		return "", fmt.Errorf("title too long (max 50 characters)")
	}

	// Send request
	req := &protocol.AssignTitleRequest{
		ServerID: serverID,
		UserID:   targetUserID,
		Title:    title,
	}

	msg, err := protocol.NewMessage(protocol.OpAssignTitle, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if err := a.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	if title == "" {
		return fmt.Sprintf("Cleared title for @%s", targetUsername), nil
	}
	return fmt.Sprintf("Set title for @%s: %s", targetUsername, title), nil
}

// handleJoinVoice handles /join-voice [#channel-name]
// With no argument it joins the currently selected voice channel (if any).
func (ch *CommandHandler) handleJoinVoice(args []string) (string, error) {
	a := ch.app
	if a.activeConn == nil || a.currentServer == nil || a.currentClientServer == nil {
		return "", errors.New("not connected to a server")
	}

	// Resolve target channel
	var target *models.Channel
	if len(args) > 0 {
		name := strings.TrimPrefix(args[0], "#")
		channels := a.activeConn.GetChannels(a.currentServer.ID)
		for _, ch := range channels {
			if ch.Type == models.ChannelTypeVoice && strings.EqualFold(ch.Name, name) {
				target = ch
				break
			}
		}
		if target == nil {
			return "", fmt.Errorf("voice channel #%s not found", name)
		}
	} else if a.currentChannel != nil && a.currentChannel.Type == models.ChannelTypeVoice {
		target = a.currentChannel
	} else {
		return "", errors.New("usage: /join-voice [#channel-name] (or select a voice channel first)")
	}

	channelID := target.ID
	payload := &protocol.VoiceStateUpdatePayload{
		ServerID:  a.currentServer.ID,
		ChannelID: &channelID,
	}
	if err := a.connMgr.SendVoiceStateUpdate(a.currentClientServer.ID, payload); err != nil {
		return "", fmt.Errorf("failed to join voice: %w", err)
	}
	return fmt.Sprintf("Joining voice channel #%s…", target.Name), nil
}

// handleLeaveVoice handles /leave-voice — leaves the current voice channel.
func (ch *CommandHandler) handleLeaveVoice(_ []string) (string, error) {
	a := ch.app
	if a.activeConn == nil || a.currentServer == nil || a.currentClientServer == nil {
		return "", errors.New("not connected to a server")
	}

	a.activeConn.mu.RLock()
	var inVoice bool
	var serverID uuid.UUID
	if a.activeConn.User != nil {
		if vs, ok := a.activeConn.VoiceStates[a.activeConn.User.ID]; ok {
			inVoice = true
			serverID = vs.ServerID
		}
	}
	a.activeConn.mu.RUnlock()

	if !inVoice {
		return "", errors.New("you are not in a voice channel")
	}

	payload := &protocol.VoiceStateUpdatePayload{
		ServerID:  serverID,
		ChannelID: nil, // nil = leave
	}
	if err := a.connMgr.SendVoiceStateUpdate(a.currentClientServer.ID, payload); err != nil {
		return "", fmt.Errorf("failed to leave voice: %w", err)
	}
	return "Left voice channel.", nil
}

// handleVoiceServerMute handles /mute-voice, /deafen-voice, /unmute-voice @user
// muted=true, deafened=true → server-deafen; muted=true, deafened=false → server-mute; muted=false → clear both.
func (ch *CommandHandler) handleVoiceServerMute(args []string, muted, deafened bool) (string, error) {
	a := ch.app
	if a.activeConn == nil || a.currentServer == nil || a.currentClientServer == nil {
		return "", errors.New("not connected to a server")
	}

	if len(args) < 1 {
		action := "mute-voice"
		if deafened {
			action = "deafen-voice"
		} else if !muted {
			action = "unmute-voice"
		}
		return "", fmt.Errorf("usage: /%s @user", action)
	}

	targetUsername := strings.TrimPrefix(args[0], "@")
	var targetID uuid.UUID
	found := false

	a.activeConn.mu.RLock()
	for _, m := range a.activeConn.Members {
		if strings.EqualFold(m.User.GetDisplayName(), targetUsername) ||
			strings.EqualFold(m.User.Username, targetUsername) {
			targetID = m.User.ID
			found = true
			break
		}
	}
	a.activeConn.mu.RUnlock()

	if !found {
		return "", fmt.Errorf("user @%s not found", targetUsername)
	}

	req := &protocol.VoiceServerMutePayload{
		ServerID: a.currentServer.ID,
		UserID:   targetID,
		Muted:    muted && !deafened,
		Deafened: deafened,
	}
	msg, err := protocol.NewMessage(protocol.OpVoiceServerMute, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}
	if err := a.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	action := "Server-muted"
	if deafened {
		action = "Server-deafened"
	} else if !muted {
		action = "Unmuted"
	}
	return fmt.Sprintf("%s @%s", action, targetUsername), nil
}

// handleMoveVoice handles /move-voice @user #channel — admin force-moves a user.
func (ch *CommandHandler) handleMoveVoice(args []string) (string, error) {
	a := ch.app
	if a.activeConn == nil || a.currentServer == nil || a.currentClientServer == nil {
		return "", errors.New("not connected to a server")
	}
	if len(args) < 2 {
		return "", errors.New("usage: /move-voice @user #channel")
	}

	targetUsername := strings.TrimPrefix(args[0], "@")
	channelName := strings.TrimPrefix(args[1], "#")

	// Resolve user.
	var targetID uuid.UUID
	found := false
	a.activeConn.mu.RLock()
	for _, m := range a.activeConn.Members {
		if strings.EqualFold(m.User.GetDisplayName(), targetUsername) ||
			strings.EqualFold(m.User.Username, targetUsername) {
			targetID = m.User.ID
			found = true
			break
		}
	}
	a.activeConn.mu.RUnlock()
	if !found {
		return "", fmt.Errorf("user @%s not found", targetUsername)
	}

	// Resolve destination voice channel.
	var destChannelID uuid.UUID
	foundCh := false
	channels := a.activeConn.GetChannels(a.currentServer.ID)
	for _, ch := range channels {
		if ch.Type == models.ChannelTypeVoice && strings.EqualFold(ch.Name, channelName) {
			destChannelID = ch.ID
			foundCh = true
			break
		}
	}
	if !foundCh {
		return "", fmt.Errorf("voice channel #%s not found", channelName)
	}

	req := &protocol.MoveVoicePayload{
		ServerID:  a.currentServer.ID,
		UserID:    targetID,
		ChannelID: destChannelID,
	}
	msg, err := protocol.NewMessage(protocol.OpMoveVoice, req)
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}
	if err := a.activeConn.Connection.Send(msg); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	return fmt.Sprintf("Moved @%s to #%s.", targetUsername, channelName), nil
}
