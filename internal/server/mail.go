package server

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net"
	"mime/quotedprintable"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// MailConfig is the [mail] section of concord-server.toml: how this server
// sends email (account verification and password reset codes). A server
// without it works as before: no verification, and no emailed resets.
type MailConfig struct {
	SMTPHost     string `toml:"smtp_host"`
	SMTPPort     int    `toml:"smtp_port"`     // 587 (STARTTLS) by default; 465 for implicit TLS
	SMTPUsername string `toml:"smtp_username"` // often the from-address; empty for no login
	SMTPPassword string `toml:"smtp_password"` // an app password for Gmail/Outlook
	From         string `toml:"from"`          // "Concord <chat@example.com>" or a bare address
	// Security is "starttls" (the default), "tls" (implicit, port 465) or
	// "none" (only for a relay on this machine or a trusted LAN).
	Security string `toml:"security"`
	// RequireVerification makes new accounts confirm their email with a
	// code before they can sign in. Unset means on whenever mail works.
	RequireVerification *bool `toml:"require_verification,omitempty"`
}

// Enabled reports whether the server can send mail.
func (m MailConfig) Enabled() bool {
	return strings.TrimSpace(m.SMTPHost) != "" && strings.TrimSpace(m.From) != ""
}

// VerificationRequired reports whether new accounts must verify their email.
func (m MailConfig) VerificationRequired() bool {
	return m.Enabled() && (m.RequireVerification == nil || *m.RequireVerification)
}

func (m MailConfig) port() int {
	if m.SMTPPort != 0 {
		return m.SMTPPort
	}
	if strings.EqualFold(m.Security, "tls") {
		return 465
	}
	return 587
}

// Email is one message: plain text, and optionally an HTML version.
type Email struct {
	To      string
	Subject string
	Text    string
	HTML    string // "" for a plain-text-only message
}

// Mailer sends one email.
type Mailer interface {
	Send(e Email) error
}

// smtpMailer sends through the configured SMTP server.
type smtpMailer struct {
	cfg MailConfig
}

const smtpTimeout = 20 * time.Second

func (s *smtpMailer) Send(e Email) error {
	cfg := s.cfg
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("mail: bad from address %q: %w", cfg.From, err)
	}
	rcpt, err := mail.ParseAddress(e.To)
	if err != nil {
		return fmt.Errorf("mail: bad recipient %q: %w", e.To, err)
	}
	host := strings.TrimSpace(cfg.SMTPHost)
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.port()))
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

	dialer := &net.Dialer{Timeout: smtpTimeout}
	var conn net.Conn
	security := strings.ToLower(strings.TrimSpace(cfg.Security))
	if security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: connect to %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * smtpTimeout))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: %w", err)
	}
	defer c.Close()

	if security == "" || security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("mail: %s doesn't offer STARTTLS (set security = \"tls\" for port 465, or \"none\" for a local relay)", addr)
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("mail: STARTTLS: %w", err)
		}
	}
	if cfg.SMTPUsername != "" {
		// PlainAuth refuses to send the password over an unencrypted
		// connection to anything but localhost.
		if err := c.Auth(smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, host)); err != nil {
			return fmt.Errorf("mail: sign in to %s: %w", host, err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if _, err := w.Write(buildMessage(from, rcpt, e)); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	return c.Quit()
}

// buildMessage assembles an RFC 5322 message: plain text, or
// multipart/alternative with an HTML version. Both parts are
// quoted-printable, which keeps the HTML's long lines within SMTP's limit.
func buildMessage(from, to *mail.Address, e Email) []byte {
	domain := "concord.local"
	if at := strings.LastIndex(from.Address, "@"); at >= 0 {
		domain = from.Address[at+1:]
	}
	var id [12]byte
	_, _ = rand.Read(id[:])
	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to.String() + "\r\n")
	b.WriteString("Subject: " + encodeHeader(e.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString(fmt.Sprintf("Message-ID: <%x@%s>\r\n", id, domain))
	b.WriteString("MIME-Version: 1.0\r\n")
	if e.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(qp(e.Text))
		return []byte(b.String())
	}
	boundary := fmt.Sprintf("concord-%x", id)
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	for _, part := range []struct{ kind, body string }{{"text/plain", e.Text}, {"text/html", e.HTML}} {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + part.kind + "; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(qp(part.body))
		b.WriteString("\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// qp quoted-printable encodes s with CRLF line endings.
func qp(s string) string {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")))
	_ = w.Close()
	return b.String()
}

// encodeHeader encodes a header value that isn't plain ASCII.
func encodeHeader(s string) string {
	for _, r := range s {
		if r > 126 {
			return "=?utf-8?q?" + qEncode(s) + "?="
		}
	}
	return s
}

func qEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c == ' ':
			b.WriteByte('_')
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "=%02X", c)
		}
	}
	return b.String()
}

// SendTestMail sends a test email with cfg (concord-server --test-mail).
func SendTestMail(cfg MailConfig, serverName, to string) error {
	if !cfg.Enabled() {
		return fmt.Errorf("no [mail] settings: set smtp_host and from in concord-server.toml, or run --reconfigure")
	}
	m := &smtpMailer{cfg: cfg}
	return m.Send(testEmail(serverName, to))
}
