package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// The plugin payloads are aliases of sdk/wire, so they can't drift. The
// SDK's slim mirrors of Concord's own types (opcodes, events, Channel,
// User, messages) can -- these tests hold them to what the server sends.

func TestWireOpcodesAndEventsMatchServer(t *testing.T) {
	ops := map[string][2]int{
		"Identify": {int(wire.OpIdentify), int(OpIdentify)}, "Heartbeat": {int(wire.OpHeartbeat), int(OpHeartbeat)},
		"SendMessage": {int(wire.OpSendMessage), int(OpSendMessage)}, "TypingStart": {int(wire.OpTypingStart), int(OpTypingStart)},
		"Dispatch": {int(wire.OpDispatch), int(OpDispatch)}, "HeartbeatAck": {int(wire.OpHeartbeatAck), int(OpHeartbeatAck)},
		"Hello": {int(wire.OpHello), int(OpHello)}, "Ready": {int(wire.OpReady), int(OpReady)},
		"InvalidSession": {int(wire.OpInvalid), int(OpInvalidSession)},
		"PluginPaneFrame": {int(wire.OpPluginPaneFrame), int(OpPluginPaneFrame)}, "PluginEvent": {int(wire.OpPluginEvent), int(OpPluginEvent)},
		"TypingStop": {int(wire.OpTypingStop), int(OpTypingStop)},
	}
	for name, v := range ops {
		if v[0] != v[1] {
			t.Errorf("Op%s: sdk/wire has %d, server has %d", name, v[0], v[1])
		}
	}
	events := map[wire.EventType]EventType{
		wire.EventPluginPaneEnter: EventPluginPaneEnter, wire.EventPluginPaneInput: EventPluginPaneInput,
		wire.EventPluginPaneResize: EventPluginPaneResize, wire.EventPluginPaneLeave: EventPluginPaneLeave,
		wire.EventPluginEvent: EventPluginEvent, wire.EventPluginConfigUpdate: EventPluginConfigUpdate,
		wire.EventChannelCreate: EventChannelCreate, wire.EventChannelUpdate: EventChannelUpdate,
		wire.EventChannelDelete: EventChannelDelete, wire.EventMessageCreate: EventMessageCreate,
	}
	for w, s := range events {
		if string(w) != string(s) {
			t.Errorf("event %q in sdk/wire is %q on the server", w, s)
		}
	}
	if wire.ChannelTypePlugin != int(models.ChannelTypePlugin) {
		t.Errorf("wire.ChannelTypePlugin = %d, server's is %d", wire.ChannelTypePlugin, models.ChannelTypePlugin)
	}
}

// roundTrip marshals what the server sends and decodes it the way a plugin
// built on the SDK would.
func roundTrip(t *testing.T, server interface{}, plugin interface{}) {
	t.Helper()
	raw, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, plugin); err != nil {
		t.Fatalf("SDK can't decode %s: %v", raw, err)
	}
}

