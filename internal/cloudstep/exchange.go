package cloudstep

import (
	"path"
	"strings"
	"unicode/utf8"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// What a scenario read in a container, for what shows a run to people (a
// demo's editor shows the file a step read).

// shownBytes and shownLines are how much of a file's text is kept to show.
const (
	shownBytes = 64 << 10
	shownLines = 400
)

// read says that a step read an object's text and found what it looked for:
// the object's name and, where it is text, the text, with the scenario's
// secrets masked. What is no text (a PDF, a workbook) is named only.
func (o Objects) read(sc *core.Scenario, container, name string, body []byte) {
	seen := &core.Message{Title: name}
	if len(body) <= shownBytes && utf8.Valid(body) && !strings.ContainsRune(string(body), 0) {
		lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
		if len(lines) > shownLines {
			lines = append(lines[:shownLines], "…")
		}
		seen.MediaType, seen.Body = "text/plain", secrets.Mask(sc, strings.Join(lines, "\n"))
	}
	sc.Exchanged(core.Exchange{
		Peer: container, Kind: o.Container,
		Sent: &core.Message{Title: "read " + path.Base(name)}, Seen: seen,
	})
}
