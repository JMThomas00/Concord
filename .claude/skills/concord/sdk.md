# The Plugin SDK

Module `github.com/JMThomas00/Concord/sdk` (tags `sdk/vX.Y.Z`; Go's module
path resolves them, so `go get github.com/JMThomas00/Concord/sdk@latest`
works). Terminal passthrough is a separate module,
`github.com/JMThomas00/Concord/sdk/pty`.

| Package | Use it for |
|---|---|
| `sdk/plugin` | connecting, events, and sending: every plugin |
| `sdk/pane` | running Bubble Tea models as panes, one per viewer |
| `sdk/table` | turn-based games (see [`games.md`](games.md)) |
| `sdk/pty` | an existing terminal program in a channel (see [`passthrough.md`](passthrough.md)) |
| `sdk/plugintest` | a fake Concord server for tests (see [`testing.md`](testing.md)) |
| `sdk/wire` | the JSON types; you rarely need them directly |

## `plugin`: the runtime

```go
cfg, underConcord := plugin.ConfigFromEnv() // false: run standalone
err := plugin.Run(ctx, cfg, plugin.Handler{ ... })
```

`Run` connects, identifies, reconnects with backoff after a drop, and returns
when `ctx` ends or the server rejects the token (`plugin.ErrRejected`: the
plugin was disabled or reinstalled; exit, Concord restarts it if needed).

**Callbacks run one at a time, in order, on one goroutine.** Plugin state
touched only from callbacks needs no locks. The flip side: never block in a
callback. Do slow work (HTTP, an AI search, `RequestMembers`) in a goroutine
and hand the result back with `c.Post(func() { ... })`, which runs it on the
callback goroutine.

`Handler` fields (all optional):

| Callback | When |
|---|---|
| `OnReady(c, self)` | after each (re)connect; `self` is the plugin's own account |
| `OnConfig(c, info)` | server settings on connect and on every admin save; `info.ConfigValues` includes secrets |
| `OnChannel(c, ch)` | each of the plugin's channels on connect, then on create/edit; `ch.PluginConfig` holds its `create_field` values |
| `OnChannelDelete(c, e)` | one of its channels was deleted: drop its state |
| `OnMessage(c, m)` | a chat message in one of its channels, or an @mention anywhere. **Ignore its own**: `m.AuthorID == c.Self().ID` |
| `OnEnter/OnInput/OnResize/OnLeave` | remote-pane viewers (use `pane.Host` instead of handling these yourself) |
| `OnEvent(c, e)` | any other `PLUGIN_EVENT` |
| `OnDisconnect(err)` | the connection dropped (Run reconnects) |

`Conn` helpers:

| Method | Does |
|---|---|
| `SendMessage(channel, text, replyTo)` | posts chat as the plugin (markdown works; at most 2000 bytes) |
| `PostMessage(ctx, channel, text, replyTo)` | the same, but **blocks** until saved and returns the message ID |
| `EditMessage(channel, id, text)` | changes one of the plugin's own messages |
| `Stream(ctx, channel, replyTo)` | a reply written as it's produced (LLM tokens): see below |
| `Typing(channel, true)` | typing indicator; lasts ~5s, so **re-send every ~3s** during slow work |
| `Frame(channel, viewer, s)` / `Broadcast(channel, s)` | a rendered screen to one viewer / all viewers |
| `Notify(text)` | message in the admin-configured notification channel |
| `NotifyUser(user, channel, text)` | toast + unread badge for one member who can see the channel |
| `SetTitle(channel, viewer, title)` | the pane's border title |
| `LeavePane(channel, viewer)` | hand that viewer's keyboard back to Concord |
| `RequestMembers(ctx, channel)` | who can see the channel, online, viewing. **Blocks**: call from a goroutine |
| `Post(fn)` | run fn on the callback goroutine |
| `DataDir()` | private folder for the plugin's files; survives updates and reinstalls |
| `Self()`, `PluginID()` | the plugin's account and install ID |

## Streaming a reply

For anything that produces text over time (an LLM), stream it rather than
posting once at the end: members see the reply grow with a cursor, and it
isn't marked "edited".

```go
go func() { // Stream blocks on the network: never inside a callback
	s := c.Stream(ctx, m.ChannelID, &m.ID)
	for token := range tokens {
		s.Write(token)
	}
	s.Close()
}()
```

- The first piece appears at once; later ones are batched into an edit
  every `s.Interval` (400ms). Write pieces of any size.
- Markdown renders correctly at every step: Concord treats an unfinished code
  block (or `` ` `` / `**`) as closed until the rest arrives.
- A reply over 2000 bytes continues in a new message, split between
  paragraphs; a code block cut in two is closed and reopened.
- `Close` on an empty stream posts nothing. Check `Write`/`Close` errors (the
  connection can drop mid-reply).
- In tests, `plugintest.Server.Posted()` and `WaitStreamDone(id)` show the
  messages as they end up.

## `pane`: Bubble Tea models as panes

```go
var host *pane.Host
host = pane.NewHost(func(v *pane.Viewer) tea.Model { return newModel(v) })
h := host.Handler()          // sets OnEnter/OnInput/OnResize/OnLeave
h.OnConfig = func(...) {...} // add others; don't overwrite the four above
plugin.Run(ctx, cfg, h)
```

- Each viewer gets **their own model**: own cursor, scroll, selection. State
  everyone shares lives outside the models; after changing it, call
  `host.Broadcast(channelID, someMsg{})` so every viewer's model re-renders.
  Call Host methods only from callbacks (or via `Post`).
- Models receive `tea.WindowSizeMsg` before the first render and on every
  resize, and `tea.KeyMsg` for keys. There's no mouse.
- Returning `tea.Quit` hands that viewer's keyboard back to Concord (the
  pane stays visible). Concord itself always reserves **Ctrl+]** for this.
- Frames are fitted to the pane (`pane.Fit`), rendered in true color and
  downsampled to each viewer's terminal. Cursor-blink commands are skipped.
- `v.Color("red", fallback)` returns the viewer's Concord theme color, so
  panes match their theme. Names: `foreground`, `comment`, `red`, `orange`,
  `yellow`, `green`, `cyan`, `purple`, `pink`, `current_line`, `selection`,
  `background`.
- `v.DisplayName` is how the viewer appears on the server; `v.ID` is stable.

Only SGR colors/styles and OSC 8 links survive to the screen; Concord strips
every other escape sequence, so no cursor movement, alt screen or clipboard.

## Writing to disk

Use `c.DataDir()` (from `CONCORD_PLUGIN_DATA_DIR`) for everything the plugin
saves. The plugin's own folder is replaced on update; the data folder never
is. Standalone, pick a folder under the user's config dir instead.
