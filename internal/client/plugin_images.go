package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/mattn/go-sixel"

	"github.com/concord-chat/concord/internal/protocol"
)

// Plugin images (To Do item D): files from a plugin's client part, fetched
// once, verified against the SHA-256 the server advertised, cached on disk
// by hash, and drawn in the plugin's pane however this terminal can.

// maxImagePixels refuses images too large to be reasonable (and decode
// bombs): 16 megapixels.
const maxImagePixels = 16 << 20

// pluginAssets fetches and caches plugin client files.
type pluginAssets struct {
	mu      sync.Mutex
	dir     string                 // ~/.concord/plugin-cache
	pending map[string]bool        // sha256 → being fetched
	failed  map[string]string      // sha256 → why
	images  map[string]image.Image // sha256 → decoded
	sounds  map[string][]int16     // sha256 → decoded 48 kHz stereo

	// Per drawn size: encoded Sixel / iTerm2 data and Kitty image IDs.
	blobs     map[string]string
	kittyIDs  map[string]int
	kittyNext int
}

func newPluginAssets(dir string) *pluginAssets {
	return &pluginAssets{
		dir: dir, pending: map[string]bool{}, failed: map[string]string{},
		images: map[string]image.Image{}, sounds: map[string][]int16{}, blobs: map[string]string{}, kittyIDs: map[string]int{}, kittyNext: 1000,
	}
}

// assets is the App's store, created on first use.
func (a *App) assets() *pluginAssets {
	if a.pluginAssets == nil {
		dir := ""
		if a.configMgr != nil && a.configMgr.configFilePath != "" {
			dir = filepath.Join(filepath.Dir(a.configMgr.configFilePath), "plugin-cache")
		}
		a.pluginAssets = newPluginAssets(dir)
	}
	return a.pluginAssets
}

// pluginAssetMsg says a plugin file finished fetching (or failed); any
// message re-renders, so the image appears.
type pluginAssetMsg struct {
	sha string
	err error
}

