package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// pluginGetsMessage reports whether the fake plugin is sent a
// MESSAGE_CREATE with content within a short wait.
func pluginGetsMessage(r *paneTestRig, content string) bool {
	deadline := time.After(700 * time.Millisecond)
	for {
		select {
		case m := <-r.plugin.send:
			if m.Type != protocol.EventMessageCreate {
				continue
			}
			var p protocol.MessageCreatePayload
			if json.Unmarshal(m.Data, &p) == nil && p.Message != nil && p.Message.Content == content {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// @mentions reach a plugin only in the channels its mention_channels
// setting lists (all of them when it's empty), and with no trigger word
// set it answers to its own name.
func TestMentionRelayHonorsChannelsAndDefaultsToTheName(t *testing.T) {
	r := newPaneTestRig(t)
	general, err := findChannelByName(r.srv, r.server.ID, "general")
	if err != nil || general == nil {
		t.Fatalf("no general channel: %v", err)
	}
	random := models.NewTextChannel(r.server.ID, "random")
	if err := r.srv.db.CreateChannel(random); err != nil {
		t.Fatal(err)
	}
	_, alice := r.member(t, "alice")

	post := func(ch *models.Channel, content string) {
		alice.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: ch.ID, Content: content})
	}
	set := func(key, value string) {
		if err := r.srv.db.SetPluginServerConfig("HelloPlugin", key, value); err != nil {
			t.Fatal(err)
		}
	}

	set("mention_enabled", "true")
	post(general, "@Hello Plugin what's up?")
	if !pluginGetsMessage(r, "@Hello Plugin what's up?") {
		t.Fatal("with no trigger set, @<plugin name> didn't reach the plugin")
	}

	set("mention_trigger", "hello")
	set("mention_channels", general.ID.String())
	post(random, "@hello in random")
	if pluginGetsMessage(r, "@hello in random") {
		t.Fatal("a mention outside the plugin's channels reached it")
	}
	post(general, "@hello in general")
	if !pluginGetsMessage(r, "@hello in general") {
		t.Fatal("a mention in an allowed channel didn't reach the plugin")
	}

	set("mention_channels", "")
	post(random, "@hello anywhere")
	if !pluginGetsMessage(r, "@hello anywhere") {
		t.Fatal("an empty channel list should allow every channel")
	}
}
