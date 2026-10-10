# The Concord Arcade Standard (`sdk/arcade`)

Games should feel like an arcade cabinet: a title screen, a menu, pixel
art, chiptune sounds, and rewards to chase. `sdk/arcade` gives every game
the same parts, so they look and behave alike. Grape Race
(`JMThomas00/concord-grape-race`) is the reference.

## The screen flow

Title (attract mode) → Enter → Menu → the game → results → back. Esc always
goes back one screen. On the title screen Esc is Concord's (it leaves the
pane), so claim Esc everywhere except the title:

```go
func (m *model) ClaimedKeys() []string {
	if m.scr == scrTitle { return nil }
	return []string{wire.PaneKeyEsc}
}
```

Menus: ↑↓ choose, Enter select, ←→ change a value (`◂ NORMAL ▸`). The
footer shows key chips. Board games get their menus from the table kit (below).

## Drawing: the canvas

```go
c := arcade.New(w, h, arcade.NewPalette(viewer.Theme)) // nil theme: Dracula
c.Logo("GRAPE RACE", x, py, 1, nil)        // shaded 5x7 pixel font + drop shadow
c.Sprite(x, py, rows, map[rune]string{'C': "cyan"}, 2) // pixel art, scaled
c.Digit(7, x, py, "orange", 1)             // seven-segment, unlit segments shown
c.Box(x, y, w, h, "comment", "TITLE", "purple")
c.Keys(x, y, arcade.Key{Key: "Enter", Does: "start"})
return c.String()                          // your model's View
```

- **Pixels are half-blocks:** `py` counts half-rows, two pixels to a cell.
  A 12 x 4 sprite is 12 columns by 2 rows.
- **Colours are roles**, filled from each viewer's Concord theme: `bg fg
  comment line purple pink green yellow orange red cyan`, plus `hi shadow
  ghost dim tire hub` and a darker `<paint>D` for each paint. Never
  hard-code a colour; roles work in every theme, including terminal-default
  (ANSI numbers).
- **Role meanings:** purple is the brand (logos, cursor, the grapes); green
  go; orange the amber lights and warnings; red danger; pink score labels
  (`1UP`, `HI-SCORE`); cyan keys and info; yellow first place; ghost for
  anything unlit or locked.
- **`arcade.Paints`** are the colours players may pick (purple is never one).
- **Layout:** design for 80 x 24 and centre the stage in larger panes. Below
  64 x 24, fall back to a plain text layout (Grape Race's `compact.go`).
- A frame is roughly 5 KB. Animate at 5 frames a second at most, and only
  what needs it (attract mode for 30 s after the last key, countdowns, live
  play). Pane frames run synchronously, so drive the clock from a goroutine
  that hands ticks back with `Conn.Post` (see Grape Race's `app.go`).


## Board games: `table.Rules.Arcade`

A `sdk/table` game gets the whole front door from the kit: set
`Rules.Arcade` and implement `table.ArcadeBoard` on the board. Tic-tac-toe
(`JMThomas00/concord-tictactoe`, `game/arcade.go`) is the reference.

```go
Rules.Arcade = &table.Arcade{
	Title: "TIC-TAC-TOE", Tagline: "THREE IN A ROW",   // ≤ 13 letters fits 80 columns
	HowTo: []string{"Three in a row wins."},
	Keys:  []arcade.Key{{Key: "1-9", Does: "place"}},   // the kit adds M and Esc
	Sounds: true,                                       // client/ has the sound kit
	Reward: "GOLD STAR", Collection: "SETS",            // each game names its own
	Unlockables: items,                                 // each with a Kind ("pieces", "board")
	Kinds: []table.Kind{{ID: "pieces", Label: "PIECES"}, {ID: "board", Label: "BOARD"}},
	Preview: preview,      // draw an unlockable on a card, in SETS, on the menu
	Attract: attract,      // the title's attract mode, 76 x 14, from a frame count
	Result:  result,       // "X WINS!", "CAT'S GAME!"
	ResultArt: catOnDraws, // optional art on the results screen
}