// clientFile finds a plugin client file in what a server advertised.
func clientFile(sc *ServerConnection, pluginID, path string) (protocol.PluginClientFile, bool) {
	if sc == nil {
		return protocol.PluginClientFile{}, false
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	for _, c := range sc.PluginClients {
		if c.PluginID != pluginID {
			continue
		}
		for _, f := range c.Files {
			if f.Path == path {
				return f, true
			}
		}
	}
	return protocol.PluginClientFile{}, false
}

// fetch returns a command that downloads a client file unless it's cached,
// being fetched, or failed already. It returns nil when nothing's needed.
func (s *pluginAssets) fetch(sc *ServerConnection, pluginID string, f protocol.PluginClientFile) tea.Cmd {
	s.mu.Lock()
	if s.pending[f.SHA256] || s.failed[f.SHA256] != "" || s.images[f.SHA256] != nil || s.sounds[f.SHA256] != nil {
		s.mu.Unlock()
		return nil
	}
	s.pending[f.SHA256] = true
	s.mu.Unlock()
	sc.mu.RLock()
	addr, token := sc.ServerInfo.GetHTTPURL(), sc.Token
	sc.mu.RUnlock()
	return func() tea.Msg {
		err := s.load(addr, token, pluginID, f)
		s.mu.Lock()
		delete(s.pending, f.SHA256)
		if err != nil {
			s.failed[f.SHA256] = err.Error()
		}
		s.mu.Unlock()
		return pluginAssetMsg{sha: f.SHA256, err: err}
	}
}

// load gets a file from the disk cache or the server, checks its hash, and
// decodes it if it's an image.
func (s *pluginAssets) load(addr, token, pluginID string, f protocol.PluginClientFile) error {
	data, err := s.cached(f.SHA256)
	if err != nil {
		data, err = download(addr, token, pluginID, f)
		if err != nil {
			return err
		}
		s.store(f.SHA256, data)
	}
	switch strings.ToLower(filepath.Ext(f.Path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		if cfg.Width*cfg.Height > maxImagePixels {
			return fmt.Errorf("%s is too large (%dx%d)", f.Path, cfg.Width, cfg.Height)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		s.mu.Lock()
		s.images[f.SHA256] = img
		s.mu.Unlock()
	case ".wav", ".ogg", ".opus":
		var pcm []int16
		if strings.EqualFold(filepath.Ext(f.Path), ".wav") {
			pcm, err = decodeWAV(data)
		} else {
			pcm, err = decodeOggOpus(data)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		s.mu.Lock()
		s.sounds[f.SHA256] = pcm
		s.mu.Unlock()
	}
	return nil
}

// cached reads a file from the disk cache, verifying it.
func (s *pluginAssets) cached(sha string) ([]byte, error) {
	if s.dir == "" {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(s.dir, sha))
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != sha {
		_ = os.Remove(filepath.Join(s.dir, sha))
		return nil, fmt.Errorf("cache entry corrupt")
	}
	return data, nil
}

func (s *pluginAssets) store(sha string, data []byte) {
	if s.dir == "" || os.MkdirAll(s.dir, 0o700) != nil {
		return
	}
	tmp := filepath.Join(s.dir, sha+".tmp")
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(s.dir, sha))
	}
}

// download fetches a client file from the server and checks its size and
// hash against the advertisement.
func download(addr, token, pluginID string, f protocol.PluginClientFile) ([]byte, error) {
	target, err := apiURL(addr, "/api/plugins/client/"+url.PathEscape(pluginID)+"/"+escapePath(f.Path))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", f.Path, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != f.Size {
		return nil, fmt.Errorf("%s: wrong size", f.Path)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.SHA256 {
		return nil, fmt.Errorf("%s: checksum doesn't match", f.Path)
	}
	return data, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// fetchPaneAssets starts fetching whatever a frame's images need.
func (a *App) fetchPaneAssets(sc *ServerConnection, pluginID string, images []protocol.PaneImage) tea.Cmd {
	var cmds []tea.Cmd
	for _, im := range images {
		if f, ok := clientFile(sc, pluginID, im.Asset); ok {
			if cmd := a.assets().fetch(sc, pluginID, f); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tea.Batch(cmds...)
}

func (s *pluginAssets) image(sha string) image.Image {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.images[sha]
}

// --- drawing ---

// rasterImage is a Sixel or iTerm2 image to paint after the frame: where
// its marker landed is found in the final screen (paintRasterImages).
type rasterImage struct {
	key  string
	blob string
	rows int
	cols int
}

// rasterMarker is a zero-width placeholder the frame carries where a raster
// image's top-left cell is. Plugins can't forge it: their frames are
// sanitized of every OSC except hyperlinks.
func rasterMarker(i int) string { return fmt.Sprintf("\x1b]8337;%d\x07", i) }

// fitCells fits an image into a box of cells keeping its aspect ratio: the
// cells it covers and its offset within the box.
func fitCells(img image.Image, cols, rows int) (fc, fr, offC, offR int) {
	cw, ch := cellPixels()
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return 0, 0, 0, 0
	}
	sx := float64(cols*cw) / float64(b.Dx())
	sy := float64(rows*ch) / float64(b.Dy())
	s := min(sx, sy)
	fc = max(1, min(cols, int(float64(b.Dx())*s/float64(cw)+.5)))
	fr = max(1, min(rows, int(float64(b.Dy())*s/float64(ch)+.5)))
	return fc, fr, (cols - fc) / 2, (rows - fr) / 2
}

// drawPaneImages draws a frame's images into its lines: half-block or
// Kitty-placeholder cells in place, or blanks plus a marker for raster
// protocols. It returns the lines and the raster images to paint.
func (a *App) drawPaneImages(lines []string, width int, sc *ServerConnection, pluginID string, images []protocol.PaneImage) ([]string, []rasterImage) {
	proto := a.graphicsProtocol()
	if proto == gfxOff || len(images) == 0 {
		return lines, nil
	}
	var (
		rasters   []rasterImage
		markers   []placedMarker
		transmits strings.Builder
	)
	store := a.assets()
	for _, im := range images {
		f, ok := clientFile(sc, pluginID, im.Asset)
		if !ok || im.Cols <= 0 || im.Rows <= 0 || im.Col < 0 || im.Row < 0 || im.Col >= width {
			continue
		}
		img := store.image(f.SHA256)
		if img == nil {
			continue // still fetching; the frame's own text shows meanwhile
		}
		cols := min(im.Cols, width-im.Col)
		fc, fr, offC, offR := fitCells(img, cols, im.Rows)
		col, row := im.Col+offC, im.Row+offR
		for len(lines) < row+fr {
			lines = append(lines, "")
		}
		key := fmt.Sprintf("%s:%dx%d", f.SHA256, fc, fr)
		switch proto {
		case gfxBlocks:
			cells := a.blockCells(store, key, img, fc, fr)
			for r := 0; r < fr; r++ {
				lines[row+r] = spliceCells(lines[row+r], col, fc, cells[r])
			}
		case gfxKitty:
			id, transmit := store.kittyImage(key, img, fc, fr)
			transmits.WriteString(transmit)
			for r := 0; r < fr; r++ {
				lines[row+r] = spliceCells(lines[row+r], col, fc, kittyCells(id, r, fc))
			}
		case gfxSixel, gfxITerm2:
			blob := store.rasterBlob(proto, key, img, fc, fr)
			for r := 0; r < fr; r++ {
				lines[row+r] = spliceCells(lines[row+r], col, fc, strings.Repeat(" ", fc))
			}
			markers = append(markers, placedMarker{row: row, col: col, idx: len(rasters)})
			rasters = append(rasters, rasterImage{key: key, blob: blob, rows: fr, cols: fc})
		}
	}
	// Escape sequences are only added once every image is in place:
	// cutting a line (spliceCells) carries the escape sequences of the part
	// it drops into what it keeps, so an earlier marker would be copied
	// along and its image painted twice. Markers go in right to left, so
	// each cut only ever drops text with no marker in it.
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].row != markers[j].row {
			return markers[i].row < markers[j].row
		}
		return markers[i].col > markers[j].col
	})
	for _, m := range markers {
		left, right := cutCells(lines[m.row], m.col)
		lines[m.row] = left + rasterMarker(m.idx) + right
	}
	if transmits.Len() > 0 && len(lines) > 0 {
		lines[0] = transmits.String() + lines[0]
	}
	return lines, rasters
}

// placedMarker is where a raster image's marker goes.
type placedMarker struct{ row, col, idx int }

// spliceCells replaces the cells [col, col+n) of line with repl (which is
// n cells wide), keeping the text on either side and its colors.
func spliceCells(line string, col, n int, repl string) string {
	left, _ := cutCells(line, col)
	if pad := col - ansi.StringWidth(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	_, right := cutCells(line, col+n)
	return left + "\x1b[0m" + repl + "\x1b[0m" + right
}

// cutCells splits line at display column col. Escape sequences stay on the
// side they're on (ansi.Truncate and TruncateLeft copy them across, which
// duplicated image markers and Kitty image data); only the colors in force
// at the cut (its SGR codes) are repeated at the start of the right part.
func cutCells(line string, col int) (left, right string) {
	var sgr strings.Builder
	w := 0
	i := 0
	for i < len(line) {
		if line[i] == '\x1b' {
			end := escapeEnd(line, i)
			if seq := line[i:end]; strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					sgr.Reset()
				}
				sgr.WriteString(seq)
			}
			i = end
			continue
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		cw := ansi.StringWidth(line[i : i+size])
		if cw > 0 && w+cw > col {
			break
		}
		w += cw
		i += size
	}
	return line[:i], sgr.String() + line[i:]
}

// escapeEnd is the index just past the escape sequence starting at i.
func escapeEnd(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI: parameters, then a final byte @..~
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']', '_', 'P', '^', 'X': // OSC, APC, DCS, PM, SOS: up to BEL or ST
		for j := i + 2; j < len(s); j++ {
			if s[j] == '\x07' {
				return j + 1
			}
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return i + 2
}

// blockCells draws an image as half-blocks, one string per row of cells.
func (a *App) blockCells(s *pluginAssets, key string, img image.Image, cols, rows int) []string {
	s.mu.Lock()
	if b, ok := s.blobs["blocks:"+key]; ok {
		s.mu.Unlock()
		return strings.Split(b, "\n")
	}
	s.mu.Unlock()
	bg := parseHexColor(a.theme.Colors.Background)
	small := scaleImage(img, cols, rows*2)
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			t, u := composite(small.NRGBAAt(c, r*2), bg), composite(small.NRGBAAt(c, r*2+1), bg)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", t.R, t.G, t.B, u.R, u.G, u.B)
		}
		b.WriteString("\x1b[0m")
		out[r] = b.String()
	}
	s.mu.Lock()
	s.blobs["blocks:"+key] = strings.Join(out, "\n")
	s.mu.Unlock()
	return out
}

