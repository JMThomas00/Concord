# Concord

A terminal-based chat application inspired by Discord, built in Go with a beautiful TUI.

```
   ____                              _ 
  / ___|___  _ __   ___ ___  _ __ __| |
 | |   / _ \| '_ \ / __/ _ \| '__/ _' |
 | |__| (_) | | | | (_| (_) | | | (_| |
  \____\___/|_| |_|\___\___/|_|  \__,_|
```

## Features

### Version 1.0 (Current)
- 🖥️ **Self-hosted servers** - Run your own chat server
- 💬 **Real-time text chat** - Instant messaging with WebSocket
- 📁 **Channels** - Organize conversations by topic
- 👥 **User presence** - See who's online
- 🔒 **Roles & permissions** - Fine-grained access control
- 💌 **Direct messages** - Private conversations
- 🎨 **Theming** - Dracula and Alucard themes included
- ⌨️ **Keyboard-driven** - Full TUI with vim-like navigation

### Version 2.0 (Planned)
- 🎤 Voice chat with PortAudio and Opus codec
- 🎨 Additional themes
- 📱 Mobile-friendly web client

## Installation

### From Source

Requirements:
- Go 1.22 or later
- GCC (for SQLite)

```bash
# Clone the repository
git clone https://github.com/concord-chat/concord.git
cd concord

# Download dependencies
make deps

# Build
make build

# The binaries will be in ./build/
```

### Pre-built Binaries

Download from the [Releases](https://github.com/concord-chat/concord/releases) page.

## Quick Start

### Running the Server

```bash
# Start with default settings
./concord-server

# Or with a config file
./concord-server -config server.toml

# Or with command line options
./concord-server -host 0.0.0.0 -port 8080 -db /path/to/concord.db
```

The server will:
- Listen on `0.0.0.0:8080` by default
- Create a SQLite database at `concord.db`
- Accept WebSocket connections at `ws://localhost:8080/ws`
- Provide REST API at `http://localhost:8080/api`

### Running the Client

```bash
# Connect to a server
./concord -server ws://localhost:8080

# Or with a config file
./concord -config client.toml

# Or with a specific theme
./concord -theme alucard
```

## Configuration

### Server Configuration

Create `server.toml`:

```toml
[server]
host = "0.0.0.0"
port = 8080
database_path = "concord.db"
max_connections = 1000
debug = false

[security]
min_password_length = 8
session_expiry_hours = 720
rate_limiting = true
max_messages_per_minute = 30

[limits]
max_message_length = 2000
max_servers_per_user = 100
max_channels_per_server = 500
```

### Client Configuration

Create `client.toml` or `~/.config/concord/client.toml`:

```toml
[server]
address = "ws://localhost:8080"

[appearance]
theme = "dracula"
themes_dir = "./themes"
use_24h_time = true
compact_mode = false

[notifications]
desktop_notifications = true
mentions_only = false

[behavior]
send_typing = true
auto_reconnect = true
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Tab` | Cycle focus between panels |
| `Shift+Tab` | Cycle focus backwards |
| `Enter` | Send message / Select item |
| `Esc` | Return to sidebar |
| `↑/k` | Navigate up / Scroll up |
| `↓/j` | Navigate down / Scroll down |
| `PgUp` | Scroll chat up |
| `PgDn` | Scroll chat down |
| `Ctrl+S` | Switch server |
| `Ctrl+C` | Quit |

## Themes

Concord comes with two built-in themes:

### Dracula (Dark)
The classic Dracula color scheme with purple accents.

### Alucard (Light)
A light variant of Dracula for daytime use.

### Custom Themes

Create a `.toml` file in the themes directory:

```toml
[meta]
name = "My Theme"
author = "Your Name"
variant = "dark"

[colors]
background = "#282A36"
foreground = "#F8F8F2"
selection = "#44475A"
comment = "#6272A4"
red = "#FF5555"
orange = "#FFB86C"
yellow = "#F1FA8C"
green = "#50FA7B"
cyan = "#8BE9FD"
purple = "#BD93F9"
pink = "#FF79C6"

[semantic]
sidebar_bg = "#282A36"
chat_bg = "#282A36"
# ... see configs/themes/dracula.toml for full example
```

## API Reference

### REST Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/register` | Create new account |
| POST | `/api/login` | Authenticate and get token |
| GET | `/api/health` | Server health check |

### WebSocket Protocol

Connect to `/ws` with a WebSocket client. See `internal/protocol/messages.go` for the full protocol specification.

#### OpCodes (Client → Server)
- `0` IDENTIFY - Authenticate with token
- `1` HEARTBEAT - Keep connection alive
- `2` REQUEST_GUILD - Request server data
- `3` SEND_MESSAGE - Send a chat message
- `4` TYPING_START - Start typing indicator
- `5` PRESENCE_UPDATE - Update user status

#### OpCodes (Server → Client)
- `10` DISPATCH - Event dispatch
- `11` HEARTBEAT_ACK - Heartbeat acknowledgment
- `12` HELLO - Initial connection info
- `13` READY - Authentication success
- `14` INVALID_SESSION - Authentication failed
- `15` RECONNECT - Reconnection required

## Project Structure

```
concord/
├── cmd/
│   ├── server/         # Server entry point
│   └── client/         # Client entry point
├── internal/
│   ├── server/         # Server implementation
│   │   ├── server.go   # HTTP/WebSocket server
│   │   ├── hub.go      # Connection hub
│   │   ├── client.go   # Client handler
│   │   └── handlers.go # Message handlers
│   ├── client/         # Client implementation
│   │   ├── app.go      # TUI application
│   │   ├── views.go    # UI views
│   │   └── connection.go # WebSocket client
│   ├── models/         # Data models
│   ├── protocol/       # WebSocket protocol
│   ├── database/       # SQLite database layer
│   └── themes/         # Theme system
├── configs/
│   └── themes/         # Theme files
├── go.mod
├── Makefile
└── README.md
```

## Development

```bash
# Run tests
make test

# Format code
make fmt

# Lint code
make lint

# Build for all platforms
make dist
```

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

MIT License - see LICENSE file for details.

## Acknowledgments

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Style definitions
- [Dracula Theme](https://draculatheme.com) - Color scheme
- [Gorilla WebSocket](https://github.com/gorilla/websocket) - WebSocket implementation
