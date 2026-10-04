# Concord

A chat app for your terminal, inspired by Discord: servers, channels, voice, games and plugins, all keyboard-driven, all self-hostable. Built in Go on the [Charm](https://charm.sh) stack.

**Website:** [concordchat.cc](https://concordchat.cc) (by [Anthoneyq](https://github.com/Anthoneyq)) · **Official server:** `server.concordchat.cc` · **Install:** [one line](#install)

![Concord: signing in, a community's channels, a code snippet, switching themes live, and a first message](demo/hero.gif)

## Features

- **Your own servers.** Run one on a home PC, a Raspberry Pi or a VPS. Every server is independent, IRC-style, with no central account.
- **Many servers at once.** One client and one profile; switch servers without dropping out of a voice call.
- **Channels and groups** in collapsible categories, with roles, per-channel permission overwrites, nicknames and custom titles.
- **Chat that feels modern:** markdown, clickable links, replies, edits, pins, `@mentions` with autocomplete, typing indicators, whispers, and toasts for new messages.
- **Voice channels:** peer-to-peer WebRTC with Opus at 48 kHz, RNNoise noise suppression, echo cancellation, automatic levelling and per-person volume.
- **File sharing** peer-to-peer (`/attach`, `/download`): nothing is stored on the server.
- **Plugins:** chess, checkers, Tak and tic-tac-toe channels, a kanban board (Tukan), an AI persona (Mynah), with achievements and leaderboards. Install them from inside Concord, or build your own with the SDK.
- **The Grapevine:** an optional, federated directory of public servers, browsable and joinable from the client.
- **40+ themes**, switchable live, plus your own.
- **Accounts done properly:** several profiles per computer, optional email verification and password reset, sign-in on every server at once.
- **Keyboard-first, mouse-friendly.** Every mouse action has a key.
- **Personality:** shaded ASCII grapes, loading screens, moods, screensavers, easter eggs and a collection to complete. 🍇

## Install

Paste one line into a terminal. A short form asks what you'd like (the client, a server, a hub, or any mix) and where. It then installs everything and helps you take your first steps.

**macOS and Linux**

```sh
curl -fsSL https://github.com/JMThomas00/Concord/releases/latest/download/install.sh | sh
```

**Windows** (PowerShell)

```powershell
irm https://github.com/JMThomas00/Concord/releases/latest/download/install.ps1 | iex
```

The installer:

- downloads the latest release for your computer and checks it against its published checksum;
- installs what the client needs (on Linux, GTK if it's missing, asking for your password once);
- puts `concord` on your PATH (and on Windows adds Concord to Windows Terminal's new-tab menu);
- sets up a server or hub: its settings, starting it in the background (from boot or when you sign in), and checking it's up;
- can list your server on the Grapevine, connect your hub to the official one, and add the official Concord server to your client.

**Updating, reconfiguring, uninstalling:** on Concord's login screen, **Ctrl+U** checks for a newer release and offers **U**pdate, **C**onfigure (the installer's questions again, starting from your current settings) and uninstall (**X**). Running the one-liner again also updates, and keeps your settings and data.

**Just looking?** Add `--dry-run` to see every step without changing anything: `curl -fsSL …/install.sh | sh -s -- --dry-run`, or on Windows set `$env:CONCORD_INSTALL_ARGS = "--dry-run"` first.

**Prefer to download?** Every [release](https://github.com/JMThomas00/Concord/releases) has the client, server and hub for Windows, macOS and Linux, the server and hub for Linux on ARM and Intel Macs, and `SHA256SUMS`.

## Getting Started

1. **Open Concord:** type `concord` (in a new terminal, if the installer just put it on your PATH).
2. **Make your profile:** a name, your email and a password. One profile signs you in to every server you join.
3. **Join a server:**
   - **The official server**, `server.concordchat.cc`, is already in your list if you said yes in the installer. Say hi!
   - **Any other server:** press **+** in the server column or **Ctrl+B**, then enter its address and port (and turn on TLS for servers behind HTTPS, like the official one on port 443).
   - **Browse the Grapevine:** **Ctrl+G** on the login screen, or **Settings → Manage Servers → B**. Pick a server and press **A** to join.
4. **Chat:** type and press **Enter**. **Tab** moves between panels, `/help` finds every command, and **Settings → Help** has the full guide.

**Running your own server?** The first person to sign up on a new server becomes its owner. Give friends its address (on your network, the installer's summary shows it; across the internet, forward the port or put it behind a tunnel). Then manage channels, roles, members and plugins from **Ctrl+B → your server**.

## Plugins

Plugins are separate programs a server runs alongside itself. They add channels with games, boards or bots, and never need changes to Concord.

- **Install one** (server admins): **Settings → Plugins → I**, then type its GitHub repo, like `JMThomas00/concord-chess`. Concord downloads the right build, checks it, and starts it with no restart. **U** updates it later.
- **Plugins we've made:**
  - games: [tic-tac-toe](https://github.com/JMThomas00/concord-tictactoe), [checkers](https://github.com/JMThomas00/concord-checkers), [Tak](https://github.com/JMThomas00/concord-tak) and [chess](https://github.com/JMThomas00/concord-chess). Each has seats, challenges, spectators, a computer opponent, achievements and a leaderboard, and each also plays standalone in a terminal, even over the network.
  - **Tukan:** a kanban board.
  - **Mynah:** AI personas that chat.
- **Build your own:** the plugin SDK (`github.com/JMThomas00/Concord/sdk`) handles the connection, panes, game tables and testing. `concord-plugin new <name> --template game|pane|bot|pty` gives you a working repo with a release workflow. [`sdk/PROTOCOL.md`](sdk/PROTOCOL.md) covers other languages, and the `/concord` skill in `.claude/skills/concord/` teaches an AI agent to build one.

## Keyboard Shortcuts

`/help` and **Settings → Help** have the complete list. The essentials:

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Move between panels |
| `Esc` | Close, cancel, go back |
| `Ctrl+S` | Settings |
| `Ctrl+B` | Server management |
| `Ctrl+T` | Theme browser |
| `Ctrl+G` | Grapevine browser (login and Add Server screens) |
| `Ctrl+P` / `Ctrl+F` / `Ctrl+U` | Profiles / forgot password / updates (login screen) |
| `[` / `]` | Hide or show the server list / member list |
| `Ctrl+M` / `Ctrl+D` | Mute / deafen yourself in voice |
| `Alt+M` | Message navigation: reply, edit, delete, links, pin, copy |
| `Ctrl+J` | New line in a message |
| `Ctrl+Q` | Quit |

In a plugin channel the pane has your keys. Esc and Tab still move between panels unless the plugin needs them for something on screen, and **Ctrl+]** always hands focus back.

## Slash Commands

Type `/` in the chat box. `/help` opens a searchable finder.

**Everyone**

| Command | What it does |
| --- | --- |
| `/whisper @user <msg>` (`/w`) | A private, ephemeral message |
| `/links [N]` | Links from the last N messages |
| `/theme [name]` | The theme browser, or a theme by name |
| `/status <message>` | Set your status (`/status clear` removes it) |
| `/nick <nickname>` | Your nickname on this server (`clear` removes it) |
| `/attach <path> [caption]` | Share a file peer-to-peer (stay online so people can download it) |
| `/download <attachment-id>` | Download a shared file |
| `/mute` / `/unmute` | Mute or unmute the current channel |
| `/join-voice [#channel]` / `/leave-voice` | Join or leave voice |
| `/mood [#CODE]` | Share today's mood, or adopt someone's for your next launch |
| `/vintage` | Your time on Concord as a wine-tasting note |
| `/collection` | Share what you've collected |

**Moderators**

| Command | What it does |
| --- | --- |
| `/create-channel <name>`, `/create-group <name>` | Make a channel or group |
| `/rename-channel <name>`, `/move-channel <group>` | Rename or move the current channel |
| `/delete-channel`, `/delete-group <name>` | Delete the current channel, or an empty group |
| `/lock` / `/unlock` | Only mods can post / everyone can |
| `/pin [N]` / `/unpin [N]` | Pin or unpin a message |
| `/kick @user [reason]`, `/timeout @user <minutes>` | Remove someone, for now or for a while |
| `/mute @user [minutes]` / `/unmute @user` | Server-mute a member |
| `/mute-voice`, `/deafen-voice`, `/unmute-voice @user` | Voice moderation |

**Admins**

| Command | What it does |
| --- | --- |
| `/roles`, `/role assign\|remove @user <role>` | List roles, give or take one |
| `/create-role <name> [text\|moderator\|admin]` | A new role, optionally from a preset |
| `/title @user <title>` | A custom title (`clear` removes it) |
| `/ban @user [reason]` / `/unban @user` | Ban or unban |
| `/move-voice @user <channel>` | Move someone to another voice channel |

## Themes

More than 40 themes are built in: Dracula (the default), Alucard, Nord, Gruvbox, Catppuccin, Tokyo Night, Kanagawa, One Dark, Everforest, Solarized and many more. `terminal-default` follows your terminal's own colours, and changes with them. Browse and preview them live with **Ctrl+T** or `/theme`.

**Your own:** put a `.toml` file in `~/.concord/themes/`. A file with a built-in theme's name replaces it. See [`internal/themes/themes/dracula.toml`](internal/themes/themes/dracula.toml) for every field.

## Voice

Voice goes directly between the people in a call (WebRTC), so a server only introduces them and never carries the audio.

- **Opus** at 48 kHz with in-band error correction and loss concealment. Codec Quality in **Settings → Audio** sets the bitrate (24–96 kbps).
- **On the way out:** echo cancellation, an 80 Hz low-cut, **RNNoise** noise suppression (keyboards, fans and coughs fade while you talk), and automatic levelling.
- **Voice detection** opens your mic only when you speak.
- **Each person's volume** can be set from the member list (select them, then **←/→**).
- **Your call stays connected** while you look at other servers.

Everyone in a call needs a current client: older ones can't play the newer audio packets.

## The Grapevine

The Grapevine is Concord's opt-in server directory. A **hub** lists the servers that register with it, and hubs can connect to share their listings. Anyone can run one; the official hub is `https://grapevine.concordchat.cc`.

- **Listing a server:** the installer asks, or set `[grapevine]` in `concord-server.toml` (below). The server registers, reports how many people are online, and re-registers by itself if the hub restarts.
- **Joining from a listing:** listings never show a server's address. When you join, the hub asks the server to accept a single-use token, and only then gives you its address.
- **Running a hub:** the installer, or `concord-hub` (a setup wizard on first run; `--setup` runs it again, `--dashboard` shows live stats).

## Running a Server by Hand

The installer does all of this for you; here's what's underneath.

The first run of `concord-server` walks through a setup wizard (terms, name, port, email, Grapevine) and writes `concord-server.toml` in the folder it runs from. Flags:

```text
--reconfigure            run the setup wizard again
--hybrid / --dashboard   live dashboard beside the log / instead of it
--debug                  verbose logging
--test-mail <address>    send a test email with the [mail] settings
--reset-password <email> give that account a temporary password (servers without mail)
```

A typical `concord-server.toml`:

```toml
host = "0.0.0.0"
port = 8080
server_name = "My Concord"
database_path = "concord.db"
plugins_dir = "Plugins"
admin_email = "you@example.com"   # this account becomes an admin
# real_ip_header = "CF-Connecting-IP"   # only behind a proxy such as a Cloudflare Tunnel

[grapevine]                       # optional: list it on a hub
enabled = true
hub_url = "https://grapevine.concordchat.cc"
public_host = "chat.example.com"  # the address people use to reach it
public_port = 8080                # 443 means it's reached over https
description = "Board games and bad puns"
category = "Gaming"
tags = ["Gaming", "Tabletop"]

[mail]                            # optional: email verification and password reset
smtp_host = "smtp.example.com"
smtp_port = 587
from = "Concord <concord@example.com>"
```

**Docker:** the `Dockerfile` and `docker-compose.yml` here run the server and hub (see the comment at the top of `docker-compose.yml`). The official server runs this way, behind a Cloudflare Tunnel.

## Building from Source

You need **Go 1.24+**. The server, hub and installer are pure Go. The client's voice needs a C compiler (**clang**) and the **Opus** libraries:

| System | Install |
| --- | --- |
| Debian / Ubuntu | `sudo apt install golang clang make pkg-config libopus-dev libopusfile-dev libgtk-3-dev` |
| Fedora | `sudo dnf install golang clang make pkgconf opus-devel opusfile-devel gtk3-devel` |
| Arch | `sudo pacman -S go clang make pkgconf opus opusfile gtk3` |
| macOS | `brew install go opus opusfile pkg-config` (and `xcode-select --install`) |
| Windows | Go from [go.dev/dl](https://go.dev/dl/), then [MSYS2](https://www.msys2.org/) with `pacman -S mingw-w64-x86_64-clang mingw-w64-x86_64-opus mingw-w64-x86_64-opusfile mingw-w64-x86_64-pkg-config make`, and `C:\msys64\mingw64\bin` on your PATH. Use clang: MinGW's GCC 16.2 crashes building the audio library. |

```sh
git clone https://github.com/JMThomas00/Concord.git && cd Concord
make build            # server, client and hub into build/
make build-windows    # the same on Windows (the client with Opus built in)
make build-installer  # concord-install; try it with: build/concord-install --from build --dry-run
make test             # every test, the SDK's too
```

`go build -tags novoice ./cmd/client` builds a client without voice and needs no C compiler.

## Project Layout

```text
cmd/            client, server, hub, install (concord-install), testplugin
internal/
  client/       the terminal app (Bubble Tea): views, settings, voice, plugins' panes
  server/       HTTP + WebSocket server, handlers, accounts, Grapevine, plugin host
  hub/          the Grapevine hub
  installer/    what concord-install does: releases, settings, auto-start, updates
  plugins/      plugin manifests, install/update, process supervision
  database/     SQLite (pure Go) and migrations
  protocol/     every opcode, event and payload
  themes/       the built-in themes
sdk/            the plugin SDK (its own Go module): wire, plugin, pane, table, plugintest
scripts/        install.sh and install.ps1, the one-line installers
```

The wire protocol lives in [`internal/protocol/messages.go`](internal/protocol/messages.go). Plugins use [`sdk/PROTOCOL.md`](sdk/PROTOCOL.md).

## Contributing

Contributions are welcome. Please open an issue before a large change, so we can talk about the approach.

## License

MIT. See LICENSE for details.

## A Huge Thank-You to Anthoneyq 🍇

Concord's website, [concordchat.cc](https://concordchat.cc), is the work of **[Anthoneyq](https://github.com/Anthoneyq)**. He designed and built all of it: the **Human** and **Terminal** layouts with their live theme switching, and a few secrets worth hunting for. He also wrote the original **live-lit ASCII grape renderer**. Concord's shaded grape logo on the login screen, the About page and the installer's welcome is a direct port of his code (see `internal/client/grape_logo.go`), shading matched character for character. The grapes have him to thank for their glow.

## Acknowledgments

- [Anthoneyq](https://github.com/Anthoneyq): the website and the shaded grape logo (see above)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [Bubbles](https://github.com/charmbracelet/bubbles), [Huh](https://github.com/charmbracelet/huh) and [Glamour](https://github.com/charmbracelet/glamour) (Charm): the TUI, its styling, components, the installer's forms and markdown
- [goldmark](https://github.com/yuin/goldmark): the markdown parser behind chat messages
- [bubblezone](https://github.com/lrstanley/bubblezone): mouse support
- [charmbracelet/log](https://github.com/charmbracelet/log) and [termenv](https://github.com/muesli/termenv): logging and terminal colour detection
- [modernc SQLite](https://gitlab.com/cznic/sqlite): pure-Go SQLite
- [go-toml](https://github.com/pelletier/go-toml): settings and themes
- [Gorilla WebSocket](https://github.com/gorilla/websocket): the connection
- [pion/webrtc](https://github.com/pion/webrtc): pure-Go WebRTC for voice and file sharing
- [miniaudio / malgo](https://github.com/gen2brain/malgo): audio input and output
- [hraban/opus](https://github.com/hraban/opus): the Opus codec
- [RNNoise](https://github.com/xiph/rnnoise) (Xiph.Org, BSD-3-Clause): noise suppression, vendored in `internal/rnnoise`
- [wazero](https://github.com/tetratelabs/wazero): the sandbox for plugins' client code
- [beeep](https://github.com/gen2brain/beeep): desktop notifications
- [sqweek/dialog](https://github.com/sqweek/dialog): the Save As dialog for `/download`
- [atotto/clipboard](https://github.com/atotto/clipboard): copying
- [Dracula Theme](https://draculatheme.com): the default colours
