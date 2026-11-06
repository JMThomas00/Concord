package server

import (
	"bytes"
	"net/mail"
	"testing"
)

// Finding 8 (security audit 2026-11-05): a subject with a line break in it
// (from a member's name in an invite) wrote extra headers, such as Bcc.
func TestSubjectsCantAddHeaders(t *testing.T) {
	from := &mail.Address{Name: "Concord", Address: "chat@example.com"}
	to := &mail.Address{Address: "amy@example.com"}
	for _, subject := range []string{
		"eve\r\nBcc: victim@example.net invited you",
		"eve\nBcc: victim@example.net",
		"évé\r\nBcc: victim@example.net",
	} {
		raw := buildMessage(from, to, Email{Subject: subject, Text: "hi"})
		msg, err := mail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if bcc := msg.Header.Get("Bcc"); bcc != "" {
			t.Errorf("%q added a Bcc header: %q", subject, bcc)
		}
		if n := len(msg.Header["Subject"]); n != 1 {
			t.Errorf("%q: %d subject headers", subject, n)
		}
	}
}
