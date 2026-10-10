// Reflex's client code: the whole game runs on the viewer's computer, so
// the timing is exact and the animation smooth whatever the network does.
// Only finished times go to the server half, for the leaderboard.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o ../client/plugin.wasm .
// (go run ../build.go does that and signs it.)
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/JMThomas00/Concord/sdk/client"
)

const (
	purple = "\x1b[1;38;2;189;147;249m"
	green  = "\x1b[1;38;2;80;250;123m"
	red    = "\x1b[1;38;2;255;85;85m"
	dim    = "\x1b[38;2;98;114;164m"
	reset  = "\x1b[0m"
)

type entry struct {
	Name string `json:"name"`
	MS   int    `json:"ms"`
}

var (
	width, height int
	phase         = "idle" // idle, waiting, go, result, early
	goAt          time.Time
	last, best    int
	spin          int
	board         []entry
)

func main() {
	client.Run(client.Handler{
		OnStart: func(e client.Event) {
			width, height = e.Width, e.Height
			if v, ok, _ := client.Get("best"); ok {
				best, _ = strconv.Atoi(v)
			}
			_ = client.ForwardKeys(false) // the game is all here
			draw()
		},
		OnResize: func(e client.Event) { width, height = e.Width, e.Height; draw() },
		OnKey: func(e client.Event) {
			if e.Key != " " && e.Key != "enter" {
				return
			}
			switch phase {
			case "idle", "result", "early":
				phase = "waiting"
				_ = client.After("go", time.Duration(1000+rand.Intn(2500))*time.Millisecond)
				_ = client.After("spin", 80*time.Millisecond)
			case "waiting":
				phase = "early"
			case "go":
				last = int(time.Since(goAt).Milliseconds())
				phase = "result"
				if best == 0 || last < best {
					best = last
					_ = client.Set("best", strconv.Itoa(best))
				}
				_ = client.Send(map[string]int{"ms": last})
			}
			draw()
		},
		OnTimer: func(id string) {
			switch {
			case id == "go" && phase == "waiting":
				phase, goAt = "go", time.Now()
			case id == "spin" && phase == "waiting":
				spin++
				_ = client.After("spin", 80*time.Millisecond)
			}
			draw()
		},
		OnServer: func(data json.RawMessage) {
			var m struct {
				Top []entry `json:"top"`
			}
			if json.Unmarshal(data, &m) == nil {
				board = m.Top
				draw()
			}
		},
	})
}

func draw() {
	var lines []string
	lines = append(lines, purple+"Reflex"+reset, "")
	switch phase {
	case "idle":
		lines = append(lines, "Press Space to start, then press it again", "the moment the screen says "+green+"NOW"+reset+".")
	case "waiting":
		dots := strings.Repeat("·", spin%12)
		lines = append(lines, red+"Wait for it…"+reset, dim+dots+reset)
	case "go":
		lines = append(lines, green+"███  NOW!  ███"+reset, "")
	case "result":
		lines = append(lines, fmt.Sprintf("%s%d ms%s", green, last, reset), "Space to go again.")
	case "early":
		lines = append(lines, red+"Too soon!"+reset, "Space to try again.")
	}
	lines = append(lines, "")
	if best > 0 {
		lines = append(lines, fmt.Sprintf("%sYour best on this computer: %d ms%s", dim, best, reset))
	}
	if len(board) > 0 {
		lines = append(lines, "", purple+"Leaderboard"+reset)
		for i, e := range board {
			lines = append(lines, fmt.Sprintf("%d. %-16s %4d ms", i+1, e.Name, e.MS))
		}
	}
	for len(lines) > height && height > 0 {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		if r := []rune(l); width > 0 && len(r) > width && !strings.Contains(l, "\x1b") {
			lines[i] = string(r[:width])
		}
	}
	_ = client.Frame(strings.Join(lines, "\n"))
}
