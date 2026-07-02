# Concord — Claude Code Reference

**A terminal-based chat application inspired by Discord**
**Last Updated:** 2026-06-30

---

## Project Overview

Concord is a self-hosted, terminal-first chat platform built in Go. Each server is independently hosted (IRC-style decentralization). The client is a full TUI application built on the Charmbracelet stack. Voice channels use WebRTC P2P audio with Opus encoding.

**Module path:** `github.com/concord-chat/concord`
**Current version:** v0.1.0 (pre-release, final QA)

---

## Technology Stack

| Layer | Library | Notes |
|---|---|---|
| Language | Go 1.24.0 | CGO required for voice client |
| TUI framework | charmbracelet/bubbletea v1.2.4 | Elm Architecture |
| Styling | charmbracelet/lipgloss v1.1.0 | Declarative layout |
| UI components | charmbracelet/bubbles v0.20.0 | Textarea, viewport, list |
| Markdown | charmbracelet/glamour v0.8.0 | Chat message rendering |
| Logging | charmbracelet/log v0.4.2 | Structured, colorized |
| WebSocket | gorilla/websocket v1.5.3 | Client/server transport |
| Database | modernc.org/sqlite v1.34.4 | **Pure Go, no CGO** |
| Voice audio | gen2brain/malgo v0.11.24 | miniaudio wrapper (CGO) |
| Voice codec | hraban/opus | libopus wrapper (CGO) |
| Voice transport | pion/webrtc/v3 v3.3.6 | Pure Go WebRTC |
| Config (client) | encoding/json (stdlib) | `~/.concord/` JSON files |
| Config (server) | pelletier/go-toml/v2 v2.2.3 | `concord-server.toml` |
| IDs | google/uuid v1.6.0 | All entities use UUID |
| Auth | golang.org/x/crypto bcrypt | Cost 14 |
| Notifications | gen2brain/beeep v0.11.2 | Cross-platform OS alerts |

---

## Build System

```bash
make build                  # Server + client + hub (voice enabled, needs CGO)
make build-server           # Server only (CGO_ENABLED=0, pure Go)
make build-client           # Client with voice (CGO_ENABLED=1)
make build-client-novoice   # Client without voice (CGO_ENABLED=0)
make build-hub              # Grapevine hub (CGO_ENABLED=0, pure Go)
make build-windows-voice    # Windows client with WASAPI audio (requires MSYS2 GCC)
make test                   # Run all tests
make fmt                    # gofmt
```

**Voice build tag:** The client uses a `novoice` build tag. Without it (the default), voice is compiled in. Add `-tags novoice` to exclude it:
- `voice_engine.go` — `//go:build !novoice` (real audio engine, CGO)
- `voice_engine_stub.go` — `//go:build novoice` (stub, CGO-free)

**Output:** `build/concord.exe` (client), `build/concord-server.exe` (server), `build/concord-hub.exe` (Grapevine hub)

---

## Project Structure

