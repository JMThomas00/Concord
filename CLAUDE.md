# Concord - Terminal Chat Application

## Project Overview

Concord is a Discord-like terminal chat application built with Go and bubbletea. It aims to provide a modern chat experience while maintaining a retro terminal aesthetic. The application follows an IRC-like decentralized model where each server is independently hosted.

**Status**: v0.1.0 feature-complete — multi-server, identity setup, auto-connect, slash commands. Phase 4 (Members panel) in progress.

---

## Technical Stack

### Core Technologies

- **Language**: Go 1.21+
- **TUI Framework**: [bubbletea](https://github.com/charmbracelet/bubbletea) (The Elm Architecture)
- **Styling**: [lipgloss](https://github.com/charmbracelet/lipgloss) (Declarative terminal styling)
- **Components**: [bubbles](https://github.com/charmbracelet/bubbles) (textarea, viewport, textinput)
- **Database**: SQLite with CGO ([mattn/go-sqlite3](https://github.com/mattn/go-sqlite3))
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
│ (~10ch)  │ (~30ch)             │ (flexible)                     │ (~30ch)             │
├──────────┼─────────────────────┼────────────────────────────────┼─────────────────────┤
│          │                     │                                │                     │
│  (D)  ●  │ ▼ TEXT CHANNELS     │ gh0st                   10:30  │ ── Admin ──         │
│          │   # general         │ Hey everyone!                  │  (A) alice          │
│  (M)  ●  │   # random          │                                │                     │
│          │   # memes           │ alice                   10:31  │ ── Moderators ──    │
│  (F)  ●  │                     │ Hi gh0st!                      │  (M) moderator1     │
│          │ ▼ VOICE CHANNELS    │                                │                     │
│   +      │   🔊 General        │ moderator1              10:32  │ ── Members ──       │
│          │   🔊 Gaming         │ Welcome!                       │  (B) bob            │
│          │                     │                                │  (C) charlie        │
│          │ ▼ ADMIN             │                                │  (G) gh0st          │
│          │   # mod-chat        │                                │                     │
│          │   # announcements   │                                │                     │
│          │                     │                                │                     │
│          │                     │                                │                     │
│          │                     │                                │                     │
├──────────┴─────────────────────┴────────────────────────────────┴─────────────────────┤
│ [gh0st@localhost:8080] Concord v0.1.0 │ #general │ ? for help                        │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

### Column Breakdown

1. **Server Icons** (~10 chars)
   - Colored circles with server initials
   - Active server highlighting
   - Unread indicators
   - `+` button to add servers

2. **Channels** (~30 chars)
   - Folder explorer-style hierarchical layout
   - Collapsible channel categories
   - Indented nested structure
   - Visual indicators (▼/▶ for collapse state)

3. **Chat** (flexible, fills remaining space)
   - Message history viewport
   - Sender with colored circle avatar
   - Timestamp
   - Input bar at bottom

4. **Members** (~30 chars)
   - Grouped by role (Admin, Moderator, Member)
   - Colored circle avatars with initials
   - Presence indicators
   - Collapsible (can overlay chat on narrow screens)

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
    "collapsed_categories": []
  }
}
```

- **LocalIdentity**: Single identity used across all servers (alias, email, password stored once)
- Theme selection, keybindings, collapsed category state

---

## Key Features

### Current (v0.1.0 Target)

- Multi-server support (IRC-like model)
- Four-column Discord-like layout
- Folder explorer-style channel categories
- Database-driven categories (admin-configurable)
- Colored circle avatars with initials
- Fixed column widths (terminal aesthetic)
- Server addition UI (address/port entry)
- Client-side server list
- Default user preferences (consistent alias across servers)

### Planned

- **v0.2.0**: Slash commands, help system, persistence
- **v0.3.0**: Channel management, unread indicators
- **v0.4.0**: Theme system, customization
- **v0.5.0**: Mentions, reactions, markdown
- **v1.0.0**: Stable text client release
- **v2.0.0**: Voice support (separate subsystem)

---

## Database Schema

### Channels Table (Extended)

```sql
CREATE TABLE channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    category TEXT NOT NULL,  -- NEW: channel category for grouping
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Categories managed through:
- Database-driven (flexible)
- Server admins can create/manage categories
- Collapsible UI state stored client-side

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
- Help overlay (`?` or `Ctrl+H`)
- Contextual footer hints

### Extensibility
- Avoid architectural dead-ends
- Plugin system (v3.0+)
- Bot support (v0.6.0+)

---

## Implementation Priorities

### Phase 1: Multi-Server Foundation ✅ COMPLETE
**Priority**: Highest - Enables remote connectivity

1. Server list management (~/.concord/servers.json) ✅
2. "Add Server" dialog UI ✅
3. Multi-connection manager ✅
4. Per-server state isolation ✅
5. Default user preferences ✅

**Rationale**: Without this, Concord is just a localhost toy. With it, becomes a real distributed chat system.

**Status**: Completed 2026-02-09. Users can now add multiple servers, connect to remote servers, and manage per-server state.

### Phase 2: Folder Explorer Categories ✅ COMPLETE
**Priority**: High - Core UI feature

1. Database schema for categories ✅
2. Collapsible category UI ✅
3. Hierarchical rendering ✅
4. Keyboard navigation ✅
5. Real-time event handling ✅

**Status**: Completed 2026-02-09. Channel tree structure implemented with collapse/expand functionality, persistent state, and real-time updates.

### Phase 3: Channel Management Commands ✅ COMPLETE
**Priority**: High - Essential for usability

1. Slash command parser (/create-channel, /delete-channel, etc.) ✅
2. Protocol opcodes for channel operations ✅
3. Server-side permission checking ✅
4. Database CRUD operations ✅
5. Real-time channel synchronization ✅

**Rationale**: Without the ability to create/manage channels via commands, users cannot test or use the Phase 2 hierarchical UI.

**Status**: Completed 2026-02-13. All slash commands functional including /create-channel, /create-category, /delete-channel, /delete-category, /rename-channel, /move-channel, and /help.

### Phase 3.5: Connection Resilience & Server Management ✅ COMPLETE
**Priority**: Critical - Prevents user lockout

1. Non-blocking WebSocket connections ✅
2. Async connection after authentication ✅
3. Connection timeouts (10s for HTTP/WebSocket) ✅
4. Auto-reconnect with exponential backoff ✅
5. Manage Servers view (Ctrl+M from login) ✅
6. Server ping/health check functionality ✅
7. Pre-authentication server deletion ✅

**Rationale**: Users were getting locked out if any server in their list was down. Login flow blocked on TCP socket connection (~30-60s timeout), making the app unusable.

**Status**: Completed 2026-02-13. Users can now login even with unreachable servers, manage servers before authentication, ping servers to check health, and benefit from automatic reconnection attempts.

### Phase 3.6: Message Broadcasting Fix ✅ CRITICAL
**Priority**: Showstopper - Messages weren't displaying

1. Auto-join channels on authentication ✅
2. Auto-join new channels when created ✅
3. Hub channelClients map population ✅

**Rationale**: Messages weren't appearing in chat because clients weren't being added to the hub's channelClients map. BroadcastToChannel() found no recipients.

**Status**: Completed 2026-02-13. Real-time messaging now works correctly.

### Phase 3.7: Identity & Auto-Connect ✅ COMPLETE
**Priority**: High - Zero-friction startup

1. LocalIdentity struct stored in config.json ✅
2. First-run ViewIdentitySetup screen ✅
3. Auto-connect on startup (all servers, background) ✅
4. HTTP-only AutoConnectHTTP (login → register fallback) ✅
5. Two-phase connect (HTTP auth + WebSocket) to fix READY race condition ✅
6. First server auto-selected in UI on launch ✅
7. Input textarea focused immediately on launch ✅
8. Server reordering (Shift+↑/↓) in Manage Servers ✅

**Status**: Completed 2026-02-17. App now opens directly to ViewMain with all servers connecting in background; no manual login step required.

### Phase 4: Members Panel 🚧 IN PROGRESS
**Priority**: Medium - Core UI feature

1. Role-grouped member list (Admin, Moderators, Members)
2. Colored circle avatars with initials
3. Presence indicators (●online ○offline ◑connecting)
4. Multi-user testing across multiple servers
5. Visual polish (borders, spacing)

---

## File Structure

```
Concord/
├── cmd/
│   ├── client/          # Client entry point
│   └── server/          # Server entry point
├── internal/
│   ├── client/
│   │   ├── app.go                   # Main application state
│   │   ├── views.go                 # UI rendering (4 columns)
│   │   ├── connection.go            # WebSocket management
│   │   ├── connection_manager.go    # Multi-server connection manager
│   │   ├── commands.go              # Slash command handling
│   │   ├── channel_tree.go          # Hierarchical channel data structure
│   │   ├── add_server_view.go       # Add server dialog UI
│   │   ├── manage_servers_view.go   # Pre-auth server management UI (Shift+↑/↓ reorder)
│   │   ├── identity_setup_view.go   # First-run identity setup screen
│   │   ├── server_info.go           # Client server info model
│   │   ├── server_ping.go           # Server health check
│   │   ├── reconnect_strategy.go    # Exponential backoff reconnection
│   │   ├── config.go                # Configuration file management
│   │   └── banners.go               # ASCII art banners
│   ├── server/
│   │   ├── server.go    # Server implementation
│   │   ├── client.go    # Client connection handler
│   │   ├── handlers.go  # WebSocket handlers
│   │   └── hub.go       # Message broadcasting hub
│   ├── database/
│   │   └── sqlite.go    # Database interface
│   ├── protocol/
│   │   └── messages.go  # WebSocket protocol definitions
│   └── models/
│       ├── message.go   # Message model
│       ├── user.go      # User model
│       ├── channel.go   # Channel model (with categories)
│       └── role.go      # Role model
├── ~/.concord/          # Client-side configuration
│   ├── servers.json     # Server list & preferences
│   └── config.json      # App preferences
└── docs/
    ├── Concord Development Roadmap.md
    ├── Technical Architecture Diagram.md
    └── CLAUDE.md        # This file
```

---

## How Users Connect to Servers

### Scenario: Friend Hosting a Server

1. **Friend starts server**: `./concord-server --port 8080`
2. **Friend shares**: "Connect to `192.168.1.100:8080`"
3. **User adds server**:
   - Click `+` in server icons column
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
- **Slash commands**: /create-channel, /create-category, /delete-channel, /delete-category, /rename-channel, /move-channel, /help
- **Connection resilience**: Non-blocking connections, exponential backoff, 10s timeouts
- **Server management**: Manage Servers view (Ctrl+M), server ping, pre-auth deletion, Shift+↑/↓ reordering
- **Message broadcasting**: Auto-join channels for proper event delivery
- **Local identity system**: Single identity (alias/email/password) stored in config.json, used across all servers
- **Auto-connect on startup**: All known servers connect in background when app launches; first server auto-selected in UI
- **Identity setup view**: First-run screen (ViewIdentitySetup) collects alias/email/password once
- **Auto-register flow**: Client HTTP-login → HTTP-register → WebSocket connect, no manual login needed
- **Two-phase connection**: `AutoConnectHTTP` (HTTP only) + `connectServerAsync` (WebSocket) prevents READY race condition
- **Input focus fix**: Textarea focused immediately on launch; typing works without needing to Tab first
- **UI polish**: Simplified input placeholder, `/help` tooltip in status bar, channel column widened to 26 chars

### In Progress 🚧

- **Phase 4**: Members Panel
  - Role-grouped member list (Admin, Moderators, Members)
  - Colored circle avatars with initials
  - Presence indicators
  - Multi-user testing across multiple servers

### Next Steps

1. Implement Members panel with role grouping (Phase 4)
2. Multi-user and multi-server integration testing
3. Presence indicators (online/offline/connecting)
4. Prepare for v0.1.0 release candidate

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

---

## Contributing

See [Concord Development Roadmap.md](Concord Development Roadmap.md) for detailed feature planning.

See [Technical Architecture Diagram.md](Technical Architecture Diagram.md) for system architecture details.

---

## Philosophy

> Build for clarity first, features second.
> A terminal app that feels _predictable_ will always outperform one that feels _clever_.

---

## Known UX Considerations

- **Category naming**: Categories are created with lowercase names but displayed in uppercase (UI inconsistency)
- **Case sensitivity**: Command arguments are case-insensitive (fixed 2026-02-13)
- **Error messaging**: Some error messages could be more user-friendly
- **Help discoverability**: `/help` now shown in status bar tooltip; Ctrl+M for server management
- **Password storage**: Identity password stored plaintext in config.json (readable only by OS user, same model as SSH keys); encryption planned for future version
- **Members panel**: Column renders but shows placeholder — role-grouped list not yet implemented (Phase 4)

---

Last Updated: 2026-02-17
