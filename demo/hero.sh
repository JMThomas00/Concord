#!/bin/bash
# Records the README's hero shot with exact timing: the real client runs in
# tmux under asciinema, these keys drive it, and agg renders the GIF.
# Needs tmux, asciinema 3 and agg in ./bin, and a monospace font (fonts-jetbrains-mono).
# FINAL=1 sends the chat message (reset the demo server first).
set -e
export LANG=C.UTF-8
cd "$(dirname "$0")"
rm -rf home && cp -r "${HOME_TEMPLATE:-vhs-home}" home
tmux kill-session -t hero 2>/dev/null || true
tmux new-session -d -s hero -x 150 -y 42 \
  "./bin/asciinema rec --overwrite --quiet --output-format asciicast-v2 -c 'HOME=$PWD/home TERM=xterm-256color ./concord-client' hero.cast"
key() { tmux send-keys -t hero "$@"; }
type() { local s="$1" d="$2" i; for ((i = 0; i < ${#s}; i++)); do tmux send-keys -t hero -l -- "${s:i:1}"; sleep "$d"; done; }

sleep 5.5; key Space
sleep 3;   type grapes-are-great 0.18
sleep 2.5; key Enter
sleep 13;  key Tab
for _ in 1 2 3 4; do sleep 0.7; key Down; done
sleep 0.8; key Enter
sleep 3.5; key Tab
sleep 0.8; type "/theme gruvbox" 0.11
sleep 1;   key Enter
sleep 3.5; type "this is the coziest chat app ever 🍇" 0.09
sleep 1.2
[ "$FINAL" = 1 ] && key Enter
sleep 4.5; key C-q
for _ in $(seq 20); do tmux has-session -t hero 2>/dev/null || break; sleep 0.5; done
tmux kill-session -t hero 2>/dev/null || true
./bin/agg --theme dracula --font-size 15 --fps-cap 24 --idle-time-limit 6 --last-frame-duration 4 hero.cast hero.gif
