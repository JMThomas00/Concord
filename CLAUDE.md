# Concord - Terminal Chat Application

## Project Overview

Concord is a Discord-like terminal chat application built with Go and bubbletea. It aims to provide a modern chat experience while maintaining a retro terminal aesthetic. The application follows an IRC-like decentralized model where each server is independently hosted.

**Status**: v0.1.0 feature-complete — multi-server, identity setup, auto-connect, slash commands, members panel, unread tracking, @mentions, theme browser, channel reordering. Phase 5 (Notifications polish & Role/Moderation) next.

---

## Technical Stack

### Core Technologies

- **Language**: Go 1.24
- **TUI Framework**: [bubbletea](https://github.com/charmbracelet/bubbletea) (The Elm Architecture)
- **Styling**: [lipgloss](https://github.com/charmbracelet/lipgloss) (Declarative terminal styling)
- **Components**: [bubbles](https://github.com/charmbracelet/bubbles) (textarea, viewport, textinput)
- **Database**: SQLite via `modernc.org/sqlite` (pure-Go, no CGO)
- **WebSocket**: [gorilla/websocket](https://github.com/gorilla/websocket)

### Themes

- **Dracula** (dark theme)
- **Alucard Dark** (dark theme variant)
- **Alucard Light** (light theme variant)

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

### Benefits

- **Privacy**: No centralized data collection
- **Control**: Server admins have complete autonomy
- **Simplicity**: No federation protocols
- **Resilience**: One server failure doesn't affect others

---

## UI Layout (Four-Column Design)

```
┌──────────┬─────────────────────┬────────────────────────────────┬─────────────────────┐
│ Servers  │ Channels            │ Chat                           │ Members             │
│ (~22ch)  │ (~26ch)             │ (flexible)                     │ (~30ch)             │
├──────────┼─────────────────────┼────────────────────────────────┼─────────────────────┤
│          │                     │                                │                     │
│  (D)  ●  │ ▼ TEXT CHANNELS     │ gh0st                   10:30  │ ── Admin ──         │
│          │   # general    ●    │ Hey everyone!                  │  (A) alice       ●  │
│  (M)  ●  │   # random  @2      │                                │                     │
│          │   # memes           │ alice                   10:31  │ ── Moderators ──    │
│  (F)  ●  │                     │ Hi gh0st!                      │  (M) mod1        ●  │
│          │ ▼ VOICE CHANNELS    │                                │                     │
│   +      │   🔊 General        │ moderator1              10:32  │ ── Members ──       │
│          │   🔊 Gaming         │ Welcome!                       │  (B) bob         ○  │
│          │                     │                                │  (C) charlie     ●  │
│          │ ▼ ADMIN             │                                │  (G) gh0st       ●  │
│          │   # mod-chat        │                                │                     │
│          │   # announcements   │                                │                     │
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
   - Shift+↑/↓ to reorder channels within a category
   - Visual indicators (▼/▶ for collapse state)

3. **Chat** (flexible, fills remaining space)
   - Message history viewport with scrollback (PgUp/PgDn)
   - Sender with colored circle avatar
   - Timestamp right-aligned
   - `@mention` highlights in your color
   - URL hyperlinks (OSC 8, Ctrl+Click in supported terminals)
   - `[DM]` prefix for whispers
   - Multi-line compose with Ctrl+J / Ctrl+Enter

4. **Members** (~30 chars)
   - Role-grouped: Admin → Moderators → Members
   - Colored circle avatars with initials (role color)
   - Presence dots: `●` online, `○` offline

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
    },
    {
      "id": "uuid-2",
      "name": "Friend's Server",
      "address": "friend.example.com",
      "port": 8080,
      "last_connected": null
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
    "muted_channels": []
  }
}
```

- **LocalIdentity**: Single identity used across all servers (alias, email, password stored once)
- Theme selection, collapsed category state, muted channels

---

## Key Features

### Current (v0.1.0 Target)

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

### Planned

- **Role & Moderation**: /role, /kick, /ban, /mute commands
- **Server First-Run Setup**: Interactive TUI setup for new servers
- **Notifications**: OS-level and terminal bell for @mentions
- **Bots**: Server-side bot framework with event hooks
- **AI Integration**: LLM-powered assistant bots, /ai command, inline compose help

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
    position INTEGER DEFAULT 0,  -- Ordering within parent/category
    category_id TEXT REFERENCES channels(id),
    is_nsfw INTEGER DEFAULT 0,
    rate_limit_per_user INTEGER DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
```

### Categories managed through:
- Same channels table (`type = 2`)
- `position` field controls ordering; Shift+↑/↓ reorders via OpChannelUpdate
- Collapsible UI state stored client-side in config.json

---

## Design Principles

### Terminal-First UX
- Respect terminal conventions
- Fixed widths for retro feel
- No image rendering (colored circles instead)
- Keyboard-driven navigation

### Incremental Complexity
- Each version adds depth without destabilizing core
- Text-first, voice later (v2.0+)

### Discoverability
- Users learn from within the app
- Help overlay (`/help` command, shown in status bar)
- Contextual footer hints

### Extensibility
- Avoid architectural dead-ends
- Bot framework (v0.6.0)
- AI integration (v0.7.0)
- Plugin system (v3.0+)

---

## Implementation Priorities

### Phase 1: Multi-Server Foundation ✅ COMPLETE
**Priority**: Highest - Enables remote connectivity

1. Server list management (~/.concord/servers.json) ✅
2. "Add Server" dialog UI ✅
3. Multi-connection manager ✅
4. Per-server state isolation ✅
5. Default user preferences ✅

**Status**: Completed 2026-02-09.

### Phase 2: Folder Explorer Categories ✅ COMPLETE
**Priority**: High - Core UI feature

1. Database schema for categories ✅
2. Collapsible category UI ✅
3. Hierarchical rendering ✅
4. Keyboard navigation ✅
5. Real-time event handling ✅

**Status**: Completed 2026-02-09.

### Phase 3: Channel Management Commands ✅ COMPLETE
**Priority**: High - Essential for usability

1. Slash command parser (/create-channel, /delete-channel, etc.) ✅
2. Protocol opcodes for channel operations ✅
3. Server-side permission checking ✅
4. Database CRUD operations ✅
5. Real-time channel synchronization ✅

**Status**: Completed 2026-02-13.

### Phase 3.5: Connection Resilience & Server Management ✅ COMPLETE
**Priority**: Critical - Prevents user lockout

1. Non-blocking WebSocket connections ✅
2. Async connection after authentication ✅
3. Connection timeouts (10s for HTTP/WebSocket) ✅
4. Auto-reconnect with exponential backoff ✅
5. Manage Servers view (Ctrl+B from login/main) ✅
6. Server ping/health check functionality ✅
7. Pre-authentication server deletion ✅

**Status**: Completed 2026-02-13.

### Phase 3.6: Message Broadcasting Fix ✅ CRITICAL
**Priority**: Showstopper - Messages weren't displaying

1. Auto-join channels on authentication ✅
2. Auto-join new channels when created ✅
3. Hub channelClients map population ✅

**Status**: Completed 2026-02-13.

### Phase 3.7: Identity & Auto-Connect ✅ COMPLETE
**Priority**: High - Zero-friction startup

1. LocalIdentity struct stored in config.json ✅
2. First-run ViewIdentitySetup screen ✅
3. Auto-connect on startup (all servers, background) ✅
4. HTTP-only AutoConnectHTTP (login → register fallback) ✅
5. Two-phase connect (HTTP auth + WebSocket) to fix READY race condition ✅
6. First server auto-selected in UI on launch ✅
7. Server reordering (Shift+↑/↓) in Manage Servers ✅

**Status**: Completed 2026-02-17.

### Phase 3.8: Real-Time Updates & UX Polish ✅ COMPLETE
**Priority**: High - Core chat reliability

1. Offline presence broadcasting (hub.unregisterClient fix) ✅
2. Real-time member add (EventServerMemberAdd broadcast on identify) ✅
3. Duplicate member upsert fix ✅
4. Chat viewport border fix (removed incorrect WindowSizeMsg pass-through) ✅
5. OSC 8 URL hyperlink rendering per message line ✅
6. @mention autocomplete popup (Tab to complete) ✅
7. @mention highlighting in received messages ✅
8. Ctrl+Q: Quit (Ctrl+C reserved for copy) ✅
9. Multi-line compose: Ctrl+J / Ctrl+Enter / Shift+Enter ✅
10. handleReady race fix: correct channels shown on startup with multiple servers ✅
11. Shift+↑/↓ channel reordering within categories ✅

**Status**: Completed 2026-02-18.

### Phase 4: Members Panel & Unread Tracking ✅ COMPLETE
**Priority**: Medium - Core UI feature

1. Role-grouped member list (Admin → Moderators → Members) ✅
2. Colored circle avatars with role color ✅
3. Presence indicators (● online, ○ offline) ✅
4. Unread channel counts with @mention badges ✅
5. /mute and /unmute per channel ✅
6. Theme browser (Ctrl+T) with real-time preview ✅
7. /whisper ephemeral DMs ✅

**Status**: Completed 2026-02-18.

### Phase 5: Role & Moderation Commands 🔜 NEXT
**Priority**: Medium - Admin tooling

1. /role assign/remove @user rolename
2. /kick @user — disconnect + remove from server
3. /ban @user — persistent ban (DB + check on join)
4. /mute @user — server-side message suppression
5. Server first-run setup TUI (--admin-email recovery flag)
6. Auto-admin for first registrant
7. OpCodes 17–22 (RoleAssign, RoleRemove, Kick, Ban, MuteMember, Whisper)

### Phase 6: Bots & AI Integration 🔮 PLANNED
**Priority**: Strategic - Extensibility

See detailed plan in [Concord Development Roadmap.md](Concord Development Roadmap.md).

1. Server-side bot framework (v0.6.0)
2. AI provider integration — OpenAI-compatible, Anthropic, Ollama (v0.7.0)
3. /ai command, AI personas, channel context windows
4. AI moderation and summarization helpers

---

## File Structure

```
Concord/
├── cmd/
│   ├── client/          # Client entry point
│   └── server/          # Server entry point
├── internal/
│   ├── client/
│   │   ├── app.go                   # Main application state & event handlers
│   │   ├── views.go                 # UI rendering (4 columns)
│   │   ├── connection.go            # WebSocket management
│   │   ├── connection_manager.go    # Multi-server connection manager
│   │   ├── commands.go              # Slash command handling
│   │   ├── channel_tree.go          # Hierarchical channel data structure
│   │   ├── add_server_view.go       # Add server dialog UI
│   │   ├── manage_servers_view.go   # Pre-auth server management UI
│   │   ├── identity_setup_view.go   # First-run identity setup screen
│   │   ├── theme_browser_view.go    # Theme browser with live preview
│   │   ├── server_info.go           # Client server info model
│   │   ├── server_ping.go           # Server health check
│   │   ├── reconnect_strategy.go    # Exponential backoff reconnection
│   │   ├── config.go                # Configuration file management
│   │   └── banners.go               # ASCII art banners
│   ├── server/
│   │   ├── server.go    # Server implementation & HTTP endpoints
│   │   ├── client.go    # Client connection handler (WebSocket read/write)
│   │   ├── handlers.go  # WebSocket opcode handlers
│   │   └── hub.go       # Message broadcasting hub & typing manager
│   ├── database/
│   │   └── sqlite.go    # Database interface (all CRUD operations)
│   ├── protocol/
│   │   └── messages.go  # WebSocket protocol definitions (opcodes, events, payloads)
│   └── models/
│       ├── message.go   # Message model
│       ├── user.go      # User model with status
│       ├── channel.go   # Channel model (text, voice, category, DM)
│       └── role.go      # Role model with permission bitfield
├── ~/.concord/          # Client-side configuration
│   ├── servers.json     # Server list & credentials
│   └── config.json      # App preferences, identity, muted channels
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

---

## How Users Connect to Servers

### Scenario: Friend Hosting a Server

1. **Friend starts server**: `./concord-server --port 8080`
2. **Friend shares**: "Connect to `192.168.1.100:8080`"
3. **User adds server**:
   - Press `+` in server icons column (or Ctrl+N)
   - Enter address: `192.168.1.100`
   - Enter port: `8080`
   - Enter name: "Friend's Server"
4. **Auto-connects**: Client uses saved `LocalIdentity` (alias, email, password) to register or log in automatically — no manual login step needed
5. **User is connected**: Lands directly in ViewMain; server appears in sidebar as connected

### Identity Across Servers

- **Single local identity**: Set once in `~/.concord/config.json` (alias + email + password)
- **Auto-registration**: Client automatically registers on new servers using saved identity
- **Token caching**: Per-server auth tokens saved in `servers.json` for fast reconnect
- **No federation**: All controlled client-side, no central directory

---

## Current Status

### Completed ✅

- Multi-server support (IRC-like model)
- Four-column Discord-like layout
- Hierarchical channel categories with collapse/expand
- Server list management (~/.concord/servers.json)
- Connection manager for multiple servers
- Per-server state isolation
- Channel tree data structure
- Keyboard navigation (arrows for selection, left/right for collapse)
- Persistent UI state (collapsed categories saved to config)
- Real-time channel event handling
- Text messaging with real-time updates
- User authentication (per-server)
- WebSocket communication with auto-reconnect
- SQLite persistence
- **Slash commands**: /create-channel, /create-category, /delete-channel, /delete-category, /rename-channel, /move-channel, /theme, /mute, /unmute, /whisper, /help
- **Connection resilience**: Non-blocking connections, exponential backoff, 10s timeouts
- **Server management**: Manage Servers view (Ctrl+B), server ping, pre-auth deletion, Shift+↑/↓ reordering
- **Message broadcasting**: Auto-join channels for proper event delivery
- **Local identity system**: Single identity (alias/email/password) stored in config.json, used across all servers
- **Auto-connect on startup**: All known servers connect in background when app launches; first server auto-selected in UI
- **Identity setup view**: First-run screen (ViewIdentitySetup) collects alias/email/password once
- **Auto-register flow**: Client HTTP-login → HTTP-register → WebSocket connect, no manual login needed
- **Two-phase connection**: `AutoConnectHTTP` (HTTP only) + `connectServerAsync` (WebSocket) prevents READY race condition
- **Members panel**: Role-grouped (Admin → Moderators → Members) with colored circle avatars and presence dots
- **Unread tracking**: Per-channel unread `●` dot and `@N` mention count, cleared on channel switch; /mute to suppress
- **@mentions**: Autocomplete popup (type `@`, Tab to complete), highlighting in received messages
- **Theme browser**: Ctrl+T from main/login views; arrow keys for live preview; Enter saves, Esc reverts
- **Whispers**: /whisper @user message — ephemeral DM, shown with [DM] prefix
- **URL hyperlinks**: OSC 8 terminal links; Ctrl+Click to open in browser (terminal-dependent)
- **Multi-line input**: Ctrl+J, Ctrl+Enter, Shift+Enter
- **Channel reordering**: Shift+↑/↓ in channel list moves channel within its category; persisted to server
- **Startup fix**: Correct channels shown immediately on startup (handleReady race condition fixed)
- **Real-time member updates**: Members appear/disappear in real-time as users connect/disconnect
- **Offline presence**: Server broadcasts StatusOffline to all servers when client disconnects

### In Progress 🚧

- **Phase 5**: Role & Moderation Commands
  - /role, /kick, /ban, /mute @user
  - Server first-run setup TUI
  - bans DB table

### Next Steps

1. Phase 5: Role & moderation slash commands
2. Server first-run TUI setup + --admin-email flag
3. Phase 6: Bot framework + AI integration
4. v1.0.0 release candidate

---

## Development Notes

### Why IRC-Like Instead of Discord-Like?

Discord's model requires:
- Centralized infrastructure
- Complex federation
- Global identity management
- Centralized server directory

IRC's model provides:
- Simple decentralization
- Direct connections
- Independent servers
- No federation complexity
- Perfect for self-hosted terminal app

### Why Fixed Column Widths?

- **Retro aesthetic**: Feels like a terminal app
- **Predictable layout**: No responsive complexity
- **Simplicity**: Easier to render and maintain
- **Performance**: Fixed layout is faster
- **Character**: Embraces terminal limitations

### Why Colored Circles for Avatars?

- **Technical limitation**: Terminals can't render images
- **Creative solution**: Colored circles with initials
- **Visual identity**: Still provides user recognition
- **Performance**: Simple to render
- **Accessibility**: Works in any terminal

### Key Architecture Decisions

- **bubbletea Elm architecture**: All state mutations happen in `Update()`, side effects return as `tea.Cmd`
- **ServerScopedMsg pattern**: Multi-server events route through `waitForConnEvent()` → `connEvents` channel, tagged with `serverID`, processed in Update loop
- **Two-phase WebSocket connect**: HTTP auth first (synchronous, reliable) then WebSocket (async, non-blocking) to prevent READY race
- **Pure-Go SQLite**: `modernc.org/sqlite` — no CGO dependency, works on all platforms without build toolchain
- **OSC 8 hyperlinks**: Use `ESC]8;;URL\ESC\\TEXT\ESC]8;;\ESC\\` (ST terminator, not BEL) for Windows Terminal compatibility

---

## Known UX Considerations

- **Category naming**: Categories created with lowercase names but displayed in uppercase (UI convention)
- **Channel reordering**: Shift+↑/↓ reorders within the same category/level only; use /move-channel to change category
- **Whispers**: Ephemeral — not stored in DB, lost if recipient is offline
- **Password storage**: Identity password stored plaintext in config.json (readable only by OS user, same model as SSH keys); encryption planned for future version
- **Terminal hyperlinks**: OSC 8 links require a supported terminal (Windows Terminal, iTerm2, Kitty, etc.) for Ctrl+Click
- **Shift+Enter newline**: Works in Kitty/modern terminals; use Ctrl+J (most reliable) or Ctrl+Enter as alternatives

---

Last Updated: 2026-02-18
