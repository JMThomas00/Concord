package server

// Invites and join modes (2026-11-05). Registering and signing in both go
// through admitMember, which decides whether someone becomes a member: on
// an open server anyone does; invite only needs a usable code; approval
// records a request a moderator answers (a code skips the wait). The owner,
// the configured admin email and the very first account always get in, so
// a server can't lock out its own admin. OpInvites (70) makes, emails and
// cancels codes, answers requests, and for administrators changes the join
// mode and the email verification switch.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

const (
	inviteCodeLength = 8
	// emailInvitesPerDay is how many invites one person may email in a day.
	emailInvitesPerDay = 20
	defaultInviteAge   = 7 * 24 * time.Hour
)

// Answers a sign-in or registration can get instead of a session.
const (
	errCodeInviteRequired  = "invite_required"  // invite only: type a code
	errCodeInviteInvalid   = "invite_invalid"   // the code typed can't be used
	errCodeApprovalPending = "approval_pending" // waiting for a moderator
	errCodeJoinDenied      = "join_denied"      // a moderator said no
	errCodeBanned          = "banned"
)

// newInviteCode is 8 characters from the unambiguous code alphabet.
func newInviteCode() (string, error) {
	b := make([]byte, inviteCodeLength)
	max := big.NewInt(int64(len(codeAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b), nil
}

// inviteCodeFrom takes what someone pasted: a bare code, or an invite
// string host:port/CODE (or a concord:// link ending in the code).
func inviteCodeFrom(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return cleanCode(s)
}

// joinRefusal is why someone isn't (yet) a member.
type joinRefusal struct {
	status int
	code   string
	msg    string
}

func (j *joinRefusal) Error() string { return j.msg }

func (j *joinRefusal) write(w http.ResponseWriter) {
	writeJSON(w, j.status, map[string]string{"error": j.msg, "code": j.code})
}

// privileged: the owner, the configured admin email (once proven, where
// the server verifies) and the first account always get in.
func (s *Server) privileged(user *models.User, srv *models.Server) bool {
	if srv.OwnerID == user.ID {
		return true
	}
	if s.config.AdminEmail != "" && strings.EqualFold(user.Email, s.config.AdminEmail) {
		if !s.config.Mail.VerificationRequired() {
			return true
		}
		if ok, _ := s.db.IsEmailVerified(user.ID); ok {
			return true
		}
	}
	n, err := s.db.CountRealUsers()
	return err == nil && n <= 1
}

// checkInvite finds a usable invite for code, for someone with email.
func (s *Server) checkInvite(code, email string) (*database.Invite, *joinRefusal) {
	in, err := s.db.GetInvite(code)
	if err == nil {
		err = in.Usable(time.Now())
	}
	if err == nil && in.Email != "" && !strings.EqualFold(in.Email, email) {
		err = errors.New("that invite was sent to a different email address")
	}
	if err != nil {
		return nil, &joinRefusal{http.StatusForbidden, errCodeInviteInvalid, capitalize(err.Error()) + "."}
	}
	return in, nil
}

// admitMember makes user a member of the server if its join mode allows,
// using invite (a code, or "") and keeping note for a join request. nil
// means they're a member now (or were already).
func (s *Server) admitMember(user *models.User, invite, note string) *joinRefusal {
	srv, everyone, err := s.db.EnsureDefaultServer(s.config.ServerName)
	if err != nil {
		DBLog.Error("Failed to get default server", "error", err)
		return nil // as before: don't fail sign-in over this
	}
	if banned, _ := s.db.IsBanned(srv.ID, user.ID); banned {
		return &joinRefusal{http.StatusForbidden, errCodeBanned, "You're banned from " + s.config.ServerName + "."}
	}
	if _, err := s.db.GetServerMember(srv.ID, user.ID); err == nil {
		return nil
	}

	var used *database.Invite
	if code := inviteCodeFrom(invite); code != "" {
		in, refusal := s.checkInvite(code, user.Email)
		if refusal != nil {
			return refusal
		}
		if err := s.db.UseInvite(in.Code, time.Now()); err != nil {
			return &joinRefusal{http.StatusForbidden, errCodeInviteInvalid, capitalize(err.Error()) + "."}
		}
		used = in
	}

	mode, _ := s.db.JoinMode(srv.ID)
	if used == nil && mode != database.JoinOpen && !s.privileged(user, srv) {
		if mode == database.JoinInvite {
			return &joinRefusal{http.StatusForbidden, errCodeInviteRequired, s.config.ServerName + " is invite only. Ask a member for an invite code."}
		}
		req, _ := s.db.GetJoinRequest(srv.ID, user.ID)
		if req != nil && req.Denied {
			return &joinRefusal{http.StatusForbidden, errCodeJoinDenied, "Your request to join " + s.config.ServerName + " was turned down."}
		}
		if err := s.db.AddJoinRequest(database.JoinRequest{UserID: user.ID, ServerID: srv.ID, Note: strings.TrimSpace(note)}); err != nil {
			DBLog.Error("Failed to record a join request", "user_id", user.ID, "error", err)
		} else if req == nil {
			AuthLog.Info("Join request", "user_id", user.ID, "username", user.Username)
			s.handlers.notifyJoinRequest(srv.ID, user.Username)
		}
		return &joinRefusal{http.StatusForbidden, errCodeApprovalPending, "Your request to join " + s.config.ServerName + " is waiting for a moderator."}
	}

	if err := s.handlers.addMember(srv.ID, everyone.ID, user.ID); err != nil {
		DBLog.Error("Failed to add member", "user_id", user.ID, "error", err)
		return nil
	}
	if used != nil {
		_ = s.db.SetMemberInvite(srv.ID, user.ID, used.Code, used.InviterID)
		AuthLog.Info("Joined with an invite", "user_id", user.ID, "code", used.Code, "inviter", used.InviterID)
	}
	_ = s.db.DeleteJoinRequest(srv.ID, user.ID)
	return nil
}

// addMember makes userID a member with the @everyone role.
func (h *Handlers) addMember(serverID, everyoneRoleID, userID uuid.UUID) error {
	if err := h.db.AddServerMember(&models.ServerMember{UserID: userID, ServerID: serverID, JoinedAt: time.Now()}); err != nil {
		return err
	}
	if err := h.db.AddMemberRole(userID, serverID, everyoneRoleID); err != nil {
		DBLog.Error("Failed to assign @everyone role", "user_id", userID, "error", err)
	}
	return nil
}

// preRegisterCheck refuses a registration that couldn't become a member,
// before an account is made for it: on an invite only server, without a
// usable code.
func (s *Server) preRegisterCheck(email, invite string) *joinRefusal {
	srv, _, err := s.db.EnsureDefaultServer(s.config.ServerName)
	if err != nil {
		return nil
	}
	if mode, _ := s.db.JoinMode(srv.ID); mode != database.JoinInvite {
		return nil
	}
	if n, err := s.db.CountRealUsers(); err == nil && n == 0 {
		return nil // the first account
	}
	if s.config.AdminEmail != "" && strings.EqualFold(email, s.config.AdminEmail) {
		return nil
	}
	code := inviteCodeFrom(invite)
	if code == "" {
		return &joinRefusal{http.StatusForbidden, errCodeInviteRequired, s.config.ServerName + " is invite only. Ask a member for an invite code."}
	}
	_, refusal := s.checkInvite(code, email)
	return refusal
}

// emailedInviteFor is a usable invite emailed to email, by code: using it
// proves the address, so the account needs no separate verification.
func (s *Server) emailedInviteFor(invite, email string) bool {
	code := inviteCodeFrom(invite)
	if code == "" {
		return false
	}
	in, refusal := s.checkInvite(code, email)
	return refusal == nil && in.Email != ""
}

// --- Join settings the handlers need from the Server ---

// joinHost is what OpInvites needs from the Server (the handlers don't own
// the config or the mailer).
type joinHost interface {
	joinMail() (enabled bool, from string)
	verificationRequired() bool
	setVerificationRequired(on bool) error
	canSaveSettings() bool
	sendMail(e Email) error
	displayName() string
}

func (s *Server) joinMail() (bool, string) {
	return s.config.Mail.Enabled() && s.mailer != nil, s.config.Mail.From
}

func (s *Server) verificationRequired() bool { return s.config.Mail.VerificationRequired() }

func (s *Server) canSaveSettings() bool { return s.configPath != "" }

func (s *Server) displayName() string { return s.config.ServerName }

func (s *Server) sendMail(e Email) error {
	if s.mailer == nil {
		return errors.New("this server can't send email")
	}
	return s.mailer.Send(e)
}

// setVerificationRequired turns email verification on or off and saves it.
// Existing accounts count as verified when it's turned on: users were
// verified (email_verified defaults to 1) unless they registered while it
// was on and never entered their code.
func (s *Server) setVerificationRequired(on bool) error {
	if on && !s.config.Mail.Enabled() {
		return errors.New("email isn't set up on this server, so it can't verify addresses")
	}
	old := s.config.Mail.RequireVerification
	s.config.Mail.RequireVerification = &on
	if err := s.saveConfig(); err != nil {
		s.config.Mail.RequireVerification = old
		return fmt.Errorf("couldn't save the setting: %w", err)
	}
	return nil
}

// --- OpInvites ---

// notifyJoinRequest tells everyone online who can let people in.
func (h *Handlers) notifyJoinRequest(serverID uuid.UUID, username string) {
	reqs, _ := h.db.ListJoinRequests(serverID)
	p := protocol.JoinRequestPayload{ServerID: serverID, Username: username, Pending: len(reqs)}
	for _, uid := range h.hub.GetOnlineUsers(serverID) {
		if h.canManageInvites(uid, serverID) {
			_ = h.hub.SendToUser(uid, protocol.EventJoinRequest, p)
		}
	}
}

// canManageInvites: everyone's invites and the join requests are for
// moderators (Kick Members) and those who manage the server.
func (h *Handlers) canManageInvites(userID, serverID uuid.UUID) bool {
	return h.checkPermission(userID, serverID, models.PermissionKickMembers) == nil ||
		h.checkPermission(userID, serverID, models.PermissionManageServer) == nil
}

// HandleInvites answers OpInvites.
func (h *Handlers) HandleInvites(c *Client, msg *protocol.Message) {
	var req protocol.InvitesRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if _, err := h.db.GetServerMember(req.ServerID, c.UserID); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Not a member of this server")
		return
	}
	canInvite := h.checkPermission(c.UserID, req.ServerID, models.PermissionCreateInvite) == nil
	canManage := h.canManageInvites(c.UserID, req.ServerID)
	canSettings := h.checkPermission(c.UserID, req.ServerID, models.PermissionAdministrator) == nil

	result := func(ok bool, text, code string) {
		_ = h.dispatchTo(c, protocol.EventInvitesResult, protocol.InvitesResult{Action: req.Action, OK: ok, Message: text, Code: code})
	}
	list := func() { h.sendInvites(c, req.ServerID, canInvite, canManage, canSettings) }

	switch req.Action {
	case protocol.InvitesList, "":
		list()

	case protocol.InvitesCreate:
		if !canInvite {
			result(false, "You can't make invites here", "")
			return
		}
		in, err := h.newInvite(c.UserID, req, "", 0)
		if err != nil {
			result(false, err.Error(), "")
			return
		}
		result(true, "Invite made", in.Code)
		list()

	case protocol.InvitesEmail:
		if !canInvite {
			result(false, "You can't make invites here", "")
			return
		}
		go func() { // sending mail takes a moment: off the read loop
			code, err := h.emailInvite(c.UserID, req)
			if err != nil {
				result(false, err.Error(), "")
				return
			}
			result(true, "Invite sent to "+strings.ToLower(strings.TrimSpace(req.Email)), code)
			list()
		}()

	case protocol.InvitesRevoke:
		in, err := h.db.GetInvite(cleanCode(req.Code))
		if err != nil || in.ServerID != req.ServerID {
			result(false, "That invite doesn't exist", "")
			return
		}
		if in.InviterID != c.UserID && !canManage {
			result(false, "You can only cancel your own invites", "")
			return
		}
		if err := h.db.RevokeInvite(in.Code); err != nil {
			result(false, "Couldn't cancel it", "")
			return
		}
		result(true, "Invite cancelled", in.Code)
		list()

	case protocol.InvitesApprove, protocol.InvitesDeny:
		if !canManage {
			result(false, "Only moderators can answer join requests", "")
			return
		}
		r, _ := h.db.GetJoinRequest(req.ServerID, req.UserID)
		if r == nil || r.Denied {
			result(false, "That request isn't waiting any more", "")
			list()
			return
		}
		name := req.UserID.String()
		if u, err := h.db.GetUserByID(req.UserID); err == nil {
			name = u.Username
		}
		if req.Action == protocol.InvitesDeny {
			if err := h.db.DenyJoinRequest(req.ServerID, req.UserID); err != nil {
				result(false, "Couldn't turn it down", "")
				return
			}
			Logger.Info("Join request turned down", "user_id", req.UserID, "by", c.UserID)
			result(true, name+" was turned down", "")
		} else {
			everyone, err := h.db.GetEveryoneRole(req.ServerID)
			if err != nil {
				result(false, "Couldn't let them in", "")
				return
			}
			if err := h.addMember(req.ServerID, everyone.ID, req.UserID); err != nil {
				result(false, "Couldn't let them in", "")
				return
			}
			_ = h.db.DeleteJoinRequest(req.ServerID, req.UserID)
			Logger.Info("Join request approved", "user_id", req.UserID, "by", c.UserID)
			result(true, name+" can join now", "")
		}
		list()

	case protocol.InvitesSettings:
		if !canSettings {
			result(false, "Only administrators can change how people join", "")
			return
		}
		var done []string
		if req.JoinMode != nil {
			if err := h.db.SetJoinMode(req.ServerID, *req.JoinMode); err != nil {
				result(false, "That isn't a join mode", "")
				return
			}
			Logger.Info("Join mode changed", "mode", *req.JoinMode, "by", c.UserID)
			done = append(done, "Join mode: "+*req.JoinMode)
		}
		if req.RequireVerification != nil {
			if h.join == nil {
				result(false, "This server can't change that", "")
				return
			}
			if err := h.join.setVerificationRequired(*req.RequireVerification); err != nil {
				result(false, capitalize(err.Error()), "")
				list()
				return
			}
			Logger.Info("Email verification changed", "on", *req.RequireVerification, "by", c.UserID)
			done = append(done, map[bool]string{true: "Email verification on", false: "Email verification off"}[*req.RequireVerification])
		}
		result(true, strings.Join(done, " · "), "")
		list()

	case protocol.InvitesTestMail:
		if !canSettings || h.join == nil {
			result(false, "Only administrators can send a test email", "")
			return
		}
		u, err := h.db.GetUserByID(c.UserID)
		if err != nil || u.Email == "" {
			result(false, "Your account has no email address", "")
			return
		}
		go func() {
			if err := h.join.sendMail(testEmail(h.join.displayName(), u.Email)); err != nil {
				result(false, "The test email didn't send: "+err.Error(), "")
				return
			}
			result(true, "Test email sent to "+u.Email, "")
		}()

	default:
		result(false, "Unknown invite action", "")
	}
}

// newInvite makes and stores a code by inviter (email: bound to it).
func (h *Handlers) newInvite(inviter uuid.UUID, req protocol.InvitesRequest, email string, maxUses int) (*database.Invite, error) {
	code, err := newInviteCode()
	if err != nil {
		return nil, errors.New("couldn't make a code")
	}
	now := time.Now()
	in := &database.Invite{Code: code, ServerID: req.ServerID, InviterID: inviter, Email: email, Note: req.Note, MaxUses: req.MaxUses, CreatedAt: now}
	if maxUses > 0 {
		in.MaxUses = maxUses
	}
	if in.MaxUses < 0 {
		in.MaxUses = 0
	}
	if req.MaxAge > 0 {
		in.ExpiresAt = now.Add(time.Duration(req.MaxAge) * time.Second)
	}
	if err := h.db.CreateInvite(in); err != nil {
		return nil, errors.New("couldn't save the invite")
	}
	Logger.Info("Invite made", "code", code, "by", inviter, "emailed", email != "")
	return in, nil
}

// emailInvite makes a one-use invite for req.Email and sends it there.
func (h *Handlers) emailInvite(inviter uuid.UUID, req protocol.InvitesRequest) (string, error) {
	if h.join == nil {
		return "", errors.New("this server can't send email")
	}
	if on, _ := h.join.joinMail(); !on {
		return "", errors.New("email isn't set up on this server")
	}
	email, ok := normalizeEmail(req.Email)
	if !ok {
		return "", errors.New("that doesn't look like an email address")
	}
	if u, _, err := h.db.FindUserByEmail(email); err == nil {
		if _, err := h.db.GetServerMember(req.ServerID, u.ID); err == nil {
			return "", errors.New("someone with that email is already a member")
		}
	}
	now := time.Now()
	if old, _ := h.db.PendingEmailInvite(req.ServerID, email, now); old != nil {
		return "", errors.New("that address already has an invite waiting (cancel it to send a new one)")
	}
	if n, _ := h.db.EmailInvitesSince(inviter, now.Add(-24*time.Hour)); n >= emailInvitesPerDay {
		return "", fmt.Errorf("you've emailed %d invites today; try again tomorrow", emailInvitesPerDay)
	}
	if req.MaxAge <= 0 {
		req.MaxAge = int(defaultInviteAge.Seconds())
	}
	in, err := h.newInvite(inviter, req, email, 1)
	if err != nil {
		return "", err
	}
	from := "Someone"
	if u, err := h.db.GetUserByID(inviter); err == nil {
		from = u.Username
	}
	e := inviteEmail(h.join.displayName(), from, strings.TrimSpace(req.Address), in.Code, strings.TrimSpace(req.Note), in.ExpiresAt)
	e.To = email
	if err := h.join.sendMail(e); err != nil {
		_ = h.db.RevokeInvite(in.Code)
		Logger.Warn("Invite email failed", "by", inviter, "error", err)
		return "", errors.New("the email didn't send; check the server's mail settings")
	}
	return in.Code, nil
}

// sendInvites answers with what c may see.
func (h *Handlers) sendInvites(c *Client, serverID uuid.UUID, canInvite, canManage, canSettings bool) {
	p := protocol.InvitesPayload{ServerID: serverID, CanInvite: canInvite, CanManage: canManage, CanSettings: canSettings, Invites: []protocol.InviteInfo{}}
	p.JoinMode, _ = h.db.JoinMode(serverID)
	if h.join != nil {
		p.MailEnabled, p.MailFrom = h.join.joinMail()
		p.RequireVerification = h.join.verificationRequired()
		p.CanSaveSettings = h.join.canSaveSettings()
	}
	if !canSettings {
		p.MailFrom = ""
	}
	who := c.UserID
	if canManage {
		who = uuid.Nil
	}
	invites, err := h.db.ListInvites(serverID, who)
	if err != nil {
		Logger.Error("Listing invites failed", "error", err)
	}
	names := map[uuid.UUID]string{}
	name := func(id uuid.UUID) string {
		if n, ok := names[id]; ok {
			return n
		}
		n := "someone"
		if u, err := h.db.GetUserByID(id); err == nil {
			n = u.Username
		}
		names[id] = n
		return n
	}
	now := time.Now()
	for _, in := range invites {
		p.Invites = append(p.Invites, protocol.InviteInfo{
			Code: in.Code, InviterID: in.InviterID, InviterName: name(in.InviterID), Email: in.Email,
			MaxUses: in.MaxUses, Uses: in.Uses, CreatedAt: in.CreatedAt, ExpiresAt: in.ExpiresAt,
			Usable: in.Usable(now) == nil,
		})
	}
	if canManage {
		reqs, _ := h.db.ListJoinRequests(serverID)
		for _, r := range reqs {
			info := protocol.JoinRequestInfo{UserID: r.UserID, Note: r.Note, CreatedAt: r.CreatedAt, Username: name(r.UserID)}
			if u, err := h.db.GetUserByID(r.UserID); err == nil {
				info.Email = u.Email
			}
			p.Requests = append(p.Requests, info)
		}
	}
	_ = h.dispatchTo(c, protocol.EventInvites, p)
}
