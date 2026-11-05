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
