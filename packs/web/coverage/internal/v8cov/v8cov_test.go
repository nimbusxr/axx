package v8cov

import (
	"bytes"
	"encoding/json"
	"maps"
	"math/rand/v2"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The cases of testdata: V8's takes of scripts (takes.json, recorded from
// Chromium), and what Node's tools make of them (record.js): the takes
// merged, and the three reports.
var cases = []string{
	"basic",  // one take of each script of a page
	"takes",  // takes of three scenarios, with navigations, merged
	"bundle", // two bundles of the same sources, through their source maps
	"fuzz",   // random nested ranges, several takes a script, UTF-16 and CRLF
	"pages",  // what the pack took of its test site's pages: a bundle, dialogs, navigations
}

type take struct {
	ScriptID  string     `json:"scriptId"`
	URL       string     `json:"url"`
	Source    string     `json:"source"`
	Functions []Function `json:"functions"`
}

var sourceMappingURL = regexp.MustCompile(`(?m)^[ \t]*//[#@][ \t]+sourceMappingURL=(\S+)[ \t]*$`)

func TestNodeToolsAgree(t *testing.T) {
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("testdata", name)
			var takes []take
			readJSON(t, filepath.Join(dir, "takes.json"), &takes)
			// The takes of each script, as record.js groups them: by URL
			// path, files only.
			byPath := map[string][][]Function{}
			sources := map[string]string{}
			for _, tk := range takes {
				u, err := url.Parse(tk.URL)
				if err != nil {
					t.Fatal(err)
				}
				if !regexp.MustCompile(`\.m?js$`).MatchString(u.Path) {
					continue
				}
				byPath[u.Path] = append(byPath[u.Path], tk.Functions)
				sources[u.Path] = tk.Source
			}
			var merged struct {
				Result []struct {
					URL       string     `json:"url"`
					Functions []Function `json:"functions"`
				} `json:"result"`
			}
			readJSON(t, filepath.Join(dir, "merged.json"), &merged)
			if len(merged.Result) != len(byPath) {
				t.Fatalf("%d scripts merged, want %d", len(byPath), len(merged.Result))
			}
			files := map[string]*FileCoverage{}
			// In the order of the merged scripts: by URL.
			for _, want := range merged.Result {
				takes, ok := byPath[want.URL]
				if !ok {
					t.Fatalf("no takes of %s", want.URL)
				}
				functions := Merge(takes)
				if !reflect.DeepEqual(functions, want.Functions) {
					t.Errorf("%s merged differs:\n got %v\nwant %v", want.URL, functions, want.Functions)
				}
				source := sources[want.URL]
				var sm *SourceMap
				if m := sourceMappingURL.FindAllStringSubmatch(source, -1); m != nil {
					b, err := os.ReadFile(filepath.Join(dir, "site", filepath.FromSlash(path.Join(path.Dir(want.URL), m[len(m)-1][1]))))
					if err != nil {
						t.Fatal(err)
					}
					if sm, err = ParseSourceMap(b); err != nil {
						t.Fatal(err)
					}
				}
				// Paths as record.js has them, in a project the site is a folder of.
				c, err := NewConverter("/project/site"+want.URL, source, sm, nil)
				if err != nil {
					t.Fatal(err)
				}
				c.Apply(functions)
				for p, fc := range c.Istanbul() {
					fc.Path = strings.TrimPrefix(p, "/project/")
					if prev, ok := files[fc.Path]; ok {
						prev.Merge(fc)
					} else {
						files[fc.Path] = fc
					}
				}
			}
			var list []*FileCoverage
			for _, p := range slices.Sorted(maps.Keys(files)) {
				list = append(list, files[p])
			}
			same(t, filepath.Join(dir, "lcov.info"), Lcov(list))
			same(t, filepath.Join(dir, "coverage-final.json"), FinalJSON(list))
			same(t, filepath.Join(dir, "coverage-summary.json"), SummaryJSON(list))
		})
	}
}

func TestMergeLeavesTheTakesAsTheyAre(t *testing.T) {
	var takes []take
	readJSON(t, filepath.Join("testdata", "fuzz", "takes.json"), &takes)
	var fs [][]Function
	for _, tk := range takes {
		if tk.URL == takes[0].URL {
			fs = append(fs, tk.Functions)
		}
	}
	before, _ := json.Marshal(fs)
	Merge(fs)
	Merge(fs[:1])
	after, _ := json.Marshal(fs)
	if !bytes.Equal(before, after) {
		t.Error("merging changed the takes")
	}
}