```
concord/
├── cmd/
│   ├── client/main.go          # Client entry point (auto-connect, identity wizard)
│   ├── hub/
│   │   ├── main.go             # Grapevine hub entry point (--setup, --debug, --dashboard)
│   │   └── setup.go            # First-run Bubbletea setup wizard
│   └── server/
│       ├── main.go             # Server entry point (--debug, --hybrid, --dashboard flags)
│       ├── setup.go            # First-run TUI setup wizard (Bubbletea form)
│       └── tos_setup.go        # Terms of service acceptance flow
│
├── internal/
│   ├── client/                 # Full TUI application
│   │   ├── app.go              # App struct, Init(), Update() — central state machine
│   │   ├── views.go            # View() and all rendering functions
│   │   ├── commands.go         # Slash command parser and handlers
│   │   ├── connection.go       # WebSocket client, protocol handling
│   │   ├── connection_manager.go # Multi-server connection management
│   │   ├── config.go           # ~/.concord/config.json + servers.json R/W
│   │   ├── channel_tree.go     # Category/channel tree data structure
│   │   ├── notifications.go    # OS notification dispatch (beeep)
│   │   ├── reconnect_strategy.go # Exponential backoff reconnection
│   │   ├── voice_engine.go     # WebRTC+Opus voice engine (!novoice build)
│   │   ├── voice_engine_stub.go # No-op stub (novoice build)
│   │   ├── voice_msgs.go       # Voice-related Bubbletea messages
│   │   ├── wasapi_devices_windows.go  # Windows WASAPI audio enumeration
│   │   ├── wasapi_devices_other.go    # Stub for non-Windows
│   │   ├── hub_browser_view.go # Grapevine Hub Browser overlay (Ctrl+G)
│   │   ├── grapevine_http.go   # HTTP client for hub REST API + token redemption
│   │   ├── hub_msgs.go         # Hub-browser Bubbletea messages
│   │   ├── *_view.go           # Individual settings/UI panels (10+ files)
│   │   ├── banners.go          # ASCII art banner definitions
│   │   └── banners_generated.go # Embedded banner data
│   │
│   ├── server/
│   │   ├── server.go           # HTTP + WebSocket server setup
│   │   ├── handlers.go         # Auth, message, channel, role, voice, retention handlers
│   │   ├── grapevine.go        # Grapevine client: hub registration, heartbeats, join tokens
│   │   ├── hub.go              # Central goroutine hub pattern
│   │   ├── client.go           # Per-connection client state
│   │   ├── stats_tracker.go    # Live connection/message statistics
│   │   ├── logger.go           # Structured log categories (Auth, Hub, Voice, etc.)
│   │   ├── banner.go           # Server startup ASCII banner
│   │   └── dashboard/          # Server dashboard TUI (--hybrid / --dashboard flag)
│   │       ├── dashboard.go
│   │       ├── activity_feed.go
│   │       ├── client_list.go
│   │       ├── message_stats.go
│   │       ├── system_stats.go
│   │       └── hybrid_renderer.go
│   │
│   ├── hub/                    # Grapevine hub (standalone discovery service)
│   │   ├── server.go           # HTTP server, route setup, background loops
│   │   ├── handlers.go         # REST handlers, HMAC auth, rate limiting, join proxy
│   │   ├── database.go         # Hub SQLite schema and queries
│   │   ├── federation.go       # Peer hub sync loop
│   │   ├── registry.go         # Offline sweep + stale purge loops
│   │   ├── auth.go             # Secret generation, HMAC sign/verify
│   │   ├── config.go           # grapevine-hub.toml loading
│   │   ├── models.go           # RegisteredServer, ServerListing, JoinToken, etc.
│   │   ├── stats.go            # Live counters + log ring buffer (dashboard)
│   │   └── dashboard.go        # --dashboard live TUI (stats, servers, activity)
│   │
│   ├── database/sqlite.go      # All DB operations + migrations (3330 lines)
│   ├── models/                 # Shared data models
│   │   ├── user.go, server.go, channel.go, message.go, role.go, voice.go, retention.go
│   ├── protocol/messages.go    # All OpCodes, EventTypes, and payload structs
│   ├── themes/
│   │   ├── theme.go            # Theme struct and loading logic
│   │   ├── embedded.go         # go:embed for all theme TOML files
│   │   └── themes/             # 40+ TOML theme files (dracula, nord, gruvbox, etc.)
│   └── testutil/testutil.go    # Shared test helpers
│
├── pkg/crypto/crypto.go        # Token hashing utilities
├── legal/                      # Client Terms.md, Server Terms.md
├── configs/themes/             # Legacy theme location (alucard.toml, dracula.toml)
├── concord-server.toml.example # Documented server config template
├── generate_themes.go          # Tool to regenerate embedded.go from theme files
├── go.mod / go.sum
└── Makefile
```

---

## Protocol Specification

### Message Envelope
```json
{ "op": 3, "d": { ... }, "s": 42, "t": "EVENT_NAME" }
```

### OpCodes — Client → Server (0–9, 16–48)

