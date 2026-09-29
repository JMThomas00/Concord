# Testing a Plugin

Three levels. Levels 1 and 2 are the bar for "done" in a coding session;
level 3 needs a release and a server, so hand it to the user as a checklist
if you can't run it.

## 1. Standalone

`go run .` exercises the same UI (or command handling) without Concord. For
games, try hotseat, each computer level, and network play (one terminal
hosts, another joins with `127.0.0.1:7412 CODE`).

## 2. `plugintest`: a fake Concord server

```go
srv := plugintest.NewServer(t)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go plugin.Run(ctx, srv.Config(), handler)   // the plugin's real Handler
srv.WaitReady()

ch := uuid.New()
srv.Channel(wire.Channel{ID: ch, Name: "t", PluginConfig: map[string]string{"board_size": "4"}})
srv.Settings(map[string]string{"prefix": "!"})    // server settings → OnConfig

alice := srv.Enter(ch, "alice", 70, 30)            // a viewer opens the pane
srv.Key(alice, "enter")                             // key names as Bubble Tea prints them
srv.Type(alice, "Nf3")                              // one key per rune
frame := srv.FrameContaining(alice, "your move")    // waits up to 5s, fails with the last frame
srv.Resize(alice, 40, 12); srv.Leave(alice)

srv.ChatMessage(ch, "bob", "!roll 2d6")             // chat → OnMessage (bob keeps one user ID: srv.UserID("bob"))
reply := srv.NextChat()                             // what the plugin posted
posted := srv.Posted()                              // every message as it stands now (streamed ones included)
final := srv.WaitStreamDone(posted[0].ID)           // a streamed reply, once finished
edit := srv.NextEdit()                              // the next EditMessage
ev := srv.NextEvent()                               // notify, notify_user, pane_title, members...
rest := srv.DrainEvents()                           // anything else already sent, without waiting
srv.AnswerMembers(ev, []wire.PluginMember{...})     // reply to a members request
srv.DropConnection()                                // test reconnect: Enter is replayed
```

It fails the test on protocol mistakes: frames to non-viewers, frames taller
than the pane, or a non-increasing `Seq`.

Tips:

- `FrameContaining` waits up to `plugintest.Timeout` (5s) and matches the
  **raw** frame, color codes included, so a phrase that spans a style change
  never matches. Assert on text drawn in one style ("last move e5", a
  header name), not on exact frames. Strip SGR (`\x1b\[[0-9;]*m`) before
  counting glyphs or reading a failure's frame by eye.
- For table-kit games, the kit's own strings and the Tab menu's order are
  listed in [`games.md`](games.md) ("What the kit draws").
- Test at odd sizes (e.g. 43×17) as well as roomy ones; overflow bugs only
  show when the math doesn't divide evenly.
- To eyeball a frame, `t.Log(frame)` and run with `-v`.
- Every test should run in well under a second, except deliberate computer-level timing tests.

## 3. A real Concord server

The fake server can't show piped stdout, real terminal sizes, real themes,
or install/update. Before release:

1. Tag a release (or host the zip anywhere over https).
2. Install it with **Server Settings → Plugins → I**, create a channel of
   its kind, and open it from two clients signed in as **different
   accounts** (plugins see viewers as users: the same account on two
   clients is one viewer, and the pane follows whichever opened it last).
3. Check: settings forms and their errors, a restart (**R**) repaints open
   panes, an update (**U**) keeps data, Ctrl+] always returns the keyboard.
4. The plugin's stdout/stderr go to the server's log, prefixed with its ID.
