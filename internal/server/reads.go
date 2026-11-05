package server

// Read positions (OpChannelRead, CHANNEL_READ) and who a message mentions.

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// HandleChannelRead moves the sender's read position in a channel to its
// newest message, and tells every connection of that account.
func (h *Handlers) HandleChannelRead(c *Client, msg *protocol.Message) {
	var req protocol.ChannelReadRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	channel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Channel not found")
		return
	}
	if _, err := h.db.GetServerMember(channel.ServerID, c.UserID); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Not a member of this server")
		return
	}
	seq, err := h.db.MarkChannelRead(c.UserID, channel.ID)
	if err != nil {
		MsgLog.Error("Failed to mark channel read", "channel_id", channel.ID, "user_id", c.UserID, "error", err)
		return
	}
	_ = h.hub.SendToUser(c.UserID, protocol.EventChannelRead, protocol.ChannelReadPayload{ChannelID: channel.ID, ReadSeq: seq})
}

// unreadsFor is the user's unread state in a server, for SERVER_CREATE.
func (h *Handlers) unreadsFor(userID, serverID uuid.UUID) []protocol.ChannelUnread {
	list, err := h.db.ServerUnreads(userID, serverID)
	if err != nil {
		MsgLog.Warn("Couldn't read unread counts", "server_id", serverID, "error", err)
		return nil
	}
	out := make([]protocol.ChannelUnread, len(list))
	for i, cu := range list {
		out[i] = protocol.ChannelUnread{ChannelID: cu.ChannelID, ReadSeq: cu.ReadSeq, Unread: cu.Unread, Mentions: cu.Mentions}
	}
	return out
}

// mentionNames is every "@name" in a message: the characters a username can
// have after an @, lowercased, without @everyone/@here.
func mentionNames(content string) []string {
	seen := map[string]bool{}
	var out []string
	for i := 0; i < len(content); i++ {
		if content[i] != '@' || (i > 0 && isNameByte(content[i-1])) {
			continue
		}
		j := i + 1
		for j < len(content) && isNameByte(content[j]) {
			j++
		}
		name := strings.ToLower(strings.TrimRight(content[i+1:j], ".-"))
		if name != "" && name != "everyone" && name != "here" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i = j - 1
	}
	return out
}

func isNameByte(b byte) bool {
	return b == '_' || b == '.' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// resolveMentions adds the members a message names with "@name" to its
// Mentions (beside any <@id> ones), leaving out its author.
func (h *Handlers) resolveMentions(m *models.Message, serverID uuid.UUID) {
	names := mentionNames(m.Content)
	if len(names) == 0 || serverID == uuid.Nil {
		return
	}
	ids, err := h.db.MemberIDsByName(serverID, names)
	if err != nil {
		return
	}
	have := map[uuid.UUID]bool{m.AuthorID: true}
	for _, id := range m.Mentions {
		have[id] = true
	}
	for _, id := range ids {
		if !have[id] {
			have[id] = true
			m.Mentions = append(m.Mentions, id)
		}
	}
}

// mentionReplied adds the author of the message m replies to (in the same
// channel) to m's Mentions as it goes out, so their client counts it as a
// mention. It isn't stored as one: the inbox finds replies by what they
// answer (database.MentionsFor), which keeps "replied to you" apart from
// "@you".
func (h *Handlers) mentionReplied(m *models.Message) {
	if m.ReplyToID == nil {
		return
	}
	parent, err := h.db.GetMessage(*m.ReplyToID)
	if err != nil || parent.ChannelID != m.ChannelID || parent.AuthorID == m.AuthorID {
		return
	}
	for _, id := range m.Mentions {
		if id == parent.AuthorID {
			return
		}
	}
	m.Mentions = append(m.Mentions, parent.AuthorID)
}

// mentionsPage is how many mentions one MENTIONS carries.
const mentionsPage = 50

// HandleRequestMentions answers a page of the sender's mentions in a
// server (OpRequestMentions), leaving out channels they can't view.
func (h *Handlers) HandleRequestMentions(c *Client, msg *protocol.Message) {
	var req protocol.MentionsRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if _, err := h.db.GetServerMember(req.ServerID, c.UserID); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Not a member of this server")
		return
	}
	limit := req.Limit
	if limit <= 0 || limit > mentionsPage {
		limit = mentionsPage
	}
	out := protocol.MentionsPayload{ServerID: req.ServerID, Before: req.Before, Items: []protocol.MentionItem{}}
	names := h.newDisplayNames(req.ServerID)
	canView := map[uuid.UUID]bool{}
	before := req.Before
	// Pages of hidden channels' mentions are skipped, a few at most.
	for tries := 0; tries < 4 && len(out.Items) < limit; tries++ {
		list, err := h.db.MentionsFor(c.UserID, req.ServerID, before, limit+1)
		if err != nil {
			MsgLog.Error("Failed to read mentions", "user_id", c.UserID, "server_id", req.ServerID, "error", err)
			c.sendError(protocol.ErrorCodeServerError, "Couldn't read your mentions")
			return
		}
		more := len(list) > limit
		if more {
			list = list[:limit]
		}
		out.HasMore = more
		for _, m := range list {
			if len(out.Items) == limit {
				out.HasMore = true // the next page starts after the last one sent
				break
			}
			before = m.Message.Seq
			ok, seen := canView[m.Message.ChannelID]
			if !seen {
				if ch, err := h.db.GetChannelByID(m.Message.ChannelID); err == nil {
					ok = h.hasChannelPermission(c.UserID, ch, models.PermissionViewChannels) == nil
				}
				canView[m.Message.ChannelID] = ok
			}
			if !ok {
				continue
			}
			if d := names.display(m.Message); d != nil {
				out.Items = append(out.Items, protocol.MentionItem{Message: d, Kind: m.Kind, Unread: m.Unread})
			}
		}
		if !more {
			break
		}
	}
	_ = h.dispatchTo(c, protocol.EventMentions, out)
}
