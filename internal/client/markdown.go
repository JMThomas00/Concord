package client

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/concord-chat/concord/internal/themes"
)

// buildThemedGlamourStyle derives a glamour ansi.StyleConfig from the
// active Concord theme, so markdown rendering (Settings > Help & Guide,
// and eventually chat messages) matches whatever theme the user has
// selected instead of always using one of glamour's own bundled palettes
// (e.g. "dark") regardless of theme. Shared across every markdown
// consumer so the mapping is built once, not reinvented per feature.
//
// theme.Colors fields can be empty strings or bare ANSI palette indices
// ("0"-"15") for the terminal-default theme (see CLAUDE.md's Themes
// section) -- themeColor below turns an empty string into a nil *string
// so glamour leaves that element unstyled (inherits the terminal's own
// color) rather than trying to apply an invalid color code.
func buildThemedGlamourStyle(theme *themes.Theme) ansi.StyleConfig {
	c := theme.Colors

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "\n",
				BlockSuffix: "\n",
				Color:       themeColor(c.Foreground),
			},
			Margin: uintPtr(2),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:  themeColor(c.Comment),
				Italic: boolPtr(true),
			},
			Indent:      uintPtr(1),
			IndentToken: stringPtr("│ "),
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: themeColor(c.Foreground),
			},
		},
		List: ansi.StyleList{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: themeColor(c.Foreground),
				},
			},
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       themeColor(c.Purple),
				Bold:        boolPtr(true),
			},
		},
		// H1-H6 deliberately carry NO "#"/"##"/... Prefix -- glamour's own
		// bundled styles (e.g. "dark") keep the literal markdown hash marks
		// as a heading-level cue, but a real live report (2026-09-11) called
		// that out as not looking like an "actual header." Since a terminal
		// can't vary font size, level is instead conveyed by color (H1/H2
		// share Heading's bold Purple; H3+ switch to bold Cyan) and a 2-
		// column Indent nesting subsections under their parent section --
		// this doc only ever uses H2 (section) and H3 (subsection), see
		// helpMarkdown below, but H1/H4-H6 get sensible fallbacks too.
		H1: ansi.StyleBlock{},
		H2: ansi.StyleBlock{},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: themeColor(c.Cyan)},
			Indent:         uintPtr(2),
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: themeColor(c.Cyan)},
			Indent:         uintPtr(2),
		},
		H5: ansi.StyleBlock{Indent: uintPtr(4)},
		H6: ansi.StyleBlock{Indent: uintPtr(4)},

		Text:          ansi.StylePrimitive{Color: themeColor(c.Foreground)},
		Strikethrough: ansi.StylePrimitive{CrossedOut: boolPtr(true)},
		Emph: ansi.StylePrimitive{
			Color:  themeColor(c.Yellow),
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Color: themeColor(c.Orange),
			Bold:  boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  themeColor(c.Comment),
			Format: "\n──────────\n",
		},

		Item:        ansi.StylePrimitive{BlockPrefix: "• ", Color: themeColor(c.Foreground)},
		Enumeration: ansi.StylePrimitive{BlockPrefix: ". ", Color: themeColor(c.Cyan)},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},

		Link: ansi.StylePrimitive{
			Color:     themeColor(c.Cyan),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: themeColor(c.Pink),
			Bold:  boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Color:     themeColor(c.Cyan),
			Underline: boolPtr(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  themeColor(c.Pink),
			Format: "Image: {{.text}} →",
		},

		Code: ansi.StyleBlock{
			// No Prefix/Suffix padding (unlike glamour's bundled "dark"
			// style) -- those literal spaces add real visible width that
			// glamour's word-wrap pass, which runs before style decoration
			// is applied, doesn't account for. A doc with many short inline
			// code spans (e.g. this one's `--flag` mentions) could then
			// wrap a hair past the requested content width. Found via
			// TestHelpMarkdownNoLineExceedsContentWidth.
			StylePrimitive: ansi.StylePrimitive{
				Color:           themeColor(c.Green),
				BackgroundColor: themeColor(c.CurrentLine),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: themeColor(c.Foreground),
				},
				Margin: uintPtr(2),
			},
			// Theme (a pre-registered chroma style name), not Chroma -- see
			// registerChromaStyleForTheme's doc comment for why glamour's
			// own Chroma-field auto-registration path is deliberately
			// avoided here (it can panic on terminal-default's bare ANSI
			// color indices, and never updates after the first theme it
			// ever sees in a process).
			Theme: registerChromaStyleForTheme(theme),
		},

		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: themeColor(c.Foreground)},
			},
			CenterSeparator: stringPtr("┼"),
			ColumnSeparator: stringPtr("│"),
			RowSeparator:    stringPtr("─"),
		},

		DefinitionList: ansi.StyleBlock{},
		DefinitionTerm: ansi.StylePrimitive{
			Color: themeColor(c.Cyan),
			Bold:  boolPtr(true),
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n🠶 ",
			Color:       themeColor(c.Foreground),
		},
	}
}