| Op | Name | Description |
|---|---|---|
| 0 | OpIdentify | Authenticate with session token |
| 1 | OpHeartbeat | Keep-alive ping |
| 2 | OpRequestGuild | Request full server data |
| 3 | OpSendMessage | Send chat message (with optional reply_to_id) |
| 4 | OpTypingStart | Typing indicator |
| 5 | OpPresenceUpdate | Change status/status_text |
| 6 | OpVoiceStateUpdate | Join/leave voice channel |
| 7 | OpChannelCreate | Create text or voice channel |
| 8 | OpChannelUpdate | Rename, reorder, type-change, lock channel |
| 9 | OpChannelDelete | Delete channel (cascades to children) |
| 16 | OpRequestMessages | Load message history (paginated) |
| 17–18 | OpRoleAssign / OpRoleRemove | Manage member roles |
| 19–20 | OpKickMember / OpBanMember | Kick or ban a user |
| 21 | OpMuteMember | Server-mute a user |
| 22 | OpWhisper | Ephemeral private message (in-channel DM) |
| 23–24 | OpPinMessage / OpUnpinMessage | Pin/unpin messages |
| 25 | OpTimeoutMember | Temporary ban for N minutes |
| 26 | OpUnbanMember | Lift a ban |
| 27–29 | OpCreateRole / OpUpdateRole / OpDeleteRole | Role CRUD |
| 30 | OpEditMessage | Edit own message content |
| 31 | OpDeleteMessage | Soft-delete a message |
| 32 | OpUnmuteMember | Lift a server-wide mute |
| 40–43 | OpGetRetentionPolicy … OpPruneMessages | Message retention management |
| 44 | OpAssignTitle | Give member a custom title |
| 45 | OpVoiceSignal | WebRTC SDP/ICE relay (C↔S↔C) |
| 46 | OpVoiceSpeaking | Report speaking state |
| 47 | OpVoiceServerMute | Admin: server-mute a voice user |
| 48 | OpMoveVoice | Admin: force-move user to another voice channel |

### OpCodes — Server → Client (10–15)

| Op | Name | Description |
|---|---|---|
| 10 | OpDispatch | Event dispatch (carries `t` EventType) |
| 11 | OpHeartbeatAck | Heartbeat acknowledgment |
| 12 | OpHello | Initial handshake (heartbeat_interval) |
| 13 | OpReady | Auth success + user/server data |
| 14 | OpInvalidSession | Auth failure |
| 15 | OpReconnect | Server requests client reconnect |

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

**Tables:** users, servers, channels, messages, roles, server_members, member_roles, sessions, bans, timeouts, mutes, invites, dm_recipients, message_mentions, message_reactions, permission_overwrites, message_retention_policies, message_prune_history, voice_states (via migration)

**Key migrations run on startup:**
- `MigrateChannelSortOrder` — adds `sort_order` column, replaces `position`
- `MigrateRoleDisplayOrder` — adds `display_order` to roles
- `MigrateServerMemberCustomTitle` — adds `custom_title` to server_members
- `MigrateMessageWhisperFields` — adds `is_whisper`, `recipient_id` to messages
- `MigrateMessageSoftDelete` — adds `is_deleted`, `deleted_at`, `deleted_by`
- `MigrateChannelIsLocked` — adds `is_locked` to channels
- `MigrateMemberKickCount` — adds `kick_count` to server_members
- `MigrateServerMembersIsBanned` — adds `is_banned` flag
- `MigrateMutesServerWide` — makes `mutes.channel_id` nullable
- `MigrateVoiceStates` / `MigrateVoiceChannelSettings` — voice state tables

**Important:** SQLite uses `modernc.org/sqlite` (pure Go) for the **server**. CGO is only needed for the **client** voice engine (malgo + opus).

---

## Client Configuration

Stored in `~/.concord/`:
- `servers.json` — list of known servers (address, port, saved credentials, per-server notification overrides)
- `config.json` — UI preferences (theme, display settings, audio settings, notification settings, identity)

Key config structs: `AppConfig`, `UIConfig`, `AudioConfig`, `NotificationConfig`, `DisplayConfig`, `ServersConfig`, `ClientServerInfo`

---

## Server Configuration

`concord-server.toml` (auto-generated by first-run wizard):
```toml
host = "0.0.0.0"
port = 8080
server_name = "Concord Server"
database_path = "concord.db"
max_connections = 1000
debug = false
admin_email = ""       # Optional; first registrant becomes owner

[message_pruning]
enabled = true
interval_hours = 24
```

**Server flags:** `--debug`, `--hybrid` (log + dashboard side-by-side), `--dashboard` (dashboard only), `--setup` (re-run first-run wizard)

---

## Slash Commands (client-side)

