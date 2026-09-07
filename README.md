# Concord

A terminal-based chat application inspired by Discord, built in Go with a beautiful TUI.

```text
   ____                              _
  / ___|___  _ __   ___ ___  _ __ __| |
 | |   / _ \| '_ \ / __/ _ \| '__/ _' |
 | |__| (_) | | | | (_| (_) | | | (_| |
  \____\___/|_| |_|\___\___/|_|  \__,_|

  Terminal Chat — IRC-like, self-hosted, keyboard-driven
```

## Features

- **Self-hosted servers** — run your own server, share a host:port, done
- **Server discovery** — optional Grapevine hub network for finding public servers
- **Multi-server** — connect to as many servers as you want simultaneously
- **Real-time messaging** — WebSocket-based chat with typing indicators, replies, edits, and soft-deletes
- **Voice channels** — WebRTC P2P audio with Opus encoding; VAD and PTT modes
- **Hierarchical channels** — collapsible categories, folder-explorer style
- **Role-based permissions** — Admin, Moderator, and custom roles with fine-grained bit flags
- **Moderation tools** — kick, ban, timeout, mute, title, force-move voice
- **Whispers** — ephemeral private messages via `/whisper @user`
- **Message pinning** — pin important messages per channel
- **Unread tracking** — per-channel unread dots and `@mention` counters
- **Theme browser** — 40+ built-in themes, real-time preview, hot-swap
- **OS notifications** — desktop alerts for mentions and DMs (cross-platform)
- **Auto-connect** — one-time identity setup, then the app just opens
- **Keyboard-driven** — full TUI, no mouse required

## Installation

### Prerequisites

The **server** is pure Go (no CGO). The **client** requires CGO for voice (GCC/MSYS2 on Windows):