// buildChatGlamourStyle derives a chat-message-appropriate variant of
// buildThemedGlamourStyle. Help & Guide is a single full-page document, so
// its style leans on glamour's normal block spacing (a left margin, a
// blank line around headings/the whole document); a chat message is one
// compact entry in a scrolling list the caller already pads/aligns itself
// (renderMessageContent's callers apply their own PaddingLeft/Right), so
// that same spacing would double up as unwanted blank lines and extra
// indentation around every single message. This zeroes those out while
// keeping the same theme-derived colors for everything else.
func buildChatGlamourStyle(theme *themes.Theme) ansi.StyleConfig {
	s := buildThemedGlamourStyle(theme)

	s.Document.Margin = uintPtr(0)
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	s.Heading.BlockSuffix = ""
	s.BlockQuote.Margin = uintPtr(0)
	s.CodeBlock.Margin = uintPtr(0)

	// Plain message text uses the chat-specific semantic color
	// (theme.Semantic.ChatFg), not the generic theme.Colors.Foreground
	// buildThemedGlamourStyle uses for Help & Guide -- matches the color
	// renderMessageContent's pre-glamour implementation always used for
	// unstyled message text, in case a theme tunes the two separately.
	chatFg := themeColor(theme.Semantic.ChatFg)
	s.Document.Color = chatFg
	s.Paragraph.Color = chatFg
	s.Text.Color = chatFg

	return s
}

// chromaHex returns v unchanged if it's a "#rrggbb"-style hex color, or ""
// otherwise. Chroma's own style-entry parser (github.com/alecthomas/chroma/v2's
// ParseStyleEntry) only understands "#rrggbb"/"bg:#rrggbb"/bold/italic/
// underline/noinherit -- unlike lipgloss/termenv (used for everything else
// in Concord's UI), it has no concept of the bare ANSI palette indices
// ("0"-"15") the terminal-default theme deliberately uses for its other
// color fields (see CLAUDE.md's Themes section). Feeding one of those
// straight to chroma doesn't degrade gracefully -- chroma.MustNewStyle
// panics outright (not a returned error) with e.g. `unknown style element
// "2"` for terminal-default's green="2". Route every chroma-bound color
// through this so an unrecognized value is simply omitted (chroma treats a
// missing color as "inherit/unstyled") instead of crashing the whole TUI.
func chromaHex(v string) string {
	if !strings.HasPrefix(v, "#") {
		return ""
	}
	return v
}

// composeChromaStyle builds one chroma style-entry string (e.g.
// "#a6e3a1 bg:#1e1e2e bold") from pre-validated (chromaHex-filtered)
// color/background values, mirroring the space-separated grammar
// chroma.ParseStyleEntry expects.
func composeChromaStyle(color, bg string, bold, italic bool) string {
	var parts []string
	if color != "" {
		parts = append(parts, color)
	}
	if bg != "" {
		parts = append(parts, "bg:"+bg)
	}
	if italic {
		parts = append(parts, "italic")
	}
	if bold {
		parts = append(parts, "bold")
	}
	return strings.Join(parts, " ")
}

