package client

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// A message arriving used to send tea.ClearScreen, which blanked the window
// for a frame (a black flash on every message). The repaint must never be
// the erasing kind.
func TestRepaintAllDoesNotEraseTheScreen(t *testing.T) {
	got := fmt.Sprintf("%T", repaintAll())
	if erase := fmt.Sprintf("%T", tea.ClearScreen()); got == erase {
		t.Fatalf("repaintAll sends %s, which erases the screen before the next frame", got)
	}
	if want := fmt.Sprintf("%T", tea.ClearScrollArea()); got != want { //nolint:staticcheck
		t.Fatalf("repaintAll sends %s, want %s (a rewrite in place)", got, want)
	}
}

// tea.ClearScreen flashes the window black, so the client may use it in one
// place only: erasing Sixel/iTerm2 pixels, which text can't overwrite.
func TestClearScreenOnlyErasesPictures(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var uses []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "ClearScreen" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "tea" {
					uses = append(uses, fset.Position(sel.Pos()).String())
				}
			}
			return true
		})
	}
	if len(uses) != 1 {
		t.Fatalf("tea.ClearScreen used at %v; use repaintAll (only the raster clear may erase)", uses)
	}
}

func benchMessages(n int) []*MessageDisplay {
	var out []*MessageDisplay
	at := time.Now().Add(-time.Duration(n) * time.Minute)
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("message %d with **bold**, a `code span` and a link https://example.com/%d, enough words to wrap a little at this width.", i, i)
		out = append(out, &MessageDisplay{
			Message:    &models.Message{ID: uuid.New(), Content: content, CreatedAt: at.Add(time.Duration(i) * time.Minute)},
			AuthorName: []string{"alice", "bob", "juniper"}[i%3],
			ShowHeader: i%2 == 0,
		})
	}
	return out
}

// What a message arriving costs: the chat rebuilt with 200 messages (about
// 2.5 ms on the dev box, markdown cached per message).
func BenchmarkUpdateChatContent200(b *testing.B) {
	a, _ := newTestAppForRendering(100, benchMessages(200))
	a.updateChatContent()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.updateChatContent()
	}
}
