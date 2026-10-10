package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// One account signed in on two computers (the 2026-09-28 bug: an install
// result and new channels only reached whichever computer connected last).
// A message sent from one reaches both.
func TestSameAccountOnTwoDevicesBothReceive(t *testing.T) {
	r := newPaneTestRig(t)
	user, laptop := r.member(t, "twice")
	desktop := r.secondDevice(t, user.ID)

	general, err := findChannelByName(r.srv, r.server.ID, "general")
	if err != nil || general == nil {
		t.Fatalf("no general channel: %v", err)
	}
	laptop.send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: general.ID, Content: "hello from the laptop"})
	for name, c := range map[string]*testWSClient{"laptop": laptop, "desktop": desktop} {
		m := c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventMessageCreate })
		if !json.Valid(m.Data) {
			t.Fatalf("%s: bad MESSAGE_CREATE", name)
		}
	}
}

// A pane follows the device that opened it last: frames go there, the
// earlier device gets its keyboard handed back and stops getting frames,
// and closing the earlier device doesn't tell the plugin the user left.
func TestPaneMovesToTheDeviceThatOpenedItLast(t *testing.T) {
	r := newPaneTestRig(t)
	alice, laptop := r.member(t, "alice")
	desktop := r.secondDevice(t, alice.ID)

	laptop.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: r.channel.ID, Width: 80, Height: 24})
	var enter protocol.PluginPaneEnterPayload
	r.pluginReceives(t, protocol.EventPluginPaneEnter, &enter)
	r.pushFrame(t, protocol.PluginPaneFramePayload{ChannelID: r.channel.ID, ViewerID: alice.ID, Frame: "on the laptop"})
	laptop.readUntil(5*time.Second, isFrame)

	desktop.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: r.channel.ID, Width: 120, Height: 40})
	r.pluginReceives(t, protocol.EventPluginPaneEnter, &enter)
	if enter.Width != 120 {
		t.Fatalf("plugin got Enter at %dx%d, want the desktop's 120x40", enter.Width, enter.Height)
	}
	laptop.readUntil(5*time.Second, func(m *protocol.Message) bool {
		var ev protocol.PluginEventPayload
		return m.Type == protocol.EventPluginEvent && json.Unmarshal(m.Data, &ev) == nil && ev.Kind == protocol.PluginEventLeavePane
	})

	r.pushFrame(t, protocol.PluginPaneFramePayload{ChannelID: r.channel.ID, ViewerID: alice.ID, Frame: "on the desktop"})
	got := desktop.readUntil(5*time.Second, isFrame)
	var frame protocol.PluginPaneFramePayload
	_ = json.Unmarshal(got.Data, &frame)
	if frame.Frame != "on the desktop" {
		t.Fatalf("desktop got frame %q", frame.Frame)
	}
	expectNoFrameBefore(t, r, alice.ID, laptop)

	// Keys from the laptop no longer reach the plugin.
	laptop.send(protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{ChannelID: r.channel.ID, KeyString: "x"})
	desktop.send(protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{ChannelID: r.channel.ID, KeyString: "y"})
	var in protocol.PluginPaneInputPayload
	r.pluginReceives(t, protocol.EventPluginPaneInput, &in)
	if in.KeyString != "y" {
		t.Fatalf("plugin got key %q from the laptop, which no longer has the pane", in.KeyString)
	}

	// The laptop closing isn't alice leaving; the desktop closing is.
	laptop.conn.Close()
	desktop.conn.Close()
	var leave protocol.PluginPaneLeavePayload
	r.pluginReceives(t, protocol.EventPluginPaneLeave, &leave)
	if leave.ViewerID != alice.ID {
		t.Fatalf("leave for %v", leave.ViewerID)
	}
	select {
	case m := <-r.plugin.send:
		if m.Type == protocol.EventPluginPaneLeave {
			t.Fatal("plugin was told alice left twice")
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// secondDevice signs userID in again, with its own session, as a second
// computer would.
func (r *paneTestRig) secondDevice(t *testing.T, userID uuid.UUID) *testWSClient {
	t.Helper()
	token, err := r.srv.handlers.CreateAuthToken(userID, "127.0.0.2", "second device")
	if err != nil {
		t.Fatal(err)
	}
	c := newTestWSClient(t, r.wsURL)
	c.identify(token)
	return c
}
