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
- **Multi-server** — connect to as many servers as you want simultaneously
- **Real-time messaging** — WebSocket-based chat with typing indicators
- **Hierarchical channels** — collapsible categories, folder-explorer style
- **Role-based permissions** — Admin, Moderator, and custom roles with fine-grained bit flags
- **Moderation tools** — `/kick`, `/ban`, `/mute`, `/role assign/remove`
- **Whispers** — ephemeral private messages via `/whisper @user`
- **Unread tracking** — per-channel unread dots and `@mention` counters
- **Theme browser** — 7 built-in themes, real-time preview, hot-swap via `Ctrl+T`
- **Auto-connect** — one-time identity setup, then the app just opens
- **Keyboard-driven** — full TUI, no mouse required

## Installation

### Prerequisites

Concord is built with **pure Go** (no CGO, no GCC required). All you need is:

- **Go 1.22+**
- **Git**
- **Make** (optional — you can run `go build` directly if preferred)

#### Windows

1. Install **Go 1.22+** from [go.dev/dl](https://go.dev/dl/) and run the installer.
2. Install **Make** via [MSYS2](https://www.msys2.org/) (optional):
   - Open **MSYS2 MinGW64** and run: `pacman -S make`
   - Add `C:\msys64\usr\bin` to your Windows PATH, or just use `go build` directly (see below).
3. Verify:

   ```powershell
   go version
   git --version
   ```

#### Linux

```bash
# Ubuntu/Debian
sudo apt update && sudo apt install golang-go git make

# Fedora/RHEL
sudo dnf install golang git make

# Arch
sudo pacman -S go git make
```

Verify: `go version`

#### macOS

```bash
brew install go git make
```

Or install Go from [go.dev/dl](https://go.dev/dl/) and Xcode Command Line Tools for `make` and `git`:

```bash
xcode-select --install
```

### Building from Source

```bash
git clone https://github.com/JMThomas00/Concord.git
cd Concord
make deps   # download Go modules
make build  # builds ./build/concord and ./build/concord-server
```

**No Make?** Build directly with Go:

```bash
go mod download
go build -o build/concord-server ./cmd/server
go build -o build/concord ./cmd/client
```

On Windows, output binaries get `.exe` automatically:

```powershell
go mod download
go build -o build\concord-server.exe .\cmd\server
go build -o build\concord.exe .\cmd\client
```

### Pre-built Binaries

Download from the [Releases](https://github.com/JMThomas00/Concord/releases) page (coming soon).

---

## Quick Start

### Step 1: Start the Server

#### First run (no config file yet)

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

#### Subsequent runs

```text
Windows:   build\concord-server.exe
Linux/mac: ./build/concord-server
```

The server reads `concord-server.toml` automatically. You can also pass flags to override:

```bash
./build/concord-server --host 0.0.0.0 --port 9000 --db /data/concord.db
```

The server is ready when you see: `Server started on :8080`

#### Admin setup

The **first user to register** on a fresh server is automatically granted the Admin role.

To grant admin to a specific user after the fact:

```bash
./build/concord-server --admin-email user@example.com
```

This can be run while the server is offline (it opens the DB directly and exits).

### Step 2: Start the Client

```text
Windows:   build\concord.exe
Linux/mac: ./build/concord
```

#### First run — identity setup

The first time you start the client, you'll see the **Identity Setup** screen. Enter:

- **Alias** — your display name across all servers
- **Email** — used for registration/login on each server
- **Password** — used for registration/login on each server

This is saved to `~/.concord/config.json` and reused automatically. You only set it once.

#### Adding your first server

From the **Login** screen:

1. Press `+` or `A` to open the **Add Server** dialog
2. Enter the server address and port (e.g. `localhost` / `8080`)
3. Press `Enter` — the client connects, registers your identity (or logs in if already registered), and opens the chat

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
database_path = "concord.db"
max_connections = 1000
debug = false
```

Or pass flags: `--host`, `--port`, `--db`, `--config <path>`, `--admin-email <email>`

### Client — `~/.concord/config.json`

```json
{
  "version": 1,
  "identity": {
    "alias": "gh0st",
    "email": "ghost@example.com",
    "password": "..."
  },
  "ui": {
    "theme": "dracula",
    "collapsed_categories": [],
    "muted_channels": []
  }
}
```

**Theme** can be any of: `dracula`, `alucard-dark`, `alucard-light`, `nord`, `gruvbox`, `monokai`, `catppuccin-mocha`

### Client — `~/.concord/servers.json`

Managed automatically. Stores the list of known servers and cached auth tokens:

```json
{
  "servers": [
    {
      "id": "uuid",
      "name": "My Server",
      "address": "localhost",
      "port": 8080,
      "last_connected": "2026-02-18T10:00:00Z",
      "saved_credentials": {
        "email": "ghost@example.com",
        "token": "..."
      }
    }
  ],
  "default_user_preferences": {
    "username": "gh0st",
    "email": "ghost@example.com"
  }
}
```

---

## Keyboard Shortcuts

### Global

| Key | Action |
| --- | --- |
| `Tab` | Cycle focus forward (servers → channels → chat) |
| `Shift+Tab` | Cycle focus backward |
| `Ctrl+C` / `Ctrl+Q` | Quit |
| `Ctrl+M` | Open **Manage Servers** (works before login too) |
| `Ctrl+T` | Open **Theme Browser** |
| `?` | Show help overlay |

### Channel Navigation (channel panel focused)

| Key | Action |
| --- | --- |
| `↑` / `↓` | Move selection |
| `←` / `→` | Collapse / expand category |
| `Enter` | Open selected channel |

### Chat (input focused)

| Key | Action |
| --- | --- |
| `Enter` | Send message |
| `Tab` | Complete `@mention` suggestion |
| `Esc` | Dismiss suggestion popup / return to sidebar |
| `PgUp` / `PgDn` | Scroll message history |
| `↑` / `↓` | Scroll message history (when input empty) |

### Manage Servers (`Ctrl+M`)

| Key | Action |
| --- | --- |
| `↑` / `↓` | Select server |
| `Shift+↑` / `Shift+↓` | Reorder server |
| `D` | Delete selected server |
| `P` | Ping selected server |
| `Esc` | Close |

---

## Slash Commands

Type `/` in the chat input to use commands. Tab-completion is available.

### Channel Commands

| Command | Description |
| --- | --- |
| `/create-channel <name> [category]` | Create a text channel |
| `/create-category <name>` | Create a channel category |
| `/delete-channel <name>` | Delete a channel |
| `/delete-category <name>` | Delete a category and its channels |
| `/rename-channel <old> <new>` | Rename a channel |
| `/move-channel <channel> <category>` | Move channel to a category |

### Theme Commands

| Command | Description |
| --- | --- |
| `/theme` | Open the interactive theme browser |
| `/theme <name>` | Directly apply a theme (e.g. `/theme nord`) |

### Notification Commands

| Command | Description |
| --- | --- |
| `/mute` | Mute the current channel (suppress unread badges) |
| `/unmute` | Unmute the current channel |

### Moderation Commands (requires appropriate role)

| Command | Description |
| --- | --- |
| `/role assign @user <rolename>` | Assign a role to a member |
| `/role remove @user <rolename>` | Remove a role from a member |
| `/kick @user` | Kick a member from the server |
| `/ban @user` | Ban a member (prevents re-registration) |
| `/mute @user` | Server-mute a member (they can't send messages) |
| `/unmute @user` | Remove server-mute from a member |

### Messaging Commands

| Command | Description |
| --- | --- |
| `/whisper @user <message>` | Send an ephemeral private message (also `/w`) |

### Other Commands

| Command | Description |
| --- | --- |
| `/help` | Show all commands and shortcuts |

---

## Themes

Concord ships 7 built-in themes, embedded directly in the binary:

| Theme | Style |
| --- | --- |
| `dracula` | Classic dark — purple accents (default) |
| `alucard-dark` | Dracula variant — dark |
| `alucard-light` | Dracula variant — light |
| `nord` | Cool blue-grey (Nord palette) |
| `gruvbox` | Warm retro (Gruvbox Dark) |
| `monokai` | Classic Sublime Text colours |
| `catppuccin-mocha` | Pastel dark (Catppuccin Mocha) |

### Switching Themes

**Interactive browser** — press `Ctrl+T` from anywhere, or run `/theme`:

- Arrow keys preview themes in real time across all 4 columns
- `Enter` saves the selection to `config.json`
- `Esc` reverts to the previous theme

**Direct apply** — `/theme nord`

### Custom Themes

Place a `.toml` file in `~/.concord/themes/`. It overrides any built-in theme with the same name.

```toml
[meta]
name = "My Theme"
author = "Your Name"
variant = "dark"

[colors]
background = "#282A36"
foreground = "#F8F8F2"
selection  = "#44475A"
comment    = "#6272A4"
red        = "#FF5555"
orange     = "#FFB86C"
yellow     = "#F1FA8C"
green      = "#50FA7B"
cyan       = "#8BE9FD"
purple     = "#BD93F9"
pink       = "#FF79C6"

[semantic]
sidebar_bg  = "#282A36"
chat_bg     = "#282A36"
# see internal/themes/themes/dracula.toml for all fields
```

---

## API Reference

### REST Endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/register` | Create new account |
| `POST` | `/api/login` | Authenticate and get token |
| `GET` | `/api/health` | Server health check |

### WebSocket Protocol

Connect to `/ws`. See [internal/protocol/messages.go](internal/protocol/messages.go) for the full spec.

#### OpCodes — Client to Server

| Code | Name | Description |
| --- | --- | --- |
| `0` | IDENTIFY | Authenticate with token |
| `1` | HEARTBEAT | Keep connection alive |
| `2` | REQUEST_GUILD | Request server data |
| `3` | SEND_MESSAGE | Send a chat message |
| `4` | TYPING_START | Start typing indicator |
| `5` | PRESENCE_UPDATE | Update user status |
| `6` | VOICE_STATE_UPDATE | Voice channel join/leave |
| `7` | CHANNEL_CREATE | Create a channel |
| `8` | CHANNEL_UPDATE | Update a channel |
| `9` | CHANNEL_DELETE | Delete a channel |
| `16` | REQUEST_MESSAGES | Request message history |
| `17` | ROLE_ASSIGN | Assign a role to a member |
| `18` | ROLE_REMOVE | Remove a role from a member |
| `19` | KICK_MEMBER | Kick a member |
| `20` | BAN_MEMBER | Ban a member |
| `21` | MUTE_MEMBER | Server-mute a member |
| `22` | WHISPER | Send an ephemeral private message |

#### OpCodes — Server to Client

| Code | Name | Description |
| --- | --- | --- |
| `10` | DISPATCH | Event dispatch |
| `11` | HEARTBEAT_ACK | Heartbeat acknowledgment |
| `12` | HELLO | Initial connection info + heartbeat interval |
| `13` | READY | Authentication success |
| `14` | INVALID_SESSION | Authentication failed |
| `15` | RECONNECT | Server requests reconnect |

---

## Project Structure

```text
concord/
├── cmd/
│   ├── server/
│   │   ├── main.go          # Server entry point, CLI flags, first-run detection
│   │   └── setup.go         # First-run interactive TUI wizard
│   └── client/
│       └── main.go          # Client entry point
├── internal/
│   ├── server/
│   │   ├── server.go        # HTTP + WebSocket server, registration
│   │   ├── hub.go           # Connection hub, broadcast, online check
│   │   ├── client.go        # Per-client WebSocket handler, opcode routing
│   │   └── handlers.go      # Message, channel, moderation, whisper handlers
│   ├── client/
│   │   ├── app.go           # TUI state machine, dispatch handlers
│   │   ├── views.go         # Four-column rendering, chat, members, themes
│   │   ├── commands.go      # Slash command parser and handlers
│   │   ├── connection.go    # WebSocket client
│   │   ├── connection_manager.go  # Multi-server state
│   │   ├── channel_tree.go  # Hierarchical channel data structure
│   │   ├── config.go        # ~/.concord/config.json + servers.json
│   │   ├── add_server_view.go     # Add server dialog
│   │   ├── manage_servers_view.go # Pre-auth server management (Ctrl+M)
│   │   ├── identity_setup_view.go # First-run identity setup
│   │   ├── reconnect_strategy.go  # Exponential backoff
│   │   ├── server_ping.go   # Health check
│   │   └── banners.go       # ASCII art
│   ├── models/
│   │   ├── user.go          # User, UserStatus
│   │   ├── message.go       # Message
│   │   ├── channel.go       # Channel, ChannelType, PermissionOverwrite
│   │   └── role.go          # Role, Permission flags, PermissionCalculator
│   ├── protocol/
│   │   └── messages.go      # OpCodes, EventTypes, all payload structs
│   ├── database/
│   │   └── sqlite.go        # SQLite (pure Go, no CGO) — all DB operations
│   └── themes/
│       ├── theme.go         # Theme struct, built-in themes, TOML loading
│       └── themes/          # Bundled theme TOML files (embedded in binary)
├── go.mod
├── Makefile
└── README.md
```

---

## Development

```bash
make deps          # Download and tidy Go modules
make build         # Build server + client → build/
make build-server  # Build server only
make build-client  # Build client only
make build-windows # Cross-compile Windows .exe from Linux/macOS
make test          # Run all tests
make fmt           # gofmt all packages
make lint          # golangci-lint (installs automatically)
make dist          # Cross-compile for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
make run-server    # Build + run server
make run-client    # Build + run client
make clean         # Remove build/ and dist/
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
- [modernc SQLite](https://gitlab.com/cznic/sqlite) — pure-Go SQLite (no CGO)
- [Gorilla WebSocket](https://github.com/gorilla/websocket) — WebSocket implementation
- [Dracula Theme](https://draculatheme.com) — colour inspiration
