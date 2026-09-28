// Command testplugin is the minimal "hello plugin" used to verify the
// plugin platform end-to-end, and the smallest example of a plugin built
// on the SDK (sdk/plugin). It:
//   - posts one activity notification each time it connects ("notify")
//   - implements one zero-field remote-pane channel kind: a per-viewer
//     keypress counter that greets each viewer by name, proving the
//     render/input relay round-trips; "q" hands the keyboard back
//
// Its companion plugin.toml (internal/plugins/testdata/HelloPlugin) declares
// the "counter" channel kind and points [process.entrypoint.*] at this binary.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

func main() {
	cfg, ok := plugin.ConfigFromEnv()
	if !ok {
		log.Fatal("testplugin only runs under Concord (CONCORD_WS_URL / CONCORD_PLUGIN_TOKEN aren't set)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Handler callbacks run one at a time, so this map needs no lock.
	type viewer struct {
		name  string
		count int
	}
	viewers := map[uuid.UUID]*viewer{}
	draw := func(c *plugin.Conn, channelID, viewerID uuid.UUID) {
		if v := viewers[viewerID]; v != nil {
			if err := c.Frame(channelID, viewerID, renderCounter(v.name, v.count)); err != nil {
				log.Printf("failed to push frame: %v", err)
			}
		}
	}

	err := plugin.Run(ctx, cfg, plugin.Handler{
		OnReady: func(c *plugin.Conn, _ *wire.User) {
			log.Println("identified successfully, sending startup notification")
			_ = c.Notify("Hello plugin is online.")
		},
		OnEnter: func(c *plugin.Conn, e wire.PluginPaneEnterPayload) {
			viewers[e.ViewerID] = &viewer{name: e.ViewerDisplayName}
			draw(c, e.ChannelID, e.ViewerID)
		},
		OnInput: func(c *plugin.Conn, e wire.PluginPaneInputPayload) {
			if e.KeyString == "q" {
				_ = c.LeavePane(e.ChannelID, e.ViewerID)
				return
			}
			if v := viewers[e.ViewerID]; v != nil {
				v.count++
			}
			draw(c, e.ChannelID, e.ViewerID)
		},
		OnResize: func(c *plugin.Conn, e wire.PluginPaneResizePayload) {
			draw(c, e.ChannelID, e.ViewerID)
		},
		OnLeave: func(_ *plugin.Conn, e wire.PluginPaneLeavePayload) {
			delete(viewers, e.ViewerID)
		},
	})
	if err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func renderCounter(name string, count int) string {
	return "Hello, " + name + "! Keypresses seen: " + strconv.Itoa(count) + "\n(press any key; q hands keys back to Concord)"
}
