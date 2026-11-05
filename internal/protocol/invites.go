package protocol

import (
	"time"

	"github.com/google/uuid"
)

// Invites and join requests (OpInvites, 70).

// Invite actions.
const (
	InvitesList     = "list"      // send INVITES
	InvitesCreate   = "create"    // make a code (MaxAge, MaxUses)
	InvitesEmail    = "email"     // make a code for Email and send it there
	InvitesRevoke   = "revoke"    // cancel Code
	InvitesApprove  = "approve"   // let UserID in (a join request)
	InvitesDeny     = "deny"      // turn UserID's request down
	InvitesSettings = "settings"  // change JoinMode / RequireVerification (administrators)
	InvitesTestMail = "test_mail" // send a test email to yourself (administrators)
)

// Join modes (InvitesPayload.JoinMode).
const (
	JoinOpen     = "open"     // anyone who can reach the server may register
	JoinInvite   = "invite"   // a code is needed
	JoinApproval = "approval" // anyone may ask; a moderator lets them in
)

// InvitesRequest is one invite action.
type InvitesRequest struct {
	ServerID uuid.UUID `json:"server_id"`
	Action   string    `json:"action"`

	// create, email
	MaxAge  int `json:"max_age,omitempty"`  // seconds; 0: never expires
	MaxUses int `json:"max_uses,omitempty"` // 0: unlimited (an emailed invite is always one use)
	// email
	Email string `json:"email,omitempty"`
	Note  string `json:"note,omitempty"`
	// Address is how the inviter reaches the server (host:port), for the
	// email; the server doesn't know its own public address.
	Address string `json:"address,omitempty"`

	Code   string    `json:"code,omitempty"`    // revoke
	UserID uuid.UUID `json:"user_id,omitempty"` // approve, deny

	// settings: nil leaves it as it is.
	JoinMode            *string `json:"join_mode,omitempty"`
	RequireVerification *bool   `json:"require_verification,omitempty"`
}

// InviteInfo is one invite.
type InviteInfo struct {
	Code        string    `json:"code"`
	InviterID   uuid.UUID `json:"inviter_id"`
	InviterName string    `json:"inviter_name"`
	Email       string    `json:"email,omitempty"`
	MaxUses     int       `json:"max_uses"`
	Uses        int       `json:"uses"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"` // zero: never
	Usable      bool      `json:"usable"`               // not expired or used up
}

// JoinRequestInfo is someone waiting to be let in.
type JoinRequestInfo struct {
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	Email     string    `json:"email,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// InvitesPayload is what the asker may see (INVITES): their invites (or
// everyone's for those who manage them), the join requests, and the join
// settings.
type InvitesPayload struct {
	ServerID uuid.UUID `json:"server_id"`

	CanInvite   bool `json:"can_invite"`   // Create Invite
	CanManage   bool `json:"can_manage"`   // everyone's invites and the requests (Kick Members or Manage Server)
	CanSettings bool `json:"can_settings"` // join settings (administrators)

	Invites  []InviteInfo      `json:"invites"`
	Requests []JoinRequestInfo `json:"requests,omitempty"`

	JoinMode            string `json:"join_mode"`
	MailEnabled         bool   `json:"mail_enabled"`
	MailFrom            string `json:"mail_from,omitempty"`
	RequireVerification bool   `json:"require_verification"`
	// CanSaveSettings is false when the server has no settings file to
	// write the verification switch to.
	CanSaveSettings bool `json:"can_save_settings"`

	// Onion: the server's onion address (host:port) and access key, so
	// invites can carry them: host:port/CODE#KEY.
	Onion    string `json:"onion,omitempty"`
	OnionKey string `json:"onion_key,omitempty"`
	TorOnly  bool   `json:"tor_only,omitempty"`
}

// InvitesResult says how an action went; Code is a new invite's.
type InvitesResult struct {
	Action  string `json:"action"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// JoinRequestPayload tells those who can let people in that someone asked.
type JoinRequestPayload struct {
	ServerID uuid.UUID `json:"server_id"`
	Username string    `json:"username"`
	Pending  int       `json:"pending"`
}
