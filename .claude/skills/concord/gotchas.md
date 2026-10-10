# Gotchas

Each of these has cost real debugging time.

**Frames must fit the pane.** A line wider than the reported width, or more
lines than its height, corrupts the whole frame (Concord wraps it again).
Lip Gloss `Width()`/`Height()` pad but never truncate. `sdk/pane` fits
frames for you; hand-built frames sent with `Conn.Frame` must fit
themselves. When dividing width across columns, round down.

**Don't block a callback.** Callbacks run one at a time; a 2-second AI
search or HTTP call inside one freezes every viewer. Use a goroutine and
`c.Post`. `RequestMembers` waits for the server's reply; inside a callback that
stalls every other event until it arrives. Call it from a goroutine.

**Answer yourself and you loop forever.** In `OnMessage`, skip messages
whose `AuthorID` is `c.Self().ID`.

**Typing indicators expire after ~5s.** For slow replies (LLMs), re-send
`Typing(ch, true)` every 3s until the reply is sent.

**Timeouts from measurements, not guesses.** An LLM reply that takes 70s
against a 60s deadline fails silently. Set deadlines from observed
latency, with the HTTP client's a little longer than the context's.

**Don't overwrite a host's callbacks.** `pane.Host.Handler()` and
`table.Kit.Handler()` return Handlers with callbacks already set. Add only
the ones they leave nil (for panes, `OnConfig`/`OnChannel` are free; for the
table kit, `OnConfig` is), or wrap the existing function and call it.

**Perspective changes after the board is built.** A board is created when
the viewer enters, often before they sit down. Anything derived from
`seat.Perspective()` (flipping, starting cursor) must be re-checked in
`Update`, not only in the constructor.

**Colors vanish on some themes.** Hard-coded black pieces, or text colored
like the background, disappear. Use `Color(name, fallback)` with theme
names, and test with a light theme too.

**Keys Concord keeps.** Ctrl+] always leaves the pane. Esc, Tab and Shift+Tab
move focus between panels (Esc and Shift+Tab to the channels, Tab to the
members), so a pane gets them only while its frame claims them (`ClaimedKeys`,
`SendFrame` with `Keys`, or `client.ClaimKeys`). Claiming Esc all the time
traps people: claim it only while there's something to cancel. The table kit
keeps M for its menu. Mouse events aren't forwarded.

**Settings arrive late and change live.** `OnConfig`/`OnChannel` run after
connect and again whenever an admin saves. Don't read settings once at
startup; keep the latest values and use them per event. (Exception: the table
kit snapshots channel options per table, so a game's options change only at
the next game.)

**`channel_select` values are channel IDs**, not names.

**The plugin's folder is replaced on update.** Save to `c.DataDir()` only.

**Stale binaries look like missing features.** If a behavior seems absent,
check the installed version in Settings > Plugins before debugging code.

**A viewer is a user, not a device.** The same account with the pane open
on two computers is one viewer to the plugin; the pane follows whichever
opened it last and the other is handed its keyboard back. Test multiplayer
with two accounts.

**Don't float the UI libraries.** `go get -u` on bubbletea/bubbles/lipgloss
can pull versions the SDK hasn't been tested with. Update the SDK instead,
and let it choose.

**Pad every frame line to the same width.** Concord before v0.1.1 centred
each line of a frame on its own, so lines of different lengths shifted
against each other and columns zig-zagged. v0.1.1 and later centre the
frame as one block, but members on older clients still see the zig-zag.
Pad every line to the pane's width: `sdk/arcade`'s canvas does; `pane.Fit`
only trims.
