package client

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/themes"
	zone "github.com/lrstanley/bubblezone"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// messageRenderCacheEntry is one cached renderMessageContent result -- see
// App.messageRenderCache's doc comment for the invalidation strategy.
type messageRenderCacheEntry struct {
	Text     string
	Width    int
	Theme    string
	Rendered string
}

// chatMarkdownHighPriority mirrors glamour's own internal renderer
// priority constant (unexported there) -- required so goldmark actually
// uses the ansi renderer instead of its default HTML renderer.
const chatMarkdownHighPriority = 1000

// newChatMarkdownRenderer builds a minimal-feature glamour-style renderer
// for chat messages. It deliberately does NOT use glamour.NewTermRenderer,
// which hardcodes goldmark's extension.GFM bundle (Linkify + Table +
// Strikethrough + TaskList) with no way to opt out of individual pieces --
// Table is exactly what item 3a(b) avoided in Help & Guide, and Linkify is
// actively harmful for chat: TestProbeChatGlamourPipeline found that
// glamour's LinkElement renderer prints both a link's text AND its href,
// so a plain autolinked bare URL (text == href) renders TWICE. Concord
// substitutes its own placeholder for every URL before rendering (see
// extractURLPlaceholders) and applies its own OSC 8/zone-marked styling
// afterward instead, so Linkify is simply excluded -- goldmark then leaves
// bare URL-shaped text (and the placeholders) alone as plain text.
// Strikethrough and TaskList are harmless for chat and kept.
//
// Headings/blockquotes/horizontal rules are core CommonMark, not part of
// the GFM extension bundle, so they can't be excluded the same way without
// forking goldmark's block parser registration -- buildChatGlamourStyle
// keeps them visually restrained (no forced blank lines/margins) rather
// than attempting to suppress them via source-text escaping, which would
// risk mangling legitimate message text that happens to start with '#'/'>'.
func newChatMarkdownRenderer(theme *themes.Theme, wordWrap int) (*chatMarkdownRenderer, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Strikethrough,
			extension.TaskList,
		),
	)
	ar := ansi.NewRenderer(ansi.Options{
		WordWrap: wordWrap,
		Styles:   buildChatGlamourStyle(theme),
	})
	md.SetRenderer(renderer.NewRenderer(
		renderer.WithNodeRenderers(util.Prioritized(ar, chatMarkdownHighPriority)),
	))
	return &chatMarkdownRenderer{md: md}, nil
}

type chatMarkdownRenderer struct {
	md goldmark.Markdown
}

func (r *chatMarkdownRenderer) Render(source string) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// urlPlaceholderOpen/Close bracket each substituted URL's index. Private
// Use Area codepoints (/) so they can never collide with real
// message text and carry no markdown meaning to goldmark -- it just sees
// an opaque short "word" where the URL used to be.
const (
	urlPlaceholderOpen  = ""
	urlPlaceholderClose = ""
)

var urlPlaceholderRegex = regexp.MustCompile("([0-9]+)")

// extractURLPlaceholders replaces every URL urlRegex finds in body with a
// placeholder token and returns the substituted text plus the URLs in
// order (placeholder index == slice index) for restoreURLPlaceholders to
// look up after rendering. See newChatMarkdownRenderer's doc comment for
// why URLs are never handed to glamour/goldmark directly.
func extractURLPlaceholders(body string) (substituted string, urls []string) {
	substituted = urlRegex.ReplaceAllStringFunc(body, func(u string) string {
		idx := len(urls)
		urls = append(urls, u)
		return fmt.Sprintf("%s%d%s", urlPlaceholderOpen, idx, urlPlaceholderClose)
	})
	return substituted, urls
}

