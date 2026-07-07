package client

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// helpMarkdown is the full Concord user guide rendered via glamour.
// Sections marked "Coming Soon" describe planned features not yet shipped.
const helpMarkdown = `# Concord — User Guide

Concord is a terminal-first, self-hosted chat application. Every server is independently operated — there is no central service or account system. Think IRC with Discord-style channels, roles, and voice.

---

## Getting Started

### Setting Up Your Identity

The first time you launch Concord, an identity setup screen appears. Fill in:

- **Alias** — your display name across all servers
- **Email** — used for server registration (not shared publicly)
- **Password** — stored locally in ` + "`~/.concord/config.json`" + `; used when auto-registering on new servers

Your identity is global to your machine. Concord automatically registers you on each server you join using these credentials.

### Adding Your First Server

Press **+** in the server icon column (far left), or open **Server Management** with **Ctrl+B**. Enter:

- **Address** — hostname or IP of the server (e.g. ` + "`192.168.1.100`" + `)
- **Port** — default is ` + "`8080`" + `
- **Name** — a friendly label shown in the server column

Concord will connect immediately and auto-register if no account exists.

### Auto-Registration & Auto-Admin

The **first user** to register on a new server is automatically granted the **Admin** role — no configuration needed. If you are hosting your own server, just connect and you will be Admin.

### Connecting to Multiple Servers

Concord supports any number of servers simultaneously. Each server icon in the left column is a live connection. Click between them to switch context — channels, members, and voice state are all per-server.

---

## Navigation

### Focus System

Concord uses a four-panel layout. **Tab** cycles focus between panels:

| Panel | Contents |
|-------|----------|
| Server List | Server icons column (left) |
| Channel List | Channels and categories |
| Chat | Message viewport and input |
| Members | Member list (right) |

The focused panel is highlighted with a purple border. Start typing in the **Chat** panel — focus moves to the input box automatically when you press a printable key.

### Keyboard Shortcuts — Global

| Key | Action |
|-----|--------|
| ` + "`Ctrl+Q`" + ` | Quit |
| ` + "`Ctrl+S`" + ` | Open Settings |
| ` + "`Ctrl+B`" + ` | Open Server Management (admin) |
| ` + "`Ctrl+G`" + ` | Open Grapevine Hub Browser (login / add-server screens) |
| ` + "`Ctrl+T`" + ` | Open Theme Browser |
| ` + "`[`" + ` | Toggle server list panel (collapse / expand) |
| ` + "`]`" + ` | Toggle members list panel (collapse / expand) |
| ` + "`Tab`" + ` | Switch focus between panels |
| ` + "`Esc`" + ` | Close overlay / cancel / go back |

### Keyboard Shortcuts — Channel List

| Key | Action |
|-----|--------|
| ` + "`↑ / ↓`" + ` | Navigate channels |
| ` + "`Enter`" + ` | Join selected channel (or join / leave voice) |
| ` + "`← / → or H / L`" + ` | Collapse / expand a category |
| ` + "`Shift+↑ / Shift+↓`" + ` | Reorder channel within its category |

### Keyboard Shortcuts — Chat

| Key | Action |
|-----|--------|
| ` + "`PgUp / PgDn`" + ` | Scroll message history |
| ` + "`Alt+M`" + ` | Enter message navigation mode |
| ` + "`Ctrl+J or Ctrl+Enter`" + ` | Insert a newline in the input box |
| ` + "`@`" + ` | Open @mention autocomplete popup |
| ` + "`↑ / ↓`" + ` in popup | Navigate mention suggestions |
| ` + "`Enter or Tab`" + ` in popup | Accept selected mention |
| ` + "`Esc`" + ` in popup | Dismiss autocomplete |

### Message Navigation Mode (Alt+M)

Press **Alt+M** from the chat panel to enter message navigation:

- **Level 1** — ↑/↓ move between messages; **Ctrl+C** copies the full message; **L** opens link browser for URLs in the selected message
- **Level 2** — press **Enter** on a message to enter edit mode; ←/→ move within the message; **Shift+←/→** select text; **Ctrl+C** copies selection
- Press **Esc** to exit either level

### Keyboard Shortcuts — Members Panel

| Key | Action |
|-----|--------|
| ` + "`↑ / ↓`" + ` | Navigate member list |
| ` + "`Enter`" + ` | Open context menu for selected member |
| ` + "`W`" + ` | Whisper (ephemeral DM) |
| ` + "`M`" + ` | Mute / unmute (requires permission) |
| ` + "`K`" + ` | Kick (requires permission) |
| ` + "`B`" + ` | Ban (requires permission) |
| ` + "`R / E`" + ` | Assign / remove role (requires permission) |
| ` + "`V`" + ` | Adjust per-user volume (voice only) |
| ` + "`X / D`" + ` | Voice mute / deafen (requires permission) |

---

## Messaging

### Sending Messages

Type in the input box at the bottom of the chat panel and press **Enter** to send. Use **Ctrl+J** or **Ctrl+Enter** for a newline inside the message.

### Slash Commands

Type ` + "`/`" + ` followed by a command name. Available to all users:

| Command | Description |
|---------|-------------|
| ` + "`/help`" + ` | Show available commands |
| ` + "`/whisper @user <msg>`" + ` | Send an ephemeral DM (alias: /w) |
| ` + "`/links [N]`" + ` | List URLs from the last N messages (default: 20) |
| ` + "`/theme [name]`" + ` | Open theme browser or apply a theme directly |
| ` + "`/status <message>`" + ` | Set your status (` + "`/status clear`" + ` to remove) |
| ` + "`/mute`" + ` | Mute the current channel (hide unread badges) |
| ` + "`/unmute`" + ` | Unmute the current channel |
| ` + "`/join-voice [#channel]`" + ` | Join a voice channel (or the currently selected one) |
| ` + "`/leave-voice`" + ` | Leave the current voice channel |

Moderator commands:

| Command | Description |
|---------|-------------|
| ` + "`/create-channel <name>`" + ` | Create a text channel |
| ` + "`/create-group <name>`" + ` | Create a channel category |
| ` + "`/delete-channel`" + ` | Delete the current channel |
| ` + "`/rename-channel <name>`" + ` | Rename the current channel |
| ` + "`/move-channel <group>`" + ` | Move current channel to a category |
| ` + "`/lock / /unlock`" + ` | Restrict posting to moderators only |
| ` + "`/mute @user [minutes]`" + ` | Server-mute a member |
| ` + "`/kick @user [reason]`" + ` | Kick a member |
| ` + "`/timeout @user <minutes>`" + ` | Temporarily ban a member |
| ` + "`/pin [N]`" + ` | Pin the Nth most recent message |
| ` + "`/mute-voice @user`" + ` | Server-mute a user in voice |
| ` + "`/deafen-voice @user`" + ` | Server-deafen a user in voice |
| ` + "`/unmute-voice @user`" + ` | Lift voice mute/deafen from a user |

Admin commands:

| Command | Description |
|---------|-------------|
| ` + "`/role assign|remove @user <role>`" + ` | Manage member roles |
| ` + "`/create-role <name> [preset]`" + ` | Create a role (presets: text, moderator, admin) |
| ` + "`/roles`" + ` | List all roles on this server |
| ` + "`/title @user <title>`" + ` | Set a display title (` + "`/title @user clear`" + ` to remove) |
| ` + "`/ban @user [reason]`" + ` | Permanently ban a member |
| ` + "`/unban @user`" + ` | Lift a ban |
| ` + "`/move-voice @user <channel>`" + ` | Force-move a user to a voice channel |

### @Mentions

Type ` + "`@`" + ` to open the autocomplete popup. Select a name with ↑/↓ and confirm with **Enter** or **Tab**. Mentioned messages are highlighted in your name colour. If **Bell on Mention** is enabled in Notification settings, a terminal bell fires on every @mention directed at you.

### Whispers (Ephemeral DMs)

` + "`/whisper @user <message>`" + ` or ` + "`/w @user <message>`" + ` sends a message only visible to you and the recipient, prefixed with ` + "`[DM]`" + `. Whispers are **not stored** — if the recipient is offline the message is lost.

### URL Hyperlinks

URLs in messages are rendered as OSC 8 terminal hyperlinks. In a supported terminal (Windows Terminal, iTerm2, Kitty) you can Ctrl+Click to open them. Use **Alt+M → L** to list links from any message without leaving Concord.

---

## Voice Channels

### Joining a Voice Channel

In the channel list, select a voice channel (prefixed with ` + "`♪`" + `) and press **Enter**. The channel badge shows the live member count ` + "`[N]`" + `. Press **Enter** again to leave.

Voice members appear at the top of the Members panel grouped by channel, separated from role sections below.

### Voice Controls

| Key | Action |
|-----|--------|
| ` + "`Ctrl+M`" + ` | Toggle self-mute |
| ` + "`Ctrl+D`" + ` | Toggle self-deafen |
| ` + "`Ctrl+Space`" + ` (default) | Push-to-Talk (hold to transmit in PTT mode) |

### Voice Activity Detection vs Push-to-Talk

**VAD** (Voice Activity Detection) transmits automatically when your microphone level exceeds the configured threshold. Because keyboard typing can trigger VAD false positives in a terminal, **Push-to-Talk is recommended** for most users.

Switch between modes in **Settings → Audio → PTT Mode**. The PTT key is configurable (default: ` + "`Ctrl+Space`" + `).

### Audio Quality (Codec Presets)

Concord encodes voice using **Opus** (libopus). Four presets are available in **Settings → Audio → Codec Preset**:

| Preset | Sample Rate | Bitrate | Best For |
|--------|------------|---------|----------|
| Low | 8 kHz | 8 kbps | Very low bandwidth |
| Medium | 16 kHz | 32 kbps | Standard voice (default) |
| High | 24 kHz | 64 kbps | High clarity |
| Ultra | 48 kHz | 128 kbps | Near-transparent quality |

Changing the preset hot-reloads the bitrate without reconnecting. A sample rate change takes effect on the next voice session.

### VU Meters & Quality Indicators

Connected voice members show:

- **` + "`↑[████░░]`" + `** — your microphone input level
- **` + "`↓[████░░]`" + `** — received audio level from a remote peer
- **` + "`◆◆◆◇ 42ms`" + `** — connection quality bar updated every 5 s (ICE round-trip time)

These can be hidden individually in **Settings → Display → Members Panel**.

---

## Channel Management

### Channel Types

| Type | Prefix | Description |
|------|--------|-------------|
| Text | ` + "`#`" + ` | Standard chat channel |
| Voice | ` + "`♪`" + ` | Real-time audio channel |
| Category | ` + "`▼`" + ` | Folder grouping channels |

### Creating Channels & Categories

Use ` + "`/create-channel <name>`" + ` or ` + "`/create-group <name>`" + ` from any channel. Alternatively, open the **Server Admin Panel** (` + "`Ctrl+B`" + ` → select server → admin key) for a full channel management UI.

### Reordering Channels

With focus on the channel list, use **Shift+↑ / Shift+↓** to reorder within the same category. Use ` + "`/move-channel <group>`" + ` to move a channel to a different category.

### Locking Channels

` + "`/lock`" + ` restricts posting to moderators and admins. ` + "`/unlock`" + ` restores normal access. A locked channel shows a ` + "`🔒`" + ` indicator.

---

## Roles & Permissions

Roles define what members can do. Each role has a **colour**, **display order**, and a **permission bitfield**. Roles are assigned with ` + "`/role assign @user <role>`" + ` or from the member context menu.

### Built-in Permission Levels

| Level | Can Do |
|-------|--------|
| Member | Read and send messages, use voice |
| Moderator | + mute, kick, timeout, pin, manage channels |
| Admin | + ban, manage roles, server configuration |
| Owner | Full control, cannot be moderated |

### The Admin Bypass Rule

A member with the **Administrator** permission bypasses all role-position hierarchy checks. This means an admin can always moderate any other non-admin member regardless of role display order.

---

## Server Administration

### Server Admin Panel

Open with **Ctrl+B**, select a server, then press the admin key. The panel has four tabs:

- **Channels** — manage channels and categories, set MaxUsers on voice channels
- **Roles** — create, edit, reorder, delete roles with a full permissions editor
- **Members** — view all members, assign roles, manage bans
- **Messages** — retention policies (age- and count-based), manual prune, prune history

### Hosting Your Own Server

Build the server binary (pure Go — no C compiler needed):

` + "```" + `bash
make build-server
# or directly:
go build -o build/concord-server ./cmd/server
` + "```" + `

Run it:

` + "```" + `bash
./concord-server
` + "```" + `

The first run walks you through a **setup wizard**: Terms of Service, server name, bind host, port (default ` + "`8080`" + `), database path, optional admin email — and an optional final step to **list your server on Grapevine** (see the Grapevine section below). A SQLite database is created automatically; no separate database setup is needed.

Useful flags: ` + "`--reconfigure`" + ` re-runs the wizard, ` + "`--hybrid`" + ` shows a live dashboard beside the logs, ` + "`--debug`" + ` enables verbose logging.

The first client to connect and register on a fresh server is automatically granted **Admin**. Share your address and port with others and they can connect immediately.

### Server Configuration

Settings are stored in ` + "`concord-server.toml`" + ` in the working directory (written by the wizard, editable by hand):

` + "```" + `toml
host = "0.0.0.0"
port = 8080
server_name = "My Concord Server"
database_path = "concord.db"

[message_pruning]
enabled = true
interval_hours = 24

[grapevine]
enabled = false   # opt-in public listing — see the Grapevine section
` + "```" + `

### Voice Across the Internet (NAT)

Voice is peer-to-peer WebRTC. Concord uses Google's public STUN server for NAT traversal, which works across most home networks with no configuration. A TURN relay fallback is not yet supported, so a small share of connections (symmetric NAT, some mobile/CGNAT networks) may fail to establish audio — configurable STUN/TURN is planned.

### Building the Client (Voice-Enabled)

Voice is part of the standard client build and requires a C compiler (GCC) and libopus:

` + "```" + `bash
# Windows (MSYS2 MinGW)
pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-opus mingw-w64-x86_64-opusfile

export PATH="/c/msys64/mingw64/bin:$PATH"
CGO_ENABLED=1 go build -o build/concord-client.exe ./cmd/client

# Headless/CI build without voice (pure Go, no CGO)
CGO_ENABLED=0 go build -tags novoice -o build/concord-client-novoice.exe ./cmd/client
` + "```" + `

---

## Grapevine — Server Discovery

**Grapevine** is Concord's decentralized server directory. Server owners opt in to list their server on a **hub**; anyone can browse a hub from inside the client and join a listed server in a couple of keystrokes. There is no central authority — anyone can host a hub, and hubs can federate to share listings.

### Browsing Servers (Hub Browser)

Open **Settings → Manage Servers** and press **B** to browse servers via a hub. Before you have any servers (login screen or the Add Server dialog), **Ctrl+G** opens it directly.

| Key | Action |
|-----|--------|
| ` + "`↑/↓ or j/k`" + ` | Navigate the server list |
| ` + "`Enter`" + ` | Open server details |
| ` + "`A`" + ` (in details) | Join — adds the server and opens login |
| ` + "`/`" + ` | Search by name, description, or tags |
| ` + "`Tab / Shift+Tab`" + ` | Cycle category filter |
| ` + "`H / L`" + ` | Switch between your hubs |
| ` + "`R`" + ` | Refresh the listing |
| ` + "`+`" + ` | Add a hub by URL (pick discovered peer hubs with ` + "`↑/↓`" + `) |
| ` + "`X`" + ` | Remove the selected hub (removing the last one restores the default) |
| ` + "`Esc`" + ` | Back / close |

Servers listed by federated peer hubs appear under a ` + "`── via <hub> ──`" + ` header. Added hubs are saved to your client config.

### How Joining Works (Address Privacy)

Server addresses are **never shown in the public listing** — only name, description, category, tags, and member counts. When you join, the hub first confirms the server is reachable and hands it a **single-use join token**; only then does the hub give you the address, and your client redeems the token with the server before adding it. You then land on the normal login screen with the connection details pre-filled. The address is visible to you once you join (it is your direct connection), just never published in the directory.

### Listing Your Server

Opt in during the server's **first-run wizard** (or later with ` + "`--reconfigure`" + `), or edit ` + "`concord-server.toml`" + ` by hand:

` + "```" + `toml
[grapevine]
enabled = true
hub_url = "http://grapevine.concord.chat"   # or your own hub
description = "A place to chat."
category = "Gaming"
tags = ["friendly", "english"]
public_host = "myserver.example.com"        # your externally reachable address
` + "```" + `

Registration is automatic on startup; the server heartbeats the hub every 30 seconds so the listing stays live, and recovers on its own if the hub forgets it. Listing is **opt-out by default** — nothing is published unless you enable it.

### Hosting Your Own Hub

The hub is its own lightweight binary:

` + "```" + `bash
make build-hub
./concord-hub               # first run launches a setup wizard
./concord-hub --setup       # re-run the setup wizard
./concord-hub --dashboard   # live TUI: stats, server list, activity log
` + "```" + `

Hubs can **federate**: add peer hubs in ` + "`grapevine-hub.toml`" + ` and their listings appear in yours (marked with the origin hub). Point clients at your hub with the **+** key in the Hub Browser.

---

## Settings Reference

Open settings with **Ctrl+S**. Navigate categories with **↑/↓**, press **Tab** to enter the form, **Tab** or **Esc** to go back.

### Theme

Live-preview available themes. **Enter** or **Tab** to apply. **Esc** reverts to the theme you had when settings were opened.

Over 40 themes are embedded, including **Dracula**, **Alucard Dark/Light**, **Nord**, **Gruvbox**, **Catppuccin Mocha**, **Tokyo Night**, **Kanagawa Wave**, **Everforest**, **Solarized Dark**, and **Terminal Default**. Apply one instantly with ` + "`/theme <name>`" + ` or browse with **Ctrl+T**.

### Notifications

| Field | Description |
|-------|-------------|
| Sounds Muted | Suppress all notification sounds |
| Mentions Only | Only play sounds for @mentions directed at you |
| Bell on Mention | Fire a terminal bell (` + "`\\a`" + `) on each @mention |
| Mention Sound | Sound for @mention alerts |
| Message Sound | Sound for all other messages |
| Mute Manager | Per-server and per-channel mute overrides |

### Display

| Field | Description |
|-------|-------------|
| Timestamp Format | 12-hour or 24-hour clock |
| Timestamp Style | Absolute (date+time) or Relative (e.g. "Today at 15:04") |
| Message Density | Compact / Normal / Spacious |
| Show Avatars | Coloured circle before each username |
| Date Separators | ` + "`──── Today ────`" + ` dividers between days |
| Message Grouping Gap | Minutes before a new header is shown for the same sender |
| Show Members Panel | Toggle the right-hand members column |
| Server List Panel | Expand or collapse the left server icon column |
| Members Panel | Expand or collapse the right members column |
| Voice Level Bar | Show/hide the ` + "`↑[████]`" + ` VU meter in Members |
| Connection Quality | Show/hide the ` + "`◆◆◆◇`" + ` quality bar in Members |
| Panel Animations | Enable/disable slide animations for Settings and Server Management panels |
| Typing Animation | Style of the typing indicator spinner (8 options) |

### Audio

| Field | Description |
|-------|-------------|
| Input Device | Microphone source (blank = system default) |
| Output Device | Speaker/headphone output (blank = system default) |
| Input Gain | Microphone amplification (0.0–2.0, default 1.0) |
| Output Volume | Playback volume (0.0–1.0, default 1.0) |
| Voice Activity Detection | Auto-transmit when mic exceeds the threshold |
| VAD Threshold | Sensitivity for VAD (0.0–1.0) |
| Push-to-Talk | Transmit only while PTT key is held |
| PTT Key | Configurable key combination (default: Ctrl+Space) |
| Noise Suppression | Reduce background noise |
| Echo Cancellation | Reduce microphone echo |
| Codec Preset | Opus quality preset (Low / Medium / High / Ultra) |

---

## Coming Soon

The following features are planned and will be available in future releases.

### 🔌 Plugin System

*Coming in a future release*

A plugin API that lets third-party extensions add new commands, event hooks, and UI panels **without modifying the core application**. Plugins run in a sandboxed process and communicate over a defined interface. The architecture is being finalized to ensure forward compatibility.

### 🤖 AI Integration & Bots

*Coming in a future release*

A server-side bot framework with event hooks (message received, user joined, reaction added, etc.), and optional LLM-powered assistant bots. Planned integrations:

- **Anthropic Claude** (via the Anthropic API)
- **OpenAI-compatible endpoints** (any provider with a compatible API)
- **Ollama** (for local self-hosted models)
- An in-client ` + "`/ai`" + ` command for inline compose assistance

### 🔔 OS-Level Notifications

*Coming in a future release*

Desktop notifications for @mentions and DMs when Concord is running in the background, using the native notification system on Windows, macOS, and Linux.

---

*Concord v0.1.0 — Built with Go, bubbletea, and lipgloss*
*Source: github.com/JMThomas00/Concord*
`

