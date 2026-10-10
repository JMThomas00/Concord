# Pictures and Sounds (`client/`)

A plugin can ship pictures and sounds in a `client/` folder next to its
`plugin.toml`. Concord serves those files to members' clients, and a client
downloads, checks and caches each file the first time it needs it. The
plugin then places pictures in its pane frames and asks clients to play
sounds. Needs SDK v0.5.0 or later (v0.5.1+ for standalone games against the
computer).

Reference: `JMThomas00/concord-tictactoe`. Its pieces are pictures in each
viewer's theme colors, and it has sounds for moves, wins and draws.

## The folder

```text
my-plugin/
  plugin.toml
  client/
    assets/x-ff5555.png
    sounds/move.wav
```

- Allowed types: `.png`, `.jpg`/`.jpeg`, `.gif` (first frame), `.wav`,
  `.ogg`/`.opus` (Ogg Opus), `.json`. Anything else is skipped, so don't rely on it.
- The whole folder must stay **under 50 MB**, and a picture over 16 megapixels
  is refused. Keep pictures small: about
  160 px is plenty for a board square.
- Paths you send are relative to `client/` (`"assets/x-ff5555.png"`) and use `/`.
- `[client]` in `plugin.toml` is optional for pictures and sounds. Listing
  `capabilities = ["images", "sound"]` documents what the plugin uses.
- The release zip must include `client/`. The scaffolder's `release.go`
  (v0.5.0+) packs it; for an older `release.go`, add a `filepath.WalkDir("client", ...)`
  that adds each file under the zip's top folder.

## Pictures

A frame can carry pictures, each placed in a box of cells:

```go
wire.PaneImage{Asset: "assets/x-ff5555.png", Col: 2, Row: 1, Cols: 11, Rows: 6}
```

`Col`/`Row` are the box's top-left cell, 0-based inside the pane, and
`Cols`/`Rows` are its size. The picture is scaled to fit and keeps its
aspect ratio (a cell is about twice as tall as it is wide).

- **Panes and games:** implement `pane.Imager` on the model:
  `Images() []wire.PaneImage`. It's called after `View`, using the same
  coordinates as the view's text. Boxes that spill past the pane are clipped.
- **Table-kit boards:** do the same on the board model. The kit moves the
  boxes down past its own header for you.
- **Direct:** `conn.FrameWithImages(channel, viewer, frame, images)`.

Each viewer's client picks how to draw them: Kitty graphics, Sixel
(Windows Terminal 1.22+, foot, WezTerm…), iTerm2, or colored half-blocks
(macOS Terminal and anything else). Members can force a method or turn
pictures off in Settings → Display → Plugin Pictures.

**Always draw sensible text under every box.** Viewers with pictures off see
that text instead, and so do standalone players. Tic-tac-toe draws each piece
as a half-block shape in the same color, so the board reads the same either way.

**Matching the theme:** pictures can't be recolored by the client. Ship one
file per color you need, and choose by `seat.Color("red", ...)` /
`v.Color(...)`. If the color isn't one you drew (a custom theme), use the
nearest one. A theme like terminal-default gives ANSI numbers (`"1"`, `"9"`) instead of
`#rrggbb`; map those to a stand-in color. See tic-tac-toe's `game/pieces.go`
and `tools/gen.go`, which generates a picture for every built-in theme's color.

## Sounds

```go
conn.PlaySound(channel, uuid.Nil, "sounds/move.wav", 0) // everyone viewing; 0 = full volume
conn.PlaySound(channel, viewerID, "sounds/your-turn.wav", 0.6)
```

- **WAV** (8- or 16-bit PCM, mono or stereo, any common rate; not float or
  24-bit) or **Ogg Opus**. Keep
  them short (under a couple of seconds) and quiet; they play over voice.
- **Table kit:** set `Rules.Sound func(g table.Game, move string) string`.
  It's called after each move with the game *after* the move, so it can pick
  a win, capture or check sound. Return `""` for silence.
- Members set the volume or mute plugin sounds in Settings → Audio → Plugin Sounds.
  The volume you pass is scaled by theirs. A client built without audio
  ignores sounds.

## Testing

- `srv.Images(viewer)` returns the pictures on that viewer's last frame. Check
  the asset names and that boxes stay inside the pane.
- `srv.NextSound()` waits for the next sound and returns it (payload, viewer).
- Test that every asset your code can name exists in `client/`, for every
  theme color. Tic-tac-toe's `TestPieceAssetsExistForEveryThemeColor` does this.