// restoreURLPlaceholders replaces each placeholder token in a rendered
// line with Concord's own clickable OSC 8 hyperlink, zone-marked so a
// mouse click on it resolves back to the URL (see handleMainViewMouse in
// mouse.go). linkIndex is shared and incremented across the whole message
// (not reset per line) so every link within one message gets a unique,
// stable zone ID for the message's lifetime, matching the scheme
// renderMessageContent always used.
func restoreURLPlaceholders(line, msgID string, urls []string, linkIndex *int, linkStyle lipgloss.Style) string {
	return urlPlaceholderRegex.ReplaceAllStringFunc(line, func(m string) string {
		sub := urlPlaceholderRegex.FindStringSubmatch(m)
		idx, err := strconv.Atoi(sub[1])
		if err != nil || idx < 0 || idx >= len(urls) {
			return m
		}
		u := urls[idx]
		zoneID := fmt.Sprintf("link:%s:%d", msgID, *linkIndex)
		*linkIndex++
		return zone.Mark(zoneID, osc8Link(u, linkStyle.Render(u)))
	})
}

// highlightMentions wraps an @mention/@everyone/@here occurrence in line
// with mentionStyle, leaving the rest of the line untouched -- including
// whatever glamour styling it already carries (bold/italic/code/etc.),
// which a blanket re-wrap of the non-matched text would otherwise nest
// redundant SGR codes around. Mirrors renderMessageContent's pre-markdown
// mention detection exactly (same @everyone/@here/personal-alias logic),
// just without URL handling -- URLs are handled separately, before this
// runs, via extractURLPlaceholders/restoreURLPlaceholders.
func highlightMentions(line, alias string, mentionStyle lipgloss.Style) string {
	lower := strings.ToLower(line)
	hasEveryone := strings.Contains(lower, "@everyone") || strings.Contains(lower, "@here")
	hasPersonal := alias != "" && strings.Contains(lower, "@"+strings.ToLower(alias))
	if !hasEveryone && !hasPersonal {
		return line
	}

	var out strings.Builder
	seg := line
	for len(seg) > 0 {
		mentionLoc := []int{-1, -1}
		everyoneLoc := []int{-1, -1}

		segLower := strings.ToLower(seg)
		if idx := strings.Index(segLower, "@everyone"); idx != -1 {
			everyoneLoc = []int{idx, idx + len("@everyone")}
		} else if idx := strings.Index(segLower, "@here"); idx != -1 {
			everyoneLoc = []int{idx, idx + len("@here")}
		}

		if alias != "" {
			token := "@" + strings.ToLower(alias)
			if idx := strings.Index(segLower, token); idx != -1 {
				mentionLoc = []int{idx, idx + len(token)}
			}
		}

		useMention := mentionLoc[0] != -1
		useEveryone := everyoneLoc[0] != -1
		if useMention && useEveryone {
			if everyoneLoc[0] < mentionLoc[0] {
				useMention = false
			} else {
				useEveryone = false
			}
		}

		switch {
		case useEveryone:
			out.WriteString(seg[:everyoneLoc[0]])
			out.WriteString(mentionStyle.Render(seg[everyoneLoc[0]:everyoneLoc[1]]))
			seg = seg[everyoneLoc[1]:]
		case useMention:
			out.WriteString(seg[:mentionLoc[0]])
			out.WriteString(mentionStyle.Render(seg[mentionLoc[0]:mentionLoc[1]]))
			seg = seg[mentionLoc[1]:]
		default:
			out.WriteString(seg)
			seg = ""
		}
	}
	return out.String()
}

// ansiResetSeq is the SGR "full reset" sequence termenv/glamour emit after
// most styled spans (bold, a heading's color, inline code, etc.) --
// confirmed via direct inspection of real rendered output (see
// TestProbeChatGlamourPipeline). ansiResetSeqShort ("\x1b[m", no explicit
// "0") is the other spelling of the same reset that shows up too --
// confirmed live 2026-09-08 the hard way: patching only the "0" form left
// a handful of gaps unpatched, right at internal line-wrap boundaries
// glamour's own renderer apparently closes with the short form. Order
// matters below: the short form is a substring-adjacent prefix of the long
// one's tail ("[m" inside "[0m"), so it must be matched as its own
// distinct sequence, not accidentally caught mid-replacement of the long
// one.
const (
	ansiResetSeq      = "\x1b[0m"
	ansiResetSeqShort = "\x1b[m"
)

