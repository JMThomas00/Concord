package models

import (
	"time"

	"github.com/google/uuid"
)

// Permission represents individual permissions as bit flags
type Permission int64

const (
	// General permissions
	PermissionViewChannels        Permission = 1 << 0
	PermissionManageChannels      Permission = 1 << 1
	PermissionManageRoles         Permission = 1 << 2
	PermissionManageServer        Permission = 1 << 3
	PermissionCreateInvite        Permission = 1 << 4
	PermissionKickMembers         Permission = 1 << 5
	PermissionBanMembers          Permission = 1 << 6
	PermissionChangeNickname      Permission = 1 << 7
	PermissionManageNicknames     Permission = 1 << 8
	
	// Text channel permissions
	PermissionSendMessages        Permission = 1 << 10
	PermissionSendMessagesThreads Permission = 1 << 11
	PermissionCreateThreads       Permission = 1 << 12
	PermissionEmbedLinks          Permission = 1 << 13
	PermissionAttachFiles         Permission = 1 << 14
	PermissionAddReactions        Permission = 1 << 15
	PermissionUseExternalEmoji    Permission = 1 << 16
	PermissionMentionEveryone     Permission = 1 << 17
	PermissionManageMessages      Permission = 1 << 18
	PermissionReadMessageHistory  Permission = 1 << 19
	PermissionPinMessages         Permission = 1 << 20
	
	// Voice channel permissions (v2)
	PermissionConnect             Permission = 1 << 30
	PermissionSpeak               Permission = 1 << 31
	PermissionMuteMembers         Permission = 1 << 32
	PermissionDeafenMembers       Permission = 1 << 33
	PermissionMoveMembers         Permission = 1 << 34
	PermissionUseVAD              Permission = 1 << 35 // Voice Activity Detection
	
	// Administrator has all permissions
	PermissionAdministrator       Permission = 1 << 63
)

// Common permission combinations
const (
	PermissionsText = PermissionViewChannels |
		PermissionSendMessages |
		PermissionReadMessageHistory |
		PermissionAddReactions |
		PermissionEmbedLinks |
		PermissionAttachFiles

	PermissionsVoice = PermissionConnect |
		PermissionSpeak |
		PermissionUseVAD

	PermissionsModerator = PermissionsText |
		PermissionManageMessages |
		PermissionKickMembers |
		PermissionMuteMembers |
		PermissionMoveMembers |
		PermissionManageNicknames |
		PermissionPinMessages

	PermissionsAdmin = PermissionAdministrator
)