// chromaStyleEntries builds the chroma.StyleEntries mapping used for
// code-block syntax highlighting, field-for-field the same color choices
// buildThemedGlamourStyle used to build inline as an *ansi.Chroma (moved
// here so they can be safely validated via chromaHex before ever reaching
// chroma's parser -- see registerChromaStyleForTheme's doc comment).
func chromaStyleEntries(theme *themes.Theme) chroma.StyleEntries {
	c := theme.Colors
	return chroma.StyleEntries{
		chroma.Text:                composeChromaStyle(chromaHex(c.Foreground), "", false, false),
		chroma.Error:               composeChromaStyle(chromaHex(c.Foreground), chromaHex(c.Red), false, false),
		chroma.Comment:             composeChromaStyle(chromaHex(c.Comment), "", false, false),
		chroma.CommentPreproc:      composeChromaStyle(chromaHex(c.Pink), "", false, false),
		chroma.Keyword:             composeChromaStyle(chromaHex(c.Pink), "", false, false),
		chroma.KeywordReserved:     composeChromaStyle(chromaHex(c.Pink), "", false, false),
		chroma.KeywordNamespace:    composeChromaStyle(chromaHex(c.Pink), "", false, false),
		chroma.KeywordType:         composeChromaStyle(chromaHex(c.Cyan), "", false, false),
		chroma.Operator:            composeChromaStyle(chromaHex(c.Pink), "", false, false),
		chroma.Punctuation:         composeChromaStyle(chromaHex(c.Foreground), "", false, false),
		chroma.Name:                composeChromaStyle(chromaHex(c.Cyan), "", false, false),
		chroma.NameBuiltin:         composeChromaStyle(chromaHex(c.Cyan), "", false, false),
		chroma.NameTag:             composeChromaStyle(chromaHex(c.Pink), "", false, false),
		chroma.NameAttribute:       composeChromaStyle(chromaHex(c.Green), "", false, false),
		chroma.NameClass:           composeChromaStyle(chromaHex(c.Cyan), "", false, false),
		chroma.NameConstant:        composeChromaStyle(chromaHex(c.Purple), "", false, false),
		chroma.NameDecorator:       composeChromaStyle(chromaHex(c.Green), "", false, false),
		chroma.NameFunction:        composeChromaStyle(chromaHex(c.Green), "", false, false),
		chroma.LiteralNumber:       composeChromaStyle(chromaHex(c.Cyan), "", false, false),
		chroma.LiteralString:       composeChromaStyle(chromaHex(c.Yellow), "", false, false),
		chroma.LiteralStringEscape: composeChromaStyle(chromaHex(c.Pink), "", false, false),
		chroma.GenericDeleted:      composeChromaStyle(chromaHex(c.Red), "", false, false),
		chroma.GenericEmph:         composeChromaStyle(chromaHex(c.Yellow), "", false, true),
		chroma.GenericInserted:     composeChromaStyle(chromaHex(c.Green), "", false, false),
		chroma.GenericStrong:       composeChromaStyle(chromaHex(c.Orange), "", true, false),
		chroma.GenericSubheading:   composeChromaStyle(chromaHex(c.Purple), "", false, false),
	}
}

// chromaStyleNameForTheme is the chroma style registry name used for a given
// Concord theme's code-block syntax highlighting -- keyed by theme name,
// the same identity already used by HelpRenderTheme's cache-invalidation
// check (help_view.go).
func chromaStyleNameForTheme(theme *themes.Theme) string {
	return "concord-chroma-" + theme.Meta.Name
}

// registerChromaStyleForTheme registers (once per theme name; cheap no-op
// on repeat calls) a chroma style for code-block syntax highlighting
// derived from the given Concord theme, and returns its registry name for
// use as ansi.StyleCodeBlock.Theme.
//
// This works around two real problems in glamour's own auto-registration
// path (glamour/ansi/codeblock.go's CodeBlockElement.Render, triggered
// whenever ansi.StyleCodeBlock.Chroma is set instead of Theme):
//
//  1. glamour always registers under one hardcoded name ("charm") and only
//     the FIRST time ever in the process (`if !ok { styles.Register(...) }`)
//     -- using whatever theme happened to be active at that moment. Every
//     later theme switch's code blocks then silently keep showing that
//     first theme's colors, never following the new one.
//  2. glamour feeds theme.Colors values straight into chroma's style-entry
//     parser with no validation. Live bug found 2026-09-11: switching to
//     the terminal-default theme (green="2", a bare ANSI palette index --
//     valid for the rest of Concord's UI, but not for chroma) and then
//     opening Settings > Help & Guide crashed the whole TUI with
//     `chroma.MustNewStyle`'s panic: `invalid entry for NameFunction:
//     unknown style element "2"` -- MustNewStyle panics outright rather
//     than returning an error.
//
// Registering our own uniquely-named (per theme) style ahead of time makes
// glamour's own `if !ok` check see it as already-registered and skip its
// path entirely, using ours instead -- every theme gets its own always-
// up-to-date entry, and chromaStyleEntries only ever emits chromaHex-
// validated colors, so chroma.NewStyle can't fail here the way
// chroma.MustNewStyle did.
func registerChromaStyleForTheme(theme *themes.Theme) string {
	name := chromaStyleNameForTheme(theme)
	if _, ok := chromastyles.Registry[name]; ok {
		return name
	}
	style, err := chroma.NewStyle(name, chromaStyleEntries(theme))
	if err != nil {
		// Should be unreachable -- chromaStyleEntries only emits
		// chromaHex-validated tokens. Fall back to "" (glamour's own
		// StylePrimitive-only rendering, no syntax highlighting) rather
		// than risk propagating a bad style name.
		return ""
	}
	chromastyles.Register(style)
	return name
}

// themeColor turns a theme color field into the *string glamour's
// ansi.StylePrimitive wants, treating "" (terminal-default's "no override,
// let the terminal's own color show through" convention) as nil rather
// than an invalid color code.
func themeColor(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func stringPtr(v string) *string { return &v }
func uintPtr(v uint) *uint       { return &v }