func TestWireDecodesWhatTheServerSends(t *testing.T) {
	ch := models.NewPluginChannel(uuid.New(), "chess-hall", "concord-chess", "table")
	ch.Topic = "Casual games"

	var created wire.ChannelCreatePayload
	roundTrip(t, ChannelCreatePayload{Channel: ch, PluginConfig: map[string]string{"seating": "seats"}}, &created)
	if created.Channel == nil || created.ID != ch.ID || created.ServerID != ch.ServerID || created.Name != ch.Name ||
		created.Topic != ch.Topic || created.Type != wire.ChannelTypePlugin || created.PluginID != "concord-chess" ||
		created.PluginChannelKind != "table" || created.PluginConfig["seating"] != "seats" {
		t.Errorf("CHANNEL_CREATE decoded as %+v / %+v", created, created.Channel)
	}

	var updated wire.ChannelUpdatePayload
	ch.PluginConfig = map[string]string{"seating": "private"}
	roundTrip(t, ChannelUpdatePayload{Channel: ch}, &updated)
	if updated.Channel == nil || updated.ID != ch.ID || updated.PluginConfig["seating"] != "private" {
		t.Errorf("CHANNEL_UPDATE decoded as %+v", updated.Channel)
	}

	var deleted wire.ChannelDeletePayload
	roundTrip(t, ChannelDeletePayload{ChannelID: ch.ID, ServerID: ch.ServerID, Type: ch.Type}, &deleted)
	if deleted.ChannelID != ch.ID || deleted.Type != wire.ChannelTypePlugin {
		t.Errorf("CHANNEL_DELETE decoded as %+v", deleted)
	}

	author := models.NewUser("alex", "alex@example.com")
	author.DisplayName = "Alex"
	reply := uuid.New()
	msg := &models.Message{ID: uuid.New(), ChannelID: ch.ID, AuthorID: author.ID, Content: "@mynah hi", ReplyToID: &reply, CreatedAt: time.Now()}
	var mc wire.MessageCreatePayload
	roundTrip(t, MessageCreatePayload{Message: msg, Author: author}, &mc)
	if mc.ChatMessage == nil || mc.ID != msg.ID || mc.ChannelID != ch.ID || mc.AuthorID != author.ID || mc.Content != msg.Content ||
		mc.ReplyToID == nil || *mc.ReplyToID != reply || mc.Author == nil || mc.Author.Name() != "Alex" || mc.Author.Username != "alex" {
		t.Errorf("MESSAGE_CREATE decoded as %+v / %+v", mc.ChatMessage, mc.Author)
	}

	var ready wire.ReadyPayload
	roundTrip(t, ReadyPayload{SessionID: "s1", User: author}, &ready)
	if ready.SessionID != "s1" || ready.User == nil || ready.User.ID != author.ID {
		t.Errorf("READY decoded as %+v", ready)
	}

	// And the other way: what an SDK plugin sends, the server decodes.
	var ident IdentifyPayload
	roundTrip(t, wire.IdentifyPayload{Token: "tok", ClientType: wire.ClientTypePlugin}, &ident)
	if ident.Token != "tok" || ident.ClientType != "plugin" {
		t.Errorf("server decoded Identify as %+v", ident)
	}
	var send SendMessagePayload
	roundTrip(t, wire.SendMessagePayload{ChannelID: ch.ID, Content: "hello", ReplyToID: &reply}, &send)
	if send.ChannelID != ch.ID || send.Content != "hello" || send.ReplyToID == nil || *send.ReplyToID != reply {
		t.Errorf("server decoded SendMessage as %+v", send)
	}
	roundTrip(t, wire.SendMessagePayload{ChannelID: ch.ID, Content: "hi", Nonce: "p1", Stream: wire.StreamWriting}, &send)
	if send.Nonce != "p1" || send.Stream != StreamWriting {
		t.Errorf("server decoded a streamed SendMessage as %+v", send)
	}
	var edit EditMessagePayload
	roundTrip(t, wire.EditMessagePayload{MessageID: msg.ID, ChannelID: ch.ID, Content: "more", Stream: wire.StreamDone}, &edit)
	if edit.MessageID != msg.ID || edit.ChannelID != ch.ID || edit.Content != "more" || edit.Stream != StreamDone {
		t.Errorf("server decoded EditMessage as %+v", edit)
	}
	if int(wire.OpEditMessage) != int(OpEditMessage) {
		t.Errorf("OpEditMessage: SDK %d, server %d", wire.OpEditMessage, OpEditMessage)
	}
	var echo wire.MessageCreatePayload
	roundTrip(t, MessageCreatePayload{Message: msg, Author: author, Nonce: "p1", Stream: StreamWriting}, &echo)
	if echo.Nonce != "p1" || echo.Stream != wire.StreamWriting {
		t.Errorf("MESSAGE_CREATE echo decoded as nonce %q stream %q", echo.Nonce, echo.Stream)
	}

	var envelope Message
	wmsg, _ := wire.NewMessage(wire.OpPluginPaneFrame, wire.PluginPaneFramePayload{ChannelID: ch.ID, Frame: "x", Seq: 1})
	roundTrip(t, wmsg, &envelope)
	if envelope.Op != OpPluginPaneFrame || len(envelope.Data) == 0 {
		t.Errorf("server decoded the SDK envelope as %+v", envelope)
	}
}
