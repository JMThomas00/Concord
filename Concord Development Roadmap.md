# Concord — Terminal Chat Application Roadmap

This document outlines the phased development roadmap. It is designed to balance early usability, architectural soundness, and future extensibility.

---

## Guiding Principles

- **Terminal-first UX**: Respect terminal conventions instead of mimicking GUI patterns blindly.
- **Incremental complexity**: Each version adds depth without destabilizing the core.
- **Discoverability**: Users should learn the app from within the app.
- **Extensibility**: Avoid architectural dead-ends early.

---

## v0.1.0 — Core Chat Client ✅ COMPLETE

### Multi-Server Foundation

- IRC-like model: independent, self-hosted servers
- Client-side server list (`~/.concord/servers.json`)
- Add/remove/reorder servers via UI
- Per-server state isolation
- Default user preferences (alias, email)

### Four-Column UI Layout

- **Column 1** — Server Icons (~22 chars): colored circles, unread indicators, `+` to add
- **Column 2** — Channels (~26 chars): collapsible categories, folder explorer style, unread badges
- **Column 3** — Chat (flexible): message history, colored avatars, timestamps, input bar
- **Column 4** — Members (~30 chars): role-grouped, presence dots, colored avatars

### Identity & Auto-Connect

- Single `LocalIdentity` stored in `~/.concord/config.json` (alias, email, password)
- First-run `ViewIdentitySetup` screen
- Auto-connect all known servers in background on startup
- HTTP login → register fallback → WebSocket connect (two-phase, race-free)
- Token caching per server for fast reconnect

### Channel Management Slash Commands

- `/create-channel <name>` — create text channel in current category
- `/create-category <name>` — create a new channel category
- `/delete-channel [name]` — delete a channel
- `/delete-category <name>` — delete a category and all its channels
- `/rename-channel <name>` — rename current channel
- `/move-channel <category>` — move current channel to a different category
- `/help` — list all commands

### Connection Resilience

- Non-blocking connections (10s timeout)
- Exponential backoff auto-reconnect
- Manage Servers view (Ctrl+B): ping, delete, reorder servers pre-auth

### Members Panel & Real-Time Updates

- Role-grouped member list: Admin → Moderators → Members
- Colored circle avatars using role color (deterministic fallback palette)
- Presence indicators: `●` online, `○` offline
- Real-time member add/remove via `EventServerMemberAdd` / `EventPresenceUpdate`
- Offline presence broadcast on disconnect

### Messaging Features

- Unread channel tracking: `●` dot and `@N` mention count
- `/mute` / `/unmute` per channel (client-side suppression)
- @mention autocomplete popup (type `@`, Tab to complete)
- @mention highlighting in received messages
- Multi-line compose: Ctrl+J / Ctrl+Enter / Shift+Enter
- URL hyperlinks: OSC 8 terminal links (Ctrl+Click in supported terminals)
- `/whisper @user <message>` — ephemeral in-memory DM (shown with `[DM]` prefix)

### Theme Browser

- Ctrl+T from main or login view
- Arrow keys for live preview across all 4 columns
- Enter saves selection to config; Esc reverts

### Channel Reordering

- Shift+↑/Shift+↓ in channel list
- Moves channel within its category; persists position to server via `OpChannelUpdate`

---

## v0.2.0 — Role & Moderation ⬜ NEXT

### Role Management

- `/role assign @user <rolename>` — grant a role
- `/role remove @user <rolename>` — revoke a role
- Requires `PermissionManageRoles`
- `EventServerMemberUpdate` broadcasts role changes to all clients

### Moderation Commands

- `/kick @user` — disconnect + remove from server
- `/ban @user [reason]` — persistent DB ban; blocked on reconnect
- `/mute @user` — server-side message suppression (server rejects messages)
- All require appropriate permissions (`PermissionKickMembers`, `PermissionBanMembers`, `PermissionMuteMembers`)

### Server First-Run Setup

- Interactive TUI setup when no config exists: server name, bind host, port, DB path
- Auto-creates `concord-server.toml`
- First registrant automatically receives Admin role
- `--admin-email <email>` flag: grants/re-grants admin role at startup (recovery path)

### New Protocol Opcodes (17–22)

| OpCode | Name | Purpose |
|--------|------|---------|
| 17 | OpRoleAssign | Assign role to member |
| 18 | OpRoleRemove | Remove role from member |
| 19 | OpKickMember | Kick a member |
| 20 | OpBanMember | Ban a member |
| 21 | OpMuteMember | Server-mute a member |
| 22 | OpWhisper | Ephemeral in-memory DM |

### New Database Tables

```sql
CREATE TABLE bans (
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
    reason    TEXT,
    banned_by TEXT NOT NULL REFERENCES users(id),
    banned_at DATETIME NOT NULL,
    PRIMARY KEY (server_id, user_id)
);
```

---