// reassertBackgroundAfterResets patches already-rendered, ANSI-styled
// content so a caller's own outer Background() wrap survives being applied
// on top of it. lipgloss's Style.Render() only opens its background once,
// at the very start of the string it's given -- an ANSI full-reset
// anywhere inside that content (exactly what markdown-rendered chat
// messages are full of: every heading, bold span, and inline code block
// ends in one) clears that background too, and nothing reopens it until
// the *next* outer style boundary. The visible result: the message-select
// highlight only "sticks" in the plain-text gaps between styled spans,
// leaving small disconnected boxes instead of one solid highlighted
// message -- confirmed 2026-09-08 live, a real regression from Phase 5's
// switch to full glamour rendering (the old plain-regex message styling
// only ever had zero or one reset in play, never enough spans for this to
// be visible).
//
// bg is rendered once to extract its own "open" ANSI prefix (via a NUL
// sentinel character, stripped back out) rather than hand-assembling an
// SGR string -- this way it automatically matches whatever color profile
// lipgloss's renderer is actually using, the same as everywhere else in
// the codebase that lets lipgloss own its own ANSI encoding.
func reassertBackgroundAfterResets(content, bg string) string {
	open := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color(bg)))
	if open == "" {
		return content
	}
	patched := strings.ReplaceAll(content, ansiResetSeq, ansiResetSeq+open)
	patched = strings.ReplaceAll(patched, ansiResetSeqShort, ansiResetSeqShort+open)
	return patched
}

// ansiOpenCode extracts just the "turn styling on" escape prefix a
// lipgloss.Style emits, by rendering a NUL sentinel and taking everything
// before it -- robust to whatever concrete SGR sequence the active color
// profile (TrueColor/ANSI256/ANSI) actually produces.
func ansiOpenCode(style lipgloss.Style) string {
	rendered := style.Render("\x00")
	idx := strings.IndexByte(rendered, 0)
	if idx < 0 {
		return ""
	}
	return rendered[:idx]
}

// sgrSequenceAt reports whether line[start:end] is one complete SGR escape
// sequence ("\x1b[" + digits/semicolons + "m") -- used by
// trimTrailingRenderedPadding to recognize a trailing escape code without
// assuming anything about how many of them are chained together or in
// what order opens/resets appear, since that shape turned out to vary
// (see that function's own doc comment for why a fixed repeating-pattern
// regex wasn't robust enough).
func sgrSequenceAt(line string, start, end int) bool {
	if end-start < 3 || line[start] != '\x1b' || line[start+1] != '[' || line[end-1] != 'm' {
		return false
	}
	for i := start + 2; i < end-1; i++ {
		if line[i] != ';' && (line[i] < '0' || line[i] > '9') {
			return false
		}
	}
	return true
}

// trimTrailingRenderedPadding strips glamour's own trailing whitespace
// padding from one already-rendered line, by scanning backward from the
// end and consuming literal spaces and complete SGR escape sequences in
// any mix until real visible content is hit. glamour pads every line out
// to the full configured word-wrap width with ANSI-wrapped space
// characters -- harmless for a left-aligned, always-full-width consumer
// like Help & Guide, but it defeats a caller's own
// Width().Align(lipgloss.Right) wrap (own chat messages, app.go): the line
// already visually measures as the full viewport width by the time that
// wrap runs, leaving no room to redistribute, so the message renders
// stuck at the left edge instead of right-aligned. Trailing whitespace is
// never meaningful in a chat message regardless of its source, so this is
// correct to strip unconditionally, not just as a workaround.
//
// A fixed regex for "one open + one space + one reset, repeated" (the
// pattern real output usually shows) isn't reliable on its own -- real
// output sometimes tacks on an extra bare reset after the last repeat,
// breaking a strict period-N match right at the point that matters (the
// very end of the string) and leaving the padding completely unstripped.
// Scanning backward byte-by-byte doesn't care about grouping or order, so
// it isn't sensitive to that kind of variation.
func trimTrailingRenderedPadding(line string) string {
	i := len(line)
	for i > 0 {
		if line[i-1] == ' ' {
			i--
			continue
		}
		if line[i-1] == 'm' {
			j := i - 2
			for j >= 0 && line[j] != '\x1b' {
				j--
			}
			if j >= 0 && sgrSequenceAt(line, j, i) {
				i = j
				continue
			}
		}
		break
	}
	return line[:i]
}
