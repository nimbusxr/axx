package cli

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/internal/secrets"
)

// What a scenario's commands did, for what shows a run to people (a demo's
// terminal): the command as it ran, and what it printed, its output and its
// error output as they came, as a terminal has them.

// printedLimit is how much of what a command printed is kept to show.
const printedLimit = 256 << 10

// shownLines is how many of a command's last lines are shown.
const shownLines = 60

// both keeps what a command printed on its two outputs, in the order it
// came, up to printedLimit.
type both struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *both) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := printedLimit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *both) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// exchanged says a command's run: the command line and its input, how it
// ended and its last lines, with the scenario's secrets masked as its logs
// have them.
func exchanged(sc *core.Scenario, name string, argv []string, stdin []byte, res *proc.Result, printed string) {
	sent := &core.Message{Title: secrets.Mask(sc, display(argv))}
	if len(stdin) > 0 && utf8.Valid(stdin) {
		sent.MediaType, sent.Body = "text/plain", secrets.Mask(sc, strings.TrimRight(string(stdin), "\n"))
	}
	seen := &core.Message{Title: fmt.Sprintf("exit code %d", res.ExitCode)}
	if text := strings.TrimRight(printed, "\n"); text != "" && utf8.ValidString(text) {
		lines := strings.Split(text, "\n")
		if n := len(lines) - shownLines; n > 0 {
			lines = append([]string{fmt.Sprintf("… %d lines before", n)}, lines[n:]...)
		}
		seen.MediaType, seen.Body = "text/plain", secrets.Mask(sc, strings.Join(lines, "\n"))
	}
	sc.Exchanged(core.Exchange{Peer: name, Kind: "command", Sent: sent, Seen: seen})
}
