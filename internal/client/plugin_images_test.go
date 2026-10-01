package client

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
)

func testImage(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0xBD, 0x93, 0xF9, 255})
		}
	}
	return img
}

// imageApp is an app whose pane shows one 8×4-cell image from a plugin
// client file that's already decoded.
func imageApp(t *testing.T, mode string) (*App, *ServerConnection) {
	t.Helper()
	a := newLayoutTestApp(t, 120, 40)
	if a.uiConfig == nil {
		a.uiConfig = &UIConfig{}
	}
	a.uiConfig.Display.Images = mode
	termGraphics = TerminalGraphics{CellW: 10, CellH: 20}
	sc := &ServerConnection{ServerID: uuid.New(), PluginClients: []protocol.PluginClientInfo{{
		PluginID: "chess", Files: []protocol.PluginClientFile{{Path: "king.png", SHA256: "abc", Size: 3}},
	}}}
	a.assets().images["abc"] = testImage(80, 80)
	return a, sc
}

func TestKittyPlaceholdersAreOneCellEach(t *testing.T) {
	cells := kittyCells(1001, 3, 7)
	if w := ansi.StringWidth(cells); w != 7 {
		t.Fatalf("7 placeholders measure %d cells", w)
	}
	if w := lipgloss.Width(cells); w != 7 {
		t.Fatalf("lipgloss measures %d cells", w)
	}
}

func TestImagesSplicedIntoTheFrameKeepItsShape(t *testing.T) {
	images := []protocol.PaneImage{{Asset: "king.png", Col: 4, Row: 1, Cols: 8, Rows: 4}}
	frame := []string{"title line here", "row one ............", "row two ............", "row three ..........", "row four ...........", "last"}
	for _, mode := range []string{"blocks", "kitty", "sixel", "iterm2"} {
		a, sc := imageApp(t, mode)
		lines, rasters := a.drawPaneImages(append([]string(nil), frame...), 40, sc, "chess", images)
		// A square image in an 8×4 box of 10×20 cells is 8 cols × 4 rows.
		for i, l := range lines {
			want := ansi.StringWidth(frame[i])
			if got := ansi.StringWidth(l); got != want {
				t.Fatalf("%s: line %d is %d cells, was %d: %q", mode, i, got, want, l)
			}
		}
		if !strings.HasPrefix(ansi.Strip(lines[1]), "row ") {
			t.Fatalf("%s: the text left of the image is gone: %q", mode, ansi.Strip(lines[1]))
		}
		switch mode {
		case "sixel", "iterm2":
			if len(rasters) != 1 || !strings.Contains(lines[1], rasterMarker(0)) {
				t.Fatalf("%s: no raster image placed", mode)
			}
			// The marker survives centering in the pane.
			out := lipgloss.NewStyle().Width(60).Height(10).Align(lipgloss.Center, lipgloss.Center).Render(strings.Join(lines, "\n"))
			if !strings.Contains(out, rasterMarker(0)) {
				t.Fatalf("%s: lipgloss dropped the marker", mode)
			}
		default:
			if len(rasters) != 0 {
				t.Fatalf("%s: raster images for a text protocol", mode)
			}
		}
	}

	// Off draws nothing; a file not advertised draws nothing.
	a, sc := imageApp(t, "off")
	if lines, _ := a.drawPaneImages(append([]string(nil), frame...), 40, sc, "chess", images); strings.Join(lines, "\n") != strings.Join(frame, "\n") {
		t.Fatal("images drawn with Images = off")
	}
	a, sc = imageApp(t, "blocks")
	if lines, _ := a.drawPaneImages(append([]string(nil), frame...), 40, sc, "chess", []protocol.PaneImage{{Asset: "queen.png", Cols: 4, Rows: 2}}); strings.Join(lines, "\n") != strings.Join(frame, "\n") {
		t.Fatal("drew an image the server never advertised")
	}
}

func TestRasterImagesRepaintOnlyWhenRowsUnderThemChange(t *testing.T) {
	a, _ := imageApp(t, "sixel")
	rasters := []rasterImage{{key: "k", blob: "<SIXEL>", rows: 2}}
	screen := func(beside string) string {
		return strings.Join([]string{"header", "xx" + rasterMarker(0) + "    " + beside, "xx      " + beside, "footer"}, "\n")
	}

	out := a.paintRasterImages(screen("a"), rasters, false)
	if strings.Contains(out, "\x1b]8337;") || !strings.Contains(out, "<SIXEL>") || !strings.Contains(out, "\x1b[2;3H") {
		t.Fatalf("first paint: %q", out)
	}
	// The same screen again, right away: still included (the previous frame
	// may not have been flushed), but not after the repaint window.
	a.rasterState.dirtyAt = time.Now().Add(-time.Second)
	if out := a.paintRasterImages(screen("a"), rasters, false); strings.Contains(out, "<SIXEL>") {
		t.Fatal("re-sent an image whose rows didn't change")
	}
	if out := a.paintRasterImages(screen("b"), rasters, false); !strings.Contains(out, "<SIXEL>") {
		t.Fatal("didn't repaint after a row under the image changed")
	}
	if out := a.paintRasterImages(screen("c"), rasters, true); strings.Contains(out, "<SIXEL>") || strings.Contains(out, "\x1b]8337;") {
		t.Fatal("painted over an overlay")
	}
	// The images go away: one full repaint clears their pixels.
	a.paintRasterImages("no images now", nil, false)
	if !a.rasterState.clear {
		t.Fatal("no repaint after the images went away")
	}
}

func TestPluginFilesAreVerifiedAndCached(t *testing.T) {
	good := []byte("\x89PNG not really")
	sum := sha256.Sum256(good)
	sha := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/bad.wav") {
			_, _ = w.Write([]byte("tampered!!!!!!!!"))
			return
		}
		_, _ = w.Write(good)
	}))
	defer srv.Close()

	if _, err := download(srv.URL, "tok", "chess", protocol.PluginClientFile{Path: "a/b c.wav", Size: int64(len(good)), SHA256: sha}); err != nil {
		t.Fatalf("good file: %v", err)
	}
	if _, err := download(srv.URL, "tok", "chess", protocol.PluginClientFile{Path: "bad.wav", Size: 16, SHA256: sha}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered file: %v", err)
	}
	if _, err := download(srv.URL, "nope", "chess", protocol.PluginClientFile{Path: "x.wav", Size: int64(len(good)), SHA256: sha}); err == nil {
		t.Fatal("downloaded without a valid session")
	}

	s := newPluginAssets(t.TempDir())
	s.store(sha, good)
	if data, err := s.cached(sha); err != nil || string(data) != string(good) {
		t.Fatalf("cache: %v", err)
	}
	if _, err := s.cached(strings.Repeat("0", 64)); err == nil {
		t.Fatal("a missing cache entry was found")
	}
}
