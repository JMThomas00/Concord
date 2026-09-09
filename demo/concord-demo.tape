# concord-demo.tape
#
# VHS (https://github.com/charmbracelet/vhs) script for a README demo GIF.
# NOT run automatically anywhere -- item 8 on the pre-v0.1.0 to-do
# explicitly defers actually recording this to Jordan: it needs a real
# terminal session (`vhs` shells out to a real PTY) and a real, already
#-running demo server + an already-registered account, neither of which an
# agent can set up or verify by itself.
#
# Prerequisites before running `vhs demo/concord-demo.tape`:
#   1. A Concord server running locally with a demo account already
#      registered (the wizard/login flow itself isn't part of this
#      recording -- it's a below-the-fold detail, not the interesting part
#      of the product). See README.md's Quick Start for how to stand one up.
#   2. `build/concord-client` built with voice included (`make build-client`
#      or `make build-windows`, see README.md's Building from Source) --
#      the recording should show the real, shipped experience.
#   3. A general channel with a couple of prior messages already in it so
#      the chat panel doesn't look empty on launch, and at least one voice
#      channel to demonstrate joining.
#   4. Concord's own config (~/.concord/) pre-populated with a saved
#      identity/server so launch goes straight to ViewMain, skipping the
#      first-run identity setup screen -- that screen is real and worth
#      knowing about, but a fresh-install wizard isn't the demo.
#
# Adjust the Type/Sleep timings below against a real run -- these are a
# reasonable starting point, not measured against an actual terminal.

Output demo/concord-demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 700
Set Theme "Dracula"
Set TypingSpeed 60ms
Set Padding 20

# Launch the client -- lands on ViewMain per the pre-populated config above.
Type "./build/concord-client"
Enter
Sleep 2s

# Let the channel list / chat panel render and settle.
Sleep 1s

# Type a chat message showing off markdown rendering (item 3): bold,
# inline code, and a link all in one line.
Type "Hey everyone -- just shipped **markdown** support with `glamour` 🎉 see https://github.com/concord-chat/concord"
Sleep 500ms
Enter
Sleep 1500ms

# Switch focus to the channel list and select a voice channel to show
# joining voice (Enter twice: once to focus, once to join).
Type "@"
Sleep 300ms
Escape
Sleep 300ms
Tab
Sleep 500ms
Down
Sleep 300ms
Down
Sleep 300ms
Enter
Sleep 2s

# Leave voice, then open the theme browser (Ctrl+T) to show live theme
# switching -- one of Concord's most visually distinctive features.
Enter
Sleep 500ms
Ctrl+T
Sleep 1s
Down
Sleep 400ms
Down
Sleep 400ms
Down
Sleep 800ms
Enter
Sleep 1500ms

# Open Settings > Help & Guide briefly to show the themed glamour rendering
# from item 3a (headings, bullet lists, inline code all matching the
# active theme).
Ctrl+S
Sleep 800ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Tab
Sleep 1500ms

# Close out.
Escape
Sleep 500ms
Escape
Sleep 500ms
Ctrl+Q
Sleep 1s
