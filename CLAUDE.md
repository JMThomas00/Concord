# Concord — Claude Code Reference

**A terminal-based chat application inspired by Discord**
**Last Updated:** 2026-09-08

---

## Project Overview

Concord is a self-hosted, terminal-first chat platform built in Go. Each server is independently hosted (IRC-style decentralization). The client is a full TUI application built on the Charmbracelet stack. Voice channels use WebRTC P2P audio with Opus encoding, and an out-of-process plugin platform lets external programs (bots, integrations) attach to a server as privileged clients.

**Module path:** `github.com/concord-chat/concord`
**Current version:** v0.1.0 (pre-release, final QA; plugin platform live)

---

## Development Commands

This repo has no `.golangci.yml` — the commands below are the actual gate used session-to-session (see the Makefile's `test`/`fmt`/`lint` targets for the same thing in `make` form). `.github/workflows/release.yml` exists (added 2026-09-08, not yet run) but only builds/publishes tagged releases — it isn't a PR-gating CI check, so this remains the real day-to-day gate.

```bash
# Full check — run this before considering any change done. The server, models,
# protocol, database, and plugin packages have no CGO dependency; -tags novoice
# additionally makes the client build/test without the voice CGO toolchain, which
# is what this environment normally has available.
go build -tags novoice ./...
go vet -tags novoice ./...
go test -tags novoice -count=1 ./...
(cd sdk && go vet ./... && go test -count=1 ./...)   # the plugin SDK is its own module
(cd sdk/pty && go vet ./... && go test -count=1 ./...)   # ...and terminal passthrough is another (its PTY test needs Linux/macOS)

# Single package or test
go test -tags novoice ./internal/server/...
go test -tags novoice -run TestChannelOverwriteEndToEndOverWebSocket ./internal/server/... -v

# Race detector — needs a C toolchain. On the Windows dev box, use MSYS2's clang
# from PowerShell (gcc 16.2 is unreliable here, see Build System):
#   $env:PATH = "C:\msys64\usr\bin;C:\msys64\mingw64\bin;$env:PATH"; $env:CC = "clang"; $env:CGO_ENABLED = "1"
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

**Windows client static linking (added 2026-09-08):** `build-windows`'s client link step passes `CGO_LDFLAGS="-LC:/msys64/mingw64/lib -Wl,-Bstatic -lopusfile -logg -lopus -Wl,-Bdynamic -lm"` so `concord-client.exe` has no `libopus-0.dll`/`libopusfile-0.dll`/`libogg-0.dll` runtime dependency (only malgo's bundled `miniaudio.c` was ever static before this). **`pkg-config --static --libs opus opusfile` alone does NOT work** — it only adds transitive libs like `-logg`, but MinGW's linker still prefers each lib's `.dll.a` import archive over its `.a` static archive regardless of that flag; the explicit `-Wl,-Bstatic ... -Wl,-Bdynamic` wrap is what actually forces static resolution. Verify with `objdump -p build/concord-client.exe | grep "DLL Name"` (should show only `KERNEL32.dll`/`msvcrt.dll`) or by moving the exe to a directory with no MSYS2 DLLs on `PATH` and launching it.

---

## Distribution & Release

**`.github/workflows/release.yml`** (added 2026-09-08, not yet run in CI): triggers on a `v*` tag push or manual `workflow_dispatch`. Builds server+client(voice)+hub natively per-OS (`windows-latest` via MSYS2+clang mirroring `build-windows`, `macos-latest` via `brew install opus opusfile`, `ubuntu-latest` via `apt-get install libopus-dev libopusfile-dev`) — native runners sidestep the voice-enabled-cross-compilation problem entirely rather than trying to solve it. Publishes a **draft** GitHub Release with all three platforms' artifacts attached; review before publishing. The `dist` Makefile target remains the quick local CGO-disabled/`novoice`-only cross-compile path for ad-hoc protocol-level testing on another OS — this workflow is the real path to voice-included cross-platform releases.

**`Dockerfile` / `docker-compose.yml` / `.dockerignore`** (added 2026-09-08, **not verified — no Docker available in the environment that wrote them**): multi-stage build (`golang:1.24-alpine` → `alpine:3.20`), contains only `concord-server` and `concord-hub` (both pure Go, no CGO) — the client is a TUI app and isn't a sensible container workload. Neither binary gets new non-interactive bootstrap config support; both already skip their first-run wizard whenever their config file exists, so the documented pattern is run-once-interactively-to-generate-config, then mount that file for all subsequent detached runs. See `docker-compose.yml`'s own top comment for the exact one-time setup steps. **Needs a real `docker build`/`docker run` smoke test before relying on it.**

**`scripts/install.sh`** (added 2026-09-08, dry-run verified with a faked `uname`/`curl` against a locally-built fake archive — the real GitHub Release download path is untested since no tagged release exists yet): `curl -fsSL <url>/install.sh | sh` — detects OS/arch, downloads the matching release asset, installs to `/usr/local/bin` (falls back to `~/.local/bin`). `CONCORD_INSTALL_BINARY` env var picks server/client/hub (defaults to client). No package-manager listing yet (AUR/APT/Homebrew/Winget/Flatpak) — see the vault to-do's item 12 for that follow-on work, deliberately deferred until a real tagged release exists to point at.

---

## Protocol Specification

### Message Envelope
```json
{ "op": 3, "d": { ... }, "s": 42, "t": "EVENT_NAME" }
```

OpCodes are defined in `internal/protocol/messages.go` and now run through op `61`. Beyond the original chat/moderation/voice set, later ranges cover: `40-44` retention policy + custom titles, `45-48` voice signaling/moderation, `49-56` the plugin platform (remote-pane enter/input/resize/leave/frame, the generic plugin event envelope, plugin config get/set), `57` channel permission overwrites, `58` self-service nicknames, `59` P2P file-transfer signaling, `60` typing-stop, `61` plugin install (admin-triggered fetch/verify/place, see Plugin Platform below). Always check this file directly for the current, authoritative list rather than trusting a cached mental model of it — it has grown considerably since v0.1.0's initial protocol design.

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
- `servers.json` — list of known servers (address, port, saved credentials, per-server notification overrides). `SavedCredentials.ProfileID` records which profile signed in there (see Accounts below)
- `config.json` — UI preferences (theme, display settings, audio settings, notification settings) and the **profiles** (`identities` + `active_identity`; the legacy single `identity` field is kept equal to the active one and is converted on first load)
- `shared_files.json` — local registry of files *you've* shared via `/attach` (`SharedFilesConfig`/`SharedFileEntry`), so they stay downloadable by others across your own client restarts — see Peer-to-Peer File Attachments below

Key config structs: `AppConfig`, `UIConfig`, `AudioConfig`, `NotificationConfig`, `DisplayConfig`, `ServersConfig`, `ClientServerInfo`, `SharedFilesConfig`

---

## Server Configuration

`concord-server.toml` (auto-generated by first-run wizard; see `concord-server.toml.example` for the documented template).

**Server flags:** `--debug`, `--hybrid` (log + dashboard side-by-side), `--dashboard` (dashboard only), `--reconfigure` (re-run first-run wizard), `--test-mail <address>` (send a test email with `[mail]`, then exit), `--reset-password <email>` (give that account a temporary password and sign it out everywhere, then exit; for servers without mail)

---

## Accounts: email verification, password reset, profiles (2026-09-30)

**Server** (`internal/server/accounts.go`, `mail.go`; DB in `internal/database/accounts.go`):
- **Mail is optional.** `[mail]` in `concord-server.toml` (SMTP only: `smtp_host`, `smtp_port`, `security` = `starttls` default / `tls` / `none`, `smtp_username`, `smtp_password`, `from`, `require_verification`) is set up by the wizard's email step. `MailConfig.VerificationRequired()` is on whenever mail is configured, unless `require_verification = false`. A server without mail behaves as before.
- **Codes:** 6 characters from an unambiguous alphabet. They're stored as a SHA-256 hash in `account_codes` (one per user and purpose: `verify`/`reset`/`change_email`), expire after 15 minutes, allow 5 wrong guesses, and have a 60s resend cooldown. A per-IP token bucket (`accountLimiter`, 20 burst / 20 per minute) covers login, register and every account route.
- **Verification:** `users.email_verified` defaults to 1, so existing accounts are grandfathered. Registering on a verifying server answers **202** with no token and emails a code. Login answers 403 `{"code": "verification_required"}` until `POST /api/account/verify` succeeds. Admin-by-`admin_email` is granted only once that address is verified (`accountActivated`). The first-registrant owner grant is unchanged.
- **Routes** (errors are JSON `{"error", "code"}`): `account/verify`, `account/resend` (needs the password), `account/fix` (correct an unverified account's email/username: the registration typo fix), `password/forgot` (same answer whether or not the address exists; 501 `mail_unavailable` without mail), `password/reset` (signs out every session). Bearer-token routes: `account/password` (signs out other sessions), `account/update` (username now and broadcast as `USER_UPDATE`; a new email waits in `users.pending_email` for `account/confirm-email` on verifying servers).
- **Emails are compared without case** (`FindUserByEmail` falls back to `COLLATE NOCASE`; new registrations are stored lowercased). Every sign-in logs `from=<ip>`.
- All HTTP routes are registered once, in `registerAPIRoutes` (all three run modes share it).

**Client** (`account_api.go`, `account_screens.go`):
- **Profiles:** several `LocalIdentity` per computer, each with a stable `ID`. On the login screen, **Ctrl+P** ("Not you?") opens Profiles: Enter switches (`signOutAll` clears every connection's token first, so nothing reconnects as the old profile), A adds (the identity form in `addingProfile` mode), E edits the alias/email (local, plus `account/update` on every server signed in now; pending email changes queue code screens), P changes the password (on every server: `syncPassword`), F forgets (twice; drops that profile's saved sign-ins).
- **Auto sign-in** (`ConnectionManager.AutoConnectHTTP`) signs in with the email this profile last used on that server. It **registers only on a server this profile has never signed in to** (`SavedCredentials.belongsTo`). Anywhere else, a wrong password or a taken email is reported rather than creating a second account. If the server's account name differs from the profile's alias, the status bar says so (the 2026-09-29 RedOak/`notagh0st` → `gh0st` case).
- **Code screen** (`ViewAccountCode`): opens for a new account awaiting verification, on unlock or when its server icon is selected (`App.pendingVerify`). Ctrl+R resends; Ctrl+E fixes the email/alias (also updates the profile). **Ctrl+F** on the login screen is **Forgot password**: pick a server, enter the emailed code and a new password. The client then changes it on every other server (with a live or saved session, or by signing in with the old password, which it still has), and unlocks. A server without mail switches the code field to a **temporary password** from the admin (`--reset-password`).

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
- **VAD:** Voice Activity Detection with 300ms hold duration and adjustable sensitivity (no push-to-talk mode -- removed 2026-09-28, see git history)
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

External programs attach to a Concord server as privileged clients — bots, integrations, anything that needs to read/post messages or drive a custom UI pane. Package `internal/plugins`; entry point `cmd/testplugin` is the minimal reference implementation (real plugins — Tukan, Mynah, and the three games — live in their own repos, all on the SDK since 2026-09-28: Tukan hosts a board per channel through `pane.Host`, Mynah streams AI replies with `Conn.Stream`).

- **Never compiled into or dynamically loaded by Concord.** A plugin is a folder dropped into the server's configured `plugins_dir` (`Plugins/<name>/`), containing its own OS binary plus a `plugin.toml` manifest (`internal/plugins/manifest.go`). Installing, updating and removing a plugin never needs code changes on Concord's side, nor (since Phase 0c) a server restart: see the live lifecycle bullet below.
- **Discovery → provision → spawn → identify**, in that order: `Registry.Discover` (`registry.go`) parses and validates every manifest in `plugins_dir`; `Manager.LoadAll`/`ensureInstalledPlugin` (`manager.go`) provisions a DB-backed service-account user + a hashed bearer token (`token.go`, same SHA-256-at-rest pattern as session tokens) the first time a plugin is seen; `process.Supervisor` (`process.go`) launches the manifest's per-OS entrypoint as a child process and restarts it on crash (backoff/max-restarts from `[process]`). The plugin process then dials the normal server WebSocket and authenticates with `OpIdentify{ClientType: "plugin"}` using that token.
- **Two channel-interaction models**, both declared per `[[channel_kind]]` in the manifest: a plain relay channel (chat messages forwarded to the plugin, either because the plugin owns the channel or via `@mention` triggering) that renders as an ordinary text channel — used by chat-bot-style plugins like Mynah; or a **remote pane** (`remote_pane = true`) where the plugin pushes full rendered frames (`OpPluginPaneFrame`) and the client forwards raw key input (`OpPluginPaneInput`) for whichever viewers currently have that channel focused (`OpPluginPaneEnter`/`Resize`/`Leave`) — used by Tukan's kanban board.
- **Generic, manifest-driven config UI** — both plugin-level settings (`[[server_config_field]]`) and per-channel-kind creation fields (`[[channel_kind.create_field]]`) are typed field descriptors (`text`/`number`/`boolean`/`select`/`channel_select`/`channel_multi_select`/`secret`, each with optional `help` text; `channel_multi_select` is comma-separated channel IDs, edited with a checklist, `channelPicker` in `plugin_fields.go`) rendered by one generic form on the client (`plugin_fields.go`'s `writePluginField`, used by both forms) — a new plugin never needs bespoke client-side UI code, just manifest entries. Values are validated server-side against the manifest (`plugins.ValidateValues`) before anything is saved, for both server settings and channel settings; a rejected save comes back per field (`PluginManageResult.FieldErrors`) and the form stays open to show it. `secret` fields (server settings only) are sealed with AES-GCM using `plugin-secrets.key`, which sits beside the database. Admin clients only ever see which secrets are set (`PluginInfo.SecretsSet`); the plugin receives the plaintext.
- **Where plugin settings live in the client (2026-09-30).**
  - **Channel settings** (`create_field`) sit behind a **Configure…** row on the Create/Edit Channel form (`renderChannelConfigPage`), never inline, so a long list can't push the form off screen. The form and every plugin settings page scroll (`newScrollSection`/`markFocus` in `settings_view.go`).
  - **Plugin-wide settings** are on the **plugin's page**: Settings > Plugins > Enter (`plugin_page.go`). It shows the plugin's settings, or each instance with its settings, plus every channel using it (Enter edits one; **+ New channel** opens the create form with its type chosen).
  - The channel-type list shows only the **current server's** plugin kinds (`currentServerKinds`), each plugin once (instance kinds are folded into their base).
- **Instances (2026-09-30)** — `[plugin] instances = true` lets an admin run several named copies (personas, e.g. Mynah's "Alice"/"Burt") of **one install**.
  - Stored in the `plugin_instances` table (`internal/database/plugin_instances.go`). `Manager` expands each base plugin into its instances (`internal/plugins/instances.go`, `withInstances`). An instance's manifest shares the base's folder, sets `BaseID`, and takes the instance name as `Name`. Each instance has its own ID (`<base>-<slug>`), service account, process, settings, channels and data dir.
  - Managed through `OpPluginManage` actions `add_instance`/`rename_instance`/`remove_instance`/`adopt_instance` (plugin page keys: `+ Add instance`, N, R, T, X twice). Renaming also renames the service account. Removing keeps the account, settings and channels. Updating the base restarts every instance.
  - The client's Plugins list (`PluginList`) hides instances; `AllPlugins` has everything.
  - **Adopt** (M in the list) turns an older hand-copied persona folder (same `product`, its own `[plugin].id`) into an instance of the base, keeping its ID and so its account, channels and settings. Its folder moves to `.backup/<id>.adopted`. The old separate-folder approach still works, but is no longer the recommended one.
- **@mention relay** (`internal/server/handlers.go`): a message in a channel the plugin doesn't own is forwarded when its `mention_enabled` setting is `"true"`, the message @mentions `mention_trigger` (empty = the plugin's/instance's `Name`), and the channel is in `mention_channels` (a `channel_multi_select`; empty = every channel). Owning channels and answering mentions combine, which gives dedicated-only, mention-only and hybrid modes. See `mention_relay_test.go`.
- **A plugin only learns about its owned channels at identify/reconnect time** (`GetChannelsByPlugin`, pushed on every identify) — there's no separate "plugin channel registry" push mechanism, so if you add a new way for a plugin to need channel state, make sure it's covered by that same identify-time push, not just the live `CHANNEL_CREATE` event.
- **Pane viewers and multiplayer (Phase 0b, 2026-09-27).** The server's routing table for remote panes is `PaneViewers` (`internal/server/pane_viewers.go`, handlers in `plugin_pane_handlers.go`).
  - **Enter** is the only step that touches the DB: it checks channel ownership and View Channels, stamps `ViewerName`/`ViewerDisplayName`, and records the viewer. Input and Resize are relayed only from registered viewers; frames and viewer-directed events are delivered only to them.
  - A frame with an empty `ViewerID` goes to every viewer of the channel.
  - A viewer is tied to the **connection** that entered (`paneViewer.conn`): frames and viewer-directed events go to that connection only, and only it can type or resize. Opening the pane on a second device moves it there (the plugin gets a fresh Enter) and the first device is sent `leave_pane`; closing the first device then isn't a Leave.
  - When a viewer's connection closes, the server sends the plugin a Leave itself (`Hub.SetClientGoneCallback`).
  - When a plugin identifies, the server replays Enter (latest size, theme and names) for everyone already viewing its channels, so a restarted plugin repaints them with no action from the viewer.
  - Enter and Resize carry the viewer's `PaneTheme` (palette + color profile).
  - Event kinds Concord understands are the `protocol.PluginEvent*` constants: `notify`, `leave_pane`, `notify_user` (toast + badge to one member who can see the channel), `members` (request/reply: who can see a channel, online, viewing), and `pane_title` (border title).
  - **Client (`plugin_pane.go`).** The wire lifecycle is driven by `syncPluginPane` on a short timer, scheduled from the `Update` wrapper, rather than from each code path that changes channel, layout or connection. It sends Enter once a selection has settled for 150ms (or immediately when focused), so arrowing past plugin channels sends nothing. It sends Resize whenever the size the pane was actually drawn at, or the theme, changes, and re-enters after a reconnect.
  - **Leaving a pane.** **Ctrl+] is reserved**: it's never forwarded to a plugin and always hands focus back. `leave_pane` does the same. Neither closes the pane — it stays open and keeps updating, and only switching channel or server sends a real Leave.
- **Trust boundary (Phase 0a of the plugin foundation plan, 2026-09-27).** A plugin is code the server runs with its own privileges, so everything plugin-admin-related (`OpPluginConfigGet/Set`, `OpPluginInstall`) requires the dedicated `PermissionManagePlugins` (bit 9), not `ManageServer`. Other rules:
  - A plugin process inherits only an allowlist of the server's env vars (`pluginBaseEnv`, `internal/plugins/util.go`: paths, locale, temp, TLS roots, proxies) plus its manifest `[process.env]` and the `CONCORD_*` vars. Anything else a plugin needs must go in its manifest.
  - Each plugin gets `CONCORD_PLUGIN_DATA_DIR` = `<plugins_dir>/../PluginData/<id>` (0700), which lives outside `Plugins/` so a reinstall never wipes it.
  - The server rejects frames for channels the plugin doesn't own, and stamps each frame with the plugin connection's `Epoch`, so the client accepts a restarted plugin's reset `Seq`.
  - The client strips every escape sequence from frames except SGR and OSC 8 (`sanitizePaneFrame`).
  - Deleting a plugin channel (directly, or via its category) sends `CHANNEL_DELETE` to the owning plugin (`notifyOwningPlugin`).
  - `channel_select` field values are channel **IDs**. The client shows them as `#name`, and `migrateChannelSelectNames` converts old name-valued settings at startup.
  - A plugin reconnecting before its old socket's cleanup runs is just a second connection for a moment; the old one's cleanup only removes itself (see Multiple connections below).
- **Live lifecycle (Phase 0c, 2026-09-27)** — install, update, uninstall, restart and rescan all happen without restarting Concord, driven by `OpPluginManage` (op 62, `PermissionManagePlugins`). `OpPluginInstall` (61) is kept as its install-only alias. Settings > Plugins keys: `I` install, `U` update, `R` restart, `X` uninstall (press twice), `S` rescan, `T` enable/disable, `M` adopt a persona copy as an instance, `Enter` the plugin's page (settings, instances, channels).
  - **Actions run off the admin's read loop.** The outcome arrives as `EventPluginManageResult`, then the refreshed plugin list. Every change to the set of plugins pushes `EventPluginRegistryUpdate` to all clients. The client keeps each server's channel kinds on its own `ServerConnection` and uses the union as `App.pluginChannelKinds`, so new channel types are available without reconnecting.
  - **`Manager` (`internal/plugins/manager.go`)** serializes every lifecycle operation behind `opMu`; `mu` guards the maps and the published `Registry`. A `Registry` is immutable once published (`with`/`without`/`Discover` build new ones).
  - **`Supervisor` (`process.go`) is single-use**: every (re)start builds a new one with a fresh token. `Stop` is safe mid-spawn or mid-backoff and waits for the loop to exit, so a stopped plugin can't spawn again. Crash backoff doubles from `restart_backoff_seconds` (default 2s) up to 60s, and resets after a run of 60s or more. `max_restarts` counts consecutive crashes.
  - **Stopping a plugin** also disconnects its service account (`onPluginStopped` → `Hub.DisconnectUser`).
  - **Sources (no checksum or plugin ID to type):** an admin enters one thing, and `ResolveSource` (`internal/plugins/source.go`) turns it into a download:
    - `owner/repo`, or its github.com URL → the latest release's `.zip` for the server's own OS and CPU (`pickAsset` matches `linux`/`windows`/`darwin` plus `amd64`/`x86_64`/`arm64`… as separate words in the asset name). A single platform-neutral zip is also accepted.
    - Any other github.com link into the repo (its Releases page, a file view) → the same; a release's own page (`/releases/tag/<tag>`) → that release. People paste whatever is in their address bar, and before 2026-09-28 those links were downloaded as HTML and failed with "not a valid zip file"; a downloaded web page now gets an error saying what to type instead.
    - A GitHub release-asset link → that file.
    - Any other https link → that file.

    Verification is automatic: GitHub's own per-asset `digest` (the REST API has published one for every release asset since 2025), else a `<url>.sha256` file, else https alone. The result message says which was used. The plugin ID comes from the archive's `plugin.toml`; for an update it must match the plugin being updated. An update with nothing typed uses the manifest's `[plugin].source_url`, so updating is `U`, Enter. An optional `SHA256` in the request still pins a checksum.
  - **Fetching:** `FetchToStaging` downloads and verifies the archive, extracts it (a zip wrapped in a single top-level folder is unwrapped) into `Plugins/.staging/fetch-*/<id>`, validates the manifest and marks the entrypoint executable. Install is https-only except for loopback. Dot-folders under `Plugins/` are never treated as plugins. `DiscardStaged` only ever deletes a `.staging/fetch-*` slot: a bug that pointed it at an installed plugin's parent once deleted the whole plugins folder in tests. `.tar.gz` assets aren't supported yet; plugin releases should ship `.zip`.
  - **`Update`:** moves the current folder to `Plugins/.backup/<id>`, swaps the new one in, starts it and waits for it to identify (`MarkRunning`, up to `startup_timeout_seconds`, default 20s). If it doesn't, the old version is restored and restarted automatically, and the failed one is kept at `.backup/<id>.failed`.
  - **`Uninstall`** deletes only the plugin folder. `PluginData/<id>`, the service account and its channels are kept for a reinstall.
  - `[plugin].source_url` is still only informational; there's no auto-update polling or catalog yet (Phase 2).

---

## Plugin SDK (`sdk/`)

Plugins are built on the SDK, a **separate Go module** nested in this repo: `github.com/JMThomas00/Concord/sdk`.

- **Releases are git tags.** `sdk/vX.Y.Z` releases the SDK and `sdk/pty/vX.Y.Z` releases the terminal passthrough module. Published: `sdk/v0.1.0` and `sdk/pty/v0.1.1` (2026-09-28), then `sdk/v0.2.0` (network play), `sdk/v0.3.0` (`computer_level` channel setting, scaffolder fixes), `sdk/v0.4.0` (streamed replies: `PostMessage`, `EditMessage`, `Stream`) , `sdk/v0.4.1` (plugintest: stable author IDs via `UserID`, `DrainEvents`) and `sdk/v0.4.2` (`PluginInfo.BaseID`/`AllowsInstances` for instances).
- **Don't use `sdk/pty/v0.1.0`.** It was tagged before its `go.mod` named a real SDK version, and a published tag is never moved.
- **After tagging either module,** bump the scaffolder's `--sdk-version`/`--pty-version` defaults (`sdk/cmd/concord-plugin/main.go`) so new plugins start on it.
- **Before tagging `sdk/pty`,** make sure its `go.mod` requires an SDK version that's already tagged. Its `replace ../` only applies inside that module, so other repos ignore it. Concord's root `go.mod` uses it through `replace … => ./sdk`, so a change to both sides lands in one commit. The root `./...` does **not** include `sdk/`: test it with `cd sdk && go test ./...`; `make test` and `make fmt` do both. The Dockerfile copies `sdk/go.mod`/`go.sum` before `go mod download`.

- **`sdk/wire`** is the single definition of everything a plugin sends or receives. `internal/protocol` **aliases** the plugin payload types (`PluginPane*`, `PluginEvent*`, `PaneTheme`, `PluginInfo`/`PluginField`/`PluginConfigListPayload`, and the event-kind constants), so edit them in `sdk/wire`, not in `internal/protocol`. The SDK's slim mirrors of Concord's own types (opcodes, event names, `Channel`, `User`, chat messages) are held to the server's actual JSON by `internal/protocol/wire_contract_test.go`.
- **`sdk/plugin`** is the runtime:
  - `ConfigFromEnv()` returns ok=false when not launched by Concord (run standalone).
  - `Run(ctx, cfg, Handler)` identifies, reconnects with backoff, and exits on `ErrRejected`. Callbacks run one at a time on one goroutine, in order.
  - `Conn` has helpers: `Frame`/`Broadcast` (automatic per-frame `Seq`), `Notify`, `NotifyUser`, `SetTitle`, `LeavePane`, `SendMessage`, `EditMessage`, `Typing`, and three that block (call them from a goroutine, not a callback): `RequestMembers`, `PostMessage` (returns the new message ID, from Concord echoing a plugin's own post with its nonce) and `Stream` (a reply written as it arrives: posted with `stream: writing`, grown by edits that don't mark it edited, finished with `stream: done`, split past 2000 bytes; the client shows a cursor and closes unfinished markdown meanwhile, `streamingMarkdown`).
  - `Run` returns only after the callback in progress finishes, however it ends.
- **`sdk/pane`** runs Bubble Tea models as panes: one model per viewer, fed `tea.WindowSizeMsg` and rebuilt `tea.KeyMsg`s (`pane.KeyMsg`).
  - `Host.Broadcast(channel, msg)` re-renders every viewer of a channel after shared state changes.
  - `tea.Quit` hands that viewer's keys back to Concord.
  - Frames are clamped to the pane (`pane.Fit`), rendered in true color, and downsampled per viewer with `colorprofile`.
  - Cursor-blink commands are skipped without being called, since calling one blocks about 530ms (the Tukan lesson).
- **`sdk/table`** hosts turn-based games. A game implements `Game` (`Turn`/`Play`/`Outcome`; moves are strings, and saved games are just the move list replayed) plus `Rules.NewBoard`, a Bubble Tea board that reads and moves through a `*Seat`. The kit supplies everything else:
  - **Seating modes**, from the channel's `seating` create_field: `seats` (one table, sit down), `challenge` (a lobby: challenge members, accept or decline) or `private` (only the two players see a game). `allow_spectators` and `computer_opponent` are the other channel settings it reads.
  - **Table menu.** Tab is reserved for it (sit, stand, resign, rematch, add computer, lobby); every other key goes to the board.
  - **Computer opponent** via `Rules.AI`, run off the event loop and posted back with `Conn.Post`.
  - **Turn notifications:** a "your turn" `notify_user` when the next player isn't watching.
  - **Saving:** JSON under `$CONCORD_PLUGIN_DATA_DIR/tables/`.
  - **Change batching:** changes made during one event are saved and redrawn once, at the end of it (`Kit.flush`).
  - **Standalone play:** `table.RunLocal(rules, opts)` runs the same board as a terminal game: hotseat, against the computer, or **over the network** (`netplay.go`).
    - One player hosts on TCP port 7412 (or any free port) and is shown their LAN addresses and a 6-character join code; the other joins with `address code`. Moves travel as JSON lines, and the host sits in seat 0.
    - Both sides validate every move and number it, so drift disconnects rather than diverging.
    - A board's `tea.Quit` is ignored standalone (`noQuit`); it only means "hand keys back" inside Concord.
- **`sdk/pty`** is a **separate module** (`github.com/JMThomas00/Concord/sdk/pty`). It runs an unmodified terminal program in a pseudo-terminal behind an `x/vt` emulator and shows it in a channel. One viewer drives (the first; `Options.Shared` lets everyone type), and the pane title says who. It's Linux/macOS only (Windows needs ConPTY). It's its own module because `x/vt` needs newer x/ansi/runewidth/colorprofile, which would otherwise float into the client (see the pinning rule below).
- **`sdk/cmd/concord-plugin new <name> --template game|pane|bot|pty [--sdk path]`** scaffolds a plugin repo:
  - `main.go`, `plugin.toml`, a README and `.gitignore`;
  - `release.go` (pure Go, so no make or zip is needed), which builds `dist/<id>_<os>_<arch>.zip` for linux/darwin/windows × amd64/arm64 and stamps the git tag into `plugin.toml`;
  - a GitHub Actions workflow that attaches those zips to a release on each `v*` tag.

  The zips are exactly what `ResolveSource`/`pickAsset` look for. Its templates use `<% %>` delimiters, because TOML's `[[channel_kind]]` collides with `[[ ]]`.
- **`sdk/examples/tictactoe`** is the reference game: rules, board, a three-level computer, one binary for standalone and plugin, and a `plugin.toml`. The table kit's tests run against it. The full games are their own repos: `JMThomas00/concord-checkers`, `concord-tak` and `concord-chess` (each `engine/` + `game/`, perft-tested, released as `v0.1.0`).
- **The `/concord` skill** (`.claude/skills/concord/`: `SKILL.md` plus topic guides) teaches an agent to build, test, package and install plugins using only the SDK. Keep it in step with the SDK: a changed helper, key, field type or install step belongs in the matching guide.
- **`sdk/PROTOCOL.md`** is the wire protocol for plugins written in other languages.
- **`sdk/plugintest`** is a fake Concord server for plugin unit tests. It sends Enter, Key, Type, Resize and Leave, plus channels, settings, chat and custom events, and reads back frames, events and chat. It also flags frames sent to non-viewers, frames taller than the pane, and a non-increasing `Seq`.
- **`cmd/testplugin`** is the smallest SDK plugin, and the server and client integration tests spawn it against the real server.
- **Keep the SDK's shared dependencies pinned to Concord's versions** (bubbletea, bubbles, lipgloss, x/ansi, colorprofile, runewidth, go-colorful, x/term). Go's module resolution takes the highest version any module asks for, so an SDK `go mod tidy` that floats one of them upgrades the client's UI stack too. This happened once: bubbles went 0.20 → 1.0 and runewidth 0.0.16 → 0.0.19. After tidying the SDK, check `git diff go.mod` at the root.

## Permissions & Channel Overwrites

- **Base model**: a `Permission` bitfield (`internal/models/role.go`) on each `Role`; a member's effective permissions are the union (OR) of their roles' bitfields. `PermissionAdministrator`, and the server owner regardless of roles, bypass every check.
- **Channel-level overwrites** (added post-v0.1.0-initial-scope, part of the same initiative that added plugins/nicknames/attachments): a channel can carry per-role or per-member Allow/Deny/Inherit overwrites, resolved by `hasChannelPermission` in `internal/server/handlers.go` — member-specific overwrite wins over any role overwrite, which wins over the `@everyone` overwrite, which falls back to the role-bitfield result if left at `Inherit`. Owner/Administrator bypass this resolution entirely, same as the base model.
- **Only `PermissionSendMessages`, `PermissionAttachFiles`, and `PermissionViewChannels` (plugin pane Enter/Resize/Input only) currently route through `hasChannelPermission`.** Don't assume every permission the Roles editor lets you toggle is actually enforced server-side for a given action — check whether the relevant handler calls `hasChannelPermission` (or does its own role-bitfield check) before relying on it. `PermissionCreateInvite` in particular is defined but unused — invites are a designed-but-not-built feature (no code exists yet).
- **Self-service nicknames**: `PermissionChangeNickname` is granted to `@everyone` by default, letting any member `/nick` themselves via `OpSetNickname` (op 58) with no admin action needed. `PermissionManageNicknames` is required only to rename *someone else's* nickname — a separate, older feature (`/title`, its own permission) already existed for custom titles and is unaffected by this.
- **`@everyone`'s role permissions are editable via `OpUpdateRole`, same as any other role** — the client's Roles > Permissions Editor deliberately supports opening it, and this is the intended way to grant default server-wide access (e.g. Attach Files) without a dedicated extra role. `HandleUpdateRole` (`internal/server/handlers.go`) only rejects an attempt to *rename* `@everyone` (`req.Name != role.Name`); a bug once had it reject the whole request for any edit at all, silently discarding permission changes — fixed 2026-09-06, see `role_update_test.go`.

---

## Peer-to-Peer File Attachments

`/attach <path> [caption]` and `/download <attachment-id>` (`internal/client/file_transfer_engine.go`, server-side `internal/server/file_transfer_test.go` for the wire contract). Deliberately **no server-side file storage** — the sending client hosts the file and streams it directly to a downloader over a WebRTC data channel, negotiated through the same signaling-relay pattern voice uses, via `OpFileTransferSignal` (op 59). Requires `PermissionAttachFiles` (enforced through the overwrite-aware `hasChannelPermission`, see above).

Because there's no server storage, **the sender must stay online for anyone to download** — this is a deliberate design tradeoff, not a bug, and should be communicated as such in any UI/error text touching this path. A local send registry (`~/.concord/shared_files.json`, see Client Configuration above) lets a client's previously-shared files remain downloadable across that client's own restarts.

- **Save destination**: `/download` opens the OS's native Save As dialog (`github.com/sqweek/dialog` — pure `syscall`/Win32 on Windows, no CGO; Cocoa/CGO on macOS; GTK3+X11/CGO on Linux, so Linux builds need `libgtk-3-dev` and the resulting binary links libgtk-3) via `commands.go`'s `handleDownload` → `promptSavePath` (`save_dialog.go`; CGO-free non-Windows builds like `make dist` get `save_dialog_nocgo.go`, which always reports the dialog unavailable so the Downloads-folder fallback applies), rather than silently writing into `~/Downloads`. Cancelling the dialog aborts the download cleanly; if the dialog itself errors (e.g. no display), it falls back to the old default-directory behavior instead of failing the command. The chosen path flows through `FileTransferEngine.RequestDownload`'s `destPath` parameter to `resolveDownloadPath` (pure, unit-tested) — a non-empty `destPath` is used exactly as chosen (the dialog already handled overwrite confirmation), while an empty one preserves the original `downloadDir` + `uniqueDownloadPath` collision-safe naming.
- **Alt+M message-highlight mode has an `A` key** that copies a highlighted message's attachment ID to the clipboard — there's no mouse-based text selection in the TUI, so this is the only practical way to get the exact UUID for pasting into `/download`.
- **`emitDone` must never use a drop-if-not-ready channel send.** Progress ticks (`FileTransferProgressMsg`) are fine to drop — another follows almost immediately — but a transfer's one-shot terminal event is not: a real bug shipped where both used the same non-blocking `select { ...; default: }` pattern, and on a fast transfer the UI couldn't drain progress ticks fast enough to keep the (32-slot buffered) event channel from filling up, silently dropping the done event and leaving the status bar stuck at the last percentage that made it through — even though the transfer itself completed successfully. Fixed by making `emitDone` block until there's room (bounded only by `e.quit`, so it can't leak past engine shutdown). See `TestEmitDoneDeliversEvenWhenEventChannelIsFull` — it's written to actually fail against the old drop-based implementation, not just pass against the fix.

