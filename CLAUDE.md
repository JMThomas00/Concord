# Concord — Claude Code Reference

**A terminal-based chat application inspired by Discord**
**Last Updated:** 2026-09-06

---

## Project Overview

Concord is a self-hosted, terminal-first chat platform built in Go. Each server is independently hosted (IRC-style decentralization). The client is a full TUI application built on the Charmbracelet stack. Voice channels use WebRTC P2P audio with Opus encoding, and an out-of-process plugin platform lets external programs (bots, integrations) attach to a server as privileged clients.

**Module path:** `github.com/concord-chat/concord`
**Current version:** v0.1.0 (pre-release, final QA; plugin platform live)

---

## Development Commands

This repo has no CI config and no `.golangci.yml` — the commands below are the actual gate used session-to-session (see the Makefile's `test`/`fmt`/`lint` targets for the same thing in `make` form).

```bash
# Full check — run this before considering any change done. The server, models,
# protocol, database, and plugin packages have no CGO dependency; -tags novoice
# additionally makes the client build/test without the voice CGO toolchain, which
# is what this environment normally has available.
go build -tags novoice ./...
go vet -tags novoice ./...
go test -tags novoice -count=1 ./...

# Single package or test
go test -tags novoice ./internal/server/...
go test -tags novoice -run TestChannelOverwriteEndToEndOverWebSocket ./internal/server/... -v

# Race detector — needs a real C toolchain (gcc) on PATH; not always available here.
go test -race -tags novoice -count=1 ./...

# Lint (golangci-lint, installed on demand by the Makefile target)
make lint
```

Only the **client**'s voice engine needs CGO (`malgo`/`opus`); server, hub, database, models, protocol, and plugins are pure Go and build/test fine with plain `go build`/`go test` too — `-tags novoice` is there so the client package compiles in the same command without requiring MSYS2 GCC on `PATH`.

---

## Build System

**Concord ships exactly three binaries, always:** `concord-server.exe`, `concord-client.exe`, `concord-hub.exe`. Voice is a **foundational feature of the client, not an optional variant** — `concord-client.exe` always includes it. There is no supported voice/novoice split as two parallel products; don't reintroduce one.

**`make build-windows`** is the canonical build on this platform — produces all three binaries, client with voice (requires MSYS2's `clang` on `PATH`: `pacman -S mingw-w64-x86_64-clang`). `make build-windows-novoice` exists only as a fallback for a machine with no C toolchain (CI, a fresh dev box); its client output is always suffixed `-novoice` and is never deployed as `concord-client.exe`.

**Why clang, not gcc:** MSYS2's `mingw-w64-x86_64-gcc` 16.2.0 has a real, reproducible internal-compiler-error/segfault bug compiling `miniaudio.c` (the `malgo`/voice dependency) — measured at a ~40-70% failure rate via repeated back-to-back compiles of the identical file (a different crash site every time). Confirmed not to be hardware or load-related: a fully serialized build crashed in 22 seconds compiling a trivial standard Go runtime file, which rules out sustained-load/thermal causes. `clang` (`mingw-w64-x86_64-clang`) builds the same code with a near-zero failure rate and is what `build-client`/`build-windows` now use via `CC=clang`. GCC 15.2.0 is **not** a safe fallback either — it has a different, 100%-reproducible bug on the same file. Full investigation trail: vault note "Concord - Voice Client Build Toolchain Bug" (2026-09-06). Revisit this once a GCC point release actually fixes it — don't silently drop `CC=clang` from the Makefile without re-running that investigation's repeated-compile test first.

**Voice build tag (implementation detail, not a release axis):** the client uses a `novoice` build tag internally so it can still compile without CGO when needed (see `-novoice` fallback above). Without it (the default), voice is compiled in:
- `voice_engine.go` — `//go:build !novoice` (real audio engine, CGO)
- `voice_engine_stub.go` — `//go:build novoice` (stub, CGO-free)

---

## Protocol Specification

### Message Envelope
```json
{ "op": 3, "d": { ... }, "s": 42, "t": "EVENT_NAME" }
```

OpCodes are defined in `internal/protocol/messages.go` and now run through op `59`. Beyond the original chat/moderation/voice set, later ranges cover: `40-44` retention policy + custom titles, `45-48` voice signaling/moderation, `49-56` the plugin platform (remote-pane enter/input/resize/leave/frame, the generic plugin event envelope, plugin config get/set), `57` channel permission overwrites, `58` self-service nicknames, `59` P2P file-transfer signaling. Always check this file directly for the current, authoritative list rather than trusting a cached mental model of it — it has grown considerably since v0.1.0's initial protocol design.

### Connection Flow
```
Client                          Server
  ├── WebSocket Connect ────────>│
  │<── OpHello (op:12) ──────────┤  { heartbeat_interval: 45000 }
  ├── HTTP POST /login ─────────>│
  │<── { token } ───────────────┤
  ├── OpIdentify (op:0) ────────>│  { token, properties }
  │<── OpReady (op:13) ──────────┤  { user, servers[] }
  │<── OpDispatch SERVER_CREATE ─┤  { channels[], members[], roles[], voice_states[] }
  │    [per server, one event each]
```

---

## Database Schema

Schema and migrations live in `internal/database/sqlite.go`.

**Important:** SQLite uses `modernc.org/sqlite` (pure Go) for the **server**. CGO is only needed for the **client** voice engine (malgo + opus).

---

## Client Configuration

Stored in `~/.concord/`:
- `servers.json` — list of known servers (address, port, saved credentials, per-server notification overrides)
- `config.json` — UI preferences (theme, display settings, audio settings, notification settings, identity)
- `shared_files.json` — local registry of files *you've* shared via `/attach` (`SharedFilesConfig`/`SharedFileEntry`), so they stay downloadable by others across your own client restarts — see Peer-to-Peer File Attachments below

Key config structs: `AppConfig`, `UIConfig`, `AudioConfig`, `NotificationConfig`, `DisplayConfig`, `ServersConfig`, `ClientServerInfo`, `SharedFilesConfig`

---

## Server Configuration

`concord-server.toml` (auto-generated by first-run wizard; see `concord-server.toml.example` for the documented template).

**Server flags:** `--debug`, `--hybrid` (log + dashboard side-by-side), `--dashboard` (dashboard only), `--setup` (re-run first-run wizard)

---

## Slash Commands (client-side)

Parsed and handled in `internal/client/commands.go`.

---

## Voice Architecture

- **Transport:** WebRTC DataChannels (SCTP, unreliable+unordered — UDP-like) via `pion/webrtc/v3`
- **Codec:** Opus via `hraban/opus` — 20ms frames, 16-bit PCM captured by malgo
- **Sample rates:** low=8kHz, medium=16kHz, high=24kHz, ultra=48kHz (configurable in Audio Settings)
- **Signaling:** Server relays SDP offer/answer and ICE candidates via `OpVoiceSignal` (op 45)
- **Collision resolution:** Peer with lexicographically lower UUID string sends the Offer
- **Audio I/O:** `gen2brain/malgo` (miniaudio) with WASAPI on Windows
- **VAD:** Voice Activity Detection with 300ms hold duration; PTT mode also supported
- **Build:** Default build includes voice (`!novoice` tag). Use `-tags novoice` for CGO-free build.

---

## Grapevine — Server Discovery

Grapevine is Concord's opt-in, decentralized server directory. A **hub** (`build/concord-hub.exe`, pure Go) maintains a public listing of registered Concord servers; hubs can federate with each other to share listings. There is no central authority — anyone can run a hub.

### Components

- **Hub** (`cmd/hub`, `internal/hub`) — standalone REST service, config in `grapevine-hub.toml` (Bubbletea wizard on first run, `--setup` to re-run), SQLite storage (`grapevine.db`). `--dashboard` runs a live TUI: pinned stats (servers/users online, joins served, rate-limited, heartbeats, federation syncs, uptime), registered-server table, and scrolling activity log
- **Server integration** (`internal/server/grapevine.go`) — opt-in via `[grapevine]` in `concord-server.toml` or the first-run wizard's Grapevine step (also in `--reconfigure`); registers with a hub, heartbeats member/online counts, persists `server_id` + `registration_secret` back into its config
- **Client Hub Browser** (`internal/client/hub_browser_view.go`) — full-screen overlay. Primary entry: **Settings → Manage Servers → B**; **Ctrl+G** still works from Login and Add Server (pre-login discovery). Esc returns to the originating view. Hub tabs (h/l), category tabs (Tab), search (/), refresh (r), add hub (+, ↑/↓ picks discovered peers), remove hub (x — removing the last falls back to `defaultHubURL`), join (Enter → A). Hub list persists in `~/.concord/config.json` `ui.hub_urls`

Hub REST routes are registered in `internal/hub/handlers.go` (`/v1/...`).

### Security model

- **HMAC-SHA256 request signing** (`X-Grapevine-Sig`): heartbeat/deregister signed with the server's `registration_secret`; hub→server signals signed the same way (no extra shared secret)
- **Join-token handshake**: listings never expose host/port. On join, the hub *synchronously* signals the server with a single-use token (proving the address belongs to the registered server; 502 if unreachable), only then reveals host/port + token. The client redeems the token at `POST /v1/grapevine/join` on the server (35s TTL, single-use) before adding the server and opening Login.
- **Self-healing registration**: heartbeat 404/401 (hub reset, hub switch) triggers automatic re-registration with fresh credentials, persisted to config
- **Rate limiting**: per-IP token bucket on join (5 burst, refill 5/min)
- **Federation**: peer sync every `federation_sync_minutes`, `?federation=1` prevents listings echoing between hubs; join requests for federated entries are proxied one hop to the origin hub (`X-Grapevine-Federated` loop guard); servers offline >30 days are purged
- **All hub DB timestamps are UTC** — always `.UTC()` values compared in SQL (SQLite compares DATETIMEs as strings)

---

## Plugin Platform

External programs attach to a Concord server as privileged clients — bots, integrations, anything that needs to read/post messages or drive a custom UI pane. Package `internal/plugins`; entry point `cmd/testplugin` is the minimal reference implementation (real plugins — Tukan, Mynah — live in sibling repos, not this one).

- **Never compiled into or dynamically loaded by Concord.** A plugin is a folder dropped into the server's configured `plugins_dir` (`Plugins/<name>/`), containing its own OS binary plus a `plugin.toml` manifest (`internal/plugins/manifest.go`). Installing/upgrading a plugin is "drop a folder in, restart" — zero code changes on Concord's side.
- **Discovery → provision → spawn → identify**, in that order: `Registry.Discover` (`registry.go`) parses and validates every manifest in `plugins_dir`; `Manager.LoadAll`/`ensureInstalledPlugin` (`manager.go`) provisions a DB-backed service-account user + a hashed bearer token (`token.go`, same SHA-256-at-rest pattern as session tokens) the first time a plugin is seen; `process.Supervisor` (`process.go`) launches the manifest's per-OS entrypoint as a child process and restarts it on crash (backoff/max-restarts from `[process]`). The plugin process then dials the normal server WebSocket and authenticates with `OpIdentify{ClientType: "plugin"}` using that token.
- **Two channel-interaction models**, both declared per `[[channel_kind]]` in the manifest: a plain relay channel (chat messages forwarded to the plugin, either because the plugin owns the channel or via `@mention` triggering) that renders as an ordinary text channel — used by chat-bot-style plugins like Mynah; or a **remote pane** (`remote_pane = true`) where the plugin pushes full rendered frames (`OpPluginPaneFrame`) and the client forwards raw key input (`OpPluginPaneInput`) for whichever viewers currently have that channel focused (`OpPluginPaneEnter`/`Resize`/`Leave`) — used by Tukan's kanban board.
- **Generic, manifest-driven config UI** — both plugin-level settings (`[[server_config_field]]`) and per-channel-kind creation fields (`[[channel_kind.create_field]]`) are typed field descriptors (`text`/`number`/`boolean`/`select`/`channel_select`) rendered by one generic form on the client (`plugin_channel_form.go`, Settings > Plugins) — a new plugin never needs bespoke client-side UI code, just manifest entries.
- **Multi-install of the same underlying plugin is supported** — two folders with the same binary but different `[plugin].id` run as fully independent installs (separate service account, process, channel, config). Set `[plugin].product` when several personas share one identity (e.g. Mynah's "Burt"/"Alice") so Settings > Plugins can group them.
- **A plugin only learns about its owned channels at identify/reconnect time** (`GetChannelsByPlugin`, pushed on every identify) — there's no separate "plugin channel registry" push mechanism, so if you add a new way for a plugin to need channel state, make sure it's covered by that same identify-time push, not just the live `CHANNEL_CREATE` event.

---

## Permissions & Channel Overwrites

- **Base model**: a `Permission` bitfield (`internal/models/role.go`) on each `Role`; a member's effective permissions are the union (OR) of their roles' bitfields. `PermissionAdministrator`, and the server owner regardless of roles, bypass every check.
- **Channel-level overwrites** (added post-v0.1.0-initial-scope, part of the same initiative that added plugins/nicknames/attachments): a channel can carry per-role or per-member Allow/Deny/Inherit overwrites, resolved by `hasChannelPermission` in `internal/server/handlers.go` — member-specific overwrite wins over any role overwrite, which wins over the `@everyone` overwrite, which falls back to the role-bitfield result if left at `Inherit`. Owner/Administrator bypass this resolution entirely, same as the base model.
- **Only `PermissionSendMessages` and `PermissionAttachFiles` currently route through `hasChannelPermission`.** Don't assume every permission the Roles editor lets you toggle is actually enforced server-side for a given action — check whether the relevant handler calls `hasChannelPermission` (or does its own role-bitfield check) before relying on it. `PermissionCreateInvite` in particular is defined but unused — invites are a designed-but-not-built feature (no code exists yet).
- **Self-service nicknames**: `PermissionChangeNickname` is granted to `@everyone` by default, letting any member `/nick` themselves via `OpSetNickname` (op 58) with no admin action needed. `PermissionManageNicknames` is required only to rename *someone else's* nickname — a separate, older feature (`/title`, its own permission) already existed for custom titles and is unaffected by this.
- **`@everyone`'s role permissions are editable via `OpUpdateRole`, same as any other role** — the client's Roles > Permissions Editor deliberately supports opening it, and this is the intended way to grant default server-wide access (e.g. Attach Files) without a dedicated extra role. `HandleUpdateRole` (`internal/server/handlers.go`) only rejects an attempt to *rename* `@everyone` (`req.Name != role.Name`); a bug once had it reject the whole request for any edit at all, silently discarding permission changes — fixed 2026-09-06, see `role_update_test.go`.

---

## Peer-to-Peer File Attachments

`/attach <path> [caption]` and `/download <attachment-id>` (`internal/client/file_transfer_engine.go`, server-side `internal/server/file_transfer_test.go` for the wire contract). Deliberately **no server-side file storage** — the sending client hosts the file and streams it directly to a downloader over a WebRTC data channel, negotiated through the same signaling-relay pattern voice uses, via `OpFileTransferSignal` (op 59). Requires `PermissionAttachFiles` (enforced through the overwrite-aware `hasChannelPermission`, see above).

Because there's no server storage, **the sender must stay online for anyone to download** — this is a deliberate design tradeoff, not a bug, and should be communicated as such in any UI/error text touching this path. A local send registry (`~/.concord/shared_files.json`, see Client Configuration above) lets a client's previously-shared files remain downloadable across that client's own restarts.

- **Save destination**: `/download` opens the OS's native Save As dialog (`github.com/sqweek/dialog` — pure `syscall`/Win32 on Windows, no CGO; Cocoa/CGO on macOS; shells out to zenity/kdialog on Linux) via `commands.go`'s `handleDownload`, rather than silently writing into `~/Downloads`. Cancelling the dialog aborts the download cleanly; if the dialog itself errors (e.g. no display), it falls back to the old default-directory behavior instead of failing the command. The chosen path flows through `FileTransferEngine.RequestDownload`'s `destPath` parameter to `resolveDownloadPath` (pure, unit-tested) — a non-empty `destPath` is used exactly as chosen (the dialog already handled overwrite confirmation), while an empty one preserves the original `downloadDir` + `uniqueDownloadPath` collision-safe naming.
- **Alt+M message-highlight mode has an `A` key** that copies a highlighted message's attachment ID to the clipboard — there's no mouse-based text selection in the TUI, so this is the only practical way to get the exact UUID for pasting into `/download`.
- **`emitDone` must never use a drop-if-not-ready channel send.** Progress ticks (`FileTransferProgressMsg`) are fine to drop — another follows almost immediately — but a transfer's one-shot terminal event is not: a real bug shipped where both used the same non-blocking `select { ...; default: }` pattern, and on a fast transfer the UI couldn't drain progress ticks fast enough to keep the (32-slot buffered) event channel from filling up, silently dropping the done event and leaving the status bar stuck at the last percentage that made it through — even though the transfer itself completed successfully. Fixed by making `emitDone` block until there's room (bounded only by `e.quit`, so it can't leak past engine shutdown). See `TestEmitDoneDeliversEvenWhenEventChannelIsFull` — it's written to actually fail against the old drop-based implementation, not just pass against the fix.

---

## Settings Pages (client)

| View | Access | Status |
|---|---|---|
| Theme Browser | Settings > Theme | ✅ Full |
| Display Settings | Settings > Display | ✅ Full (live preview) |
| Notification Settings | Settings > Notifications | ✅ Full (per-server overrides) |
| Audio Settings | Settings > Audio | ✅ Full (device picker, VAD, PTT, codec) |
| Help & Guide | Settings > Help | ✅ Full (glamour markdown) |
| Server Management | Ctrl+B | ✅ Full (add/edit/remove servers) |
| Server Settings | In-server panel | ✅ Full |
| ↳ Channels | Channels tab | ✅ Full (create/delete/rename/reorder) |
| ↳ Roles | Roles tab | ✅ Full (CRUD, permissions editor, display order) |
| ↳ Members | Members tab | ✅ Full (list, kick/ban/role assign) |
| ↳ Messages | Messages tab | ✅ Full (retention policies, prune) |

---

## Themes

40+ themes embedded via `go:embed themes/*.toml` in `internal/themes/embedded.go`. Runtime switch via `/theme <name>` or Settings > Theme.

Selected themes: dracula, alucard-dark, alucard-light, catppuccin-mocha, gruvbox, nord, tokyo-night, onedark, everforest-dark-hard, kanagawa-wave, solarized-dark, terminal-default, and many more.

**Custom themes: just add a TOML file to `internal/themes/themes/` and rebuild** — `ListAvailableThemes()`/`GetTheme()` read the embedded directory at runtime via `fs.ReadDir`, there is no generated file to regenerate. (The root-level `generate_themes.go` predates this and is not part of the live discovery path — don't rely on it; a theme file with no test coverage referencing it by name can silently sit unreachable-but-present, which is exactly what happened to `terminal-default` before 2026-09-06.) Always add a small test asserting a new theme name appears in `ListAvailableThemes()`, per `internal/themes/terminal_default_test.go`.

**`terminal-default`** is the "follow the terminal/OS theme live" option — every color field is either a bare ANSI palette index ("0"-"15", passed through to the terminal's own current palette, never pre-resolved to RGB) or an empty string (no escape code emitted at all, so the terminal's own default fg/bg shows through). This is what makes an OS-level theme switch (e.g. Omarchy re-theming the terminal emulator) repaint Concord automatically on the next redraw, with no reconnect or restart. It is not the global fresh-install default (that's still Dracula, set in `config.go`/`app.go`/`tos_view.go`) — a user opts into it explicitly via `/theme terminal-default`.

---

## Key Architectural Decisions

### Client
- **Single source of truth:** `App` struct in `app.go` owns all state
- **Bubbletea pattern:** `Init()` → `Update(msg)` → `View()` loop
- **Multi-server:** `connection_manager.go` manages a map of active `*Connection`
- **Channel tree:** Categories and channels stored in a tree for sidebar rendering

### Server
- **Hub pattern:** Single goroutine owns client map; sends/receives via Go channels (thread-safe)
- **Per-client goroutine:** Each WebSocket connection gets isolated goroutines for read/write
- **Auth flow:** HTTP POST /login → session token → OpIdentify → OpReady
- **Voice signaling:** Server is a dumb relay — forwards VoiceSignal payloads without inspecting them

### Security
- bcrypt cost 14 for passwords
- SHA-256 token hashing before DB storage
- Credentials never logged
- Soft-deletes for messages (audit trail preserved)
- Permission bitfield on roles; server owner bypasses all checks

---

## Known Issues / Remaining Work

1. **Voice multi-user mesh** — P2P WebRTC with N>2 clients has not been fully stress-tested. The ICE/STUN negotiation and mesh complexity (N×(N-1) peer connections) need validation.
2. **Message retention UI** — The 'N' (channel override) and 'D' (delete override) options in Server Settings > Messages need to be wired up.
3. **Most permissions still only checked at the role-bitfield level, not the overwrite-aware path** — see Permissions & Channel Overwrites above; only `PermissionSendMessages`/`PermissionAttachFiles` go through `hasChannelPermission`. A permission the Roles editor lets you toggle isn't guaranteed to be enforced for the action it implies.
4. **Invites (`PermissionCreateInvite`) are unbuilt** — the permission bit exists, no invite-generation/redemption code does. Deferred to its own future initiative.
5. **Auto re-connect after server offline** — Users currently have to re-login after a server goes offline and comes back. Seamless reconnect with saved token should be implemented.
6. **`-race` needs a real C toolchain** — unavailable on some machines this project is developed from; a concurrency bug can slip through a normal `go test` pass. Worth an explicit `-race` run (see Development Commands) whenever touching shared state (connection lifecycle, plugin supervision, the hub's client map) if a toolchain is available.
