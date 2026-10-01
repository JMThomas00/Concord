# Client Code (WebAssembly in `client/`)

A plugin can ship code that runs **inside each viewer's Concord client**,
next to its pane. Most plugins don't need it: the server half
(`sdk/plugin`) does everything. Reach for client code when the server can't
do it well:

- **instant feedback**: keys handled with no round trip (cursor moves,
  menus, typing);
- **animation**: smooth, timer-driven frames drawn locally;
- **exact timing**: reaction games, rhythm, stopwatches;
- **remembering things on the viewer's computer** (preferences, personal bests).

Reference: `sdk/examples/reflex` (a reaction-time game: all play is local,
and the server half only keeps a leaderboard).

## The sandbox

Client code runs in wazero, in the viewer's client:

- No files, no network, no other programs. Only what the capabilities allow.
- 256 MB of memory at most.
- **2 seconds per event.** Code that doesn't come back for its next event in
  time is stopped. Never loop waiting; use `client.After` for anything
  periodic.
- Members are asked first. The prompt shows the plugin's name, its
  publisher key's fingerprint and what each capability allows. Their answer
  is remembered per plugin and key. A different key, or new capabilities,
  asks again. Members can turn plugin code off in Settings > Display > Plugin Code.
- If code isn't allowed or stops, the viewer sees the server half's frames.
  **Always make the server half draw something sensible**, even if it's
  only "allow this plugin's code to play".

## Manifest

```toml
[client]
wasm = "client/plugin.wasm"
capabilities = ["pane", "storage", "server"]   # only what you use
publisher_key = "ed25519:..."                  # from concord-plugin keygen
```

| Capability | Allows |
|---|---|
| `pane` | `Frame`, `ClearFrame`, `ForwardKeys`, `After`; key, resize and server-frame events |
| `images` | pictures in `Frame` (from `client/`, see [`media.md`](media.md)) |
| `sound` | `PlaySound` |
| `storage` | `Get`/`Set`/`Delete`: 1 MB per plugin and publisher, keys up to 256 bytes |
| `server` | `Send`, and `OnServer` messages from the server half |

## Signing

Concord only runs code signed by the manifest's `publisher_key`. The server
won't offer unsigned or mis-signed code, and clients check again.

```sh
concord-plugin keygen                   # once: writes publisher.key, prints the plugin.toml line
GOOS=wasip1 GOARCH=wasm go build -o client/plugin.wasm ./clientcode
concord-plugin sign client/plugin.wasm  # writes client/plugin.wasm.sig
```

- **Never commit `publisher.key`** (the scaffolder's `.gitignore` has it).
  Back it up: a new key makes every member agree again.
- For CI releases, store the key file's contents as the secret
  `CONCORD_PUBLISHER_KEY`, which `sign` reads. Build and sign the wasm
  before `release.go` packs `client/`.
- `sign` refuses a key that doesn't match `plugin.toml`.

## Code (`sdk/client`)

```go
package main // in its own directory, e.g. clientcode/

import "github.com/JMThomas00/Concord/sdk/client"

func main() {
	client.Run(client.Handler{
		OnStart:  func(e client.Event) { /* e.Width, e.Height, e.Theme, e.Capabilities */ },
		OnKey:    func(e client.Event) { /* e.Key: "enter", "a", "ctrl+c"; e.Runes */ },
		OnResize: func(e client.Event) {},
		OnServerFrame: func(e client.Event) { /* e.Text: what the server half drew */ },
		OnServer: func(data json.RawMessage) { /* from Conn.SendToClient */ },
		OnTimer:  func(id string) {},
	})
}
```

- `client.Frame(text, images...)` draws the pane. It replaces the server's
  frames until `client.ClearFrame()`. The same rules as server frames apply:
  SGR colors only, at most the pane's size.
- **Colors:** lipgloss can't detect a terminal inside WebAssembly, so it
  renders plain text. Write SGR codes yourself (`"\x1b[1;38;2;r;g;bm"`), using
  `e.Theme.Palette` from the start event to match the viewer's theme.
- `client.ForwardKeys(false)` stops keys going to the server half too. By
  default both get every key.
- `client.After(id, d)` gives a timer event: at least 16ms, 16 pending at most.
- `client.Send(v)` sends to the server half, which receives it in
  `plugin.Handler.OnClientMessage(c, viewer, m)` (with `m.ViewerName`). It
  answers with `c.SendToClient(channel, viewer, v)`. Messages are capped at 64 KB.
- Treat everything client code sends as **untrusted**: a member can run
  anything. The server half validates it (Reflex rejects impossible times).
- `client.Log(...)` writes to the viewer's Concord log.

## Testing

`sdk/client/clienttest` runs client code in ordinary `go test`:

```go
h := clienttest.New(t, handler(), "pane", "storage", "server")
h.Start(40, 12)
h.Key("enter")
h.Server(map[string]any{"top": ...})
h.Timer("spin")                    // fire a timer the code set
h.LastFrame(); h.Sent(); h.Storage(); h.Sounds(); h.Timers()
```

Every event call returns after the code has handled it. Capabilities are
enforced like Concord does. Don't use `t.Parallel` with it.

On the server half, `plugintest` has `srv.ClientMessage(viewer, data)` and
`srv.NextClientMessage()`.

## Other languages

Anything that compiles to `wasip1` can be client code; `sdk/PROTOCOL.md`
documents the host interface (module `concord`, JSON messages).
