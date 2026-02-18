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

### Prerequisites

Before installing Concord, you need to install the following:

#### Windows

1. **Install Go 1.22+**
   - Download from [go.dev/dl](https://go.dev/dl/)
   - Run the installer (it will automatically set up your PATH)
   - Verify installation:
     ```powershell
     go version
     ```

2. **Install GCC and Make** (required for SQLite)
   - Install [MSYS2](https://www.msys2.org/) (installs to `C:\msys64` by default)
   - Open **MSYS2 MinGW64** terminal and run:
     ```bash
     pacman -S mingw-w64-x86_64-gcc make
     ```
   - **Add to Windows PATH:**
     1. Press `Win + R`, type `sysdm.cpl`, press Enter
     2. Go to **Advanced** tab → **Environment Variables**
     3. Under "System variables", select **Path** → **Edit**
     4. Click **New** and add: `C:\msys64\mingw64\bin`
     5. Click **OK** on all dialogs
     6. **Restart your terminal** (PowerShell/CMD) for changes to take effect
   - Verify installation from PowerShell or CMD:
     ```powershell
     gcc --version
     make --version
     ```

#### Linux

1. **Install Go 1.22+**

   Ubuntu/Debian:
   ```bash
   sudo apt update
   sudo apt install golang-go
   go version
   ```

   Fedora/RHEL:
   ```bash
   sudo dnf install golang
   go version
   ```

   Arch:
   ```bash
   sudo pacman -S go
   go version
   ```

2. **Install GCC and Make** (usually pre-installed)

   Ubuntu/Debian:
   ```bash
   sudo apt install build-essential
   ```

   Fedora/RHEL:
   ```bash
   sudo dnf groupinstall "Development Tools"
   ```

   Arch:
   ```bash
   sudo pacman -S base-devel
   ```

#### macOS

1. **Install Go 1.22+**

   Using Homebrew:
   ```bash
   brew install go
   go version
   ```

   Or download from [go.dev/dl](https://go.dev/dl/)

2. **Install GCC and Make**

   Install Xcode Command Line Tools:
   ```bash
   xcode-select --install
   ```

### Building from Source

Once prerequisites are installed, build Concord:

#### Windows (PowerShell or CMD)

```powershell
# Clone the repository
git clone https://github.com/JMThomas00/Concord.git
cd Concord

# Download dependencies
make deps

# Build
make build

# Binaries will be in .\build\
```

**Note:** If `make` doesn't work in PowerShell, run the build commands from the MSYS2 MinGW64 shell instead.

#### Linux

```bash
# Clone the repository
git clone https://github.com/JMThomas00/Concord.git
cd Concord

# Download dependencies
make deps

# Build
make build

# Binaries will be in ./build/
```

#### macOS

```bash
# Clone the repository
git clone https://github.com/JMThomas00/Concord.git
cd Concord

# Download dependencies
make deps

# Build
make build

# Binaries will be in ./build/
```

### Pre-built Binaries

Download from the [Releases](https://github.com/JMThomas00/Concord/releases) page (coming soon).

## Quick Start

### Step 1: Start the Server

The server hosts chat rooms and manages all client connections. You need to run the server before clients can connect.

#### Windows

```powershell
# Navigate to the build directory
cd build

# Start server with default settings (localhost:8080)
.\concord-server.exe

# Or with custom settings
.\concord-server.exe -host 0.0.0.0 -port 8080 -db concord.db
```

#### Linux/macOS

```bash
# Navigate to the build directory
cd build

# Start server with default settings (localhost:8080)
./concord-server

# Or with custom settings
./concord-server -host 0.0.0.0 -port 8080 -db concord.db
```

**What the server does:**
- Listens on `0.0.0.0:8080` by default
- Creates a SQLite database at `concord.db`
- Accepts WebSocket connections at `ws://localhost:8080/ws`
- Provides REST API at `http://localhost:8080/api`

**Server is ready when you see:** `Server started on :8080`

### Step 2: Connect with the Client

Open a **new terminal** (keep the server running) and start the client.

#### Windows

```powershell
# Navigate to the build directory
cd build

# Connect to local server
.\concord.exe -server ws://localhost:8080

# Or connect to a remote server
.\concord.exe -server ws://example.com:8080

# Or use a specific theme
.\concord.exe -server ws://localhost:8080 -theme alucard
```

#### Linux/macOS

```bash
# Navigate to the build directory
cd build

# Connect to local server
./concord -server ws://localhost:8080

# Or connect to a remote server
./concord -server ws://example.com:8080

# Or use a specific theme
./concord -server ws://localhost:8080 -theme alucard
```

### Step 3: Create an Account

1. When the client starts, you'll see the login screen
2. Press `Tab` to switch to "Register" mode
3. Enter a username and password
4. Press `Enter` to create your account
5. You'll be automatically logged in

### Step 4: Start Chatting!

- Use `Tab` to navigate between sidebar and chat
- Use `↑`/`↓` or `j`/`k` to navigate channels
- Type your message and press `Enter` to send
- Press `Ctrl+C` to quit

### Using Configuration Files (Optional)

Instead of command-line flags, you can create config files:

#### Server Config

Create `server.toml`:
```toml
[server]
host = "0.0.0.0"
port = 8080
database_path = "concord.db"
```

Run with:
```bash
./concord-server -config server.toml
```

#### Client Config

Create `client.toml`:
```toml
[server]
address = "ws://localhost:8080"

[appearance]
theme = "dracula"
```

Run with:
```bash
./concord -config client.toml
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
