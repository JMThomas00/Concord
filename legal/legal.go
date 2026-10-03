// Package legal embeds Concord's terms, so a program can show them without
// the legal folder beside it (the installer, a release binary).
package legal

import _ "embed"

// ServerTerms is "Server Terms.md": what someone agrees to by running a
// Concord server.
//
//go:embed "Server Terms.md"
var ServerTerms string

// ClientTerms is "Client Terms.md".
//
//go:embed "Client Terms.md"
var ClientTerms string
