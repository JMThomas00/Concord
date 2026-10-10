# The hero shot

`hero.gif` (the README's recording) and `hero.png` (a still for the README
and the website) come from a real client talking to a real server, both
scripted, so they can be made again after the interface changes.

1. **A demo server nobody has signed up to yet.** The home lab's VM 113
   runs one in Docker on port 8090 ("The Vineyard"). For a fresh one: stop
   it, delete `server-data/concord.db`, start it again.
2. **Fill it:** `go run ./tools/demoseed -server http://<host>:8090`. Seven
   members, channel groups, channels and a conversation; everyone's password
   is `grapes-are-great`. The first member, merlot, owns it.
3. **A Linux client** next to the scripts, built without voice (the
   recording doesn't need it):
   `CGO_ENABLED=0 GOOS=linux go build -tags novoice -o concord-client ./cmd/client`
4. **Record**, from a folder holding `concord-client`, `hero.sh`,
   `still.tape` and `vhs-home` (merlot's profile, the server in the list,
   mood `#00C1S` locked so every take looks the same):

   ```sh
   ./hero.sh                 # a practice take: types the message, doesn't send it
   FINAL=1 ./hero.sh         # the real one (reset the server first, step 1)
   rm -rf home && cp -r vhs-home home
   docker run --rm --network host -v "$PWD:/vhs" ghcr.io/charmbracelet/vhs still.tape
   ```

   `hero.sh` needs `tmux`, a monospace font (`fonts-jetbrains-mono`) and,
   in `./bin`, [asciinema](https://github.com/asciinema/asciinema/releases) 3
   and [agg](https://github.com/asciinema/agg/releases). Change the address
   in `vhs-home/.concord/servers.json` for another server.

**Things to know:**

- **Why not VHS for the GIF:** VHS screenshots its browser terminal, and
  with the login stage's animations it managed about 2 frames a second
  while stamping each as 1/25 s, so a 55-second take played in under 5.
  asciinema records what the client actually printed, with timestamps, so
  the GIF plays in real time. VHS is still fine for the stills.
- **Loading screen:** the first key skips it ("any key skips"), so the
  script presses Space before typing the password.
- **Focus:** it starts on the server list. Tab moves to the channels and
  then the messages.
- **Window size:** the GIF is 150×42 cells at font size 15; Concord needs
  about 120 columns.
