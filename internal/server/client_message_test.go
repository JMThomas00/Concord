package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
)

// A viewer's client code and its plugin exchange client_message events.
// Only someone who entered the pane can send one, Concord stamps who it's
// from, and every other event kind stays plugin-only.
func TestClientMessagesBetweenViewerCodeAndPlugin(t *testing.T) {
	r := newPaneTestRig(t)
	alice, c := r.member(t, "alice")
	clientMessage := func(data string) {
		raw, _ := json.Marshal(protocol.PluginClientMessagePayload{ChannelID: r.channel.ID, Data: json.RawMessage(data)})
		c.send(protocol.OpPluginEvent, protocol.PluginEventPayload{Kind: protocol.PluginEventClientMessage, Payload: raw})
	}

	clientMessage(`{"before":"enter"}`) // not a viewer yet: dropped
	c.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: r.channel.ID, Width: 80, Height: 24})
	var enter protocol.PluginPaneEnterPayload
	r.pluginReceives(t, protocol.EventPluginPaneEnter, &enter)

	clientMessage(`{"move":"e4"}`)
	var ev protocol.PluginEventPayload
	r.pluginReceives(t, protocol.EventPluginEvent, &ev)
	var got protocol.PluginClientMessagePayload
	_ = json.Unmarshal(ev.Payload, &got)
	if ev.Kind != protocol.PluginEventClientMessage || ev.ViewerID != alice.ID || got.ViewerName != "alice" || string(got.Data) != `{"move":"e4"}` {
		t.Fatalf("plugin got %+v / %+v (the message sent before Enter must not arrive)", ev, got)
	}

	// The plugin answers that viewer's code.
	r.pluginEvent(t, protocol.PluginEventClientMessage, alice.ID, protocol.PluginClientMessagePayload{ChannelID: r.channel.ID, Data: json.RawMessage(`{"ok":true}`)})
	m := c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginEvent })
	if !strings.Contains(string(m.Data), `"client_message"`) || !strings.Contains(string(m.Data), `{"ok":true}`) {
		t.Fatalf("viewer got %s", m.Data)
	}

	// Members can't send any other kind.
	c.send(protocol.OpPluginEvent, protocol.PluginEventPayload{Kind: protocol.PluginEventNotify, Payload: json.RawMessage(`{"content":"spoof"}`)})
	c.readUntil(5*time.Second, func(m *protocol.Message) bool { return strings.Contains(string(m.Data), "Only plugin connections") })

	// Oversized data is refused.
	clientMessage(`"` + strings.Repeat("x", protocol.MaxClientMessageBytes) + `"`)
	c.readUntil(5*time.Second, func(m *protocol.Message) bool { return strings.Contains(string(m.Data), "too large") })
}
