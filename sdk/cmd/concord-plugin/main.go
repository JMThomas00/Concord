// Command concord-plugin creates new Concord plugin projects.
//
//	go install github.com/JMThomas00/Concord/sdk/cmd/concord-plugin@latest
//	concord-plugin new concord-chess --template game
//
// The generated project builds one program that runs standalone in a
// terminal and as a Concord plugin, with release packaging that Concord's
// installer understands (an admin types "you/concord-chess" in Server
// Settings > Plugins > Install).
package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"unicode"
)

//go:embed all:templates
var templates embed.FS

// Templates, and what each is for.
var kinds = map[string]string{
	"game": "a turn-based game on the table kit: seats, spectators, lobby, computer opponent",
	"pane": "your own full-screen app in a channel (a Bubble Tea model per viewer)",
	"bot":  "a chat bot: reads messages in its channel or when @mentioned, replies",
	"pty":  "an existing terminal program shown in a channel (Linux/macOS servers)",
}

// Project is what templates are rendered with.
type Project struct {
	ID          string // plugin id and binary name: "concord-chess"
	Name        string // display name: "Concord Chess"
	GoName      string // identifier-safe: "concordchess"
	Module      string // Go module path
	Kind        string
	SDKVersion  string
	PTYVersion  string // sdk/pty is versioned separately
	SDKReplace  string // local SDK checkout, for developing before a release
	ChannelKind string // the manifest's channel kind
}

// Repo is what admins type to install it: the module path without
// "github.com/" (Concord resolves owner/repo to its latest release).
func (p Project) Repo() string { return strings.TrimPrefix(p.Module, "github.com/") }

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func main() {
	if len(os.Args) >= 2 && (os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help") {
		usage()
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "keygen" {
		keygen(os.Args[2:])
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "sign" {
		sign(os.Args[2:])
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "new" {
		usage()
		os.Exit(2)
	}
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	kind := fs.String("template", "pane", "game | pane | bot | pty")
	module := fs.String("module", "", "Go module path (default github.com/you/<name>)")
	name := fs.String("name", "", "display name (default from <name>)")
	sdk := fs.String("sdk", "", "path to a local Concord sdk/ checkout to build against (adds a replace)")
	sdkVersion := fs.String("sdk-version", "v0.11.0", "SDK version to require")
	ptyVersion := fs.String("pty-version", "v0.1.2", "sdk/pty version to require (pty template)")
	_ = fs.Parse(reorder(os.Args[2:]))
	if fs.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	id := fs.Arg(0)
	if !idPattern.MatchString(id) {
		fail("the plugin name must be lowercase letters, digits and dashes, starting with a letter (e.g. concord-chess)")
	}
	if _, ok := kinds[*kind]; !ok {
		fail("unknown template %q", *kind)
	}
	p := Project{
		ID: id, Name: *name, Kind: *kind, Module: *module,
		GoName: strings.ReplaceAll(id, "-", ""), SDKVersion: *sdkVersion, PTYVersion: *ptyVersion,
	}
	if p.Name == "" {
		p.Name = titleCase(id)
	}
	if p.Module == "" {
		p.Module = "github.com/you/" + id
	}
	p.ChannelKind = map[string]string{"game": "table", "pane": "pane", "bot": "chat", "pty": "terminal"}[p.Kind]
	if *sdk != "" {
		abs, err := filepath.Abs(*sdk)
		if err != nil {
			fail("%v", err)
		}
		p.SDKReplace = filepath.ToSlash(abs)
		p.SDKVersion, p.PTYVersion = "v0.0.0", "v0.0.0"
	}
	if _, err := os.Stat(id); err == nil {
		fail("%s already exists", id)
	}
	if err := render(p); err != nil {
		fail("%v", err)
	}
	fmt.Printf(`Created %s/ (%s).

Next:
  cd %s
  go mod tidy
  go run .              # try it in this terminal
  go run release.go     # zips for every server OS/CPU in dist/

Publish: push to GitHub and tag a release (git tag v0.1.0 && git push --tags);
the included workflow attaches the zips. Server admins then install it from
Server Settings > Plugins > I with just "%s".
`, id, kinds[p.Kind], id, p.Repo())
}

// render writes templates/<kind>/** (and templates/common/**) into ./<id>.
func render(p Project) error {
	for _, root := range []string{"templates/common", "templates/" + p.Kind} {
		err := fs.WalkDir(templates, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel := strings.TrimPrefix(path, root+"/")
			rel = strings.TrimSuffix(rel, ".tmpl")
			rel = strings.ReplaceAll(rel, "_dot_", ".")
			src, err := templates.ReadFile(path)
			if err != nil {
				return err
			}
			t, err := template.New(rel).Delims("<%", "%>").Parse(string(src))
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			var out bytes.Buffer
			if err := t.Execute(&out, p); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			dest := filepath.Join(p.ID, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dest, out.Bytes(), 0o644)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// reorder moves flags before the positional name, so both
// "new chess --template game" and "new --template game chess" work.
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

func titleCase(id string) string {
	parts := strings.Split(id, "-")
	for i, s := range parts {
		r := []rune(s)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: concord-plugin new <name> [--template game|pane|bot|pty] [--module path] [--name \"Display Name\"] [--sdk path/to/Concord/sdk]")
	fmt.Fprintln(os.Stderr, "       concord-plugin keygen [--out publisher.key]      make a key to sign client code")
	fmt.Fprintln(os.Stderr, "       concord-plugin sign [--key publisher.key] client/plugin.wasm")
	fmt.Fprintln(os.Stderr, "\ntemplates:")
	for _, k := range []string{"game", "pane", "bot", "pty"} {
		fmt.Fprintf(os.Stderr, "  %-5s %s\n", k, kinds[k])
	}
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "concord-plugin: "+format+"\n", args...)
	os.Exit(1)
}
