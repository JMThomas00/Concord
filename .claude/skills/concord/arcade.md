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
footer shows key chips. Board games' menus: 1 PLAYER VS CPU, 2 PLAYERS,
WATCH, OPTIONS, RECORDS.

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

## Rewards: Pit Passes

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