## v0.3.0 — Messaging Enhancements ⬜ PLANNED

### Message Features

- Message reactions (ASCII emoji subset: `:+1:`, `:heart:`, `:laugh:`)
- `/me <action>` — emote messages
- Basic markdown: `**bold**`, `*italic*`, `` `code` ``, ` ```codeblock``` `
- Message edit / delete (own messages, or admin)
- Message pinning per channel

### Notifications

- Terminal bell on @mention
- OS-level desktop notifications (where supported)
- Mention-only notification mode
- Per-server notification preferences

---

## v0.4.0 — Theming & Customization ⬜ PLANNED

### Theme System Overhaul

- Embed bundled themes at compile time via `//go:embed`
- User theme overrides: `~/.concord/themes/<name>.toml`
- New bundled themes:
  - Nord (cool blue-grey)
  - Gruvbox Dark (warm retro)
  - Monokai (classic Sublime Text)
  - Catppuccin Mocha (pastel dark)
- `/theme <name>` — apply theme directly from command line

### UI Customization

- Keybinding remapping via config
- Optional compact / dense message layout
- Configurable timestamp format
- Column width preferences

### Accessibility

- High-contrast themes
- Colorblind-safe palette option
- Screen reader compatibility hints

---

## v0.5.0 — Direct Messages & Voice (Preview) ⬜ PLANNED

### Direct Messages

- `/dm @user` — open a persistent DM channel
- DM channel list in server column (separate section)
- Unread DM indicators
- DM history stored in DB

### Voice Preview (Experimental)

- Join / leave voice channels (UI only)
- Speaking indicators in members panel
- Status: "In voice: General" shown in member tooltip

---

## v0.6.0 — Bots & Automation ⬜ PLANNED

### Bot User Type

- `is_bot` flag on the `users` table
- Bot badge (`🤖`) shown in members panel
- Bots excluded from presence tracking
- Rate limits enforced separately from human users

### Bot Registration Protocol

- HTTP `POST /api/bots` — register a bot, receive a bot token
- Bot tokens are long-lived (no expiry by default)
- `/bot list` — show active bots in current server
- `/bot kick <name>` — disconnect and deregister a bot

### Bot Gateway

- Bots connect via the same WebSocket protocol
- `OpIdentify` with `is_bot: true` flag
- Event subscription: bots declare which events they want (allow-list)
- Bots can send messages (`OpSendMessage`), react, and read history

### Event Hooks (Server-Side)

- `on_message_create` — fired for every channel message
- `on_member_join` / `on_member_leave`
- `on_channel_create` / `on_channel_delete`
- `on_presence_update`

### Built-In Bot Scripts

- **Welcome bot**: greets new members in a configurable channel
- **Moderation bot**: auto-mute / auto-kick based on configurable rules
- **Logger bot**: archives messages to a local file
- Lua or Starlark scripting for custom logic (server-side embedded interpreter)

### Bot Commands

- Bot command prefix configurable per server (default `!`)
- Command routing: `!ping`, `!help`, `!uptime` built-in
- Custom command registration via script

---

## v0.7.0 — AI Integration ⬜ PLANNED

> Build AI as a first-class citizen — not a bolt-on. All AI runs through the existing bot framework, keeping the core protocol clean.

### AI Provider Configuration (Server-Side)

Server admins configure AI via `concord-server.toml`:

```toml
[ai]
enabled     = true
provider    = "openai"          # "openai" | "anthropic" | "ollama"
api_url     = "https://api.openai.com/v1"
api_key     = "sk-..."
model       = "gpt-4o-mini"
temperature = 0.7
max_tokens  = 512

[ai.rate_limits]
per_user_per_minute  = 5
per_channel_per_hour = 60
```

Compatible with any OpenAI-compatible API (Ollama, LM Studio, Together AI, etc.).

### AI Bot Instance

- Runs as a special bot user (`@ai`, `@concord-ai`, or admin-configured alias)
- Connects via the bot gateway — no special server-side privileges
- Subscribes to `on_message_create` events; responds only when addressed

### /ai Slash Command

- `/ai <prompt>` — send a one-off AI query in the current channel context
- Last N messages from the channel are automatically included as conversation context
- Response appears as a message from the AI bot user
- `/ai --private <prompt>` — response is a whisper (only visible to requesting user)

### Channel AI Personas

Per-channel AI configuration (admin-only):

```
/ai-config system-prompt "You are a helpful coding assistant. Focus on Go and terminal tools."
/ai-config context-window 20
/ai-config enabled true
```

Personas are stored in the DB per channel and injected into every AI request.

### AI Compose Helper (Client-Side)

- `Ctrl+Space` in the input box: send current draft to AI for completion / improvement
- Response inserted at cursor — user reviews before sending
- Purely client-side: calls the server's AI endpoint, not the chat protocol
- Configurable: can be disabled or pointed at a local Ollama instance

