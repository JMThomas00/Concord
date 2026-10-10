package server

import (
	"encoding/json"
	"time"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// Threads (2026-10-09; vault: "Concord - Threads Plan"). A reply in a thread
// carries thread_id, the thread's first message. Channel history leaves the
// replies out and gives each first message a summary (attachThreadSummaries);
// a client loads a thread's replies with OpRequestThread. Every change to a
// thread is broadcast to the channel as THREAD_UPDATE.

// resolveThread checks a message being posted into a thread and returns the
// thread's ID: the message named, or the thread it's already in (threads are
// one level deep). It must be in the same channel.
func (h *Handlers) resolveThread(channelID, named uuid.UUID) (uuid.UUID, bool) {
	root, err := h.db.GetMessage(named)
	if err != nil || root == nil || root.ChannelID != channelID {
		return uuid.Nil, false
	}
	if root.ThreadID != nil {
		return *root.ThreadID, true
	}
	return root.ID, true
}

// followAfterReply records who follows a thread once reply is posted in it:
// its author (read up to now), the thread starter, and anyone it @mentions.
func (h *Handlers) followAfterReply(threadID uuid.UUID, reply *models.Message) {
	_ = h.db.FollowThread(reply.AuthorID, threadID, true)
	if root, err := h.db.GetMessage(threadID); err == nil && root.AuthorID != uuid.Nil && root.AuthorID != reply.AuthorID {
		_ = h.db.FollowThread(root.AuthorID, threadID, false)
	}
	for _, id := range reply.Mentions {
		if id != reply.AuthorID {
			_ = h.db.FollowThread(id, threadID, false)
		}
	}
}

// threadSummary builds a thread's summary from its stats, for userID: with
// their own Following/Unread when follow is given.
func (h *Handlers) threadSummary(threadID, channelID uuid.UUID, st *database.ThreadStat, follow *database.ThreadFollow, userID uuid.UUID, serverID uuid.UUID) *protocol.ThreadSummary {
	sum := &protocol.ThreadSummary{ThreadID: threadID, ChannelID: channelID}
	if st == nil {
		return sum
	}
	sum.ReplyCount = st.ReplyCount
	sum.Participants = st.Participants
	if !st.LastReplyAt.IsZero() {
		at := st.LastReplyAt
		sum.LastReplyAt = &at
	}
	if last, err := h.db.GetMessage(st.LastReplyID); err == nil {
		sum.LastReply = h.messageDisplay(last, serverID)
	}
	if follow != nil {
		sum.Following = true
		lastFromOther := sum.LastReply != nil && sum.LastReply.AuthorID != userID
		sum.Unread = lastFromOther && (follow.LastReadAt == nil || st.LastReplyAt.After(*follow.LastReadAt))
	}
	return sum
}

// messageDisplay pairs a message with its author and their membership (for
// nicknames).
func (h *Handlers) messageDisplay(m *models.Message, serverID uuid.UUID) *protocol.MessageDisplay {
	d := &protocol.MessageDisplay{Message: m}
	if m.AuthorID != uuid.Nil {
		d.Author, _ = h.db.GetUserByID(m.AuthorID)
		if serverID != uuid.Nil {
			d.Member, _ = h.db.GetServerMember(serverID, m.AuthorID)
		}
	}
	return d
}

// attachThreadSummaries gives every message in a channel's history that
// starts a thread its summary, as seen by userID.
func (h *Handlers) attachThreadSummaries(msgs []*protocol.MessageDisplay, channelID, serverID, userID uuid.UUID) {
	ids := make([]uuid.UUID, 0, len(msgs))
	for _, d := range msgs {
		if d.Message != nil {
			ids = append(ids, d.ID)
		}
	}
	stats, err := h.db.GetThreadStats(ids, userID)
	if err != nil {
		MsgLog.Warn("Failed to summarise threads", "channel_id", channelID, "error", err)
		return
	}
	if len(stats) == 0 {
		return
	}
	threadIDs := make([]uuid.UUID, 0, len(stats))
	for id := range stats {
		threadIDs = append(threadIDs, id)
	}
	follows, _ := h.db.GetThreadFollows(userID, threadIDs)
	for _, d := range msgs {
		if d.Message == nil {
			continue
		}
		if st := stats[d.ID]; st != nil {
			var f *database.ThreadFollow
			if fl, ok := follows[d.ID]; ok {
				f = &fl
			}
			d.Thread = h.threadSummary(d.ID, channelID, st, f, userID, serverID)
		}
	}
}

// broadcastThreadUpdate tells everyone in the channel a thread changed: its
// new summary, with its followers so each client can tell whether it's
// theirs and unread.
func (h *Handlers) broadcastThreadUpdate(channelID, threadID uuid.UUID) {
	serverID := uuid.Nil
	if ch, err := h.db.GetChannelByID(channelID); err == nil {
		serverID = ch.ServerID
	}
	stats, err := h.db.GetThreadStats([]uuid.UUID{threadID}, uuid.Nil)
	if err != nil {
		MsgLog.Warn("Failed to summarise a thread", "thread_id", threadID, "error", err)
		return
	}
	sum := h.threadSummary(threadID, channelID, stats[threadID], nil, uuid.Nil, serverID)
	sum.Followers, _ = h.db.GetThreadFollowers(threadID)
	h.hub.BroadcastToChannel(channelID, protocol.EventThreadUpdate, sum, nil)
}

// HandleRequestThread answers OpRequestThread with the thread's replies and
// its summary for this user.
func (h *Handlers) HandleRequestThread(c *Client, msg *protocol.Message) {
	var req protocol.ThreadRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid thread request")
		return
	}
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 200
	}
	root, err := h.db.GetMessage(req.ThreadID)
	if err != nil || root.ChannelID != req.ChannelID {
		c.sendError(protocol.ErrorCodeNotFound, "Thread not found")
		return
	}
	channel, err := h.db.GetChannelByID(req.ChannelID)
	if err != nil {
		c.sendError(protocol.ErrorCodeNotFound, "Channel not found")
		return
	}
	if !c.IsPlugin && channel.ServerID != uuid.Nil {
		if err := h.hasChannelPermission(c.UserID, channel, models.PermissionViewChannels); err != nil {
			c.sendError(protocol.ErrorCodeForbidden, "You can't see this channel")
			return
		}
	}
	replies, err := h.db.GetThreadMessages(req.ThreadID, req.Limit, c.UserID)
	if err != nil {
		c.sendError(protocol.ErrorCodeServerError, "Failed to load the thread")
		MsgLog.Error("Failed to load thread", "thread_id", req.ThreadID, "error", err)
		return
	}
	out := &protocol.ThreadMessagesPayload{ChannelID: req.ChannelID, ThreadID: req.ThreadID}
	for _, m := range replies {
		out.Messages = append(out.Messages, h.messageDisplay(m, channel.ServerID))
	}
	stats, _ := h.db.GetThreadStats([]uuid.UUID{req.ThreadID}, c.UserID)
	follows, _ := h.db.GetThreadFollows(c.UserID, []uuid.UUID{req.ThreadID})
	var f *database.ThreadFollow
	if fl, ok := follows[req.ThreadID]; ok {
		f = &fl
	}
	out.Summary = h.threadSummary(req.ThreadID, req.ChannelID, stats[req.ThreadID], f, c.UserID, channel.ServerID)
	_ = h.dispatchTo(c, protocol.EventThreadMessages, out)
}

// HandleThreadRead marks a thread read for this user, and tells all their
// connections, so the unread mark clears everywhere.
func (h *Handlers) HandleThreadRead(c *Client, msg *protocol.Message) {
	var req protocol.ThreadReadPayload
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid thread read")
		return
	}
	if err := h.db.MarkThreadRead(c.UserID, req.ThreadID); err != nil {
		MsgLog.Warn("Failed to mark thread read", "thread_id", req.ThreadID, "error", err)
		return
	}
	follows, _ := h.db.GetThreadFollows(c.UserID, []uuid.UUID{req.ThreadID})
	if _, ok := follows[req.ThreadID]; !ok {
		return // not following: nothing to clear
	}
	read := time.Now()
	stats, _ := h.db.GetThreadStats([]uuid.UUID{req.ThreadID}, c.UserID)
	serverID := uuid.Nil
	if ch, err := h.db.GetChannelByID(req.ChannelID); err == nil {
		serverID = ch.ServerID
	}
	sum := h.threadSummary(req.ThreadID, req.ChannelID, stats[req.ThreadID], &database.ThreadFollow{LastReadAt: &read}, c.UserID, serverID)
	_ = h.hub.SendToUser(c.UserID, protocol.EventThreadUpdate, sum)
}
