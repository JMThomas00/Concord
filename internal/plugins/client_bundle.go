package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JMThomas00/Concord/sdk/codesign"
)

// A plugin's client part (To Do item D): files in its client/ folder that
// members' clients download, verify and use: images and sounds now,
// WebAssembly code later. Concord decodes every file itself, so only file
// types it knows how to handle are served.

// ClientDef is [client] in plugin.toml. Every key is optional: a plugin
// with a client/ folder of images and sounds needs no [client] at all.
type ClientDef struct {
	// WASM is client code, relative to the plugin folder (e.g.
	// "client/plugin.wasm"). It requires PublisherKey.
	WASM string `toml:"wasm"`
	// Capabilities the client code asks for; the member approves them.
	Capabilities []string `toml:"capabilities"`
	// PublisherKey identifies who signs the plugin: "ed25519:<base64>".
	PublisherKey string `toml:"publisher_key"`
}

// ClientCapabilities are the capabilities client code can ask for.
var ClientCapabilities = map[string]string{
	"pane":    "draw in its pane",
	"images":  "show images",
	"sound":   "play sounds",
	"storage": "keep a little data on your computer",
	"server":  "talk to its server-side part",
}

func (c ClientDef) validate(pluginID string) error {
	for _, cap := range c.Capabilities {
		if _, ok := ClientCapabilities[cap]; !ok {
			return fmt.Errorf("plugin %q: unknown [client] capability %q", pluginID, cap)
		}
	}
	if c.PublisherKey != "" {
		if _, err := ParsePublisherKey(c.PublisherKey); err != nil {
			return fmt.Errorf("plugin %q: [client].publisher_key: %w", pluginID, err)
		}
	}
	if c.WASM != "" {
		if !strings.HasPrefix(filepath.ToSlash(c.WASM), ClientDir+"/") || !strings.HasSuffix(c.WASM, ".wasm") {
			return fmt.Errorf("plugin %q: [client].wasm must be a .wasm file in client/", pluginID)
		}
		if c.PublisherKey == "" {
			return fmt.Errorf("plugin %q: [client].wasm needs a publisher_key", pluginID)
		}
	}
	return nil
}

// ParsePublisherKey decodes "ed25519:<base64 of 32 bytes>".
func ParsePublisherKey(s string) ([]byte, error) {
	return codesign.ParsePublicKey(s)
}

// VerifyClientCode checks a plugin's client code (in the plugin folder dir)
// against its signature file, <wasm>.sig, and publisher key. Plugins without
// client code pass.
func VerifyClientCode(dir string, c ClientDef) error {
	if c.WASM == "" {
		return nil
	}
	module, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c.WASM)))
	if err != nil {
		return fmt.Errorf("client code: %w", err)
	}
	sig, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c.WASM)+".sig"))
	if err != nil {
		return fmt.Errorf("client code has no signature (%s.sig): sign it with concord-plugin sign", c.WASM)
	}
	if err := codesign.Verify(c.PublisherKey, module, sig); err != nil {
		return fmt.Errorf("client code: %w", err)
	}
	return nil
}

// ClientDir is the folder, inside a plugin's folder, that holds its client part.
const ClientDir = "client"

// MaxClientBundleBytes caps a plugin's client part.
const MaxClientBundleBytes = 50 << 20

// clientTypes are the file types a client part may contain, by extension,
// with the Content-Type they're served as.
var clientTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".opus": "audio/ogg",
	".wasm": "application/wasm",
	".json": "application/json",
	".sig":  "text/plain", // a .wasm file's signature (sdk/codesign)
}

// ClientContentType is the Content-Type for a client file, or "" when the
// type isn't allowed.
func ClientContentType(name string) string {
	return clientTypes[strings.ToLower(path.Ext(name))]
}

// ClientFile is one file of a client part. Path is relative to client/,
// with forward slashes.
type ClientFile struct {
	Path   string
	Size   int64
	SHA256 string
}

// ClientBundle is a plugin's indexed client part.
type ClientBundle struct {
	Files []ClientFile
	Hash  string // over every file's path and hash
	Size  int64
}

// File returns the indexed file at rel, if there is one.
func (b *ClientBundle) File(rel string) (ClientFile, bool) {
	i := sort.Search(len(b.Files), func(i int) bool { return b.Files[i].Path >= rel })
	if i < len(b.Files) && b.Files[i].Path == rel {
		return b.Files[i], true
	}
	return ClientFile{}, false
}

// hashCache keeps file hashes between indexings, keyed by absolute path,
// and is reused while a file's size and modification time are unchanged.
var hashCache = struct {
	sync.Mutex
	m map[string]cachedHash
}{m: map[string]cachedHash{}}

type cachedHash struct {
	size int64
	mod  time.Time
	sum  string
}

// IndexClientBundle indexes the client/ folder of the plugin in dir: every
// allowed file's path, size and SHA-256. It returns nil, nil when there's
// no client/ folder. Dot-files and files of other types are skipped, and a
// folder over MaxClientBundleBytes is an error.
func IndexClientBundle(dir string) (*ClientBundle, error) {
	root := filepath.Join(dir, ClientDir)
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, nil
	}
	b := &ClientBundle{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || ClientContentType(d.Name()) == "" {
			return nil // not a regular file of a known type (symlinks included)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.Size += info.Size()
		if b.Size > MaxClientBundleBytes {
			return fmt.Errorf("client/ is over %d MB", MaxClientBundleBytes>>20)
		}
		sum, err := fileHash(p, info)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b.Files = append(b.Files, ClientFile{Path: filepath.ToSlash(rel), Size: info.Size(), SHA256: sum})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(b.Files) == 0 {
		return nil, nil
	}
	sort.Slice(b.Files, func(i, j int) bool { return b.Files[i].Path < b.Files[j].Path })
	h := sha256.New()
	for _, f := range b.Files {
		fmt.Fprintf(h, "%s\x00%s\n", f.Path, f.SHA256)
	}
	b.Hash = hex.EncodeToString(h.Sum(nil))
	return b, nil
}

func fileHash(p string, info fs.FileInfo) (string, error) {
	hashCache.Lock()
	c, ok := hashCache.m[p]
	hashCache.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.sum, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	hashCache.Lock()
	hashCache.m[p] = cachedHash{info.Size(), info.ModTime(), sum}
	hashCache.Unlock()
	return sum, nil
}

// ClientFilePath is where an indexed client file lives on disk, or "" if
// rel isn't a file of the bundle (so nothing outside client/ is reachable).
func ClientFilePath(dir string, b *ClientBundle, rel string) string {
	if b == nil {
		return ""
	}
	if _, ok := b.File(rel); !ok {
		return ""
	}
	return filepath.Join(dir, ClientDir, filepath.FromSlash(rel))
}
