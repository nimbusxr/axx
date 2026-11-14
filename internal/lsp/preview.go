package lsp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif" // pictures a step names
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/image/draw"
)

// The hover of a value that names a file: the file itself, its first lines or
// a picture's thumbnail, so that a scenario's seed, payload or picture reads
// without leaving the scenario. It shows the file the step reads, found as
// the link finds it (fileLinks), and only shows it: edits happen in the file.

const (
	// previewLines is how many lines of a text file the hover shows.
	previewLines = 40
	// previewLineWidth is the characters of a line it shows.
	previewLineWidth = 200
	// previewBytes is how much of a file it reads.
	previewBytes = 1 << 20
	// thumbnailWidth and thumbnailHeight bound a picture's thumbnail.
	thumbnailWidth, thumbnailHeight = 320, 240
	// maxPixels is the largest picture it makes a thumbnail of.
	maxPixels = 64 << 20
)

// fenceLanguages are the languages of the code blocks that show text files,
// by extension, for the editor's highlighting.
var fenceLanguages = map[string]string{
	".yaml": "yaml", ".yml": "yaml",
	".json": "json", ".axxmodel": "json", ".avsc": "json", ".gltf": "json", ".jsonl": "json", ".ndjson": "json",
	".csv": "csv", ".tsv": "tsv",
	".xml": "xml", ".svg": "xml", ".xsd": "xml", ".wsdl": "xml",
	".html": "html", ".htm": "html",
	".sql":     "sql",
	".graphql": "graphql", ".gql": "graphql",
	".proto":      "protobuf",
	".feature":    "gherkin",
	".md":         "markdown",
	".properties": "properties",
	".js":         "javascript", ".ts": "typescript",
	".sh":  "shell",
	".txt": "text", ".log": "text", ".pem": "text", ".crt": "text", ".key": "text",
}

// pictures are the extensions of the pictures the hover makes a thumbnail of.
var pictures = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

// thumbnails keeps the pictures' thumbnails, as hovers ask again and again.
var thumbnails = struct {
	sync.Mutex
	m map[thumbnailKey]string
}{m: map[thumbnailKey]string{}}

type thumbnailKey struct {
	path string
	size int64
	mod  time.Time
}

// preview is the Markdown of a hover that shows the file at path.
func (p *project) preview(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("`%s`: %v", p.rel(path), err)
	}
	head := fmt.Sprintf("**`%s`** · %s", p.rel(path), size(info.Size()))
	ext := strings.ToLower(filepath.Ext(path))
	if pictures[ext] {
		if md, dims := thumbnail(path, info); md != "" {
			return fmt.Sprintf("**`%s`** · %s, %s\n\n%s", p.rel(path), dims, size(info.Size()), md)
		}
	}
	data, whole, err := readHead(path)
	if err != nil {
		return head + "\n\n" + err.Error()
	}
	if !isText(data, whole) {
		return head + "\n\nNo preview: the file is neither text nor a picture."
	}
	lang := fenceLanguages[ext]
	if lang == "json" && whole {
		data = indented(data)
	}
	return head + "\n\n" + codeBlock(lang, string(data), whole)
}

// readHead reads the first previewBytes of a file, and whether that is all
// of it.
func readHead(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, previewBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > previewBytes {
		return data[:previewBytes], false, nil
	}
	return data, true, nil
}

// isText reports whether data, the head of a file (all of it when whole), is
// text: UTF-8 without NUL bytes.
func isText(data []byte, whole bool) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	if !whole {
		// The head may end inside a character.
		for i := 0; i < utf8.UTFMax && len(data) > 0 && !utf8.Valid(data); i++ {
			data = data[:len(data)-1]
		}
	}
	return utf8.Valid(data)
}

// indented is JSON that has long lines, a stub's or a minified payload's,
// indented, so that its first lines show its structure. Other JSON stays
// as written.
func indented(data []byte) []byte {
	long := false
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if len(line) > previewLineWidth {
			long = true
			break
		}
	}
	var b bytes.Buffer
	if !long || json.Indent(&b, data, "", "  ") != nil {
		return data
	}
	return b.Bytes()
}

// codeBlock is a fenced code block of the first previewLines lines of text,
// each cut at previewLineWidth characters, and how many lines it leaves out.
func codeBlock(lang, text string, whole bool) string {
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := strings.Split(text, "\n")
	shown := lines[:min(len(lines), previewLines)]
	for i, l := range shown {
		if utf8.RuneCountInString(l) > previewLineWidth {
			shown[i] = string([]rune(l)[:previewLineWidth]) + "…"
		}
	}
	body := strings.Join(shown, "\n")
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	out := fence + lang + "\n" + body + "\n" + fence
	switch rest := len(lines) - len(shown); {
	case !whole:
		out += "\n\n… and the rest of the file"
	case rest == 1:
		out += "\n\n… and 1 more line"
	case rest > 1:
		out += fmt.Sprintf("\n\n… and %d more lines", rest)
	}
	return out
}

// thumbnail is the Markdown image of a picture's thumbnail, in a data URI
// (editors show no local file in a hover), and the picture's size in pixels.
func thumbnail(path string, info os.FileInfo) (string, string) {
	key := thumbnailKey{path, info.Size(), info.ModTime()}
	thumbnails.Lock()
	md, ok := thumbnails.m[key]
	thumbnails.Unlock()
	if !ok {
		md = makeThumbnail(path)
		thumbnails.Lock()
		if len(thumbnails.m) >= 64 {
			clear(thumbnails.m)
		}
		thumbnails.m[key] = md
		thumbnails.Unlock()
	}
	if md == "" {
		return "", ""
	}
	dims, md, _ := strings.Cut(md, "\n")
	return md, dims
}

// makeThumbnail makes a thumbnail's Markdown, after a line of the picture's
// size; "" when the file is no picture it can read.
func makeThumbnail(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return ""
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return ""
	}
	w, h := cfg.Width, cfg.Height
	if w > thumbnailWidth || h > thumbnailHeight {
		scale := min(float64(thumbnailWidth)/float64(w), float64(thumbnailHeight)/float64(h))
		w, h = max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var b bytes.Buffer
	mime := "image/png"
	if dst.Opaque() {
		mime = "image/jpeg"
		err = jpeg.Encode(&b, dst, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&b, dst)
	}
	if err != nil {
		return ""
	}
	name := strings.NewReplacer("[", "", "]", "").Replace(filepath.Base(path))
	return fmt.Sprintf("%d × %d\n![%s](data:%s;base64,%s)", cfg.Width, cfg.Height, name, mime, base64.StdEncoding.EncodeToString(b.Bytes()))
}

// size is a file's size for people: 418 bytes, 84 KB, 1.2 MB.
func size(n int64) string {
	switch {
	case n == 1:
		return "1 byte"
	case n < 1000:
		return fmt.Sprintf("%d bytes", n)
	case n < 1000*1000:
		return fmt.Sprintf("%d KB", (n+500)/1000)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
}
