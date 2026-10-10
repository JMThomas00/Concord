package server

import (
	"bytes"
	"html/template"
	"strings"
)

// Concord's emails look like Concord: a terminal window in the Dracula
// theme, the same look as the concord-site terminal layout (title bar with
// the app chip, a bordered pane, a shell prompt, purple key chips). Email
// clients ignore most CSS, so it's tables and inline styles only; every
// message also has a plain-text part.

// mailBanner is the "Slant" Concord banner (pure ASCII, so any monospace
// font in any mail client draws it).
const mailBanner = `   ______                                __
  / ____/___  ____  _________  _________/ /
 / /   / __ \/ __ \/ ___/ __ \/ ___/ __  /
/ /___/ /_/ / / / / /__/ /_/ / /  / /_/ /
\____/\____/_/ /_/\___/\____/_/   \__,_/`

// Dracula, as on concord-site's terminal layout.
const (
	mailBG     = "#191A21"
	mailPane   = "#282A36"
	mailSel    = "#44475A"
	mailBorder = "#6272A4"
	mailFG     = "#F8F8F2"
	mailDim    = "#C9C9C4" // fg at ~82% over the pane
	mailPurple = "#BD93F9"
	mailGreen  = "#50FA7B"
	mailCyan   = "#8BE9FD"
	mailMono   = `'JetBrains Mono',ui-monospace,SFMono-Regular,Menlo,Consolas,'Liberation Mono',monospace`
)

// mailView is what the template draws.
type mailView struct {
	Title   string // window title, e.g. "Sequoia"
	Section string // pane label, e.g. "#verify"
	Prompt  string // e.g. "gh0st@sequoia"
	Command string // e.g. "concord verify"
	Lines   []string
	Code    string        // the code, or "" for none
	Facts   [][2]string   // key/value rows under the code
	OK      string        // a green "[ OK ]" line, or ""
	Note    string        // the closing grey line
	Banner  template.HTML // the ASCII banner, escaped
}

var mailTemplate = template.Must(template.New("mail").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark"><meta name="supported-color-schemes" content="dark">
<title>{{.Title}}</title></head>
<body style="margin:0;padding:0;background:` + mailBG + `;" bgcolor="` + mailBG + `">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + mailBG + `" style="background:` + mailBG + `;">
<tr><td align="center" style="padding:28px 12px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:600px;border:1px solid ` + mailBorder + `;border-radius:8px;overflow:hidden;background:` + mailPane + `;" bgcolor="` + mailPane + `">
<!-- title bar -->
<tr><td bgcolor="` + mailSel + `" style="background:` + mailSel + `;padding:0;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td bgcolor="` + mailPurple + `" style="background:` + mailPurple + `;color:` + mailPane + `;font:700 13px/30px ` + mailMono + `;padding:0 14px;white-space:nowrap;" width="1">concord</td>
<td style="color:` + mailFG + `;font:400 13px/30px ` + mailMono + `;padding:0 12px;white-space:nowrap;">{{.Title}}</td>
<td align="right" style="color:` + mailGreen + `;font:400 13px/30px ` + mailMono + `;padding:0 14px;white-space:nowrap;">&#9679; connected</td>
</tr></table></td></tr>
<!-- banner -->
<tr><td style="padding:22px 22px 6px;">
<pre style="margin:0;color:` + mailPurple + `;font:400 12px/1.2 ` + mailMono + `;white-space:pre;">{{.Banner}}</pre>
</td></tr>
<!-- pane -->
<tr><td style="padding:14px 22px 22px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border:1px solid ` + mailPurple + `;border-radius:7px;">
<tr><td style="padding:10px 16px 0;color:` + mailPurple + `;font:700 12.5px/1.5 ` + mailMono + `;">{{.Section}}</td></tr>
<tr><td style="padding:10px 16px 18px;font:400 14px/1.6 ` + mailMono + `;color:` + mailFG + `;">
<div style="margin:0 0 12px;"><span style="color:` + mailGreen + `;">{{.Prompt}}:~$</span> {{.Command}}</div>
{{range .Lines}}<div style="margin:0 0 6px;color:` + mailDim + `;">{{.}}</div>
{{end}}{{if .OK}}<div style="margin:10px 0 0;"><span style="color:` + mailGreen + `;">[ OK ]</span> <span style="color:` + mailDim + `;">{{.OK}}</span></div>{{end}}
{{if .Code}}<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:18px 0 6px;"><tr>
<td style="color:` + mailCyan + `;font:400 14px/1.6 ` + mailMono + `;padding:0 18px 0 0;vertical-align:middle;">code</td>
<td bgcolor="` + mailPurple + `" style="background:` + mailPurple + `;color:` + mailPane + `;font:700 30px/1 ` + mailMono + `;letter-spacing:8px;padding:12px 10px 12px 18px;border-radius:4px;">{{.Code}}</td>
</tr></table>{{end}}
{{if .Facts}}<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:12px 0 0;">
{{range .Facts}}<tr><td style="color:` + mailCyan + `;font:400 13px/1.7 ` + mailMono + `;padding:0 22px 0 0;white-space:nowrap;vertical-align:top;">{{index . 0}}</td><td style="color:` + mailFG + `;font:400 13px/1.7 ` + mailMono + `;">{{index . 1}}</td></tr>
{{end}}</table>{{end}}
<div style="margin:18px 0 0;color:` + mailBorder + `;">{{.Note}} <span style="background:` + mailFG + `;color:` + mailFG + `;">&nbsp;</span></div>
</td></tr></table>
</td></tr>
<!-- status line -->
<tr><td bgcolor="` + mailSel + `" style="background:` + mailSel + `;color:` + mailBorder + `;font:400 12px/28px ` + mailMono + `;padding:0 14px;">
<span style="color:` + mailFG + `;">Concord</span> &middot; chat that lives in your terminal &middot; <a href="https://github.com/JMThomas00/Concord" style="color:` + mailCyan + `;text-decoration:underline;">github.com/JMThomas00/Concord</a>
</td></tr>
</table>
</td></tr></table>
</body></html>`))