// kittyResend is how long a Kitty image's data keeps being included after
// it's first needed: a frame can be replaced before the renderer flushes
// it, and repeating the data costs nothing once its line stops changing.
const kittyResend = time.Second

// kittyImage returns the Kitty image ID for an image drawn at a size, and
// its transmission while it should still be sent.
func (s *pluginAssets) kittyImage(key string, img image.Image, cols, rows int) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.kittyIDs[key]
	if !ok {
		s.kittyNext++
		id = s.kittyNext
		s.kittyIDs[key] = id
		var b bytes.Buffer
		cw, ch := cellPixels()
		_ = ansi.WriteKittyGraphics(&b, scaleImage(img, cols*cw, rows*ch), &kitty.Options{
			ID: id, Action: kitty.TransmitAndPut, Transmission: kitty.Direct, Format: kitty.PNG,
			VirtualPlacement: true, Columns: cols, Rows: rows, Quite: 2, Chunk: true,
		})
		s.blobs["kitty:"+key] = b.String()
		s.blobs["kitty-at:"+key] = time.Now().Format(time.RFC3339Nano)
	}
	at, _ := time.Parse(time.RFC3339Nano, s.blobs["kitty-at:"+key])
	if time.Since(at) < kittyResend {
		return id, s.blobs["kitty:"+key]
	}
	return id, ""
}

