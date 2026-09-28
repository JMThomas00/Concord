# Turn-Based Games (`sdk/table`)

Read this before building any game. The table kit supplies everything
except the rules and the board: seats, spectators, three seating modes,
the Tab menu (sit, stand, resign, rematch, add computer, lobby), "your turn"
notifications, saving and resuming, a computer opponent off the event loop,
and standalone play in a terminal (hotseat, computer, and network play).

Start from `concord-plugin new <name> --template game`, then replace its
stone-pile game. The template is one `main.go`; split it into the layout below
as soon as the rules are more than a toy (all the reference games do). Reference implementations, from smallest:

- `sdk/examples/tictactoe` in the Concord repo: one file, read it first
- github.com/JMThomas00/concord-checkers
- github.com/JMThomas00/concord-tak: board-size and komi channel options
- github.com/JMThomas00/concord-chess: SAN input, promotion prompt, flipped board

## Repo layout

```
engine/   the rules: positions, legal moves, notation, the computer. Pure Go, no UI.
game/     game.go: the table.Game adapter + table.Rules; board.go: the Bubble Tea board
main.go   ConfigFromEnv → table.RunLocal (standalone) or plugin.Run(table.New(rules).Handler())
```

Keep `engine/` free of the SDK so it can be tested hard on its own.

## The three pieces you write

```go
var Rules = table.Rules{
	Name:      "Chess",
	SeatNames: []string{"White", "Black"},           // count = number of players
	New:       func(opts map[string]string) table.Game { return New(opts) },
	NewBoard:  func(s *table.Seat) tea.Model { return newBoard(s) },
	AI:        func(g table.Game, level int) string { ... }, // optional; levels 1-3
}
```

1. **`Game`**: `Turn() int` (seat to move, **-1 once over**), `Play(move
   string) error` (validate fully; the error text is shown to the player),
   `Outcome() table.Outcome{Over, Winner (-1 draw), Reason}`. Moves are
   strings in the game's usual notation. The kit saves only the move list and
   rebuilds games by replaying it, so `Game` needs no save code, and `Play`
   must be deterministic.
2. **The board**: a Bubble Tea model reading everything through the `Seat`:
   - `seat.Game()`: the game (type-assert to yours). Read-only.
   - `seat.MyTurn()`: may this viewer move now?
   - `seat.Play(move)`: make a move; show the error if it fails.
   - `seat.Perspective()`: the seat to draw from (flip the board for it).
     It **changes** when a spectator sits down or a hotseat turn passes, so
     re-derive anything based on it (like a starting cursor) in `Update`.
   - `seat.Index` (-1 spectating), `seat.Players()`, `seat.SeatName(i)`, `seat.Moves()`.
   - `seat.Color(name, fallback)`: the viewer's theme color.
   - On `table.ChangedMsg` (any move, new game, resign): clear selection and errors.
   - `tea.WindowSizeMsg` is the board's **exact** budget: the kit has already
     taken its own header (players, result) and footer (hints, menu) off it. Draw
     at most that many lines, status line included; anything more is cut from
     the bottom and the kit's footer disappears. Shrink cells in steps to fit.
   - Until every seat is filled, `MyTurn()` is false for everyone; show a
     "waiting" state (`seat.Players()[i].Empty()`). `Players()[turn].Computer`
     tells you to show "thinking…".
   - Every key except **Tab** (the kit's menu) reaches the board. `q`/`esc` →
     `tea.Quit` hands keys back to Concord (ignored standalone).
3. **`AI`** (optional): return a legal move for the side to move in `g` (a
   private copy, rebuilt with `Rules.New(options)` and the moves replayed, so
   channel options reach it). It runs in a goroutine; keep the hardest level
   under about 3 seconds. Level 1 should be beatable (shallow search plus
   randomness). In Concord the level comes from the channel's
   `computer_level` setting; standalone, the player picks it.

## Channel settings

Declare these `create_field`s (the scaffolded `plugin.toml` has them all):

| key | values | effect |
|---|---|---|
| `seating` | `seats` / `challenge` / `private` | one shared table / a lobby with challenges / per-pair private games |
| `allow_spectators` | boolean | non-players can watch |
| `computer_opponent` | boolean | the menu offers "play the computer" |
| `computer_level` | `easy` / `normal` / `hard` | the computer's strength (`Rules.AI` level 1-3) |

Add your game's own options as more `create_field`s (e.g. Tak's
`board_size` select). They reach `Rules.New(options)`. Standalone, pass the
same keys to `table.RunLocal(rules, options)` from command-line flags. Parse
defensively: missing or bad values fall back to defaults.

