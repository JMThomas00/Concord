package server

import (
	"encoding/json"
	"slices"

	"github.com/concord-chat/concord/internal/emoji"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// maxReactionEmoji is how many different emoji one message can carry;
// more people can still add to those already there.
const maxReactionEmoji = 20

// HandleReaction adds or takes off the sender's reaction to a message
// (OpReaction). Reacting needs Add Reactions where the message is; taking
// your own reaction off needs only to see it. A whisper's reactions go to
// its two people only, like the whisper.
func (h *Handlers) HandleReaction(c *Client, msg *protocol.Message) {
	var req protocol.ReactionRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid reaction request")
		return
	}
	// One emoji the picker knows (any skin tone), not arbitrary text.
	if _, ok := emoji.Lookup(req.Emoji); !ok || len(req.Emoji) > 64 {
		c.sendError(protocol.ErrorCodeInvalidPayload, "That isn't an emoji")
		return
	}
	message, err := h.db.GetMessage(req.MessageID)
	if err != nil || message == nil || message.ChannelID != req.ChannelID || message.Deleted {
		c.sendError(protocol.ErrorCodeNotFound, "Message not found")
		return
	}
	if message.IsWhisper && c.UserID != message.AuthorID &&
		(message.RecipientID == nil || c.UserID != *message.RecipientID) {
		c.sendError(protocol.ErrorCodeNotFound, "Message not found")
		return
	}
	channel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Channel not found")
		return
	}
	if h.hasChannelPermission(c.UserID, channel, models.PermissionViewChannels) != nil {
		c.sendError(protocol.ErrorCodeForbidden, "You can't see that channel")
		return
	}

	event := protocol.EventMessageReactionAdd
	if req.Remove {
		event = protocol.EventMessageReactionRemove
		removed, err := h.db.RemoveReaction(req.MessageID, c.UserID, req.Emoji)
		if err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Couldn't remove the reaction")
			MsgLog.Error("Failed to remove reaction", "message_id", req.MessageID, "error", err)
			return
		}
		if !removed {
			return // nothing to take off
		}
	} else {
		if h.hasChannelPermission(c.UserID, channel, models.PermissionAddReactions) != nil {
			c.sendError(protocol.ErrorCodeForbidden, "You need Add Reactions to react here")
			return
		}
		existing, err := h.db.ReactionEmoji(req.MessageID)
		if err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Couldn't add the reaction")
			return
		}
		if len(existing) >= maxReactionEmoji && !slices.Contains(existing, req.Emoji) {
			c.sendError(protocol.ErrorCodeInvalidPayload, "That message has as many different reactions as it can hold")
			return
		}
		added, err := h.db.AddReaction(req.MessageID, c.UserID, req.Emoji)
		if err != nil {
			c.sendError(protocol.ErrorCodeServerError, "Couldn't add the reaction")
			MsgLog.Error("Failed to add reaction", "message_id", req.MessageID, "error", err)
			return
		}
		if !added {
			return // already reacted with it
		}
	}

	payload := &protocol.ReactionPayload{
		UserID: c.UserID, ChannelID: req.ChannelID, MessageID: req.MessageID,
		ServerID: channel.ServerID, Emoji: req.Emoji,
	}
	if message.IsWhisper {
		h.hub.SendToUser(message.AuthorID, event, payload)
		if message.RecipientID != nil && *message.RecipientID != message.AuthorID {
			h.hub.SendToUser(*message.RecipientID, event, payload)
		}
		return
	}
	h.hub.BroadcastToChannel(req.ChannelID, event, payload, nil)
}