// kittyCells is one row of Kitty Unicode placeholders for image id: the
// foreground color carries the ID, and diacritics the row and column.
func kittyCells(id, row, cols int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	for c := 0; c < cols; c++ {
		b.WriteRune(kitty.Placeholder)
		b.WriteRune(kitty.Diacritic(row))
		b.WriteRune(kitty.Diacritic(c))
	}
	b.WriteString("\x1b[39m")
	return b.String()
}

// rasterBlob is an image encoded as Sixel or an iTerm2 inline image, drawn
// at cols×rows cells.
func (s *pluginAssets) rasterBlob(proto gfxProtocol, key string, img image.Image, cols, rows int) string {
	k := proto.String() + ":" + key
	s.mu.Lock()
	if b, ok := s.blobs[k]; ok {
		s.mu.Unlock()
		return b
	}
	s.mu.Unlock()
	cw, ch := cellPixels()
	pw, ph := cols*cw, rows*ch
	scaled := scaleImage(img, pw, ph)
	var blob string
	if proto == gfxSixel {
		var b bytes.Buffer
		_ = sixel.NewEncoder(&b).Encode(scaled)
		// go-sixel asks for background fill and gives no size, which some
		// terminals (Windows Terminal) paint far past the image. Ask for
		// transparent unset pixels and state the size instead.
		blob = fmt.Sprintf("\x1bP0;1;0q\"1;1;%d;%d", pw, ph) + strings.TrimPrefix(b.String(), "\x1bP0;0;8q\"1;1")
	} else {
		var b bytes.Buffer
		_ = png.Encode(&b, scaled)
		blob = fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=0;size=%d:%s\x07",
			cols, rows, b.Len(), base64.StdEncoding.EncodeToString(b.Bytes()))
	}
	s.mu.Lock()
	s.blobs[k] = blob
	s.mu.Unlock()
	return blob
}

// --- painting raster images over the final screen ---

// rasterRepaint is how long raster images keep being re-sent after the
// rows under them changed; see kittyResend for why.
const rasterRepaint = 300 * time.Millisecond

