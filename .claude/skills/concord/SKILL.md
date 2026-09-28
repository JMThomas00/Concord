---
name: concord
description: >
  REQUIRED for building, packaging, testing or installing a Concord plugin: a
  separate program that attaches to a Concord chat server as a bot, game table,
  custom full-screen pane, or wrapped terminal app. Triggers: Concord plugin,
  concord-plugin, plugin.toml, channel_kind, create_field, server_config_field,
  remote pane, pane frame, bot for Concord, game channel, table kit, seats,
  challenge, hotseat, computer opponent, network play, pty passthrough, plugin
  SDK (github.com/JMThomas00/Concord/sdk), plugintest, release zip, "install
  a plugin on my Concord server". Excludes developing Concord itself (the
  server, client or hub source).
---

# Concord Plugin Skill

Build plugins for [Concord](https://github.com/JMThomas00/Concord), a
self-hosted terminal chat platform. A plugin is **its own complete program**
in its own repo: Concord launches it, and it connects back to the server as
a privileged client. Concord only gives people a way to interact with it
(a chat channel, or a pane in the channel view).

This skill is for plugin authors. It is not for changing Concord's source.

## When This Skill MUST Be Used

**Always use this skill when the request involves any of these:**

- Making a bot, game, tool or app "for Concord" or "in a Concord channel"
- Writing or editing a `plugin.toml`
- Code importing `github.com/JMThomas00/Concord/sdk/...`
- Packaging, releasing or installing a Concord plugin
- Testing a plugin with `plugintest` or against a real Concord server

**Do NOT use it for Concord core work** (files under Concord's `internal/`,
`cmd/server`, `cmd/client`, `cmd/hub`). If a plugin seems to need a Concord
change, stop and say so; see "Critical Rules".

## Topic Guides

Read the matching guide before starting:

- [`sdk.md`](sdk.md): the SDK packages, `plugin.Run`, `Handler`, `Conn` helpers, and the pane host
- [`manifest.md`](manifest.md): every `plugin.toml` field, and how settings become UI forms
- [`games.md`](games.md): turn-based games on the table kit (seating modes, computer opponent, standalone and network play)
- [`passthrough.md`](passthrough.md): showing an existing terminal program in a channel
- [`packaging.md`](packaging.md): release zips, GitHub releases, and installing on a server
- [`testing.md`](testing.md): unit tests with `plugintest`, then a live server
- [`gotchas.md`](gotchas.md): mistakes that have cost real debugging time
- The wire protocol, for plugins not written in Go: `sdk/PROTOCOL.md` in the Concord repo

## Critical Rules

1. **Zero Concord changes per plugin.** Everything plugin-specific lives in
   the plugin's repo: its manifest, its binary, its own event kinds. If
   something truly can't be done without a Concord change, stop and tell the
   user; it becomes a separate Concord task, never part of the plugin.
2. **One binary, two modes.** `plugin.ConfigFromEnv()` returns `ok=false`
   when Concord didn't launch the program. Then it runs standalone in the
   terminal (the same UI, hotseat, a REPL for bots). Every template does this.
3. **No configuration files for admins.** Settings are `server_config_field`
   (server-wide) or `create_field` (per channel) entries in `plugin.toml`, and
   Concord renders them as forms. Never ask an admin to edit TOML, YAML or env.
4. **Secrets only in `type = "secret"` server fields.** They're encrypted at
   rest and only the plugin sees the value. Never put a key in `[process.env]`,
   a `create_field`, the repo, or a log line.
5. **Pure Go, `CGO_ENABLED=0`.** Servers often run in Alpine Docker. The
   generated `release.go` already builds this way; keep dependencies pure Go.
6. **Never touch a server's `Plugins/` folder by hand.** Install, update and
   remove through Server Settings → Plugins (below). Hand edits race the
   plugin manager and skip verification.
7. **Treat plugin power seriously.** A plugin runs with the server's
   privileges. Never wrap a shell, never run commands built from chat text,
   and keep a pty plugin to one specific program.
8. **Stay on the SDK's dependency versions.** Don't upgrade bubbletea,
   bubbles, lipgloss or x/ansi past what the SDK requires unless the user asks.

## Command Discovery

The scaffolder is the starting point for every new plugin:

```bash
# Create a plugin repo (templates: game, pane, bot, pty)
go run github.com/JMThomas00/Concord/sdk/cmd/concord-plugin@latest new my-plugin \
  --template game --name "My Game" --module github.com/<owner>/my-plugin

# Help
go run github.com/JMThomas00/Concord/sdk/cmd/concord-plugin@latest --help
```

It writes `main.go`, `plugin.toml`, a README, `.gitignore`, `release.go` and
a GitHub Actions release workflow. After scaffolding, make sure `go.mod`
requires the newest `github.com/JMThomas00/Concord/sdk` tag (list them with
`go list -m -versions github.com/JMThomas00/Concord/sdk`), then `go mod tidy`.
Older SDKs lack features these guides describe (v0.1.0 has no network play,
and before v0.3.0 the computer is always "normal" inside Concord).

Inside a plugin repo:

```bash
go run .                 # standalone: play or try it in this terminal
go test ./...            # unit tests (plugintest)
go run release.go        # dist/<id>_<os>_<arch>.zip for every server platform
```

Read the SDK source for details; it's small and documented:
`go doc github.com/JMThomas00/Concord/sdk/plugin`, `.../sdk/pane`,
`.../sdk/table`, `.../sdk/plugintest`.

## Decision Framework

Pick the template from what the plugin *is*:

| The plugin... | Template | Model |
|---|---|---|
| reads and answers chat (commands, an LLM, notifications) | `bot` | relay channel: messages in its channels (and @mentions) reach `OnMessage`; it replies with `SendMessage` |
| is a turn-based game for 1+ players | `game` | table kit: you write rules + a board; seating, spectators, lobby, saving, computer and network play are provided. See [`games.md`](games.md) |
| is its own interactive UI (a board, dashboard, editor) | `pane` | remote pane: one Bubble Tea model per viewer, shared state + `Broadcast` |
| is an existing terminal program (top, a roguelike) | `pty` | passthrough: the program runs in a pseudo-terminal, everyone watches, one drives. Linux/macOS servers only. See [`passthrough.md`](passthrough.md) |

When unsure between `pane` and `game`: if players take turns and a move is a
string you can validate, it's `game`.

## Installing on a Concord Server

An admin with the **Manage Plugins** permission (or the server owner) opens
**Server Settings → Plugins** in the Concord client:

- **I**: install. Type `owner/repo` (the latest GitHub release's zip for the
  server's OS/CPU is picked and verified automatically), a GitHub release
  asset link, or any https zip link.
- **U**: update (Enter alone uses the manifest's `source_url`). A failed
  update rolls back automatically.
- **R** restart · **T** enable/disable · **X** uninstall (twice) · **S** rescan ·
  **Enter** configure (the manifest's settings form).

Then create a channel in **Server Settings → Channels** and choose the
plugin's channel kind as its type; the kind's `create_field`s appear as that
channel's options. No server restart, no files to edit.

## Example Requests

- "Make a dice bot for Concord" → `bot` template; commands in `answer()`; test with `go run .` then `plugintest`
- "Make a Connect Four plugin" → `game` template; follow [`games.md`](games.md) (two seats, moves are column numbers)
- "Let us play chess against each other in a channel" → `game`; a finished example is github.com/JMThomas00/concord-chess
- "Show htop in a channel" → `pty` template; ship the static binary in the release zip ([`passthrough.md`](passthrough.md))
- "A shared todo board in a channel" → `pane` template; shared state + `Host.Broadcast`
- "Add an API key setting" → a `type = "secret"` `server_config_field`; read it in `OnConfig`
- "Let admins pick a board size per channel" → a `select` `create_field`; the table kit passes it to `Rules.New`
- "Release it" / "install it on my server" → [`packaging.md`](packaging.md)

## Out of Scope

- Editing Concord's own source (`internal/`, `cmd/server|client|hub`)
- New opcodes or protocol changes: plugins use existing ops, and add
  behaviour through their own `PLUGIN_EVENT` kinds
- Hosting or administering the Concord server itself
