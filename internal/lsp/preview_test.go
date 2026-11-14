package lsp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strings"
	"testing"
)

// pngOf is a PNG of w×h pixels, opaque or half transparent.
func pngOf(t *testing.T, w, h int, opaque bool) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	a := uint8(255)
	if !opaque {
		a = 128
	}
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 200, a})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestHoverShowsTheFileAValueNames(t *testing.T) {
	var long strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&long, "- parcel: PX-%04d\n", i)
	}
	stub := `{"version":1,"api":"android.mlkit.text-recognition","keys":[[0.25,0.5,0.75]],"answers":[{"text":"PX-MOB-9501"}],"note":"` + strings.Repeat("x", 300) + `"}`
	_, uri := writeProject(t, map[string]string{
		"axx.yaml":                             "version: 1\n",
		"axx-packs.yaml":                       packsFile,
		"features/seeds/manifest-kestrel.yaml": "manifest_lines:\n  - id: ML-KES-0412-1\n    status: NEW\n",
		"features/seeds/long.yaml":             long.String(),
		"features/models/torn-label.axxmodel":  stub,
		"features/seeds/fences.md":             "a ``` fence\n",
		"features/labels/label.png":            pngOf(t, 800, 600, true),
		"features/labels/sticker.png":          pngOf(t, 100, 50, false),
		"features/labels/lifted.mp4":           "\x00\x00\x00\x18ftypmp42",
		"features/seeds/large.yaml":            strings.Repeat("parcels: []\n", previewBytes/12+10),
	})
	text := `Feature: Previews

  Scenario: seeded
    Given a seeds/manifest-kestrel.yaml db seed
    And a seeds/long.yaml db seed
    And a models/torn-label.axxmodel db seed
    And a seeds/fences.md db seed
    And a labels/label.png db seed
    And a labels/sticker.png db seed
    And a labels/lifted.mp4 db seed
    And a seeds/large.yaml db seed

  Scenario Outline: outline
    Given a <seed> db seed

    Examples:
      | seed                          |
      | seeds/manifest-kestrel.yaml |
`
	c := startServer(t)
	c.open(uri, text)
	lines := strings.Split(text, "\n")
	hoverOn := func(value string, line int) hover {
		t.Helper()
		ch := strings.Index(lines[line], value) + 3
		raw := c.request("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": ch}})
		var h hover
		if err := json.Unmarshal(raw, &h); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		return h
	}

	t.Run("a text file", func(t *testing.T) {
		h := hoverOn("seeds/manifest-kestrel.yaml", 3)
		want := "**`features/seeds/manifest-kestrel.yaml`** · 54 bytes\n\n```yaml\nmanifest_lines:\n  - id: ML-KES-0412-1\n    status: NEW\n```"
		if h.Contents.Value != want {
			t.Errorf("hover:\n%s\nwant:\n%s", h.Contents.Value, want)
		}
		// The hover is the path's, not the step's.
		if h.Range == nil || *h.Range != span(lines, 3, 12, 12+len("seeds/manifest-kestrel.yaml")) {
			t.Errorf("range %+v", h.Range)
		}
	})

	t.Run("a long file's first lines", func(t *testing.T) {
		v := hoverOn("seeds/long.yaml", 4).Contents.Value
		if !strings.Contains(v, "- parcel: PX-0040\n```") || strings.Contains(v, "PX-0041") || !strings.HasSuffix(v, "… and 60 more lines") {
			t.Errorf("hover:\n%s", v)
		}
	})

	t.Run("JSON on one long line, indented", func(t *testing.T) {
		v := hoverOn("models/torn-label.axxmodel", 5).Contents.Value
		if !strings.Contains(v, "```json\n{\n  \"version\": 1,\n  \"api\": \"android.mlkit.text-recognition\",") {
			t.Errorf("hover:\n%s", v)
		}
	})

	t.Run("a fence in the file", func(t *testing.T) {
		v := hoverOn("seeds/fences.md", 6).Contents.Value
		if !strings.Contains(v, "````markdown\na ``` fence\n````") {
			t.Errorf("hover:\n%s", v)
		}
	})

	thumbnailOf := func(v string) (string, image.Config) {
		t.Helper()
		m := regexp.MustCompile(`!\[[^\]]*\]\(data:(image/[a-z]+);base64,([A-Za-z0-9+/=]+)\)`).FindStringSubmatch(v)
		if m == nil {
			t.Fatalf("no thumbnail in:\n%.300s", v)
		}
		data, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if "image/"+format != m[1] {
			t.Errorf("a %s in a %s data URI", format, m[1])
		}
		return m[1], cfg
	}

	t.Run("a picture's thumbnail", func(t *testing.T) {
		v := hoverOn("labels/label.png", 7).Contents.Value
		if !strings.HasPrefix(v, "**`features/labels/label.png`** · 800 × 600, ") {
			t.Errorf("hover:\n%.200s", v)
		}
		// An opaque picture's thumbnail is a JPEG, within 320×240.
		mime, cfg := thumbnailOf(v)
		if mime != "image/jpeg" || cfg.Width != 320 || cfg.Height != 240 {
			t.Errorf("%s of %d×%d", mime, cfg.Width, cfg.Height)
		}
	})

	t.Run("a transparent picture's", func(t *testing.T) {
		v := hoverOn("labels/sticker.png", 8).Contents.Value
		// Its transparency kept, and no larger than it is.
		mime, cfg := thumbnailOf(v)
		if mime != "image/png" || cfg.Width != 100 || cfg.Height != 50 {
			t.Errorf("%s of %d×%d", mime, cfg.Width, cfg.Height)
		}
	})

	t.Run("a file that is neither", func(t *testing.T) {
		want := "**`features/labels/lifted.mp4`** · 12 bytes\n\nNo preview: the file is neither text nor a picture."
		if v := hoverOn("labels/lifted.mp4", 9).Contents.Value; v != want {
			t.Errorf("hover:\n%s", v)
		}
	})

	t.Run("a file too large to read", func(t *testing.T) {
		v := hoverOn("seeds/large.yaml", 10).Contents.Value
		if !strings.HasPrefix(v, "**`features/seeds/large.yaml`** · 1.0 MB\n\n```yaml\nparcels: []\n") || !strings.HasSuffix(v, "… and the rest of the file") {
			t.Errorf("hover:\n%.200s…%s", v, v[max(0, len(v)-100):])
		}
	})

	t.Run("an Examples cell", func(t *testing.T) {
		if v := hoverOn("seeds/manifest-kestrel.yaml", 17).Contents.Value; !strings.Contains(v, "status: NEW") {
			t.Errorf("hover:\n%s", v)
		}
	})

	t.Run("the rest of the step", func(t *testing.T) {
		v := hoverOn("db seed", 3).Contents.Value
		if !strings.Contains(v, "sql") || strings.Contains(v, "status: NEW") {
			t.Errorf("hover:\n%s", v)
		}
	})
}
