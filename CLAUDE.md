# Concord - Terminal Chat Application

## Project Overview

Concord is a Discord-like terminal chat application built with Go and bubbletea. It aims to provide a modern chat experience while maintaining a retro terminal aesthetic. The application follows an IRC-like decentralized model where each server is independently hosted.

**Status**: v0.1.0 — Voice channels, moderation commands, and settings fully integrated. Next sprint (`Misc` branch): miscellaneous polish and UX improvements.

---

## Technical Stack

### Core Technologies

- **Language**: Go 1.24
- **TUI Framework**: [bubbletea](https://github.com/charmbracelet/bubbletea) (The Elm Architecture)
- **Styling**: [lipgloss](https://github.com/charmbracelet/lipgloss) (Declarative terminal styling)
- **Components**: [bubbles](https://github.com/charmbracelet/bubbles) (textarea, viewport, textinput)
- **Database**: SQLite via `modernc.org/sqlite` (pure-Go, no CGO)
- **WebSocket**: [gorilla/websocket](https://github.com/gorilla/websocket)
- **WebRTC**: [pion/webrtc/v3](https://github.com/pion/webrtc) — pure-Go, P2P audio transport
- **Audio I/O**: [gen2brain/malgo](https://github.com/gen2brain/malgo) — CGO wrapper for miniaudio (WASAPI/PulseAudio/CoreAudio)

### Themes

- **Dracula** (dark theme)
- **Alucard Dark** (dark theme variant)
- **Alucard Light** (light theme variant)

### Build Tags

- **Default** (`go build`): Voice enabled — requires a C toolchain (GCC via MSYS2 on Windows)
- **`-tags novoice`**: Audio engine replaced with stubs — no CGO, pure-Go, for headless/CI builds

---

## Architecture Model

### IRC-Like Multi-Server (Decentralized)

Concord follows an **IRC-like model**, not Discord's centralized model:

| Aspect | Discord | Concord (IRC-Like) |
|--------|---------|-------------------|
| **Hosting** | Single centralized service | Independent self-hosted servers |
| **Server List** | Centralized directory | Client-side configuration |
| **Identity** | Global account | Single local identity, auto-registers on each server |
| **Connection** | Single gateway | Direct IP:Port connections |
| **Data** | Centralized database | Per-server databases |

### Voice Architecture

- **P2P WebRTC**: Clients connect directly via ICE/DTLS; server is a dumb signaling relay
- **Audio transport**: WebRTC SCTP DataChannels (unreliable, unordered — behaves like UDP)
- **Audio format**: 16-bit PCM, sample rate determined by codec preset (8/16/24/48 kHz), mono, 20ms frames
- **Offer/answer collision**: Peer with lexicographically smaller UUID always sends the Offer
- **STUN fallback**: `stun:stun.l.google.com:19302` when no server STUN config is present

---

## UI Layout (Four-Column Design)

```
┌──────────┬─────────────────────┬────────────────────────────────┬─────────────────────┐
│ Servers  │ Channels            │ Chat                           │ Members             │
│ (~22ch)  │ (~26ch)             │ (flexible)                     │ (~30ch)             │
├──────────┼─────────────────────┼────────────────────────────────┼─────────────────────┤
│          │                     │                                │                     │
│  (D)  ●  │ ▼ TEXT CHANNELS     │ gh0st                   10:30  │ ── ♪ GENERAL (2) ── │
│          │   # general    ●    │ Hey everyone!                  │  ↑[████░░░░]        │
│  (M)  ●  │   # random  @2      │                                │> (G) gh0st ♪ ◆◆◆◆ ● │
│          │   # memes           │ alice                   10:31  │  ↓[██░░░░░░]        │
│  (F)  ●  │                     │ Hi gh0st!                      │  (N) notagh0st ♪ ◆◆◆◇ 42ms ● │
│          │ ▼ VOICE CHANNELS    │                                │                     │
│   +      │   ♪ General  [2]   │                                │ ── ADMIN (1) ──     │
│          │   ♪ Gaming          │                                │  (A) alice       ●  │
│          │                     │                                │                     │
│          │ ▼ ADMIN             │                                │ ── MEMBERS (2) ──   │
│          │   # mod-chat        │                                │  (B) bob         ○  │
│          │   # announcements   │                                │  (C) charlie     ●  │
│          │                     │                                │                     │
│          │                     │ [DM from alice] hello!         │                     │
├──────────┴─────────────────────┴────────────────────────────────┴─────────────────────┤
│ [gh0st@localhost:8080] Concord v0.1.0 │ #general │ Ctrl+Q: Quit · /help for commands │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

### Column Breakdown

1. **Server Icons** (~22 chars)
   - Colored circles with server initials
   - Active server highlighting
   - Unread `●` dot below icon when server has unread messages
   - `+` button to add servers

2. **Channels** (~26 chars)
   - Folder explorer-style hierarchical layout
   - Collapsible channel categories (Left/Right or H/L)
   - Unread `●` dot and `@N` mention count after channel name
   - Voice channels show `♪` prefix and live member count badge `[N]`
   - Shift+↑/↓ to reorder channels within a category
   - Visual indicators (▼/▶ for collapse state)
   - Enter to join/leave the selected voice channel

3. **Chat** (flexible, fills remaining space)
   - Message history viewport with scrollback (PgUp/PgDn)
   - Sender with colored circle avatar
   - Timestamp right-aligned
   - `@mention` highlights in your color
   - URL hyperlinks (OSC 8, Ctrl+Click in supported terminals)
   - `[DM]` prefix for whispers
   - Multi-line compose with Ctrl+J / Ctrl+Enter
   - **Two-level message navigation** (Alt+M):
     - **Level 1**: Navigate between messages with ↑/↓, copy entire message with Ctrl+C
     - **Level 2**: Press Enter to edit mode, navigate within message with arrows, select text with Shift+arrows, copy selection with Ctrl+C
     - **Link browser**: Press L to view/open URLs in selected message

4. **Members** (~30 chars)
   - **Voice groups at top**: Users in voice channels shown first, grouped by channel name with `── ♪ CHANNEL (N) ──` header
   - Voice member rows: VU level bar (Row 1) + name + voice icon + quality bar + dot (Row 2) + optional title/status
   - `↑` prefix = local mic input, `↓` prefix = received audio from remote peer
   - Quality bar: `◆◆◆◆` (<50ms), `◆◆◆◇` (<100ms), `◆◆◇◇` (<200ms), `◆◇◇◇` (≥200ms), `◇◇◇◇` (no data)
   - Voice icons: `▶` speaking, `♪` in voice, `✕` muted, `≈` deafened
   - Role-grouped below voice: Admin → Moderators → Members
   - Colored circle avatars with initials (role color)
   - Presence dots: `●` online, `○` offline
   - Enter to open member context menu

### Member Context Menu

Actions shown based on current user's role permissions:

| Key | Action | Condition |
|-----|--------|-----------|
| W | Whisper | Always |
| M | Mute / Unmute | PermissionKickMembers + can moderate |
| K | Kick | PermissionKickMembers + can moderate |
| T | Timeout | PermissionKickMembers + can moderate |
| B | Ban | PermissionBanMembers + can moderate + not banned |
| N | Unban | PermissionBanMembers + can moderate + is banned |
| R | Assign Role | PermissionManageRoles |
| E | Remove Role | PermissionManageRoles |
| V | Adjust Volume | Target in voice |
| O | Move Voice | PermissionMuteMembers + in voice |
| X | Voice Mute | PermissionMuteMembers + in voice + not server-muted |
| D | Voice Deafen | PermissionMuteMembers + in voice + not server-muted |
| U | Voice Unmute | PermissionMuteMembers + in voice + server-muted |

### Terminal Requirements

- **Minimum Width**: 160 columns (comfortable)
- **Recommended**: 200+ columns (spacious)
- **Fixed Widths**: Maintains retro terminal aesthetic

---

## Client-Side Configuration

### ~/.concord/servers.json

Stores known servers and user preferences:

```json
{
  "servers": [
    {
      "id": "uuid-1",
      "name": "My Local Server",
      "address": "localhost",
      "port": 8080,
      "last_connected": "2026-02-05T10:30:00Z",
      "saved_credentials": {
        "email": "user@example.com",
        "token": "encrypted_token_here"
      }
    }
  ],
  "default_user_preferences": {
    "username": "gh0st",
    "email": "ghost@example.com"
  }
}
```

### ~/.concord/config.json

Application preferences and local identity:

```json
{
  "version": 1,
  "identity": {
    "alias": "gh0st",
    "email": "ghost@example.com",
    "password": "..."
  },
  "ui": {
    "theme": "alucard-dark",
    "collapsed_categories": {},
    "muted_channels": [],
    "audio": {
      "input_device": "",
      "output_device": "",
      "input_gain": 1.0,
      "output_volume": 1.0,
      "vad_enabled": true,
      "vad_threshold": 0.4,
      "ptt_enabled": false,
      "ptt_key": "ctrl+space",
      "noise_suppress": false,
      "echo_cancellation": false,
      "codec_preset": "medium",
      "per_user_volumes": {}
    }
  }
}
```

---

## Key Features

### Current (v0.1.0)

- Multi-server support (IRC-like model)
- Four-column Discord-like layout
- Folder explorer-style channel categories
- Database-driven categories (admin-configurable)
- Colored circle avatars with initials and role colors
- Fixed column widths (terminal aesthetic)
- Server addition UI (address/port entry)
- Client-side server list
- Default user preferences (consistent alias across servers)
- Role-grouped members panel with presence indicators
- Unread channel tracking with @mention counts
- URL hyperlink rendering (OSC 8 terminal links)
- @mention autocomplete popup and highlighting
- Theme browser with real-time preview (Ctrl+T)
- /whisper ephemeral DMs
- Shift+↑/↓ channel reordering
- **Two-level message navigation & copying** (Alt+M)
- **Moderation commands**: /kick, /ban, /unban, /mute, /unmute, /timeout, /role, /move-voice
- **Settings pages**: Theme, Notifications, Display, Audio (Ctrl+S or Settings menu)
- **Voice channels** (integrated, no build flag required):
  - Real-time P2P audio via WebRTC DataChannels
  - Voice Activity Detection (VAD) with configurable threshold
  - Push-to-Talk (PTT) mode — critical for terminal use
  - Self-mute (Ctrl+M) / Self-deafen (Ctrl+D)
  - Admin server-mute and server-deafen
  - Per-user volume control (0–200%) in member context menu
  - VU level bars with local `↑` / remote `↓` direction indicators
  - Connection quality bar (`◆◆◆◇ 42ms`) updated every 5s via ICE RTT stats
  - Codec quality presets: low (8kHz), medium (16kHz), high (24kHz), ultra (48kHz)
  - Voice channel member count badge in channel list
  - Voice groups at top of Members panel, excluded from role sections
  - Max-users capacity enforcement per channel
  - Graceful voice disconnect on server connection loss

### Planned

- **Bots**: Server-side bot framework with event hooks
- **AI Integration**: LLM-powered assistant bots, /ai command, inline compose help
- **Notifications**: OS-level and terminal bell for @mentions

---

## Database Schema

### Channels Table

```sql
CREATE TABLE channels (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    topic TEXT,
    type INTEGER NOT NULL,       -- 0=text, 1=voice, 2=category, 3=dm
    position INTEGER DEFAULT 0,
    category_id TEXT REFERENCES channels(id),
    is_nsfw INTEGER DEFAULT 0,
    rate_limit_per_user INTEGER DEFAULT 0,
    max_users INTEGER DEFAULT 0, -- 0 = unlimited (voice channels)
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
```

### Voice States Table

```sql
CREATE TABLE voice_states (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    server_id  TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    is_self_muted      INTEGER DEFAULT 0,
    is_self_deafened   INTEGER DEFAULT 0,
    is_server_muted    INTEGER DEFAULT 0,
    is_server_deafened INTEGER DEFAULT 0,
    joined_at  DATETIME NOT NULL,
    PRIMARY KEY (user_id, server_id)
);
```

---

## Design Principles

### Terminal-First UX
- Respect terminal conventions
- Fixed widths for retro feel
- No image rendering (colored circles instead)
- Keyboard-driven navigation

### Incremental Complexity
- Each version adds depth without destabilizing core
- Voice integrated as base functionality in v0.1.0

### Discoverability
- Users learn from within the app
- Help overlay (`/help` command, shown in status bar)
- Contextual footer hints

### Extensibility
- Avoid architectural dead-ends
- Bot framework planned
- AI integration planned
- Plugin system (future)

---

## Implementation Phases

### Phase 1–4: Foundation ✅ COMPLETE
Multi-server, categories, channel management, connection resilience, identity, members panel, unread tracking, themes, whispers. Completed 2026-02-18.

### Phase 5 (Voice): Voice Channels ✅ COMPLETE
Full P2P voice chat via WebRTC. VAD, PTT, self-mute/deafen, admin voice controls, per-user volume, VU meters, quality bars, codec presets, voice grouping in Members panel, capacity enforcement, graceful disconnect. Completed 2026-04-16.

### Phase 6 (Voice-Settings Branch): Settings & Moderation ✅ COMPLETE

1. Settings UI with four pages: Theme, Notifications, Display, Audio ✅
2. Audio settings: device picker, gain/volume sliders, VAD, PTT, noise suppress, echo cancel, codec preset ✅
3. Moderation slash commands: /kick, /ban, /unban, /mute, /unmute, /timeout, /role ✅
4. Member context menu: all commands permission-gated by role ✅
5. Admin context menu fix: PermissionAdministrator bypasses role-position hierarchy check ✅
6. Mute/Unmute toggle in context menu based on current mute state ✅
7. Per-user volume slider: 1% increments, 0–200% range ✅
8. Consistent selection highlight across all settings pages (uses SidebarSelected, theme-safe) ✅
9. Channel type persistence fix: voice/text type change now saves correctly ✅
10. Voice build tag removed: voice is now the default; `-tags novoice` for headless builds ✅

**Status**: Completed 2026-04-16.

### Next: Misc Branch 🔜
Miscellaneous polish and UX improvements.

### Future: Bots & AI 🔮
Server-side bot framework, AI provider integration (Anthropic, OpenAI-compatible, Ollama).

---

## File Structure

```
Concord/
├── cmd/
│   ├── client/          # Client entry point
│   └── server/          # Server entry point
├── internal/
│   ├── client/
│   │   ├── app.go                      # Main application state & event handlers
│   │   ├── views.go                    # UI rendering (4 columns)
│   │   ├── connection.go               # WebSocket management
│   │   ├── connection_manager.go       # Multi-server connection manager
│   │   ├── commands.go                 # Slash command handling
│   │   ├── channel_tree.go             # Hierarchical channel data structure
│   │   ├── config.go                   # Configuration file management
│   │   ├── add_server_view.go          # Add server dialog UI
│   │   ├── manage_servers_view.go      # Pre-auth server management UI (Ctrl+B)
│   │   ├── identity_setup_view.go      # First-run identity setup screen
│   │   ├── settings_view.go            # Settings overlay (Theme/Notifs/Display/Audio)
│   │   ├── audio_settings_view.go      # Audio settings page render + key handler
│   │   ├── server_management_view.go   # Server admin panel (roles, channels, members)
│   │   ├── theme_browser_view.go       # Theme browser with live preview (Ctrl+T)
│   │   ├── voice_engine.go             # Real voice engine: malgo + pion WebRTC (!novoice)
│   │   ├── voice_engine_stub.go        # No-op stubs for headless builds (novoice)
│   │   ├── voice_msgs.go               # tea.Msg types for voice events
│   │   ├── wasapi_devices_windows.go   # Pure-Go WASAPI device enumeration (novoice+windows)
│   │   ├── wasapi_devices_other.go     # Linux/macOS device enumeration (novoice+!windows)
│   │   ├── server_info.go              # Client server info model
│   │   ├── server_ping.go              # Server health check
│   │   ├── reconnect_strategy.go       # Exponential backoff reconnection
│   │   └── banners.go                  # ASCII art banners
│   ├── server/
│   │   ├── server.go    # Server implementation & HTTP endpoints
│   │   ├── client.go    # Client connection handler (WebSocket read/write)
│   │   ├── handlers.go  # WebSocket opcode handlers (incl. voice signaling)
│   │   └── hub.go       # Message broadcasting hub, voice channel tracking
│   ├── database/
│   │   └── sqlite.go    # Database interface (all CRUD + voice_states)
│   ├── protocol/
│   │   └── messages.go  # WebSocket protocol definitions (opcodes, events, payloads)
│   └── models/
│       ├── message.go   # Message model
│       ├── user.go      # User model with status
│       ├── channel.go   # Channel model (text, voice, category, DM)
│       ├── role.go      # Role model with permission bitfield
│       └── voice.go     # VoiceState model
├── ~/.concord/          # Client-side configuration
│   ├── servers.json     # Server list & credentials
│   └── config.json      # App preferences, identity, audio config
└── docs/
    ├── Concord Development Roadmap.md
    ├── Technical Architecture Diagram.md
    └── CLAUDE.md        # This file
```

---

## Protocol Summary

| OpCode | Name | Direction | Purpose |
|--------|------|-----------|---------|
| 0 | OpIdentify | C→S | Authentication |
| 1 | OpHeartbeat | C→S | Keep-alive |
| 3 | OpSendMessage | C→S | Chat message |
| 4 | OpTypingStart | C→S | Typing indicator |
| 5 | OpPresenceUpdate | C→S | Status change |
| 7 | OpChannelCreate | C→S | Create channel/category |
| 8 | OpChannelUpdate | C→S | Rename/move/reorder channel |
| 9 | OpChannelDelete | C→S | Delete channel/category |
| 10 | OpDispatch | S→C | All server events |
| 13 | OpReady | S→C | Auth success + initial state |
| 14 | OpInvalidSession | S→C | Auth failure |
| 16 | OpRequestMessages | C→S | Fetch message history |
| 17 | OpRoleAssign | C→S | Assign role to member |
| 18 | OpRoleRemove | C→S | Remove role from member |
| 19 | OpKickMember | C→S | Kick a member |
| 20 | OpBanMember | C→S | Ban a member |
| 21 | OpMuteMember | C→S | Server-mute a member |
| 22 | OpWhisper | C→S | Ephemeral DM |
| 45 | OpVoiceSignal | C↔S↔C | SDP/ICE relay for WebRTC |
| 46 | OpVoiceSpeaking | C→S | Speaking state broadcast |
| 47 | OpVoiceServerMute | C→S | Admin voice mute/deafen |

---

## How Users Connect to Servers

### Scenario: Friend Hosting a Server

1. **Friend starts server**: `./concord-server --port 8080`
2. **Friend shares**: "Connect to `192.168.1.100:8080`"
3. **User adds server**: Press `+` in server icons column, enter address/port/name
4. **Auto-connects**: Uses saved `LocalIdentity` (alias, email, password) — no manual login
5. **User is connected**: Lands directly in ViewMain; auto-granted Admin if first registrant

### Identity Across Servers

- **Single local identity**: Set once in `~/.concord/config.json` (alias + email + password)
- **Auto-registration**: Client automatically registers on new servers using saved identity
- **Token caching**: Per-server auth tokens saved in `servers.json` for fast reconnect
- **Auto-admin**: First registrant on a server is automatically granted the Admin role

---

## Current Status

### Completed ✅

- Multi-server support (IRC-like model)
- Four-column Discord-like layout with voice groups in Members panel
- Hierarchical channel categories with collapse/expand
- Voice channels with real-time P2P audio (WebRTC + malgo)
- Voice Activity Detection (VAD), Push-to-Talk (PTT), self-mute/deafen
- Admin voice mute/deafen/unmute, move-voice
- Per-user volume control (0–200%, 1% steps)
- VU level bars, connection quality bars, speaking indicators in Members panel
- Codec quality presets (low/medium/high/ultra — wired to capture sample rate)
- Server list management (~/.concord/servers.json)
- Connection manager for multiple servers with auto-reconnect
- Local identity system (alias/email/password in config.json)
- Auto-connect on startup; first server auto-selected
- Role-grouped members panel with presence dots
- Member context menu with all moderation commands, permission-gated by role
- Unread tracking with @mention badges
- @mention autocomplete and highlighting
- Theme browser (Ctrl+T) with live preview
- Settings overlay (Ctrl+S): Theme, Notifications, Display, Audio
- Moderation: /kick, /ban, /unban, /mute, /unmute, /timeout, /role assign/remove
- Channel management: /create-channel, /delete-channel, /rename-channel, /move-channel
- Whispers (/whisper), URL hyperlinks (OSC 8), multi-line input
- Channel reordering (Shift+↑/↓), two-level message navigation (Alt+M)
- Server management view (Ctrl+B): add, reorder, ping, delete servers
- Server admin panel: manage roles, channels (type/MaxUsers), members

### Next Steps

1. Multi-user voice testing (external)
2. `Misc` branch: miscellaneous polish and UX improvements
3. Bot framework + AI integration

---

## Development Notes

### Building

```bash
# Default (voice enabled) — requires GCC / MSYS2 on Windows
CGO_ENABLED=1 go build -o build/concord-client ./cmd/client
go build -o build/concord-server ./cmd/server

# Windows with MSYS2 GCC
PATH="/c/msys64/mingw64/bin:$PATH" CGO_ENABLED=1 go build -o build/concord-client.exe ./cmd/client

# Headless / CI (no CGO, no audio)
CGO_ENABLED=0 go build -tags novoice -o build/concord-client-novoice ./cmd/client

# Makefile targets
make build-windows-voice   # Windows with WASAPI audio
make build-client-novoice  # No audio, no CGO
make build-server          # Server (no CGO ever needed)
```

### Key Architecture Decisions

- **bubbletea Elm architecture**: All state mutations happen in `Update()`, side effects return as `tea.Cmd`
- **ServerScopedMsg pattern**: Multi-server events route through `waitForConnEvent()` → `connEvents` channel, tagged with `serverID`, processed in Update loop
- **Two-phase WebSocket connect**: HTTP auth first (synchronous, reliable) then WebSocket (async, non-blocking) to prevent READY race
- **Pure-Go SQLite**: `modernc.org/sqlite` — no CGO dependency, works on all platforms without build toolchain
- **OSC 8 hyperlinks**: Use `ESC]8;;URL\ESC\\TEXT\ESC]8;;\ESC\\` (ST terminator, not BEL) for Windows Terminal compatibility
- **Voice goroutine isolation**: Audio goroutines communicate with bubbletea ONLY via `chan interface{}` (`eventOut`) — never call tea methods directly
- **P2P signaling**: Server relays SDP/ICE via OpVoiceSignal; clients establish direct DataChannel connections
- **novoice build tag**: Opt-out from audio; `voice_engine_stub.go` provides identical signatures so the rest of the client always compiles

---

## Known UX Considerations

- **Category naming**: Categories created with lowercase names but displayed in uppercase (UI convention)
- **Channel reordering**: Shift+↑/↓ reorders within the same category/level only; use /move-channel to change category
- **Whispers**: Ephemeral — not stored in DB, lost if recipient is offline
- **Password storage**: Identity password stored plaintext in config.json (readable only by OS user, same model as SSH keys); encryption planned
- **Terminal hyperlinks**: OSC 8 links require a supported terminal (Windows Terminal, iTerm2, Kitty, etc.) for Ctrl+Click
- **Shift+Enter newline**: Works in Kitty/modern terminals; use Ctrl+J (most reliable) or Ctrl+Enter as alternatives
- **Voice TURN**: Without a TURN relay server, ~15% of connections fail (symmetric NAT); Google public STUN is the fallback
- **PTT in terminal**: Push-to-Talk (Ctrl+Space default) is recommended over VAD because keyboard typing triggers VAD false positives

---

Last Updated: 2026-04-16
