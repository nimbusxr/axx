package report

import (
	"bytes"
	"compress/gzip"
	_ "embed" // HTML formatter assets
	"encoding/json"
	"io"
	"strings"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/runner"
)

// The html reporter reproduces @cucumber/html-formatter (see
// THIRD_PARTY_NOTICES.md for the vendored version). The files in html/ are
// the package's prebuilt assets, unmodified except that main.js is stored
// gzip-compressed to keep the binary small. To update them:
//
//	npm pack @cucumber/html-formatter && tar xzf cucumber-html-formatter-*.tgz
//	cp package/dist/src/index.mustache package/dist/src/icon.url package/dist/main.css package/LICENSE internal/report/html/
//	gzip -9 -n -c package/dist/main.js > internal/report/html/main.js.gz
//
// then update the version and checksum in THIRD_PARTY_NOTICES.md and
// htmlJSSHA256 in report_test.go.
var (
	//go:embed html/index.mustache
	htmlTemplate string
	//go:embed html/main.css
	htmlCSS string
	//go:embed html/icon.url
	htmlIcon string
	//go:embed html/main.js.gz
	htmlJSGzip []byte
)

// htmlAssets are the pieces of the page; tests substitute small stand-ins.
type htmlAssets struct {
	template string
	css      string
	icon     string
	js       func(w io.Writer) error
}

var embeddedAssets = htmlAssets{
	template: htmlTemplate,
	css:      htmlCSS,
	icon:     htmlIcon,
	js: func(w io.Writer) error {
		zr, err := gzip.NewReader(bytes.NewReader(htmlJSGzip))
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, zr); err != nil {
			return err
		}
		return zr.Close()
	},
}

// htmlTitle is the page title (the Node formatter's default is "Cucumber").
const htmlTitle = "axx"

// htmlReporter streams envelopes into the page as they arrive, exactly like
// CucumberHtmlStream: the template up to the messages array is written
// before the first envelope, envelopes are comma-separated JSON, and the
// script is written on Finish.
type htmlReporter struct {
	out   *writer
	a     htmlAssets
	pre   bool
	post  bool
	first bool
	err   error
}

func newHTML(out *writer, a htmlAssets) *htmlReporter {
	return &htmlReporter{out: out, a: a}
}

func (h *htmlReporter) Envelope(e *messages.Envelope) {
	if h.post {
		return
	}
	h.writePre()
	b, err := json.Marshal(e)
	if err != nil {
		if h.err == nil {
			h.err = err
		}
		return
	}
	if h.first {
		h.out.put(",")
	}
	h.first = true
	// Like the Node formatter, never let "<" into the script element
	// (json.Marshal already escapes it as <; this is belt and braces).
	h.out.put(strings.ReplaceAll(string(b), "<", `\x3C`))
}

func (h *htmlReporter) ScenarioFinished(*runner.ScenarioResult) {}

func (h *htmlReporter) Finish(*runner.RunResult) error {
	if !h.post {
		h.post = true
		h.writePre()
		h.out.put(h.between("{{messages}}", "{{script}}"))
		if err := h.a.js(h.out); err != nil && h.err == nil {
			h.err = err
		}
		h.out.put(h.between("{{script}}", "{{custom_script}}"))
		h.out.put(h.between("{{custom_script}}", ""))
	}
	if h.out.err != nil {
		return h.out.err
	}
	return h.err
}

func (h *htmlReporter) writePre() {
	if h.pre {
		return
	}
	h.pre = true
	h.out.put(h.between("", "{{title}}"))
	h.out.put(htmlTitle)
	h.out.put(h.between("{{title}}", "{{icon}}"))
	h.out.put(h.a.icon)
	h.out.put(h.between("{{icon}}", "{{css}}"))
	h.out.put(h.a.css)
	h.out.put(h.between("{{css}}", "{{custom_css}}"))
	h.out.put(h.between("{{custom_css}}", "{{messages}}"))
}

// between returns the template text between two placeholders ("" means the
// start or end of the template).
func (h *htmlReporter) between(begin, end string) string {
	t := h.a.template
	start := 0
	if begin != "" {
		i := strings.Index(t, begin)
		if i < 0 {
			return ""
		}
		start = i + len(begin)
	}
	stop := len(t)
	if end != "" {
		i := strings.Index(t[start:], end)
		if i < 0 {
			return ""
		}
		stop = start + i
	}
	return t[start:stop]
}
