// Command testplugin is the minimal "hello plugin" used to verify the
// plugin platform end-to-end without needing Tukan built yet. It:
//   - identifies to Concord as a plugin (OpIdentify, ClientType "plugin")
//   - posts one activity notification on startup (OpPluginEvent, kind "notify")
//   - implements one zero-field remote-pane channel kind: a per-viewer
//     keypress counter, proving the render/input relay round-trips
//
// Its companion plugin.toml (in the same folder) declares the "counter"
// channel kind and points [process.entrypoint.*] at this binary.
package main

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/concord-chat/concord/internal/protocol"
)

func main() {
	wsURL := os.Getenv("CONCORD_WS_URL")
	pluginID := os.Getenv("CONCORD_PLUGIN_ID")
	token := os.Getenv("CONCORD_PLUGIN_TOKEN")
	if wsURL == "" || pluginID == "" || token == "" {
		log.Fatal("testplugin requires CONCORD_WS_URL, CONCORD_PLUGIN_ID, CONCORD_PLUGIN_TOKEN")
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Fatalf("failed to connect to %s: %v", wsURL, err)
	}
	defer conn.Close()

	p := &plugin{conn: conn, pluginID: pluginID, counters: make(map[uuid.UUID]int)}

	identify := protocol.IdentifyPayload{Token: token, ClientType: "plugin"}
	if err := p.send(protocol.OpIdentify, identify); err != nil {
		log.Fatalf("failed to send identify: %v", err)
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("connection closed: %v", err)
			return
		}
		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		p.handle(&msg)
	}
}

type plugin struct {
	conn     *websocket.Conn
	pluginID string
	mu       sync.Mutex
	counters map[uuid.UUID]int
}

func (p *plugin) send(op protocol.OpCode, data interface{}) error {
	msg, err := protocol.NewMessage(op, data)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.WriteMessage(websocket.TextMessage, raw)
}

func (p *plugin) handle(msg *protocol.Message) {
	// READY is sent as a plain OpReady message (not a Dispatch), so it's
	// identified by Op, not Type — every event after it arrives via
	// OpDispatch and is identified by Type instead.
	if msg.Op == protocol.OpReady {
		log.Println("identified successfully, sending startup notification")
		p.notify("Hello plugin is online.")
		return
	}

	switch msg.Type {
	case protocol.EventPluginPaneEnter:
		var payload protocol.PluginPaneEnterPayload
		if json.Unmarshal(msg.Data, &payload) == nil {
			p.mu.Lock()
			p.counters[payload.ViewerID] = 0
			p.mu.Unlock()
			p.renderFrame(payload.ChannelID, payload.ViewerID)
		}

	case protocol.EventPluginPaneInput:
		var payload protocol.PluginPaneInputPayload
		if json.Unmarshal(msg.Data, &payload) == nil {
			p.mu.Lock()
			p.counters[payload.ViewerID]++
			p.mu.Unlock()
			p.renderFrame(payload.ChannelID, payload.ViewerID)
		}

	case protocol.EventPluginPaneResize:
		var payload protocol.PluginPaneResizePayload
		if json.Unmarshal(msg.Data, &payload) == nil {
			p.renderFrame(payload.ChannelID, payload.ViewerID)
		}

	case protocol.EventPluginPaneLeave:
		var payload protocol.PluginPaneLeavePayload
		if json.Unmarshal(msg.Data, &payload) == nil {
			p.mu.Lock()
			delete(p.counters, payload.ViewerID)
			p.mu.Unlock()
		}
	}
}

func (p *plugin) renderFrame(channelID, viewerID uuid.UUID) {
	p.mu.Lock()
	count := p.counters[viewerID]
	p.mu.Unlock()

	frame := protocol.PluginPaneFramePayload{
		ChannelID: channelID,
		ViewerID:  viewerID,
		Frame:     renderCounter(count),
		Seq:       int64(count),
	}
	if err := p.send(protocol.OpPluginPaneFrame, frame); err != nil {
		log.Printf("failed to push frame: %v", err)
	}
}

func renderCounter(count int) string {
	return "Hello Plugin — keypresses seen: " + strconv.Itoa(count) + "\n(press any key)"
}

func (p *plugin) notify(content string) {
	payload := protocol.PluginNotifyEventPayload{Content: content}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	event := protocol.PluginEventPayload{
		PluginID: p.pluginID,
		Kind:     "notify",
		Payload:  raw,
	}
	if err := p.send(protocol.OpPluginEvent, event); err != nil {
		log.Printf("failed to send notify event: %v", err)
	}
}
