// codeguest is client code for plugin_code_test.go, built with
// GOOS=wasip1 GOARCH=wasm. Each key exercises one part of the host.
package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/JMThomas00/Concord/sdk/client"
	"github.com/JMThomas00/Concord/sdk/wire"
)

func main() {
	client.Run(client.Handler{
		OnStart: func(e client.Event) {
			client.Log("started %s", e.PluginID)
			_ = client.Frame(fmt.Sprintf("hello %dx%d %v", e.Width, e.Height, e.Capabilities))
		},
		OnResize: func(e client.Event) { _ = client.Frame(fmt.Sprintf("size %dx%d", e.Width, e.Height)) },
		OnKey: func(e client.Event) {
			switch e.Key {
			case "spin":
				for {
				}
			case "send":
				_ = client.Send(map[string]string{"from": "code"})
			case "sound":
				_ = client.PlaySound("sounds/a.wav", 0.5)
			case "image":
				_ = client.Frame("pic", wire.PaneImage{Asset: "pics/x.png", Cols: 2, Rows: 1})
			case "badimage":
				err := client.Frame("pic", wire.PaneImage{Asset: "nope.png", Cols: 2, Rows: 1})
				_ = client.Frame(fmt.Sprint("badimage: ", err))
			case "timer":
				_ = client.After("t1", 20*time.Millisecond)
			case "set":
				_ = client.Set("k", "remembered")
			case "get":
				v, ok, err := client.Get("k")
				_ = client.Frame(fmt.Sprintf("got %q %v %v", v, ok, err))
			case "denied":
				err := client.Send("x")
				_ = client.Frame(fmt.Sprint("denied: ", err))
			case "local":
				_ = client.ForwardKeys(false)
			case "clear":
				_ = client.ClearFrame()
			case "burst":
				for i := 0; i < 200; i++ {
					_ = client.Frame(fmt.Sprint("burst ", i))
				}
			}
		},
		OnTimer:       func(id string) { _ = client.Frame("tick " + id) },
		OnServerFrame: func(e client.Event) { _ = client.Frame("saw " + e.Text) },
		OnServer: func(data json.RawMessage) {
			_ = client.Frame("server " + string(data))
		},
	})
}