// table.ArcadeBoard: the kit draws the top bar, the players' panels, the
// score and move counter, the status row and the keys around it.
func (b *Board) Draw(c *arcade.Canvas, x, y, w, h int)
func (b *Board) DrawSeat(c *arcade.Canvas, seat, x, y, w, h int) // the piece on a player's panel
func (b *Board) Status() string                                  // e.g. "not your turn", or ""
```

- **Every viewer starts on the title screen**, every time they open the
  channel: it's the game's chance to set its personality.
- **The menu follows the channel's seating mode:** 1 PLAYER VS CPU
  `◂ NORMAL ▸` (a game of your own, resumed if unfinished), then TAKE A SEAT
  (seats: sits you in the first open seat), or 2 PLAYERS and WATCH
  (challenge), or NEW GAME and YOUR GAMES (private). Then the collection,
  HALL OF FAME (by wins), HOW TO PLAY, OPTIONS (sound, effects).
- **Each player sees their own unlockables:** read them with
  `seat.Equipped("pieces")`. Ids must be unique across kinds (tic-tac-toe
  prefixes boards with `board:`).
- **Passes are automatic:** the kit grants them in its records (each new
  achievement, every 3 wins in a row, every 10 games) and runs the draft.
- **After a game,** Enter opens the results; Enter there asks for a
  rematch, which starts when every person at the table has asked.
- **Blinking and attract mode:** `seat.Frame()` counts the kit's ticks (5 a
  second, for 30 seconds after the last key, never when a player's Effects
  are off). Draw from the frame count; never keep animation state.
- **Callouts after a move** ("DOUBLE!", "KING ME!"): implement
  `table.Animator` (`Animating() bool`, e.g. for 1.4 s after `ChangedMsg`);
  the kit redraws while it's true, then once more to clear it. Skip
  callouts when `seat.Effects() != ""`: those players get no animation.
- **Under 64 x 24** the kit shows a plain "Enter to play" door and the plain
  table. A board that isn't an `ArcadeBoard` keeps the plain table view
  behind the arcade's menus.
- Board backgrounds: the palette's `<colour>B` roles (`greenB` a
  chalkboard, `yellowB` a winning square) and `Canvas.Shade`.

## Sounds

```go
//go:generate go run ./tools/gensounds   // arcade.WriteSoundKit("client")
conn.PlaySound(channel, viewer, arcade.SoundCoin, 0.7)
```

The kit: `SoundBlip` (menu move), `SoundSelect`, `SoundBack`, `SoundCoin`
(PRESS ENTER), `SoundAmber` and `SoundGo` (countdown), `SoundRed` (a foul),
`SoundMistake`, `SoundFinish`, `SoundRecord` (a record or an unlock).
Commit the generated files, declare `[client] capabilities = ["sound"]`,
and honour each player's mute and volume (an Options screen).

## Rewards: passes (Pit Passes, Gold Stars)

Unlockables (cars, piece sets, boards) have tiers: `Starter` (owned from
the start), `Common`, `Rare`, `Legendary`. Keep an `arcade.Rewards` in each
player's saved record.

```go
rec.Rewards.Grant(1, items)            // capped at what's left to unlock
offer := rec.Rewards.Deal(items, rnd)  // up to 3 locked items, weighted 6/3/1; saved until picked
rec.Rewards.Pick(offer[i])             // spends a pass, unlocks it
rec.Rewards.Owns(id, items)
```

Grant passes for each new achievement, every 3 wins in a row and every 10
games finished. Show them on the results screen, flag them in the menu, and
spend them in a 1-of-3 draft with a reveal (`SoundRecord`). Save the record
after `Deal` so the offer can't be re-rolled.

Name the passes and the collection to suit the game (Pit Passes and the
GARAGE in Grape Race, Gold Stars and SETS in tic-tac-toe); only the words on
screen change. Board games on the table kit get all of this from
`Rules.Arcade`.
