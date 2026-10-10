// Package recording keeps what a scenario's apps showed as it ran, for the
// packs that drive apps on a screen (desktop, mobile), as the web pack keeps
// its pages: a trace, one HTML page with each app's screen after each step;
// a video, the scenario's screen as it ran with its steps beside it. Each pack
// captures its own screens; this package keeps them the same way for all.
package recording

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Keeps is whether a policy (failed, always, never) keeps a scenario's file.
func Keeps(policy string, failed bool) bool {
	return policy == "always" || policy == "failed" && failed
}

// TraceStep is a step of a trace: the step, and an app's screen after it.
type TraceStep struct {
	Keyword, Text string
	Line          int
	At            time.Duration
	// Image is the screen, encoded as MIME (image/png when empty); none when
	// it could not be captured.
	Image []byte
	MIME  string
	// Scale is the display's, which the page shows the screen at.
	Scale float64
}

// WriteTrace keeps a trace of the app in .axx/<area>/traces, logs where it is
// and attaches it to the report. controls are the app's controls, as the
// scenario failed.
func WriteTrace(sc *core.Scenario, area, app string, steps []TraceStep, failed bool, controls string) {
	path := Path(sc, area, "traces", app, ".html")
	if err := os.WriteFile(path, TraceHTML(sc, app, steps, failed, controls), 0o644); err != nil {
		return
	}
	sc.Log("the %s app's trace: %s (open it in a browser)", app, Relative(sc, path))
	sc.Attach("text/html", mustRead(path), "the "+app+" app's trace")
	Announce(sc, "trace", path)
}

// TraceHTML is a trace's page: each step, and the app's screen after it.
func TraceHTML(sc *core.Scenario, app string, steps []TraceStep, failed bool, controls string) []byte {
	var b strings.Builder
	esc := html.EscapeString
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>%s · %s</title><style>
:root{color-scheme:light dark;--fg:#1d2330;--muted:#5b6475;--bg:#f6f7f9;--card:#fff;--line:#d9dde5;--bad:#c62828}
@media (prefers-color-scheme:dark){:root{--fg:#e6e8ee;--muted:#9aa3b5;--bg:#14171d;--card:#1d222b;--line:#2c333f;--bad:#ef5350}}
body{margin:0;padding:24px 16px;background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,sans-serif}
main{max-width:1100px;margin:0 auto;display:grid;gap:16px}
h1{font-size:20px;margin:0}p{margin:0;color:var(--muted)}
section{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:12px 16px;display:grid;gap:8px}
section.failed{border-color:var(--bad)}
h2{font-size:15px;margin:0;font-weight:600}h2 b{font-weight:700}h2 span{color:var(--muted);font-weight:400;font-size:13px}
img{max-width:100%%;height:auto;border:1px solid var(--line);border-radius:4px}
pre{margin:0;overflow:auto;font-size:12px;max-height:480px}
</style></head><body><main><h1>%s</h1><p>The %s app after each step of %s (%s:%d).</p>`,
		esc(sc.Name), esc(app), esc(sc.Name), esc(app), esc(sc.Name), esc(sc.URI), sc.Line)
	for i, st := range steps {
		class, status := "", ""
		if failed && i == len(steps)-1 {
			class, status = ` class="failed"`, " · failed"
		}
		if len(st.Image) == 0 {
			fmt.Fprintf(&b, `<section%s><h2><b>%s</b> %s <span>line %d · %.1fs%s</span></h2><p>No capture of the screen.</p></section>`,
				class, esc(st.Keyword), esc(st.Text), st.Line, st.At.Seconds(), status)
			continue
		}
		mime := st.MIME
		if mime == "" {
			mime = "image/png"
		}
		size := ""
		if w, h, ok := pngSize(st.Image); ok && st.Scale > 1 {
			size = fmt.Sprintf(` width="%d" height="%d"`, int(float64(w)/st.Scale), int(float64(h)/st.Scale))
		}
		fmt.Fprintf(&b, `<section%s><h2><b>%s</b> %s <span>line %d · %.1fs%s</span></h2><img alt="the %s app after this step"%s src="data:%s;base64,%s"></section>`,
			class, esc(st.Keyword), esc(st.Text), st.Line, st.At.Seconds(), status, esc(app), size, mime, base64.StdEncoding.EncodeToString(st.Image))
	}
	if controls != "" {
		fmt.Fprintf(&b, `<section class="failed"><h2>The %s app's controls, as the scenario failed</h2><pre>%s</pre></section>`, esc(app), esc(controls))
	}
	b.WriteString("</main></body></html>\n")
	return []byte(b.String())
}

// pngSize is a PNG's width and height, from its header.
func pngSize(b []byte) (w, h int, ok bool) {
	if len(b) < 24 || string(b[1:4]) != "PNG" || string(b[12:16]) != "IHDR" {
		return 0, 0, false
	}
	return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24])), true
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9]+`)

// Path is where a scenario keeps a file of an app, in .axx/<area>/<kind>
// (.axx/desktop/videos), as the web pack keeps its pages'.
func Path(sc *core.Scenario, area, kind, app, ext string) string {
	dir := filepath.Join(sc.Suite().ProjectDir(), ".axx", area, kind)
	_ = os.MkdirAll(dir, 0o755)
	name := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(sc.Name), "-"), "-")
	if len(name) > 60 {
		name = name[:60]
	}
	id := unsafeName.ReplaceAllString(sc.ID, "")
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	if app == "" {
		return filepath.Join(dir, fmt.Sprintf("%s-%s%s", name, id, ext))
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s-%s%s", name, app, id, ext))
}

// Relative is a path from the project's folder, when it is in it.
func Relative(sc *core.Scenario, path string) string {
	if rel, err := filepath.Rel(sc.Suite().ProjectDir(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

// Announce tells the run's reporters of a file the scenario kept.
func Announce(sc *core.Scenario, kind, path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sc.Suite().Announce(kind, "path", abs, "location", fmt.Sprintf("%s:%d", sc.URI, sc.Line))
}

func mustRead(path string) []byte {
	b, _ := os.ReadFile(path)
	return b
}
