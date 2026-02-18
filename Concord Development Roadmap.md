# Terminal Chat Application Roadmap

This document outlines a phased development roadmap for the terminal-based Discord-like chat application. It is designed to balance early usability, architectural soundness, and future extensibility (notably voice support).

---

## Guiding Principles

- **Terminal-first UX**: Respect terminal conventions instead of mimicking GUI patterns blindly.
    
- **Incremental complexity**: Each version adds depth without destabilizing the core.
    
- **Discoverability**: Users should learn the app from within the app.
    
- **Extensibility**: Avoid architectural dead-ends early.
    

---

## v0.1.0 — Discord-Like UI Refactor (Current Focus)

### Core Features

- Text-only messaging

- Multi-server support (IRC-like model)

- Client-side server list management

- Folder explorer-style channel categories

- Message send/receive

- Scrollable message history


### Four-Column UI Layout

- **Column 1**: Server Icons (~10 chars) - Colored circles with server initials

- **Column 2**: Channels (~30 chars) - Collapsible categories, folder explorer style

- **Column 3**: Chat (flexible, fills remaining space) - Messages and input

- **Column 4**: Members (~30 chars) - Grouped by role with colored circle avatars


### Terminal Requirements

- **Minimum Width**: 160 columns (comfortable viewing)

- **Recommended**: 200+ columns (spacious)

- **Fixed Column Widths**: Maintains retro terminal aesthetic


### UX Basics

- Empty-state messaging ("No messages yet. Say hello!")

- Status bar with connection state

- Server addition UI (address/port entry)

- Collapsible channel categories

- Collapsible members list (can overlay chat if needed)


### Technical Goals

- Stable networking layer

- Clear client/server message protocol

- Deterministic rendering loop

- Client-side configuration (~/.concord/servers.json)

- Client-side user preferences (default username/email)
    

---

## v0.2.0 — Interaction & Discoverability

### Input & Navigation

- Slash command system (`/join`, `/leave`, `/nick`, `/theme`, etc.)
    
- Pane focus switching (keyboard-driven)
    
- Scrollback independent of input
    

### Help & Onboarding

- Help overlay (`?` or `Ctrl+H`)
    
- Contextual footer hints
    
- Inline error feedback for invalid commands
    

### Persistence

- Local message history cache
    
- Graceful reconnects
    
- Optional plaintext logging per server/channel
    

---

## v0.2.0 — Multi-Server Architecture (HIGH PRIORITY)

### IRC-Like Multi-Server Model

- Each server is an independent entity (decentralized)

- Client connects to multiple servers simultaneously

- Client-side server list (~/.concord/servers.json)

- Server addition/removal UI within the application


### Server Management

- Add server dialog (address, port, optional saved credentials)

- Server switching via keybindings or clicking server icons

- Per-server state isolation

- Automatic reconnection handling


### Identity Management

- Client-side default preferences (username, email)

- Per-server authentication (register once per server)

- Consistent alias across servers via client preferences

- Optional saved credentials per server (encrypted tokens)


### Implementation Priority

**Phase 1**: Multi-server foundation (CRITICAL - enables remote connectivity)

**Phase 2**: Folder explorer-style channel categories (database-driven)

**Phase 3**: Enhanced panels (server icons, member grouping, role colors)


## v0.3.0 — Channel Management & Categories

### Channels

- Database-driven channel categories (flexible, admin-configurable)

- Folder explorer-style layout (hierarchical, collapsible)

- Create/delete channels (permissions permitting)

- Category creation/management

- Unread indicators

- Channel sorting within categories


### Category Features

- Collapsible/expandable sections

- Nested channel organization

- Visual hierarchy with indentation

- Keyboard navigation through categories
    

---

## v0.4.0 — Theming & Customization

### Theme System

- Theme schema with semantic color roles
    
- Built-in themes:
    
    - Dracula (dark)
        
    - Alucard Dark
        
    - Alucard Light
        
- Runtime theme switching
    

### UI Customization

- Config file support
    
- Keybinding remapping
    
- Optional compact / dense layouts
    

### Accessibility

- High-contrast themes
    
- Colorblind-safe palette option
    
- Keyboard-only navigation validation
    

---

## v0.5.0 — Messaging Enhancements

### Message Features

- Mentions with highlighting
    
- Message reactions (emoji / ASCII)
    
- `/me` actions
    
- Basic markdown subset (bold, italics, code)
    

### Notifications

- Terminal bell notifications
    
- OS-level notifications (where supported)
    
- Mention-only notification mode
    

---

## v0.6.0 — Bots & Automation (Optional but Strategic)

### Bot Support

- Server-side bot framework (minimal)
    
- Event hooks (message received, user joined, etc.)
    
- Simple scripting interface
    

### Automation

- Auto-join channels
    
- Startup scripts
    
- Scheduled messages (server-side)
    

---

## v1.0.0 — Stable Text Client Release

### Stability Goals

- Protocol freeze
    
- Backward compatibility guarantees
    
- Comprehensive error handling
    
- Performance tuning for large channels
    

### Documentation

- Full README
    
- Command reference
    
- Theming guide
    
- Contribution guidelines
    

---

## v2.0.0 — Voice Support

### Voice Architecture

- Parallel voice subsystem (separate from text)
    
- Client/server voice session management
    
- Opus codec
    
- Jitter buffering and packet loss handling
    

### Voice UX

- Join/leave voice channels
    
- Push-to-talk
    
- Mute / Deafen
    
- Speaking indicators
    

### Audio Backend

- Linux: PipeWire / PulseAudio
    
- Windows: WASAPI
    
- macOS: CoreAudio
    

---

## v2.1.0 — Advanced Voice Features

- Voice activation detection
    
- Per-user volume control
    
- Audio device switching at runtime
    
- Network diagnostics overlay
    

---

## v3.0.0 — Ecosystem & Extensibility

### Plugin System

- Plugin lifecycle hooks
    
- UI extension points
    
- Custom commands
    

### Federation (Optional)

- Cross-server bridging
    
- Shared identity support
    

### Power Features

- TUI layouts per workspace
    
- Scripting via embedded language
    
- Advanced moderation tools
    

---

## Long-Term Considerations

- Security & encryption
    
- Rate limiting and abuse prevention
    
- Scalability testing
    
- Screen reader compatibility
    

---

## Philosophy Reminder

> Build for clarity first, features second.  
> A terminal app that feels _predictable_ will always outperform one that feels _clever_.