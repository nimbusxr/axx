package lifecycle

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"sync"
)

const (
	// tailLines is how many output lines are kept per app for failure reports.
	tailLines = 200
	// errorTailLines is how many of them an error message quotes.
	errorTailLines = 20
	// maxLineBytes splits pathological lines (no newline for megabytes).
	maxLineBytes = 1 << 20
)

// ring keeps the last n lines of an app's output.
type ring struct {
	mu    sync.Mutex
	lines []string
	next  int
	full  bool
}

func newRing(n int) *ring { return &ring{lines: make([]string, n)} }

func (r *ring) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines[r.next] = line
	r.next = (r.next + 1) % len(r.lines)
	if r.next == 0 {
		r.full = true
	}
}

// snapshot returns the retained lines, oldest first.
func (r *ring) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]string(nil), r.lines[:r.next]...)
	}
	out := make([]string, 0, len(r.lines))
	out = append(out, r.lines[r.next:]...)
	return append(out, r.lines[:r.next]...)
}

// console writes whole lines to the user's writers so that output from
// concurrently running apps never interleaves mid-line. One lock guards both
// writers because they are often the same terminal or buffer.
type console struct {
	mu     sync.Mutex
	stdout io.Writer
	stderr io.Writer
}

func newConsole(stdout, stderr io.Writer) *console {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &console{stdout: stdout, stderr: stderr}
}

// write writes s, which should end in a newline, to w in one call.
func (c *console) write(w io.Writer, s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = io.WriteString(w, s)
}

// Println writes an unprefixed line to stdout (IDE protocol lines, banners).
func (c *console) Println(line string) { c.write(c.stdout, line+"\n") }

// prefixed returns a line handler that writes "[name] line" to w.
func (c *console) prefixed(w io.Writer, name string) func(string) {
	prefix := "[" + name + "] "
	return func(line string) { c.write(w, prefix+line+"\n") }
}

// pump reads r line by line until EOF or a read error and hands each line,
// without its line ending, to each. A final line without a newline is
// delivered too.
func pump(r io.Reader, each func(line string)) {
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		full := errors.Is(err, bufio.ErrBufferFull)
		if full && len(buf) < maxLineBytes {
			continue
		}
		if len(buf) > 0 {
			each(strings.TrimRight(string(buf), "\r\n"))
			buf = buf[:0]
		}
		if err != nil && !full {
			return
		}
	}
}

// formatTail renders the last n lines indented for an error message.
func formatTail(lines []string, n int) string {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("\n  ")
		b.WriteString(l)
	}
	return b.String()
}