// Takes that come several times merge as their copies do, in any order.
func TestCountedTakesMergeAsTheirCopies(t *testing.T) {
	for _, name := range cases {
		var takes []take
		readJSON(t, filepath.Join("testdata", name, "takes.json"), &takes)
		byURL := map[string][][]Function{}
		for _, tk := range takes {
			byURL[tk.URL] = append(byURL[tk.URL], tk.Functions)
		}
		rnd := rand.New(rand.NewPCG(1, uint64(len(takes))))
		for u, fs := range byURL {
			times := make([]int, len(fs))
			var copies [][]Function
			for i := range fs {
				times[i] = 1 + rnd.IntN(4)
				for range times[i] {
					copies = append(copies, fs[i])
				}
			}
			want := Merge(copies)
			if got := MergeCounted(fs, times); !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: counted %v\nwant %v", name, u, got, want)
			}
			// In another order: the same ranges (a function's name is the
			// first take's, and the fuzz takes name functions at random).
			rnd.Shuffle(len(copies), func(i, j int) { copies[i], copies[j] = copies[j], copies[i] })
			if got := Merge(copies); !reflect.DeepEqual(unnamed(got), unnamed(want)) {
				t.Errorf("%s %s: shuffled %v\nwant %v", name, u, got, want)
			}
			one := make([]int, len(fs))
			for i := range one {
				one[i] = 1
			}
			if got, want := MergeCounted(fs, one), Merge(fs); !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: once each %v\nwant %v", name, u, got, want)
			}
		}
	}
}

func unnamed(fs []Function) []Function {
	out := slices.Clone(fs)
	for i := range out {
		out[i].FunctionName = ""
	}
	return out
}

func TestSourcesAreNamedAsTraceMappingNamesThem(t *testing.T) {
	var cases [][2]string
	readJSON(t, filepath.Join("testdata", "uri.json"), &cases)
	for _, c := range cases {
		if got := resolveURI(c[0]); got != c[1] {
			t.Errorf("%q: %q, want %q", c[0], got, c[1])
		}
	}
}

func TestABrokenSourceMapIsAnError(t *testing.T) {
	for _, m := range []string{
		`{"version": 2, "sources": [], "mappings": ""}`,
		`{"version": 3, "sections": []}`,
		`{"version": 3, "sources": ["a.ts"], "mappings": "AAAA!"}`,
		`{"version": 3, "sources": ["a.ts"], "mappings": "AAAAg"}`,
		`not json`,
	} {
		if _, err := ParseSourceMap([]byte(m)); err == nil {
			t.Errorf("%s: no error", m)
		}
	}
}

func TestAMapThatPointsNowhereConvertsWithoutFailing(t *testing.T) {
	// Mappings to a source and lines the map has not.
	sm, err := ParseSourceMap([]byte(`{"version": 3, "sources": ["a.ts"], "sourcesContent": ["let a = 1;\n"], "mappings": "AAAA,EAAE;ACAA,EAAoB;AAAoB"}`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewConverter("/app.js", "var a = 1;\nvar b = 2;\nvar c = 3;\n", sm, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Apply([]Function{{Ranges: []Range{{0, 33, 1}, {11, 22, 0}}, IsBlockCoverage: true}})
	if files := c.Istanbul(); len(files) != 1 || files["/a.ts"] == nil {
		t.Errorf("files: %v", files)
	}
}

func TestOriginalsWithoutContentAreRead(t *testing.T) {
	sm, err := ParseSourceMap([]byte(`{"version": 3, "sources": ["../src/a.ts", "../src/b.ts"], "mappings": "AAAA;ACAA"}`))
	if err != nil {
		t.Fatal(err)
	}
	var read []string
	c, err := NewConverter("/dist/app.js", "a();\nb();\n", sm, func(p string) (string, error) {
		read = append(read, p)
		return "a();\n", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/src/a.ts", "/src/b.ts"}; !slices.Equal(read, want) {
		t.Errorf("read %v, want %v", read, want)
	}
	c.Apply([]Function{{Ranges: []Range{{0, 10, 1}}, IsBlockCoverage: true}})
	if files := c.Istanbul(); files["/src/a.ts"] == nil || files["/src/b.ts"] == nil {
		t.Errorf("files: %v", files)
	}
	if _, err := NewConverter("/dist/app.js", "a();\n", sm, nil); err == nil {
		t.Error("no error without the originals' content")
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// same checks that got is the recorded file, byte for byte.
func same(t *testing.T, path string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, want) {
		return
	}
	gl, wl := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := range max(len(gl), len(wl)) {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Errorf("%s differs from line %d:\n got %.300s\nwant %.300s", path, i+1, g, w)
			return
		}
	}
}
