package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestStreamingMarkdownClosesWhatIsOpen(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Hello", "Hello▍"},
		{"Here's code:\n```go\nfunc main() {", "Here's code:\n```go\nfunc main() {▍\n```"},
		{"Done:\n```go\nx := 1\n```\nand then", "Done:\n```go\nx := 1\n```\nand then▍"},
		{"~~~~\nstill ``` inside\n", "~~~~\nstill ``` inside\n▍\n~~~~"},
		{"use `fmt.Prin", "use `fmt.Prin▍`"},
		{"this is **really", "this is **really▍**"},
		{"a **bold** word", "a **bold** word▍"},
		{"    ```\nindented, not a fence", "    ```\nindented, not a fence▍"},
	} {
		if got := streamingMarkdown(c.in); got != c.want {
			t.Errorf("streamingMarkdown(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// A reply streamed in pieces renders as markdown at every step: an open
// code block stays a code block, and nothing leaks raw fence markers.
func TestStreamingMarkdownRendersAtEveryStep(t *testing.T) {
	app, _ := newTestAppForRendering(80, nil)
	full := "Sure! Here's how:\n\n1. **Open** the file\n2. Run:\n\n```sh\ngo test ./...\n```\n\nThat's `it`."
	for i := 1; i <= len(full); i += 7 {
		partial := full[:i]
		out := ansi.Strip(app.renderChatMarkdownBody("", streamingMarkdown(partial), 60, ""))
		if strings.Contains(out, "```") {
			t.Fatalf("raw fence leaked at %d chars:\n%s", i, out)
		}
		if !strings.Contains(out, streamCursor) {
			t.Fatalf("no cursor at %d chars:\n%s", i, out)
		}
	}
}