// Role represents a server role with permissions
type Role struct {
	ID          uuid.UUID  `json:"id"`
	ServerID    uuid.UUID  `json:"server_id"`
	Name        string     `json:"name"`
	Color       int        `json:"color"`       // RGB color as integer
	Permissions Permission `json:"permissions"`
	Position    int        `json:"position"`    // Higher = more important
	IsHoisted   bool       `json:"is_hoisted"`  // Show separately in member list
	IsMentionable bool     `json:"is_mentionable"`
	IsDefault   bool       `json:"is_default"`  // @everyone role
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// NewRole creates a new role with default permissions
func NewRole(serverID uuid.UUID, name string) *Role {
	now := time.Now()
	return &Role{
		ID:          uuid.New(),
		ServerID:    serverID,
		Name:        name,
		Color:       0,
		Permissions: PermissionsText, // Default text permissions
		Position:    0,
		IsHoisted:   false,
		IsMentionable: true,
		IsDefault:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// NewEveryoneRole creates the default @everyone role for a server
func NewEveryoneRole(serverID uuid.UUID) *Role {
	now := time.Now()
	return &Role{
		ID:          uuid.New(),
		ServerID:    serverID,
		Name:        "everyone",
		Color:       0,
		Permissions: PermissionViewChannels | PermissionSendMessages | PermissionReadMessageHistory | PermissionAddReactions,
		Position:    0,
		IsHoisted:   false,
		IsMentionable: false,
		IsDefault:   true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// HasPermission checks if the role has a specific permission
func (r *Role) HasPermission(perm Permission) bool {
	// Administrator has all permissions
	if r.Permissions&PermissionAdministrator != 0 {
		return true
	}
	return r.Permissions&perm != 0
}

// AddPermission adds a permission to the role
func (r *Role) AddPermission(perm Permission) {
	r.Permissions |= perm
	r.UpdatedAt = time.Now()
}

// RemovePermission removes a permission from the role
func (r *Role) RemovePermission(perm Permission) {
	r.Permissions &^= perm
	r.UpdatedAt = time.Now()
}

// SetPermissions sets all permissions at once
func (r *Role) SetPermissions(perms Permission) {
	r.Permissions = perms
	r.UpdatedAt = time.Now()
}

// SetColor sets the role color
func (r *Role) SetColor(color int) {
	r.Color = color
	r.UpdatedAt = time.Now()
}

// GetColorHex returns the color as a hex string
func (r *Role) GetColorHex() string {
	if r.Color == 0 {
		return ""
	}
	return "#" + intToHex(r.Color)
}

// intToHex converts an integer to a 6-digit hex color
func intToHex(n int) string {
	const hexChars = "0123456789ABCDEF"
	result := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		result[i] = hexChars[n&0xF]
		n >>= 4
	}
	return string(result)
}

// PermissionCalculator helps compute effective permissions for a user
type PermissionCalculator struct {
	ServerOwnerID uuid.UUID
	EveryoneRole  *Role
}

// NewPermissionCalculator creates a new permission calculator
func NewPermissionCalculator(ownerID uuid.UUID, everyoneRole *Role) *PermissionCalculator {
	return &PermissionCalculator{
		ServerOwnerID: ownerID,
		EveryoneRole:  everyoneRole,
	}
}

// ComputeBasePermissions calculates base permissions for a member
func (pc *PermissionCalculator) ComputeBasePermissions(member *ServerMember, roles []*Role) Permission {
	// Server owner has all permissions
	if member.UserID == pc.ServerOwnerID {
		return Permission(^0) // All bits set
	}

	// Start with @everyone permissions
	permissions := pc.EveryoneRole.Permissions

	// Apply role permissions (OR them together)
	for _, role := range roles {
		permissions |= role.Permissions
		
		// If admin, grant all permissions
		if role.HasPermission(PermissionAdministrator) {
			return Permission(^0)
		}
	}

	return permissions
}

// ComputeOverwrites applies channel permission overwrites
func (pc *PermissionCalculator) ComputeOverwrites(
	basePermissions Permission,
	member *ServerMember,
	channel *Channel,
) Permission {
	// Administrator bypasses all overwrites
	if basePermissions&PermissionAdministrator != 0 {
		return basePermissions
	}

	permissions := basePermissions

	// Apply @everyone overwrites first
	for _, ow := range channel.PermissionOverwrites {
		if ow.ID == pc.EveryoneRole.ID {
			permissions &^= Permission(ow.Deny)
			permissions |= Permission(ow.Allow)
			break
		}
	}

	// Apply role overwrites
	var allow, deny Permission
	for _, ow := range channel.PermissionOverwrites {
		if ow.Type == "role" && member.HasRole(ow.ID) {
			deny |= Permission(ow.Deny)
			allow |= Permission(ow.Allow)
		}
	}
	permissions &^= deny
	permissions |= allow

	// Apply member-specific overwrites last
	for _, ow := range channel.PermissionOverwrites {
		if ow.Type == "member" && ow.ID == member.UserID {
			permissions &^= Permission(ow.Deny)
			permissions |= Permission(ow.Allow)
			break
		}
	}

	return permissions
}

// PermissionNames returns human-readable names for permissions
var PermissionNames = map[Permission]string{
	PermissionViewChannels:        "View Channels",
	PermissionManageChannels:      "Manage Channels",
	PermissionManageRoles:         "Manage Roles",
	PermissionManageServer:        "Manage Server",
	PermissionCreateInvite:        "Create Invite",
	PermissionKickMembers:         "Kick Members",
	PermissionBanMembers:          "Ban Members",
	PermissionChangeNickname:      "Change Nickname",
	PermissionManageNicknames:     "Manage Nicknames",
	PermissionSendMessages:        "Send Messages",
	PermissionSendMessagesThreads: "Send Messages in Threads",
	PermissionCreateThreads:       "Create Threads",
	PermissionEmbedLinks:          "Embed Links",
	PermissionAttachFiles:         "Attach Files",
	PermissionAddReactions:        "Add Reactions",
	PermissionUseExternalEmoji:    "Use External Emoji",
	PermissionMentionEveryone:     "Mention Everyone",
	PermissionManageMessages:      "Manage Messages",
	PermissionReadMessageHistory:  "Read Message History",
	PermissionPinMessages:         "Pin Messages",
	PermissionConnect:             "Connect to Voice",
	PermissionSpeak:               "Speak",
	PermissionMuteMembers:         "Mute Members",
	PermissionDeafenMembers:       "Deafen Members",
	PermissionMoveMembers:         "Move Members",
	PermissionUseVAD:              "Use Voice Activity",
	PermissionAdministrator:       "Administrator",
}
