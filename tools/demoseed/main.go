// Command demoseed fills a fresh Concord server with a believable community
// (members, channel groups, channels and a conversation) for screenshots
// and the README's hero shot. Every message is sent by its own member over
// a normal connection, so it all looks exactly like real use.
//
//	go run ./tools/demoseed -server http://192.168.1.66:8090
//
// The first member registered becomes the server's owner, so run it on a
// server nobody has signed up to yet. Running it again adds nothing new
// except the messages; members sign in instead of registering.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// cast is the community, the owner first. Everyone shares one password
// (they're demo accounts on a demo server).
var cast = []string{"merlot", "juniper", "pixel", "sage", "vex", "nova", "bramble"}

const password = "grapes-are-great"

// layout is the server's channel groups and channels, in order.
var layout = []struct {
	group    string
	channels []string
	voice    []string
}{
	{"Welcome", []string{"announcements", "general"}, nil},
	{"The Vineyard", []string{"show-and-tell", "terminal-setups", "help"}, nil},
	{"Hangout", []string{"games", "music"}, []string{"Lounge", "Late Night"}},
}

// script is the conversation: who says what, where.
var script = []struct{ who, where, text string }{
	{"merlot", "announcements", "**Welcome to The Vineyard!** 🍇\nGrab a seat in #general, show off your setup in #terminal-setups, and come say hi in **Lounge** for voice."},
	{"merlot", "general", "morning, vineyard ☀️ who's around?"},
	{"juniper", "general", "here! just switched to the `tokyo-night` theme and I can't go back"},
	{"pixel", "general", "tokyo-night is elite. I'm on `catppuccin-mocha` this week"},
	{"sage", "general", "you can preview every theme live with **Ctrl+T** btw, it's dangerous for productivity"},
	{"vex", "general", "anyone up for chess later? the new board looks *so* good"},
	{"nova", "general", "@vex always. loser buys the next round of grape juice 🧃"},
	{"bramble", "general", "I keep finding new loading screens. got the **dial-up** one this morning and actually laughed"},
	{"juniper", "general", "wait there are loading screens??"},
	{"bramble", "general", "check **Settings > About**, there's a whole collection to fill in"},
	{"pixel", "general", "the grapes follow your mouse on the login screen too 👀"},
	{"merlot", "general", "reminder: voice in **Lounge** tonight at 8, bring snacks 🎧"},
	{"sage", "general", "ok who left the `/party` command where I could find it 🎉"},
	{"vex", "terminal-setups", "my setup: kitty + JetBrains Mono + catppuccin. Concord in a split next to nvim"},
	{"juniper", "terminal-setups", "Windows Terminal gang 🙋 the Concord profile the installer adds is a nice touch"},
	{"pixel", "show-and-tell", "built a little plugin that posts the weather every morning:\n```go\nconn.SendMessage(channel, forecast.Today())\n```\nthe SDK made it ~40 lines"},
	{"sage", "help", "how do I add a server again?"},
	{"merlot", "help", "**Ctrl+B** or the **+** in the server column, then its address. or browse the Grapevine with **Ctrl+G** 🍇"},
}

func main() {
	server := flag.String("server", "http://localhost:8080", "the server's address")
	flag.Parse()
	base := strings.TrimRight(*server, "/")
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/ws"

	conns := map[string]*conn{}
	for _, name := range cast {
		token, err := signIn(base, name)
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		c, err := dial(wsURL, token)
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		conns[name] = c
		log.Printf("%s is in", name)
	}
	owner := conns[cast[0]]
	srv := owner.waitServer()

	// Channel groups and channels, reusing any that exist.
	ids := map[string]uuid.UUID{}
	for i, g := range layout {
		gid := owner.channel(srv, g.group, models.ChannelTypeCategory, nil, i)
		for j, name := range g.channels {
			ids[name] = owner.channel(srv, name, models.ChannelTypeText, &gid, j)
		}
		for j, name := range g.voice {
			owner.channel(srv, name, models.ChannelTypeVoice, &gid, len(g.channels)+j)
		}
	}

	for _, line := range script {
		ch, ok := ids[line.where]
		if !ok {
			log.Fatalf("no channel %s", line.where)
		}
		conns[line.who].send(protocol.OpSendMessage, protocol.SendMessagePayload{ChannelID: ch, Content: line.text, Nonce: uuid.NewString()})
		time.Sleep(450 * time.Millisecond)
	}
	time.Sleep(time.Second)
	log.Printf("done: %d members, %d messages. Everyone's password: %s", len(cast), len(script), password)
}

