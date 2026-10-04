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
4. **Record**, from a folder holding `concord-client`, the two `.tape`
   files and a fresh copy of `vhs-home` (merlot's profile, the server in
   the list, mood `#00C1S` locked so every take looks the same):

   ```sh
   rm -rf home && cp -r vhs-home home
   docker run --rm --network host -v "$PWD:/vhs" ghcr.io/charmbracelet/vhs hero.tape
   rm -rf home && cp -r vhs-home home
   docker run --rm --network host -v "$PWD:/vhs" ghcr.io/charmbracelet/vhs still.tape
   ```

   Change the address in `vhs-home/.concord/servers.json` for another server.
   Each take of `hero.tape` sends a message, so reset the server (step 1)
   before a final take.

**Things to know:**

- **Loading screen:** the first key skips it ("any key skips"), so the
  script presses Space before typing the password.
- **Focus:** it starts on the server list. Tab moves to the channels and
  then the messages.
- **Window size:** the window is 1440×810 at font size 15; Concord needs
  about 120 columns.
