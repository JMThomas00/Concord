# The Concord plugin protocol

This is what a plugin process and a Concord server say to each other. Go
plugins use the SDK (`sdk/plugin`, `sdk/pane`, `sdk/table`) and never see
it; a plugin in any other language implements this page. The Go types in
`sdk/wire` are the authoritative definitions -- field names below are their
JSON names.

## Lifecycle

1. **Installing.** An admin installs a plugin in Server Settings → Plugins. That's a
   GitHub repo or a link to a `.zip` holding the plugin's folder: its
   program plus `plugin.toml` (the manifest: channel kinds, settings,
   entrypoint per OS). No server restart.
2. **Launching.** Concord starts the program with:
   - `CONCORD_WS_URL`: WebSocket to dial (loopback)
   - `CONCORD_PLUGIN_ID`: this install's id
   - `CONCORD_PLUGIN_TOKEN`: authenticates this process; new every launch
   - `CONCORD_PLUGIN_DATA_DIR`: a private folder for saved state, kept across
     updates and reinstalls

   Nothing else from the server's environment is passed through, except
   paths, locale, temp folders, TLS roots and proxies. When none of these
   are set, the program was started some other way. Run as a normal
   standalone app.
3. **Restarts.** Concord restarts it if it crashes (with backoff), stops it on
   disable/uninstall/update, and relaunches with a fresh token.

## Connecting

Every WebSocket message is a JSON envelope:

```json
{ "op": 10, "t": "PLUGIN_PANE_INPUT", "s": 42, "d": { ... } }
```

`op` is the opcode, `d` the payload, `t` the event name (dispatches only),
`s` a sequence number you can ignore.

1. Dial `CONCORD_WS_URL`. The server sends `op 12` (Hello) at once.
2. Send `op 0` (Identify): `{"token": "<CONCORD_PLUGIN_TOKEN>", "client_type": "plugin"}`.
3. Read until `op 13` (Ready): `{"session_id": "...", "user": {"id": ..., "username": ...}}`.
   That's your service account. `op 14` means the token was rejected: exit.
4. **Keepalive.** The server sends WebSocket pings every ~54s; answer them with
   pongs (most WebSocket libraries do this for you). There's no app-level
   heartbeat.
5. **Initial state.** Right after Ready you receive your settings, one `CHANNEL_UPDATE` per
   channel you own, and a `PLUGIN_PANE_ENTER` for everyone already viewing
   one of your panes. That's the same as a fresh viewer, so a restarted
   plugin repaints everyone.
6. **Reconnecting.** If the connection drops, reconnect with backoff (1s → 30s) and identify
   again with the same token.

Handle events in order. Don't block your read loop on slow work.

## Events you receive (`op 10`, by `t`)

| `t` | payload | when |
|---|---|---|
| `PLUGIN_CONFIG_UPDATE` | `{"plugins": [PluginInfo]}` | on connect and whenever an admin saves your settings. `config_values` has every value; `secret` fields come in plaintext (to you only) |
| `CHANNEL_UPDATE` | a Channel | each of your channels on connect, and when one is edited |
| `CHANNEL_CREATE` | a Channel + `plugin_config` | a channel of your kind was created |
| `CHANNEL_DELETE` | `{"channel_id", "server_id", "type"}` | one of your channels was deleted: drop its state |
| `PLUGIN_PANE_ENTER` | `{"channel_id", "viewer_id", "width", "height", "viewer_name", "viewer_display_name", "theme"}` | someone opened one of your panes |
| `PLUGIN_PANE_INPUT` | `{"channel_id", "viewer_id", "key_string", "runes", "viewer_name", ...}` | they pressed a key |
| `PLUGIN_PANE_RESIZE` | same fields as Enter | their pane changed size or theme |
| `PLUGIN_PANE_LEAVE` | `{"channel_id", "viewer_id"}` | they left or disconnected |
| `MESSAGE_CREATE` | `{"id", "channel_id", "author_id", "content", "reply_to_id", "author": User}` | a chat message in one of your channels, or, in any channel, one that @mentions you (see below) |
| `PLUGIN_EVENT` | `{"kind", "payload", ...}` | e.g. the reply to a `members` request |

A **Channel** has `id`, `server_id`, `name`, `topic`, `type` (5 = plugin),
`plugin_id`, `plugin_channel_kind`, and `plugin_config`, which holds its
`create_field` values, such as a seating mode.