// rasterState tracks what was painted, to know when to paint again.
type rasterState struct {
	prevLines []string
	prevKeys  string
	dirtyAt   time.Time
	flip      bool
	hadImages bool
	clear     bool // the images went away: repaint the whole screen once
	// prevRects are where the last painted images were; erase clears old
	// pixels before a paint (see paintRasterImages).
	prevRects []cellRect
	erase     string
}

// cellRect is a box of cells on the screen.
type cellRect struct{ row, col, rows, cols int }

// paintRasterImages finds the raster markers in the final screen, removes
// them, and appends the images to the last line so they're painted after
// the rows Bubble Tea rewrote. Images are only re-sent when a row under
// them changed (and briefly after, in case that frame was never flushed).
//
// Everything appended is escape sequences only: Bubble Tea trims every
// line to the terminal's width, so any text appended here would be cut.
func (a *App) paintRasterImages(view string, rasters []rasterImage, overlay bool) string {
	st := &a.rasterState
	if !strings.Contains(view, "\x1b]8337;") {
		if st.hadImages {
			st.hadImages, st.clear = false, true
		}
		st.prevLines, st.prevRects, st.erase = nil, nil, ""
		return view
	}
	lines := strings.Split(view, "\n")
	type placed struct{ row, col, idx int }
	var found []placed
	for r, line := range lines {
		for {
			i := strings.Index(line, "\x1b]8337;")
			if i < 0 {
				break
			}
			end := strings.IndexByte(line[i:], '\x07')
			if end < 0 {
				break
			}
			var idx int
			fmt.Sscanf(line[i+len("\x1b]8337;"):i+end], "%d", &idx)
			if idx < len(rasters) {
				found = append(found, placed{r, ansi.StringWidth(line[:i]), idx})
			}
			line = line[:i] + line[i+end+1:]
		}
		lines[r] = line
	}
	if overlay || len(found) == 0 {
		return strings.Join(lines, "\n")
	}

	var keys strings.Builder
	var rects []cellRect
	dirty := len(st.prevLines) != len(lines)
	for _, p := range found {
		im := rasters[p.idx]
		fmt.Fprintf(&keys, "%s@%d,%d;", im.key, p.row, p.col)
		rects = append(rects, cellRect{p.row, p.col, im.rows, im.cols})
		for r := p.row; !dirty && r < p.row+im.rows && r < len(lines); r++ {
			dirty = st.prevLines[r] != lines[r]
		}
	}
	if keys.String() != st.prevKeys {
		// Images moved, changed or appeared. A transparent image doesn't
		// cover what was painted under it, and text Bubble Tea didn't
		// rewrite doesn't clear old pixels, so erase first: the cells of
		// every new image (blank by construction), and those of old images
		// that are still blank (if text moved there, its row was rewritten,
		// which already cleared them).
		dirty = true
		var e strings.Builder
		for _, rc := range rects {
			eraseRect(&e, rc)
		}
	old:
		for _, rc := range st.prevRects {
			for _, n := range rects {
				if n == rc {
					continue old
				}
			}
			if rectIsBlank(lines, rc) {
				eraseRect(&e, rc)
			}
		}
		st.erase = e.String()
	} else if dirty {
		st.erase = ""
	}
	st.prevRects = rects
	st.prevKeys = keys.String()
	st.prevLines = append(st.prevLines[:0], lines...)
	st.hadImages = true
	if dirty {
		st.dirtyAt = time.Now()
		st.flip = !st.flip
	}
	if time.Since(st.dirtyAt) > rasterRepaint {
		return strings.Join(lines, "\n")
	}
	var paint strings.Builder
	if st.flip { // keeps the last line different from the previous paint
		paint.WriteString("\x1b[m")
	} else {
		paint.WriteString("\x1b[0m")
	}
	paint.WriteString("\x1b7" + st.erase)
	for _, p := range found {
		paint.WriteString(ansi.CursorPosition(p.col+1, p.row+1))
		paint.WriteString(rasters[p.idx].blob)
	}
	paint.WriteString("\x1b8")
	lines[len(lines)-1] += paint.String()
	return strings.Join(lines, "\n")
}

