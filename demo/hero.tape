# Concord hero shot, for the README and the website. Recorded against the
# seeded demo server on VM 113 (go run ./tools/demoseed). Run with:
#   rm -rf home && cp -r vhs-home home && docker run --rm --network host -v "$PWD:/vhs" ghcr.io/charmbracelet/vhs hero.tape
# (see demo/README.md)
Output hero.gif
Set Shell bash
Set FontSize 15
Set Width 1440
Set Height 810
Set Padding 12
Set Theme "Dracula"
Set Framerate 24

Hide
Type "clear; HOME=/vhs/home TERM=xterm-256color ./concord-client" Enter
Show
Sleep 5.5s
Space
Sleep 3s
Type@180ms "grapes-are-great"
Sleep 2.5s
Enter
Sleep 11s
Screenshot still-general.png
Sleep 2s
Tab
Sleep 700ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 700ms
Enter
Sleep 3s
Screenshot still-code.png
Tab
Sleep 700ms
Type@110ms "/theme gruvbox"
Sleep 1s
Enter
Sleep 3.5s
Screenshot still-theme.png
Type@90ms "this is the coziest chat app ever 🍇"
Sleep 1.2s
Enter
Sleep 4s
Screenshot still-end.png
