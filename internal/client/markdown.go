package client

import (
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
		H1: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "# "}},
		H2: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "## "}},
		H3: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "### "}},
		H4: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "#### "}},
		H5: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "##### "}},
		H6: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "###### "}},

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
			Chroma: &ansi.Chroma{
				Text:                ansi.StylePrimitive{Color: themeColor(c.Foreground)},
				Error:               ansi.StylePrimitive{Color: themeColor(c.Foreground), BackgroundColor: themeColor(c.Red)},
				Comment:             ansi.StylePrimitive{Color: themeColor(c.Comment)},
				CommentPreproc:      ansi.StylePrimitive{Color: themeColor(c.Pink)},
				Keyword:             ansi.StylePrimitive{Color: themeColor(c.Pink)},
				KeywordReserved:     ansi.StylePrimitive{Color: themeColor(c.Pink)},
				KeywordNamespace:    ansi.StylePrimitive{Color: themeColor(c.Pink)},
				KeywordType:         ansi.StylePrimitive{Color: themeColor(c.Cyan)},
				Operator:            ansi.StylePrimitive{Color: themeColor(c.Pink)},
				Punctuation:         ansi.StylePrimitive{Color: themeColor(c.Foreground)},
				Name:                ansi.StylePrimitive{Color: themeColor(c.Cyan)},
				NameBuiltin:         ansi.StylePrimitive{Color: themeColor(c.Cyan)},
				NameTag:             ansi.StylePrimitive{Color: themeColor(c.Pink)},
				NameAttribute:       ansi.StylePrimitive{Color: themeColor(c.Green)},
				NameClass:           ansi.StylePrimitive{Color: themeColor(c.Cyan)},
				NameConstant:        ansi.StylePrimitive{Color: themeColor(c.Purple)},
				NameDecorator:       ansi.StylePrimitive{Color: themeColor(c.Green)},
				NameFunction:        ansi.StylePrimitive{Color: themeColor(c.Green)},
				LiteralNumber:       ansi.StylePrimitive{Color: themeColor(c.Cyan)},
				LiteralString:       ansi.StylePrimitive{Color: themeColor(c.Yellow)},
				LiteralStringEscape: ansi.StylePrimitive{Color: themeColor(c.Pink)},
				GenericDeleted:      ansi.StylePrimitive{Color: themeColor(c.Red)},
				GenericEmph:         ansi.StylePrimitive{Color: themeColor(c.Yellow), Italic: boolPtr(true)},
				GenericInserted:     ansi.StylePrimitive{Color: themeColor(c.Green)},
				GenericStrong:       ansi.StylePrimitive{Color: themeColor(c.Orange), Bold: boolPtr(true)},
				GenericSubheading:   ansi.StylePrimitive{Color: themeColor(c.Purple)},
			},
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