### AI Moderation Assistant

- Background mode: AI reviews messages for policy violations (configured threshold)
- Flagged messages shown in a `#ai-moderation` channel for human review
- Auto-actions (kick/warn) require human confirmation by default (can be enabled by admin)
- False-positive feedback loop: moderators can mark flags as incorrect

### AI Summarization

- `/summarize [N]` — summarize the last N messages in current channel (default: 50)
- `/summarize --since 2h` — summarize messages from the last 2 hours
- Summary posted as a bot message or whispered to the requesting user

### Privacy Controls

- Users can opt out of AI context inclusion: `/ai-optout`
- Messages from opted-out users are excluded from AI context windows
- Server-wide opt-out disclosure shown in server info (`/server-info`)
- All AI queries logged server-side with user ID for audit trail

### Architecture: AI as Bot

```
User ──/ai query──► Chat Protocol ──► Server ──► AI Bot (WebSocket)
                                                      │
                                          AI Provider API (HTTP)
                                                      │
                                      AI Response ──► OpSendMessage
                                                      │
                         All clients ◄──── EventMessageCreate (AI reply)
```

This keeps the AI cleanly separated from the core chat protocol. Disabling AI is as simple as not running the AI bot — no server code changes needed.

---

## v1.0.0 — Stable Text Client Release ⬜ PLANNED

### Stability Goals

- Protocol freeze (semver: no breaking changes without major bump)
- Backward compatibility for servers running v0.6.0+
- Comprehensive error handling and user-friendly error messages
- Performance tuning for large channels (1000+ messages, 500+ members)
- Graceful degradation on narrow terminals (< 160 columns)

### Documentation

- Full README with quick-start guide
- Man page (`concord-client(1)`, `concord-server(1)`)
- Command reference (all slash commands, all keybindings)
- Theming guide (TOML schema, color roles)
- Bot development guide
- Self-hosting guide (systemd unit, nginx reverse proxy, TLS)

### Security Review

- Rate limiting on all endpoints
- Input sanitization (XSS-equivalent in terminal output)
- Token rotation support
- Optional TLS for WebSocket connections
- Password hashing audit (bcrypt cost factor review)

---

## v2.0.0 — Voice Support ⬜ PLANNED

### Voice Architecture

```
Microphone ──► Capture ──► Opus Encoder ──► Voice Client ◄──► Voice Gateway
                                                                      │
                                              Opus Decoder ◄── Voice Mixer
                                                    │
                                               Audio Output ──► Speakers
```

- Completely separate from text pipeline (independent failure domain)
- UDP transport where possible; TCP fallback
- Opus codec (48 kHz, 20ms frames)
- Jitter buffer and packet loss concealment

### Voice UX

- Join/leave voice channels (shown in members panel)
- Push-to-talk (configurable key, default: Ctrl+Space when in voice)
- Mute / deafen (own audio only vs. incoming)
- Speaking indicators (pulsing avatar circle)
- Per-user volume control (client-side)

### Audio Backends

- **Linux**: PipeWire (primary), PulseAudio (fallback)
- **Windows**: WASAPI
- **macOS**: CoreAudio

### Voice-Aware AI (v2.1+)

- Real-time voice transcription (Whisper API or local model)
- Transcripts posted to a companion text channel
- Voice command detection: "Hey Concord, summarize this meeting"

---

## v3.0.0 — Ecosystem & Extensibility ⬜ PLANNED

### Plugin System

- Plugin lifecycle hooks (load, unload, reload)
- UI extension points: custom columns, custom message renderers
- Custom slash commands registered by plugins
- Plugin marketplace (community-hosted index)

### Federation (Optional)

- Cross-server message bridging (opt-in per channel)
- Shared identity via cryptographic key pairs (no central authority)
- Bridge bots for IRC, Matrix, Slack interoperability

### Power Features

- Saved TUI layouts per workspace
- Advanced scripting via embedded Lua or Starlark
- Workflow automations (IFTTT-style: "when X, do Y")
- Advanced moderation dashboard (separate TUI panel)

### AI at Scale (v3.x)

- Fine-tuned server-specific models (train on server message history)
- AI-powered search (`/search what did alice say about the deadline?`)
- Semantic channel recommendations
- Meeting summarization with action item extraction

---

## Long-Term Considerations

- End-to-end encryption for DMs (Signal protocol or Noise Protocol Framework)
- Screen reader compatibility (full keyboard navigation audit)
- Scalability: horizontal server scaling, message queue (NATS/Redis)
- Abuse prevention: spam detection, IP bans, CAPTCHA for registration
- Mobile companion app (future, separate project)

---

## Philosophy Reminder

> Build for clarity first, features second.
> A terminal app that feels _predictable_ will always outperform one that feels _clever_.
>
> AI should enhance the conversation, not dominate it.
> Every AI feature must have a clean off switch.
