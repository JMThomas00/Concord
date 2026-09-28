# Terminal Passthrough (`sdk/pty`)

Shows an unmodified terminal program in a channel. Use it when the program
already exists (htop, a roguelike, a TUI tool) and rewriting it as a pane
isn't worth it.

```bash
concord-plugin new my-top --template pty
```

```go
plugin.Run(ctx, cfg, pty.Handler(pty.Options{
	Command: filepath.Join(exeDir, "htop"), // ship it in the zip
	Args:    []string{"-d", "10"},
	// Shared: true   // everyone types (default: one driver)
	// Env: []string{"KEY=value"}, Dir: ..., FPS: 30
}))
```

How it behaves:

- One session per channel, started when the first viewer enters. Everyone
  watching sees the same screen, emulated at the first viewer's size.
- **One driver** types: the first viewer, then the next when they leave.
  The pane title says who's driving. `Shared: true` lets everyone type.
- The program gets a minimal environment: `TERM`, `COLORTERM`, `LANG`,
  `PATH`, and `HOME` set to the plugin's data folder (plus `Env`).
- It's a **separate module** (`github.com/JMThomas00/Concord/sdk/pty`) because
  its terminal emulator needs newer libraries than Concord's UI stack.

Rules:

- **Linux and macOS servers only.** Windows needs ConPTY, which isn't
  supported yet; leave the Windows entrypoint out of `plugin.toml`.
- **Ship the program inside the release zip**, statically linked for each
  OS/CPU. A Docker-hosted server can't `apk add` anything.
- **Never wrap a shell**, or a program that can spawn one (editors with `:!`,
  pagers with `!`). The driver would get a shell on the server with the
  plugin's privileges. Prefer programs with no command execution at all.
- `go run .` standalone can't show the passthrough; test on a Linux server
  (see [`testing.md`](testing.md)).