// eraseRect erases a box of cells (ECH on each row), which also removes
// any image pixels there, without touching anything around it.
func eraseRect(b *strings.Builder, rc cellRect) {
	for r := rc.row; r < rc.row+rc.rows; r++ {
		b.WriteString(ansi.CursorPosition(rc.col+1, r+1))
		fmt.Fprintf(b, "\x1b[%dX", rc.cols)
	}
}

// rectIsBlank reports whether a box of cells holds only spaces now.
func rectIsBlank(lines []string, rc cellRect) bool {
	for r := rc.row; r < rc.row+rc.rows; r++ {
		if r >= len(lines) {
			continue
		}
		_, rest := cutCells(lines[r], rc.col)
		cells, _ := cutCells(rest, rc.cols)
		if strings.TrimSpace(ansi.Strip(cells)) != "" {
			return false
		}
	}
	return true
}

// --- image helpers ---

// scaleImage resizes src to w×h with a box filter, keeping alpha.
func scaleImage(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	for y := 0; y < h; y++ {
		y0, y1 := sb.Min.Y+y*sb.Dy()/h, sb.Min.Y+(y+1)*sb.Dy()/h
		for x := 0; x < w; x++ {
			x0, x1 := sb.Min.X+x*sb.Dx()/w, sb.Min.X+(x+1)*sb.Dx()/w
			var r, g, bl, al, n uint64
			for yy := y0; yy < max(y1, y0+1); yy++ {
				for xx := x0; xx < max(x1, x0+1); xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					r += uint64(c.R) * uint64(c.A)
					g += uint64(c.G) * uint64(c.A)
					bl += uint64(c.B) * uint64(c.A)
					al += uint64(c.A)
					n++
				}
			}
			if al > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r / al), uint8(g / al), uint8(bl / al), uint8(al / n)})
			}
		}
	}
	return dst
}

// composite flattens a pixel onto a background color.
func composite(c, bg color.NRGBA) color.NRGBA {
	a := int(c.A)
	mix := func(f, b uint8) uint8 { return uint8((int(f)*a + int(b)*(255-a)) / 255) }
	return color.NRGBA{mix(c.R, bg.R), mix(c.G, bg.G), mix(c.B, bg.B), 255}
}

// parseHexColor reads "#RRGGBB" (black when it's anything else, e.g. an
// ANSI palette index from the terminal-default theme).
func parseHexColor(s string) color.NRGBA {
	var r, g, b uint8
	if len(s) == 7 && s[0] == '#' {
		if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err == nil {
			return color.NRGBA{r, g, b, 255}
		}
	}
	return color.NRGBA{0, 0, 0, 255}
}

// --- sounds ---

// playPluginSound plays a sound a plugin asked for: fetched and decoded
// the first time, then from memory. Volume multiplies the member's
// plugin-sound volume; nothing plays while plugin sounds are muted.
func (a *App) playPluginSound(sc *ServerConnection, pluginID string, p protocol.PluginPlaySoundPayload) {
	if !sfxAvailable || a.audioConfig.PluginSoundsMuted {
		return
	}
	f, ok := clientFile(sc, pluginID, p.Asset)
	if !ok {
		return
	}
	gain := p.Volume
	if gain <= 0 || gain > 1 {
		gain = 1
	}
	gain *= a.audioConfig.PluginSoundVolume
	if a.sfx == nil {
		a.sfx = newSFXPlayer(a.audioConfig.OutputDevice)
	}
	store, player := a.assets(), a.sfx
	go func() {
		store.mu.Lock()
		pcm, failed := store.sounds[f.SHA256], store.failed[f.SHA256]
		store.mu.Unlock()
		if pcm == nil && failed == "" {
			if cmd := store.fetch(sc, pluginID, f); cmd != nil {
				if m, _ := cmd().(pluginAssetMsg); m.err != nil {
					log.Printf("plugin sound: %v", m.err)
					return
				}
			}
			store.mu.Lock()
			pcm = store.sounds[f.SHA256]
			store.mu.Unlock()
		}
		if pcm != nil {
			if err := player.Play(pcm, gain); err != nil {
				log.Printf("plugin sound: %v", err)
			}
		}
	}()
}