---

## Settings Pages (client)

| View | Access | Status |
|---|---|---|
| Theme Browser | Settings > Theme | ✅ Full |
| Display Settings | Settings > Display | ✅ Full (live preview) |
| Notification Settings | Settings > Notifications | ✅ Full — two sections: **Desktop Notifications** (OS-native popup mode off/mentions/all, scope all-servers/current-server — `notifications.go`, `beeep.Notify`) and **Audio Notifications** (sound/bell alerts, per-server overrides) |
| Audio Settings | Settings > Audio | ✅ Full (device picker, VAD, noise suppression, echo cancellation, codec) |
| Help & Guide | Settings > Help | ✅ Full (glamour markdown, theme-derived style — `buildThemedGlamourStyle`) |
| About | Settings > About | ✅ Full (client build info; server build info once connected; shaded grape logo when there's room) |
| Server Management | Ctrl+B | ✅ Full (add/edit/remove servers) |
| Server Settings | In-server panel | ✅ Full |
| ↳ Channels | Channels tab | ✅ Full (create/delete/rename/reorder) |
| ↳ Roles | Roles tab | ✅ Full (CRUD, permissions editor, display order) |
| ↳ Members | Members tab | ✅ Full (list, kick/ban/role assign) |
| ↳ Messages | Messages tab | ✅ Full (retention policies incl. per-channel custom overrides, prune) |
| ↳ Plugins | Plugins tab | ✅ Full (enable/disable, config, admin install via `OpPluginInstall`) |
| ↳ About | About tab | ✅ Full (connected server's build info) |

Chat messages themselves render markdown too (bold/italic/inline code/fenced code/lists, plus clickable OSC 8 links and @mention highlighting) via a separate, minimal-feature-set glamour renderer — see `internal/client/message_markdown.go` and `buildChatGlamourStyle`. Deliberately excludes glamour's Table/Linkify extensions (goldmark's GFM Linkify auto-links bare URLs, and glamour's own link renderer then prints the link text and href as two separate visible runs — confirmed via `TestProbeChatGlamourPipeline`); Concord substitutes/restores URLs itself instead via an opaque placeholder token, both for that reason and to keep its own OSC 8/zone-marked clickable-link behavior.

**Login/register logo area:** a random text banner (327, `banners_generated.go`) in a fixed-height slot so the form never moves; Ctrl+R shuffles it with one of 8 intro animations (`banner_anim.go`). Beside it, when there's room, the **shaded grape logo** (`grape_logo.go`), also on Settings > About: a Go port of the concord-site project's ASCII renderer (github.com/Anthoneyq/concord-site, `app.js` + `data/logo.py`). The eight grapes are spheres shaded per character like donut.c under a light that powers on, orbits when idle, and follows clicks/drags; leaf and stem are traced characters. `grape_logo_data.go` is generated by `go run ./tools/grapelogo` from `ConcordLogo.png`; `go run ./tools/grapelogo -cols 50 -json` reproduces the site's `logo.json` byte-for-byte (verified 2026-09-27), and the shading matched the site's JS exactly across multiple light angles. The light's start/stop lives in the `Update` wrapper (`syncGrapeLight`), not in individual navigation paths.

---

## Themes

40+ themes embedded via `go:embed themes/*.toml` in `internal/themes/embedded.go`. Runtime switch via `/theme <name>` or Settings > Theme.

Selected themes: dracula, alucard-dark, alucard-light, catppuccin-mocha, gruvbox, nord, tokyo-night, onedark, everforest-dark-hard, kanagawa-wave, solarized-dark, terminal-default, and many more.

**User themes need no rebuild:** a `~/.concord/themes/<name>.toml` file adds a new theme, or overrides a built-in one with the same name (`GetTheme` checks that folder first; `ListAvailableThemes` appends its names). **Bundled themes: add a TOML file to `internal/themes/themes/` and rebuild** — `ListAvailableThemes()`/`GetTheme()` read the embedded directory at runtime via `fs.ReadDir`, there is no generated file to regenerate. (The root-level `generate_themes.go` predates this and is not part of the live discovery path — don't rely on it; a theme file with no test coverage referencing it by name can silently sit unreachable-but-present, which is exactly what happened to `terminal-default` before 2026-09-06.) Always add a small test asserting a new theme name appears in `ListAvailableThemes()`, per `internal/themes/terminal_default_test.go`.

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
- **Multiple connections per account** (2026-09-28): `Hub.clients` maps a user to **all** their live connections. Anything sent to a user, server or channel reaches every one; a user is announced offline only when their last connection closes; voice belongs to the connection that joined (`voiceUserEntry.conn`), so closing another device doesn't drop the call. Replies to a request (plugin action results, the plugin list) go to the requesting connection via `Handlers.dispatchTo`, not `SendToUser`. `Client.Send` is safe after close (`sendMu`/`sendClosed`, closed through `closeSend`), since slow work can finish after its connection is gone. Kick/ban/timeout use `DisconnectUser`, which closes every connection.
- **Per-client goroutine:** Each WebSocket connection gets isolated goroutines for read/write
- **Auth flow:** HTTP POST /login → session token → OpIdentify → OpReady
- **Voice signaling:** Server is a dumb relay — forwards VoiceSignal payloads without inspecting them

### Security
- bcrypt (`bcrypt.DefaultCost`, 10) for passwords — earlier notes said 14; the code has always used the default
- SHA-256 token hashing before DB storage
- Credentials never logged
- Soft-deletes for messages (audit trail preserved)
- Permission bitfield on roles; server owner bypasses all checks

---

## Known Issues / Remaining Work

1. **Voice multi-user mesh** — P2P WebRTC with N>2 clients has not been fully stress-tested. The ICE/STUN negotiation and mesh complexity (N×(N-1) peer connections) need validation.
2. **Most permissions still only checked at the role-bitfield level, not the overwrite-aware path** — see Permissions & Channel Overwrites above; only `PermissionSendMessages`/`PermissionAttachFiles` go through `hasChannelPermission`. A permission the Roles editor lets you toggle isn't guaranteed to be enforced for the action it implies.
3. **Invites (`PermissionCreateInvite`) are unbuilt** — the permission bit exists, no invite-generation/redemption code does. Deferred to its own future initiative.
4. **`-race` needs a real C toolchain** — unavailable on some machines this project is developed from; a concurrency bug can slip through a normal `go test` pass. Worth an explicit `-race` run (see Development Commands) whenever touching shared state (connection lifecycle, plugin supervision, the hub's client map) if a toolchain is available.
5. ~~One live connection per account~~ — fixed 2026-09-28 (see Multiple connections per account above). It had shown up as an install form stuck on "Working…" and new channels not appearing, because a second computer signed in as the same user was getting them.
6. **This branch's CI/Docker/install-script additions are unverified** — `.github/workflows/release.yml` has never actually run (needs a real tag push), the `Dockerfile`/`docker-compose.yml` have never been built/run (no Docker in the environment that wrote them), and `scripts/install.sh` was only dry-run tested against a local fake archive, not a real GitHub Release. See Distribution & Release above.