| Command | Description |
|---|---|
| `/create-channel <name>` | Create text channel (in current category if focused) |
| `/create-group <name>` | Create category |
| `/delete-channel <name>` | Delete channel |
| `/rename-channel <old> <new>` | Rename channel |
| `/move-channel` | Reorder (also Shift+↑/↓ in sidebar) |
| `/theme <name>` | Switch theme live |
| `/mute` / `/unmute` | Mute current channel (self) |
| `/mute @user [minutes]` | Server-mute a member |
| `/role @user <role>` | Assign role |
| `/create-role <name>` | Create new role |
| `/kick @user [reason]` | Kick member |
| `/ban @user [reason]` | Ban member |
| `/unban <username>` | Unban member |
| `/timeout @user <minutes>` | Temporary ban |
| `/pin` / `/unpin` | Pin/unpin highlighted message |
| `/whisper @user <text>` | Send whisper (ephemeral DM) |
| `/links` | Show links in current channel |
| `/status <text>` | Set status text |
| `/title @user <title>` | Assign custom title |
| `/lock` / `/unlock` | Lock/unlock channel |
| `/join-voice <channel>` | Join voice channel |
| `/leave-voice` | Leave voice channel |
| `/mute-voice @user` | Server-mute in voice |
| `/deafen-voice @user` | Server-deafen in voice |
| `/move-voice @user <channel>` | Force-move user |
| `/help` | Show command reference |

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
- **Client Hub Browser** (`internal/client/hub_browser_view.go`) — full-screen overlay: **Ctrl+G** from Login, Main, or Add Server views (returns to the originating view on Esc). Hub tabs (h/l), category tabs (Tab), search (/), refresh (r), add hub (+, digits quick-add discovered peers), join (Enter → A)

### Hub REST API (`/v1/`)

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /v1/health` | — | Liveness + hub name |
| `POST /v1/servers` | — | Register server → `{server_id, registration_secret}` |
| `POST /v1/servers/{id}/heartbeat` | HMAC | Update stats; marks online (204) |
| `DELETE /v1/servers/{id}` | HMAC | Deregister — marks offline, row retained |
| `GET /v1/servers` | — | Public listing (`?category=`, `?q=`, `?all=1`, `?federation=1` local-only) |
| `POST /v1/join/{id}` | rate-limited | Issue join token; proxies to origin hub for federated entries |
| `GET /v1/hubs` / `POST /v1/hubs` | — / admin token | List / add peer hubs |

### Security model

- **HMAC-SHA256 request signing** (`X-Grapevine-Sig`): heartbeat/deregister signed with the server's `registration_secret`; hub→server signals signed the same way (no extra shared secret)
- **Join-token handshake**: listings never expose host/port. On join, the hub *synchronously* signals the server with a single-use token (proving the address belongs to the registered server; 502 if unreachable), only then reveals host/port + token. The client redeems the token at `POST /v1/grapevine/join` on the server (35s TTL, single-use) before adding the server and opening Login.
- **Self-healing registration**: heartbeat 404/401 (hub reset, hub switch) triggers automatic re-registration with fresh credentials, persisted to config
- **Rate limiting**: per-IP token bucket on join (5 burst, refill 5/min)
- **Federation**: peer sync every `federation_sync_minutes`, `?federation=1` prevents listings echoing between hubs; join requests for federated entries are proxied one hop to the origin hub (`X-Grapevine-Federated` loop guard); servers offline >30 days are purged
- **All hub DB timestamps are UTC** — always `.UTC()` values compared in SQL (SQLite compares DATETIMEs as strings)

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

40+ themes embedded via `go:embed` in `internal/themes/embedded.go`. Runtime switch via `/theme <name>` or Settings > Theme.

Selected themes: dracula, alucard-dark, alucard-light, catppuccin-mocha, gruvbox, nord, tokyo-night, onedark, everforest-dark-hard, kanagawa-wave, solarized-dark, terminal-default, and many more.

Custom themes: add a TOML file to `internal/themes/themes/`, then run `go generate` or `go run generate_themes.go` to regenerate `embedded.go`.

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
3. **Some permissions not enforced** — 13 of 19 permissions are defined in the role editor but not yet checked server-side (file uploads, reactions, pins, @everyone, etc. await their feature implementations).
4. **Auto re-connect after server offline** — Users currently have to re-login after a server goes offline and comes back. Seamless reconnect with saved token should be implemented.
5. **Typing indicator latency** — Response time for typing indicators can be tightened.

---

## Building & Running

```bash
# Server (pure Go, no CGO)
make build-server
./build/concord-server                    # First run: launches TUI setup wizard
./build/concord-server --hybrid           # Log + dashboard side-by-side

# Client (with voice, requires GCC/MSYS2 on Windows)
make build-client
./build/concord

# Client without voice (pure Go)
make build-client-novoice
./build/concord-novoice

# Windows with voice (requires MSYS2 GCC at C:/msys64/mingw64/bin)
make build-windows-voice
```