**Options are a snapshot per table.** The kit copies the channel's settings
into a table when it's created and again on each rematch; a table's game
never changes size mid-game. So an admin's edit applies from the next game
(in `seats` mode, the next rematch), and games saved before it keep their options.

## What the kit draws

The kit owns the lines above and below the board. Tests match these strings:

- Header: `▸ ` marks the seat to move, then `Seat: name` per seat
  (`X: alice   ▸ O: Computer (hard)`). Then `waiting for players` until the
  table is full, or the result (`alice wins — <Outcome.Reason>`, `Draw — …`).
  Computers are `Computer (easy)`, `Computer`, `Computer (hard)`.
- Footer: `Tab: sit down` (seats mode, not seated, table not full),
  `Spectating · Tab: menu`, or `Tab: menu`.
- The header doesn't shorten itself; on very narrow panes long names are cut
  off at the right edge. That's the kit's, not your board's, to fix.

**The Tab menu** is a row of items (←/→ to move, Enter to pick). Only items
that apply appear, in this order:

| Item | Shown when |
|---|---|
| `Sit as <seat>` (per empty seat) | seats mode, you're not seated, table not full or game over |
| `Computer plays <seat>` (per empty seat) | seats mode, you're seated, table not full, computer allowed |
| `Stand up` | seats mode, seated, table not full or game over |
| `Resign` | seated, game in progress |
| `Rematch` | seated, game over (seats swap) |
| `Back to lobby` | challenge and private modes |
| `Close menu` | always |

So in a fresh seats table: `tab`,`enter` sits you in the first empty seat;
seated alone, `tab`,`enter` gives the next seat to the computer; after a
game, `tab`,`right`,`enter` is Rematch.

## Board UX that has worked

- Arrow keys **and** hjkl move a cursor; Enter selects then confirms; Esc cancels.
- A `:` prompt to type a move in notation, for players who know it.
- Highlight: cursor (only on your turn), selected piece, legal targets, last move, check.
- A one-line status that changes with context ("your move: …", "choose where it goes",
  the last error, "last move Nf3"). Keep it under ~55 columns. The kit doesn't
  write "your move"; your board does, so pick a phrase your tests can find.
- Show the board from the player's side.
- Unicode pieces render fine; color them with theme colors, not black/white
  (a "black" piece is invisible on dark themes). E.g. `foreground` vs `orange`.

## Testing a game

- `engine/`: perft (legal move counts vs published numbers) if the game has
  them; otherwise hand-built positions for every rule edge; notation round trips;
  the computer takes a win in one and blocks a loss in one, at every level;
  the hardest level stays within time.
- `game/`: the adapter (options, turn, illegal moves, game end), plus a
  `plugintest` session: two viewers `Enter`, each presses `tab` then `enter`
  to sit, then play a few moves with keys and assert on `FrameContaining`
  (see [`testing.md`](testing.md)). This catches perspective and cursor bugs.
  Also test the board at small sizes: no frame taller than the pane, and the
  kit's footer still visible.
- Standalone: `go run .` for hotseat, the computer, and host/join network play
  on port 7412 (network play needs SDK v0.2.0 or later).
