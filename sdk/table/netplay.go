package table

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Standalone network play: one player hosts (listens on a port and shows
// a join code), the other joins with the host's address and the code.
// Messages are JSON lines. Both sides validate every move against the
// rules, so a mismatch disconnects instead of letting the games diverge.

// DefaultPort is where a hosted game listens unless told otherwise.
const DefaultPort = 7412

type netMsg struct {
	Type  string            `json:"type"` // hello, welcome, reject, move, resign, rematch, bye
	Game  string            `json:"game,omitempty"`
	Code  string            `json:"code,omitempty"`
	Name  string            `json:"name,omitempty"`
	Move  string            `json:"move,omitempty"`
	Index int               `json:"index,omitempty"` // which move this is (0-based), to catch drift
	Opts  map[string]string `json:"options,omitempty"`
	Error string            `json:"error,omitempty"`
}

// netPeer is the connection to the other player.
type netPeer struct {
	conn  net.Conn
	r     *bufio.Reader // the only reader of conn, so no buffered bytes are lost
	enc   *json.Encoder
	inbox chan tea.Msg
}

// Messages the network brings into the local program.
type (
	netListeningMsg struct {
		ln    net.Listener
		addrs []string
		code  string
	}
	netConnectedMsg struct {
		peer     *netPeer
		opponent string
		mySeat   int
		options  map[string]string
	}
	netRemoteMsg netMsg
	netErrMsg    struct{ err error }
)

func (p *netPeer) send(m netMsg) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.enc.Encode(m)
}

// read delivers the peer's messages to the program until the connection
// ends.
func (p *netPeer) readLoop() {
	for {
		line, err := readLine(p.r)
		if err != nil {
			break
		}
		var m netMsg
		if json.Unmarshal(line, &m) == nil {
			p.inbox <- netRemoteMsg(m)
		}
	}
	p.inbox <- netErrMsg{fmt.Errorf("the other player disconnected")}
	close(p.inbox)
}

// next waits for the peer's next message.
func (p *netPeer) next() tea.Cmd {
	return func() tea.Msg {
		m, ok := <-p.inbox
		if !ok {
			return nil
		}
		return m
	}
}

// joinCode is six easy-to-read characters.
func joinCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

// lanAddresses lists this machine's non-loopback IPv4 addresses.
func lanAddresses() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ipn.IP.String())
			}
		}
	}
	if len(out) == 0 {
		out = []string{"127.0.0.1"}
	}
	return out
}

// hostCmd starts listening and reports the address and code.
func hostCmd(port int) tea.Cmd {
	return func() tea.Msg {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			ln, err = net.Listen("tcp", ":0") // port busy: any free one
		}
		if err != nil {
			return netErrMsg{err}
		}
		return netListeningMsg{ln: ln, addrs: lanAddresses(), code: joinCode()}
	}
}

// acceptCmd waits for a player with the right code. The host sits in seat
// 0 and moves first.
func acceptCmd(ln net.Listener, code, game, myName string, options map[string]string) tea.Cmd {
	return func() tea.Msg {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return netErrMsg{err}
			}
			_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
			r := bufio.NewReader(conn)
			var hello netMsg
			line, err := readLine(r)
			if err != nil || json.Unmarshal(line, &hello) != nil || hello.Type != "hello" {
				conn.Close()
				continue
			}
			enc := json.NewEncoder(conn)
			if !strings.EqualFold(hello.Code, code) || hello.Game != game {
				why := "wrong join code"
				if hello.Game != game {
					why = "that's a game of " + game + ", not " + hello.Game
				}
				_ = enc.Encode(netMsg{Type: "reject", Error: why})
				conn.Close()
				continue
			}
			_ = conn.SetReadDeadline(time.Time{})
			ln.Close()
			peer := &netPeer{conn: conn, r: r, enc: enc, inbox: make(chan tea.Msg, 16)}
			if err := peer.send(netMsg{Type: "welcome", Name: myName, Opts: options}); err != nil {
				return netErrMsg{err}
			}
			go peer.readLoop()
			return netConnectedMsg{peer: peer, opponent: hello.Name, mySeat: 0, options: options}
		}
	}
}

// joinCmd connects to "host[:port] CODE".
func joinCmd(target, game, myName string) tea.Cmd {
	return func() tea.Msg {
		fields := strings.Fields(target)
		if len(fields) != 2 {
			return netErrMsg{fmt.Errorf("type the host's address and the code, e.g. 192.168.1.20 K7Q2XM")}
		}
		addr, code := fields[0], fields[1]
		if !strings.Contains(addr, ":") {
			addr = fmt.Sprintf("%s:%d", addr, DefaultPort)
		}
		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			return netErrMsg{fmt.Errorf("couldn't reach %s: %w", addr, err)}
		}
		enc := json.NewEncoder(conn)
		if err := enc.Encode(netMsg{Type: "hello", Game: game, Code: code, Name: myName}); err != nil {
			conn.Close()
			return netErrMsg{err}
		}
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		r := bufio.NewReader(conn)
		line, err := readLine(r)
		if err != nil {
			conn.Close()
			return netErrMsg{fmt.Errorf("the host didn't answer")}
		}
		var reply netMsg
		if err := json.Unmarshal(line, &reply); err != nil || reply.Type != "welcome" {
			conn.Close()
			if reply.Error != "" {
				return netErrMsg{fmt.Errorf("%s", reply.Error)}
			}
			return netErrMsg{fmt.Errorf("the host refused")}
		}
		_ = conn.SetReadDeadline(time.Time{})
		peer := &netPeer{conn: conn, r: r, enc: enc, inbox: make(chan tea.Msg, 16)}
		go peer.readLoop()
		return netConnectedMsg{peer: peer, opponent: reply.Name, mySeat: 1, options: reply.Opts}
	}
}

// readLine reads one JSON line (at most 64KB).
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > 64*1024 {
			return nil, fmt.Errorf("message too long")
		}
		if !isPrefix {
			return line, nil
		}
	}
}