// signIn registers a member, or signs them in if they already exist.
func signIn(base, name string) (string, error) {
	body := map[string]string{"username": name, "email": name + "@vineyard.example", "password": password}
	if tok, err := post(base+"/api/register", body); err == nil {
		return tok, nil
	}
	return post(base+"/api/login", map[string]string{"email": body["email"], "password": password})
}

func post(url string, body map[string]string) (string, error) {
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated || out.Token == "" {
		return "", fmt.Errorf("%s: %s %s", url, resp.Status, out.Error)
	}
	return out.Token, nil
}

// conn is one member's connection.
type conn struct {
	ws       *websocket.Conn
	mu       sync.Mutex
	servers  chan *serverInfo
	channels chan *models.Channel
}

func dial(url, token string) (*conn, error) {
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	c := &conn{ws: ws, servers: make(chan *serverInfo, 8), channels: make(chan *models.Channel, 64)}
	go c.read()
	c.send(protocol.OpIdentify, protocol.IdentifyPayload{Token: token, Properties: protocol.ConnectionProperties{OS: "demo", Browser: "demoseed"}})
	return c, nil
}

func (c *conn) send(op protocol.OpCode, data any) {
	m, err := protocol.NewMessage(op, data)
	if err != nil {
		log.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ws.WriteJSON(m); err != nil {
		log.Fatal(err)
	}
}

// read keeps the connection alive and picks out what the seeding needs.
func (c *conn) read() {
	for {
		var m protocol.Message
		if err := c.ws.ReadJSON(&m); err != nil {
			return
		}
		switch {
		case m.Op == protocol.OpHello:
			go func() {
				for range time.Tick(30 * time.Second) {
					c.send(protocol.OpHeartbeat, nil)
				}
			}()
		case m.Type == "SERVER_CREATE":
			var s serverInfo
			if json.Unmarshal(m.Data, &s) == nil {
				c.servers <- &s
			}
		case m.Type == protocol.EventChannelCreate:
			var ch models.Channel
			if json.Unmarshal(m.Data, &ch) == nil {
				select {
				case c.channels <- &ch:
				default:
				}
			}
		}
	}
}

func (c *conn) waitServer() *serverInfo {
	select {
	case s := <-c.servers:
		return s
	case <-time.After(10 * time.Second):
		log.Fatal("the server never sent its details")
		return nil
	}
}

// channel finds a channel by name and type, or creates it.
func (c *conn) channel(srv *serverInfo, name string, typ models.ChannelType, parent *uuid.UUID, pos int) uuid.UUID {
	for _, ch := range srv.Channels {
		if strings.EqualFold(ch.Name, name) && ch.Type == typ {
			return ch.ID
		}
	}
	c.send(protocol.OpChannelCreate, protocol.ChannelCreateRequest{ServerID: srv.ID, Name: name, Type: typ, CategoryID: parent, Position: pos})
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ch := <-c.channels:
			if strings.EqualFold(ch.Name, name) {
				srv.Channels = append(srv.Channels, ch)
				return ch.ID
			}
		case <-timeout:
			log.Fatalf("channel %s was never created", name)
		}
	}
}

// serverInfo is the part of SERVER_CREATE the seeding uses.
type serverInfo struct {
	ID       uuid.UUID         `json:"id"`
	Channels []*models.Channel `json:"channels"`
}
