package server

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

// fakeSMTP accepts one message (no TLS, no login) and returns its DATA.
func fakeSMTP(t *testing.T, starttls bool) (port int, data <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				if starttls {
					say("250-fake")
					say("250 STARTTLS")
				} else {
					say("250 fake")
				}
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				say("250 OK")
			case cmd == "DATA":
				say("354 go ahead")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				out <- b.String()
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("502 no")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, out
}

func TestSMTPMailerSendsAMessage(t *testing.T) {
	port, data := fakeSMTP(t, false)
	m := &smtpMailer{cfg: MailConfig{SMTPHost: "127.0.0.1", SMTPPort: port, From: "Concord <chat@example.com>", Security: "none"}}
	if err := m.Send(codeEmailTo("amy@example.com")); err != nil {
		t.Fatal(err)
	}
	msg := <-data
	for _, want := range []string{"From: \"Concord\" <chat@example.com>", "To: <amy@example.com>", "Subject: Your Home verification code: ABC 234", "    ABC 234", "text/html"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message lacks %q:\n%s", want, msg)
		}
	}
}

func codeEmailTo(to string) Email {
	e := codeEmail("Home", "amy", purposeVerify, "ABC234")
	e.To = to
	return e
}

// The default security setting refuses to send in the clear.
func TestSMTPMailerRequiresSTARTTLSByDefault(t *testing.T) {
	port, _ := fakeSMTP(t, false)
	m := &smtpMailer{cfg: MailConfig{SMTPHost: "127.0.0.1", SMTPPort: port, From: "chat@example.com"}}
	err := m.Send(Email{To: "amy@example.com", Subject: "s", Text: "b"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v", err)
	}
}