// renderMailHTML draws v as an HTML email.
func renderMailHTML(v mailView) string {
	v.Banner = template.HTML(template.HTMLEscapeString(mailBanner))
	var b bytes.Buffer
	if err := mailTemplate.Execute(&b, v); err != nil {
		return ""
	}
	return b.String()
}

// promptHost turns a server name into a shell-prompt host ("Sequoia" →
// "sequoia", "My Server!" → "my-server").
func promptHost(serverName string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(serverName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	host := strings.Trim(b.String(), "-")
	if host == "" {
		return "concord"
	}
	return host
}

// codeEmail is a verification, reset or email-change code email (without
// its recipient).
func codeEmail(serverName, username, purpose, code string) Email {
	spaced := code[:3] + " " + code[3:]
	v := mailView{
		Title:  serverName,
		Prompt: username + "@" + promptHost(serverName),
		Code:   code,
		Facts:  [][2]string{{"server", serverName}, {"expires", "in 15 minutes"}},
	}
	var subject, text string
	switch purpose {
	case purposeReset:
		subject = "Your " + serverName + " password reset code: " + spaced
		text = "Hi " + username + ",\n\nSomeone asked to reset your password on the Concord server \"" + serverName + "\".\n\n" +
			"Your code is:\n\n    " + spaced + "\n\nEnter it in Concord within 15 minutes. If this wasn't you, ignore this email; your password hasn't changed.\n"
		v.Section, v.Command = "#password-reset", "concord passwd --reset"
		v.Lines = []string{"Hi " + username + ",", "Someone asked to reset your password on " + serverName + ".", "Enter this code in Concord with your new password:"}
		v.Note = "Wasn't you? Ignore this email. Your password hasn't changed."
	case purposeChangeEmail:
		subject = "Confirm your new email for " + serverName + ": " + spaced
		text = "Hi " + username + ",\n\nTo use this address for your account on the Concord server \"" + serverName + "\", enter this code in Concord within 15 minutes:\n\n    " + spaced + "\n\nIf this wasn't you, ignore this email.\n"
		v.Section, v.Command = "#confirm-email", "concord email --confirm"
		v.Lines = []string{"Hi " + username + ",", "To use this address for your account on " + serverName + ", enter this code in Concord:"}
		v.Note = "Wasn't you? Ignore this email. Nothing changes."
	default:
		subject = "Your " + serverName + " verification code: " + spaced
		text = "Welcome to " + serverName + ", " + username + "!\n\nYour verification code is:\n\n    " + spaced + "\n\nEnter it in Concord within 15 minutes to finish creating your account. If you didn't sign up, ignore this email.\n"
		v.Section, v.Command = "#verify", "concord verify"
		v.Lines = []string{"Welcome to " + serverName + ", " + username + "!", "Enter this code in Concord to finish creating your account:"}
		v.Note = "Didn't sign up? Ignore this email. Nothing happens."
	}
	return Email{Subject: subject, Text: text, HTML: renderMailHTML(v)}
}

// testEmail is the concord-server --test-mail message.
func testEmail(serverName, to string) Email {
	text := "This is a test from your Concord server \"" + serverName + "\".\n\nIf you're reading it, verification and password reset emails will arrive too.\n"
	return Email{
		To:      to,
		Subject: "Concord test email from " + serverName,
		Text:    text,
		HTML: renderMailHTML(mailView{
			Title:   serverName,
			Section: "#mail-test",
			Prompt:  "admin@" + promptHost(serverName),
			Command: "concord-server --test-mail",
			Lines:   []string{"This is a test from your Concord server " + serverName + "."},
			OK:      "mail delivered: verification and password reset codes will arrive too",
			Note:    "You can delete this email.",
		}),
	}
}