**@mentions** in other channels reach you only when your settings include:

- `mention_enabled`: a `boolean` server_config_field, which an admin turns on;
- `mention_trigger`: a `text` field holding the name to answer to, so `@burt` matches trigger `burt` but `@burton` doesn't.

Messages you post yourself are never relayed back to you.

**Keys** (`key_string`) use Bubble Tea's names: single characters (`"a"`,
`"A"`, `"5"`, `" "`), `"enter"`, `"tab"`, `"esc"`, `"backspace"`, `"up"`,
`"down"`, `"left"`, `"right"`, `"home"`, `"end"`, `"pgup"`, `"pgdown"`,
`"delete"`, `"f1"`..., and modifiers as `"ctrl+c"` and `"alt+left"`. You
never receive **Ctrl+]**: Concord keeps it so a viewer can always get their
keyboard back. **Esc, Tab and Shift+Tab** reach you only while the
frame on screen claims them (`keys`, below); otherwise Concord uses them to
move focus between panels, as in every channel.

**Theme** (`theme`) holds the viewer's Concord colors so you can match them:

- `palette` maps `background`, `foreground`, `comment`, `selection`,
  `current_line`, `red`, `orange`, `yellow`, `green`, `cyan`, `purple` and
  `pink` to `#rrggbb` values or ANSI indexes.
- `color_profile` is `truecolor`, `ansi256`, `ansi` or `ascii`. Don't send
  richer colors than it allows.

## What you send

**Frames** (`op 53`): a whole screen of text for one viewer.

```json
{ "channel_id": "...", "viewer_id": "...", "frame": "line 1\nline 2", "seq": 17 }
```

- **Size.** Keep it within the viewer's `width` × `height`. A line too wide is wrapped
  by the client and garbles the screen.
- **Styling.** Only SGR styling (`ESC[...m`) and OSC 8 links survive; every
  other escape sequence is stripped.
- **`seq`.** It must increase on every frame you send to a viewer (a global counter is
  fine). The client drops a frame whose `seq` isn't higher.
- **Broadcast.** Leave `viewer_id` out to send the same frame to everyone viewing the
  channel, as a shared terminal does.
- **Who receives it.** Frames only reach users currently viewing one of *your* channels.
- **Claiming keys.** Add `"keys": ["esc"]` (any of `"esc"`, `"tab"`,
  `"shift+tab"`) to get those keys while this frame is on screen. Claim Esc
  only while there's something to cancel, Tab and Shift+Tab while a form's
  fields are open; unclaimed, Esc and Shift+Tab take the viewer back to the
  channel list and Tab on to the member list.
- **Pictures.** Add `"images": [{"asset", "col", "row", "cols", "rows"}]` to
  place pictures from your `client/` folder (below) over the frame, each in a
  box of cells (0-based, inside the pane). Each client draws them however its
  terminal can, down to colored half-blocks, or not at all if the viewer
  turned pictures off, so keep readable text under every box.

**Events** (`op 54`): `{"plugin_id": "<yours>", "kind": "...", "payload": {...}, "viewer_id": "..."}`.

| `kind` | payload | effect |
|---|---|---|
| `notify` | `{"content"}` | a line in the plugin's activity channel, if an admin set one |
| `notify_user` | `{"user_id", "channel_id", "content"}` | a toast and a badge on your channel for one member who can see it ("your turn") |
| `members` | `{"channel_id", "request_id"}` | asks who can see the channel; the reply is a `PLUGIN_EVENT` of kind `members` with `{"request_id", "members": [{"user_id", "username", "display_name", "online", "viewing"}]}` |
| `pane_title` | `{"channel_id", "title"}` | the pane's border title, for `viewer_id` or everyone if it's omitted. `""` restores the channel name |
| `leave_pane` | `{"channel_id"}` + `viewer_id` | hand that viewer's keyboard back to Concord. The pane stays open and keeps updating |
| `play_sound` | `{"channel_id", "asset", "volume"}` | plays a WAV (8/16-bit PCM) or Ogg Opus file from your `client/` folder for `viewer_id`, or everyone viewing the channel if it's omitted. `volume` is 0 to 1 (0 means 1), scaled by the member's own setting |
| `client_message` | `{"channel_id", "data"}` + `viewer_id` | sends `data` (any JSON, at most 64 KB) to that viewer's copy of your client code (below) |
| `game_result` | `{"game", "result", "computer", "reason"}` + `viewer_id` | how a finished game went for that player (`result` is `win`, `loss` or `draw`; `computer` if they played the computer). Their client counts wins towards its achievements. Sent while they're viewing your pane; the Go table kit sends it for you |
| anything else | anything | relayed as-is to `viewer_id`'s client, if they're viewing your pane |