- **Go 1.24.0+**
- **Git**
- **Make** (optional)
- **GCC** (client with voice only — on Windows, use [MSYS2](https://www.msys2.org/) MinGW64)

#### Windows

1. Install **Go 1.24.0+** from [go.dev/dl](https://go.dev/dl/) and run the installer.
2. For the **voice client**, install **MSYS2**:
   - Download and install from [msys2.org](https://www.msys2.org/)
   - Open **MSYS2 MinGW64** and run: `pacman -S mingw-w64-x86_64-gcc make`
   - Add `C:\msys64\mingw64\bin` to your Windows PATH
3. Verify:

   ```powershell
   go version
   gcc --version
   ```

#### Linux

```bash
# Ubuntu/Debian
sudo apt update && sudo apt install golang git make gcc

# Fedora/RHEL
sudo dnf install golang git make gcc

# Arch
sudo pacman -S go git make gcc
```

#### macOS

```bash
brew install go git make
xcode-select --install   # provides GCC/Clang
```

### Building from Source

```bash
git clone https://github.com/JMThomas00/Concord.git
cd Concord
```

**Server** (pure Go, no CGO):
```bash
go build -o build/concord-server.exe ./cmd/server   # Windows
go build -o build/concord-server ./cmd/server        # Linux/macOS
```

**Client with voice** (requires GCC):
```bash
# Windows (MSYS2 GCC must be in PATH)
CGO_ENABLED=1 go build -o build/concord.exe ./cmd/client

# Linux/macOS
CGO_ENABLED=1 go build -o build/concord ./cmd/client
```

**Client without voice** (pure Go, no CGO):
```bash
go build -tags novoice -o build/concord-novoice.exe ./cmd/client   # Windows
go build -tags novoice -o build/concord-novoice ./cmd/client        # Linux/macOS
```

**Grapevine hub** (pure Go, optional):
```bash
go build -o build/concord-hub.exe ./cmd/hub   # Windows
go build -o build/concord-hub ./cmd/hub        # Linux/macOS
```

**Using Make:**
```bash
make build                  # Server + client (voice)
make build-server           # Server only
make build-client           # Client with voice
make build-client-novoice   # Client without voice
make build-hub              # Grapevine hub
make build-windows-voice    # Windows client via MSYS2
```

### Pre-built Binaries

Download from the [Releases](https://github.com/JMThomas00/Concord/releases) page (coming soon).

---

## Quick Start

### Step 1: Start the Server

The first time you run the server, an interactive setup wizard launches automatically:

```text
Windows:   build\concord-server.exe
Linux/mac: ./build/concord-server
```

The wizard asks for:

- **Server name** (default: your hostname)
- **Bind host** (default: `0.0.0.0`)
- **Port** (default: `8080`)
- **Database path** (default: `concord.db`)

It writes `concord-server.toml` to the current directory and prints the address to share with users.

**Re-run the wizard at any time:**
```bash
./build/concord-server --setup
```

**Server flags:**
```bash
./build/concord-server               # Normal mode (logs to stdout)
./build/concord-server --hybrid      # Log + live dashboard side-by-side
./build/concord-server --dashboard   # Dashboard only
./build/concord-server --debug       # Verbose debug logging
```

The **first user to register** on a fresh server is automatically granted the Admin role.

### Step 2: Start the Client

```text
Windows:   build\concord.exe
Linux/mac: ./build/concord
```

**First run:** the **Identity Setup** screen asks for your alias, email, and password. This is saved to `~/.concord/config.json` and reused automatically — you only set it once.

**Adding your first server:**

1. From the **Login** screen, press `A` to open **Add Server**
2. Enter the server address and port (e.g. `localhost` / `8080`)
3. Press `Enter` — the client connects, registers your identity (or logs in if already registered), and opens chat

**Discovering servers (Grapevine hub browser):**

1. Open **Settings → Manage Servers** and press `B`
2. Browse public servers by category, search by name or tag
3. Press `Enter` on a listing then `A` to join

### Step 3: Start Chatting

- Type your message and press `Enter` to send
- Use `↑`/`↓` to navigate channels
- Use `/help` to see all slash commands
- Press `Ctrl+C` or `Ctrl+Q` to quit

---

## Configuration

### Server — `concord-server.toml`

Generated by the first-run wizard. Edit by hand if needed:

```toml
host = "0.0.0.0"
port = 8080
server_name = "Concord Server"
database_path = "concord.db"
max_connections = 1000
debug = false
admin_email = ""

[message_pruning]
enabled = true
interval_hours = 24

# Optional: register with a Grapevine hub for public discovery
[grapevine]
enabled = true
hub_url = "https://hub.concord.chat"
server_name = "My Server"
description = "A cool Concord server"
category = "general"
tags = ["friendly", "english"]
```

### Client — `~/.concord/config.json`

Managed automatically. Contains identity, UI preferences, audio settings, and hub URLs:

```json
{
  "version": 1,
  "identity": { "alias": "user", "email": "user@example.com", "password": "..." },
  "ui": {
    "theme": "dracula",
    "hub_urls": ["https://hub.concord.chat"]
  }
}
```

### Client — `~/.concord/servers.json`

Managed automatically. Stores the list of known servers and cached auth tokens.

---

## Keyboard Shortcuts

### Global

| Key | Action |
| --- | --- |
| `Tab` | Cycle focus forward (servers → channels → chat) |
| `Shift+Tab` | Cycle focus backward |
| `Ctrl+C` / `Ctrl+Q` | Quit |
| `Ctrl+B` | Open **Manage Servers** |
| `Ctrl+T` | Open **Theme Browser** |
| `Ctrl+G` | Open **Hub Browser** (from Login / Add Server only) |
| `?` | Show help overlay |

### Channel Navigation (channel panel focused)

| Key | Action |
| --- | --- |
| `↑` / `↓` | Move selection |
| `←` / `→` | Collapse / expand category |
| `Enter` | Open selected channel |
| `Shift+↑` / `Shift+↓` | Reorder channel |

### Chat (input focused)

| Key | Action |
| --- | --- |
| `Enter` | Send message |
| `Tab` | Complete `@mention` suggestion |
| `Esc` | Dismiss popup / return to sidebar |
| `PgUp` / `PgDn` | Scroll message history |
| `↑` / `↓` | Scroll message history (when input empty) |
| `Alt+M` | Enter message-highlight mode |

### Message Highlight Mode (`Alt+M`)

| Key | Action |
| --- | --- |
| `↑` / `↓` | Move highlight |
| `R` | Reply to highlighted message |
| `E` | Edit own highlighted message |
| `D` | Delete own highlighted message |
| `L` | Open links in highlighted message |
| `A` | Copy highlighted message's attachment ID (for `/download`) |
| `P` | Pin / unpin highlighted message (mod+) |
| `Esc` | Exit highlight mode |

### Link Browser (`/links`, or `L` in Message Highlight Mode)

| Key | Action |
| --- | --- |
| `↑` / `↓` | Move selection |
| `1`-`9` | Jump to and open the link at that row on screen |
| `Enter` | Open selected link |
| `C` | Copy selected link |
| `PgUp` / `PgDn` | Scroll a full page (list scrolls automatically past 10 links) |
| `/` | Search — filters the list live as you type; `Enter` applies, `Esc` clears |
| `A` / `D` | Sort ascending / descending |
| `Esc` | Close (or cancel search, if searching) |

### Manage Servers (`Ctrl+B`)

| Key | Action |
| --- | --- |
| `↑` / `↓` | Select server |
| `Shift+↑` / `Shift+↓` | Reorder server |
| `N` | Add new server |
| `E` | Edit selected server |
| `D` | Delete selected server |
| `P` | Ping selected server |
| `B` | Open **Hub Browser** |
| `Esc` | Close |

### Hub Browser (Settings → Manage Servers → `B`)

| Key | Action |
| --- | --- |
| `H` / `L` | Previous / next hub tab |
| `Tab` | Cycle category filter |
| `/` | Search |
| `↑` / `↓` | Navigate listings |
| `Enter` | View server details |
| `A` | Join selected server |
| `S` | Cycle sort (name / online / members) |
| `R` | Refresh listings |
| `+` / `↑↓` | Add hub (pick from discovered peers) |
| `X` | Remove hub tab |
| `Esc` | Return to previous screen |

---

## Slash Commands

Type `/` in the chat input to use commands. Tab-completion is available. Use `/help` for the full in-app reference.

### Channel & Category

| Command | Description |
| --- | --- |
| `/create-channel <name>` | Create a text channel |
| `/create-group <name>` | Create a channel category |
| `/delete-channel <name>` | Delete a channel |
| `/rename-channel <old> <new>` | Rename a channel |
| `/lock` / `/unlock` | Lock or unlock the current channel |

### Messaging

| Command | Description |
| --- | --- |
| `/whisper @user <message>` | Send an ephemeral private message |
| `/pin` / `/unpin` | Pin or unpin the highlighted message |
| `/links` | Show all links in the current channel |
| `/status <text>` | Set your status text |

### Voice

| Command | Description |
| --- | --- |
| `/join-voice [#channel]` | Join a voice channel |
| `/leave-voice` | Leave the current voice channel |

### Theme

| Command | Description |
| --- | --- |
| `/theme` | Open the interactive theme browser |
| `/theme <name>` | Directly apply a theme (e.g. `/theme nord`) |

### Notifications

| Command | Description |
| --- | --- |
| `/mute` | Mute the current channel (suppress unread badges) |
| `/unmute` | Unmute the current channel |

### Moderation (requires appropriate role)

| Command | Description |
| --- | --- |
| `/kick @user [reason]` | Kick a member from the server |
| `/ban @user [reason]` | Ban a member |
| `/unban <username>` | Unban a member |
| `/timeout @user <minutes>` | Temporarily ban for N minutes |
| `/mute @user [minutes]` | Server-mute a member |
| `/unmute @user` | Remove server-mute |
| `/role @user <role>` | Assign a role |
| `/create-role <name>` | Create a new role |
| `/title @user <title>` | Give a member a custom title |
| `/mute-voice @user` | Server-mute a user in voice |
| `/deafen-voice @user` | Server-deafen a user in voice |
| `/move-voice @user <channel>` | Force-move user to a voice channel |

---

## Themes

Concord ships **40+ built-in themes** embedded directly in the binary. Switch themes in real time — no restart needed.

### Selected Themes

| Theme | Style |
| --- | --- |
| `dracula` | Classic dark — purple accents (default) |
| `alucard-dark` | Dracula variant — dark |
| `alucard-light` | Dracula variant — light |
| `nord` | Cool blue-grey (Nord palette) |
| `gruvbox` | Warm retro (Gruvbox Dark) |
| `catppuccin-mocha` | Pastel dark (Catppuccin Mocha) |
| `tokyo-night` | Dark blue (Tokyo Night) |
| `kanagawa-wave` | Japanese ink aesthetic |
| `onedark` | One Dark Pro |
| `everforest-dark-hard` | Nature-inspired green |
| `solarized-dark` | Solarized Dark |
| `terminal-default` | System terminal colours |

Use `/theme` or `Ctrl+T` to open the interactive browser and preview all themes live.

### Custom Themes

Place a `.toml` file in `~/.concord/themes/`. It overrides any built-in theme with the same name. See `internal/themes/themes/dracula.toml` for the full field reference.

---

## Voice Channels

Voice uses **WebRTC DataChannels** (P2P, SCTP) with **Opus** codec — no relay server required for LAN use.

- **Sample rates:** 8 kHz / 16 kHz / 24 kHz / 48 kHz (configurable in Settings → Audio)
- **VAD:** voice activity detection with 300 ms hold; press-to-talk mode also supported
- **Codec:** Opus at 20 ms frames
- **Platform:** WASAPI on Windows (via miniaudio); CoreAudio on macOS; ALSA/PulseAudio on Linux

Join with `/join-voice <channel>` or navigate to a voice channel in the sidebar.

> **Note:** WAN voice requires a STUN/TURN server. LAN and same-machine use works out of the box.

---

## Grapevine — Server Discovery

Grapevine is Concord's opt-in, decentralized server directory. A **hub** maintains a public listing of registered Concord servers; hubs can federate with each other. There is no central authority — anyone can run a hub.

### For server owners

Enable Grapevine in `concord-server.toml` under `[grapevine]` (see Configuration above), or run the setup wizard with `--setup`. The server registers with the hub, heartbeats its online/member counts, and automatically re-registers if the hub resets.

### For users

Open **Settings → Manage Servers → `B`** to launch the hub browser. Browse, search, filter by category, and join in one step. The client handles the full join-token handshake — host/port is never exposed in listings.

### Running your own hub

```bash
build\concord-hub.exe              # First run: interactive setup wizard
build\concord-hub.exe --dashboard  # Run with live TUI dashboard
build\concord-hub.exe --setup      # Re-run setup wizard
build\concord-hub.exe --debug      # Verbose per-heartbeat logging
```

The hub stores its config in `grapevine-hub.toml` and data in `grapevine.db`.

---

## Project Structure

```text
concord/
├── cmd/
│   ├── client/main.go          # Client entry point
│   ├── hub/
│   │   ├── main.go             # Grapevine hub entry point
│   │   └── setup.go            # First-run setup wizard
│   └── server/
│       ├── main.go             # Server entry point
│       ├── setup.go            # First-run TUI setup wizard
│       └── tos_setup.go        # Terms of service acceptance
├── internal/
│   ├── client/                 # Full TUI application
│   │   ├── app.go              # Central state machine (Init/Update)
│   │   ├── views.go            # View() and all rendering functions
│   │   ├── commands.go         # Slash command parser and handlers
│   │   ├── connection.go       # WebSocket client, protocol handling
│   │   ├── connection_manager.go # Multi-server connection management
│   │   ├── config.go           # ~/.concord/ config R/W
│   │   ├── voice_engine.go     # WebRTC+Opus voice engine (!novoice)
│   │   ├── voice_engine_stub.go # No-op stub (novoice build)
│   │   ├── hub_browser_view.go # Grapevine hub browser overlay
│   │   ├── grapevine_http.go   # Hub REST API client
│   │   ├── reconnect_strategy.go # Exponential backoff reconnection
│   │   └── *_view.go           # Settings and UI panels
│   ├── server/
│   │   ├── server.go           # HTTP + WebSocket server
│   │   ├── handlers.go         # All message and moderation handlers
│   │   ├── grapevine.go        # Hub registration and heartbeats
│   │   ├── hub.go              # Connection hub, broadcast, typing
│   │   └── dashboard/          # --hybrid / --dashboard TUI
│   ├── hub/                    # Grapevine hub service
│   │   ├── server.go           # HTTP server and route setup
│   │   ├── handlers.go         # REST handlers, HMAC auth, rate limiting
│   │   ├── federation.go       # Peer hub sync
│   │   └── dashboard.go        # Live stats TUI
│   ├── database/sqlite.go      # All DB operations + migrations
│   ├── models/                 # Shared data models
│   ├── protocol/messages.go    # All OpCodes, EventTypes, payload structs
│   └── themes/                 # 40+ embedded TOML themes
├── go.mod
├── Makefile
└── README.md
```

---

## WebSocket Protocol

Connect to `/ws`. Full spec in [internal/protocol/messages.go](internal/protocol/messages.go).

### OpCodes — Client to Server

| Code | Name | Description |
| --- | --- | --- |
| `0` | IDENTIFY | Authenticate with token |
| `1` | HEARTBEAT | Keep connection alive |
| `2` | REQUEST_GUILD | Request server data |
| `3` | SEND_MESSAGE | Send a chat message (optional reply_to_id) |
| `4` | TYPING_START | Start typing indicator |
| `5` | PRESENCE_UPDATE | Update status/status_text |
| `6` | VOICE_STATE_UPDATE | Join / leave voice channel |
| `7` | CHANNEL_CREATE | Create a channel |
| `8` | CHANNEL_UPDATE | Rename, reorder, lock channel |
| `9` | CHANNEL_DELETE | Delete a channel |
| `16` | REQUEST_MESSAGES | Request message history (paginated) |
| `17` | ROLE_ASSIGN | Assign a role to a member |
| `18` | ROLE_REMOVE | Remove a role from a member |
| `19` | KICK_MEMBER | Kick a member |
| `20` | BAN_MEMBER | Ban a member |
| `21` | MUTE_MEMBER | Server-mute a member |
| `22` | WHISPER | Ephemeral private message |
| `23` | PIN_MESSAGE | Pin a message |
| `24` | UNPIN_MESSAGE | Unpin a message |
| `25` | TIMEOUT_MEMBER | Temporary ban for N minutes |
| `26` | UNBAN_MEMBER | Lift a ban |
| `27–29` | ROLE_CRUD | Create / update / delete roles |
| `30` | EDIT_MESSAGE | Edit own message |
| `31` | DELETE_MESSAGE | Soft-delete a message |
| `44` | ASSIGN_TITLE | Give member a custom title |
| `45` | VOICE_SIGNAL | WebRTC SDP/ICE relay |
| `46` | VOICE_SPEAKING | Report speaking state |
| `47` | VOICE_SERVER_MUTE | Server-mute in voice |
| `48` | MOVE_VOICE | Force-move user to voice channel |

### OpCodes — Server to Client

| Code | Name | Description |
| --- | --- | --- |
| `10` | DISPATCH | Event dispatch (carries event type) |
| `11` | HEARTBEAT_ACK | Heartbeat acknowledgment |
| `12` | HELLO | Initial handshake + heartbeat interval |
| `13` | READY | Auth success + user/server data |
| `14` | INVALID_SESSION | Auth failure |
| `15` | RECONNECT | Server requests reconnect |

---

## Development

```bash
make test          # Run all tests
make fmt           # gofmt all packages
make clean         # Remove build/
```

---

## Contributing

Contributions are welcome. Please open an issue before submitting large changes so we can discuss the approach.

## License

MIT License — see LICENSE file for details.

## Acknowledgments

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — terminal styling
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components
- [Glamour](https://github.com/charmbracelet/glamour) — markdown rendering
- [charmbracelet/log](https://github.com/charmbracelet/log) — structured logging
- [modernc SQLite](https://gitlab.com/cznic/sqlite) — pure-Go SQLite (server, no CGO)
- [Gorilla WebSocket](https://github.com/gorilla/websocket) — WebSocket transport
- [pion/webrtc](https://github.com/pion/webrtc) — pure-Go WebRTC
- [miniaudio / malgo](https://github.com/gen2brain/malgo) — cross-platform audio I/O
- [hraban/opus](https://github.com/hraban/opus) — Opus codec bindings
- [beeep](https://github.com/gen2brain/beeep) — cross-platform desktop notifications
- [Dracula Theme](https://draculatheme.com) — colour inspiration