// renderHelpContent renders the full user guide using glamour markdown rendering.
// The right edge of the content area carries a scrollbar showing position in the document.
func (a *App) renderHelpContent(width, height int) string {
	s := a.settingsState

	dimStyle   := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Yellow)).Bold(true)
	thumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Yellow))
	trackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))

	layout := calculateSettingsLayout(width, height, 2, 0)

	// Reserve 2 chars on the right of each middle line for the scrollbar (" █" / " │").
	contentWidth := layout.interiorWidth - 2

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(titleStyle.Render("Help & User Guide"))
	top.writeLine(dimStyle.Render("Complete reference for Concord v0.1.0"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE — glamour-rendered markdown with inline scrollbar ──
	// Cache rendered lines; invalidate when content width changes.
	if s != nil && (s.HelpRenderedLines == nil || s.HelpRenderWidth != contentWidth) {
		s.HelpRenderedLines = renderHelpMarkdown(contentWidth)
		s.HelpRenderWidth = contentWidth
	}

	var allLines []string
	if s != nil && s.HelpRenderedLines != nil {
		allLines = s.HelpRenderedLines
	}

	totalLines  := len(allLines)
	trackHeight := layout.middleLines

	// Clamp scroll offset.
	offset := 0
	if s != nil {
		offset = s.HelpScrollOffset
		maxOffset := totalLines - trackHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if offset > maxOffset {
			offset = maxOffset
			s.HelpScrollOffset = offset
		}
		if offset < 0 {
			offset = 0
			s.HelpScrollOffset = 0
		}
	}

	end := offset + trackHeight
	if end > totalLines {
		end = totalLines
	}
	window := allLines[offset:end]

	// Compute scrollbar thumb position and size.
	thumbPos, thumbSize := helpScrollbarThumb(offset, totalLines, trackHeight)

	// Build each middle line as [content padded to contentWidth] + [2-char scrollbar].
	var middleBuf strings.Builder
	for i := 0; i < trackHeight; i++ {
		// Content — pad/clip to contentWidth so the scrollbar column stays aligned.
		contentLine := ""
		if i < len(window) {
			contentLine = window[i]
		}
		line := lipgloss.NewStyle().Width(contentWidth).Render(contentLine)

		// Scrollbar glyph: thumb (█) or track (│), shown only when content overflows.
		var scrollGlyph string
		if totalLines > trackHeight {
			if i >= thumbPos && i < thumbPos+thumbSize {
				scrollGlyph = thumbStyle.Render(" █")
			} else {
				scrollGlyph = trackStyle.Render(" │")
			}
		} else {
			scrollGlyph = "  "
		}

		middleBuf.WriteString(line + scrollGlyph)
		if i < trackHeight-1 {
			middleBuf.WriteString("\n")
		}
	}

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	focused := s != nil && s.FocusOnForm
	if focused {
		bottom.writeLine(dimStyle.Render("↑↓ / PgUp PgDn / scroll wheel · Tab back to menu · Esc close"))
	} else {
		bottom.writeLine(dimStyle.Render("Tab for keyboard scroll · scroll wheel anywhere · Esc close"))
	}
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middleBuf.String(), bottom.String())
	borderColor := lipgloss.Color(a.theme.Colors.Comment)
	if focused {
		borderColor = lipgloss.Color(a.theme.Colors.Yellow)
	}
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).Render(content)
}

// helpScrollbarThumb computes the scrollbar thumb start row and height in track coordinates.
func helpScrollbarThumb(offset, totalLines, trackHeight int) (thumbPos, thumbSize int) {
	if totalLines <= trackHeight {
		return 0, trackHeight
	}
	thumbSize = trackHeight * trackHeight / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxThumbPos := trackHeight - thumbSize
	maxOffset   := totalLines - trackHeight
	if maxOffset > 0 {
		thumbPos = offset * maxThumbPos / maxOffset
	}
	return
}

// renderHelpMarkdown renders the help markdown document via glamour and returns
// it as a slice of lines ready for the scroll-window display.
func renderHelpMarkdown(width int) []string {
	wrapWidth := width - 2
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return []string{"[glamour init error: " + err.Error() + "]"}
	}

	rendered, err := r.Render(helpMarkdown)
	if err != nil {
		return []string{"[glamour render error: " + err.Error() + "]"}
	}

	// Split into individual lines; preserve empty lines for spacing.
	lines := strings.Split(rendered, "\n")
	// Trim trailing empty lines that glamour adds.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