**Chat**:

- `op 3` sends a message: `{"channel_id", "content", "reply_to_id"}`.
- `op 4` shows "typing…" and `op 60` clears it: `{"channel_id"}`.

**Your `client/` folder** holds the pictures and sounds above (`.png`,
`.jpg`, `.gif`, `.wav`, `.ogg`/`.opus`, `.json`; under 50 MB in total) and ships in your release zip
next to `plugin.toml`. Concord indexes it when the plugin loads and serves
each file to signed-in members at `GET /api/plugins/client/<plugin id>/<path>`.
Clients check every download against the SHA-256 Concord lists for it.
Asset paths are relative to `client/`, e.g. `"assets/king.png"`.

## Client code (WebAssembly)

Your `client/` folder can also hold code that runs inside each viewer's
client, next to your pane: `[client] wasm = "client/plugin.wasm"`, with the
`capabilities` it uses (`pane`, `images`, `sound`, `storage`, `server`) and
a `publisher_key` (`ed25519:<base64>`). Concord runs it only when:

- `<wasm>.sig` holds the base64 ed25519 signature of the module, made with
  that key (`concord-plugin sign`);
- the viewer agreed (they're shown the key's fingerprint and the capabilities).

It must be a `wasip1` command module (it runs `_start`) with no filesystem
or network. It gets 256 MB of memory, and 2 seconds to handle each event.

It talks to Concord through imports from module `concord`, all in JSON:

| Import | Does |
|---|---|
| `next_event(buf i32, cap i32) -> i32` | blocks for the next event and copies it into `buf`, returning its length. If that's more than `cap`, nothing is copied: call again with that much room. `-1` means stop: return from `_start` |
| `call(req i32, len i32) -> i32` | a host call, `{"fn": ..., ...}`. Negative is an error: `-1` bad request, `-2` capability not granted, `-3` unknown function, `-4` over a limit, `-5` not found. Otherwise it's the length of the result |
| `result(buf i32, cap i32) -> i32` | copies the last call's result into `buf` |

Events (`"type"`):

- `start`: `plugin_id`, `channel_id`, `viewer_id`, `width`, `height`, `theme`, `capabilities`
- `key`: `key` (Bubble Tea's name, as in pane input), `runes`
- `resize`: `width`, `height`, `theme`
- `server_frame`: `text`, `images` (what you sent this viewer)
- `server`: `data` (a `client_message` from you)
- `timer`: `id`

Calls (`"fn"`):

| `fn` | Fields | Needs |
|---|---|---|
| `log` | `text` | |
| `frame` | `text`, `images` | `pane` (`images` too for pictures) |
| `clear_frame` | | `pane` |
| `forward_keys` | `forward` (keys also go to you as pane input; default true) | `pane` |
| `claim_keys` | `keys` (of `"esc"`, `"tab"`, `"shift+tab"`; empty gives them back) while the code's frame shows | `pane` |
| `timer` | `id`, `ms` (16 ms to 1 h, 16 pending) | `pane` |
| `play_sound` | `asset`, `volume` | `sound` |
| `storage_get` | `key` → `{"value": string or null}` | `storage` |
| `storage_set` | `key`, `value` (null deletes; 1 MB per plugin) | `storage` |
| `send_server` | `data` (≤ 64 KB): arrives as `PLUGIN_EVENT` kind `client_message` with `viewer_id`, `viewer_name`, `viewer_display_name` | `server` |

## Settings

Declare settings in `plugin.toml`, never in files an admin edits:

- `[[server_config_field]]` for server-wide settings, shown in Server Settings → Plugins.
- `[[channel_kind.create_field]]` for per-channel settings, set when the channel is created or edited.

Each field has `key`, `label`, `type`, and optionally `options`, `default`,
`required` and `help`. The types:

- `text`, `number` and `boolean`
- `select`, with `options`
- `channel_select`, whose value is a channel's ID
- `secret`, for API keys: stored encrypted, admins see only whether it's
  set, and only you get the value. Server settings only.

Concord validates values against these declarations before saving, so you
can rely on them being well-formed.
