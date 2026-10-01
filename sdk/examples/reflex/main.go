// Command reflex is the smallest plugin with client code: a reaction-time
// game whose play happens entirely in each viewer's Concord client (see
// clientcode/), with this server half keeping the channel's leaderboard.
// Viewers who don't run the code see what this half draws.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"

	"github.com/google/uuid"

	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/wire"
)

type entry struct {
	Name string `json:"name"`
	MS   int    `json:"ms"`
}

type viewer struct{ channel, id uuid.UUID }

func main() {
	cfg, underConcord := plugin.ConfigFromEnv()
	if !underConcord {
		fmt.Println("reflex runs inside Concord: install it on a server, then open its channel.")
		return
	}
	best := map[uuid.UUID]map[string]int{} // channel → name → best ms
	viewers := map[viewer]bool{}
	top := func(ch uuid.UUID) []entry {
		var out []entry
		for name, ms := range best[ch] {
			out = append(out, entry{name, ms})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].MS < out[j].MS })
		return out[:min(len(out), 5)]
	}
	h := plugin.Handler{
		OnEnter: func(c *plugin.Conn, e wire.PluginPaneEnterPayload) {
			viewers[viewer{e.ChannelID, e.ViewerID}] = true
			_ = c.Frame(e.ChannelID, e.ViewerID, "Reflex runs on your computer.\n\nAllow its code to play (or turn plugin code on in\nSettings > Display > Plugin Code).")
			_ = c.SendToClient(e.ChannelID, e.ViewerID, map[string]any{"top": top(e.ChannelID)})
		},
		OnLeave: func(c *plugin.Conn, e wire.PluginPaneLeavePayload) {
			delete(viewers, viewer{e.ChannelID, e.ViewerID})
		},
		OnClientMessage: func(c *plugin.Conn, from uuid.UUID, m wire.PluginClientMessagePayload) {
			var score struct{ MS int }
			if json.Unmarshal(m.Data, &score) != nil || score.MS <= 0 || score.MS > 60000 {
				return
			}
			name := m.ViewerDisplayName
			if best[m.ChannelID] == nil {
				best[m.ChannelID] = map[string]int{}
			}
			if old, ok := best[m.ChannelID][name]; !ok || score.MS < old {
				best[m.ChannelID][name] = score.MS
			}
			board := map[string]any{"top": top(m.ChannelID)}
			for v := range viewers {
				if v.channel == m.ChannelID {
					_ = c.SendToClient(v.channel, v.id, board)
				}
			}
		},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := plugin.Run(ctx, cfg, h); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
