package progress

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// screen is what a terminal shows of what was written to it: a carriage
// return goes back to the start of the line, and what is written there
// replaces what was.
type screen struct {
	mu  sync.Mutex
	raw bytes.Buffer
}

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.Write(p)
}

func (s *screen) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.String()
}

// lines are the screen's lines, the one being written last.
func (s *screen) lines() []string {
	var out []string
	for _, line := range strings.Split(s.String(), "\n") {
		var row []rune
		col := 0
		for _, r := range line {
			if r == '\r' {
				col = 0
				continue
			}
			if col < len(row) {
				row[col] = r
			} else {
				row = append(row, r)
			}
			col++
		}
		out = append(out, strings.TrimRight(string(row), " "))
	}
	return out
}

// clock is a time tests move.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func reporter(out io.Writer, opts Options) (*Reporter, *clock) {
	r := New(out, opts)
	c := &clock{t: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)}
	r.now, r.every = c.now, time.Millisecond
	return r, c
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPlainStepSaysThatItStillGoesOn(t *testing.T) {
	var out screen
	r, c := reporter(&out, Options{})
	s := r.Start("axx: downloading axx 0.8.0 (once; kept for later runs)", "downloading axx 0.8.0")
	if got := out.String(); got != "axx: downloading axx 0.8.0 (once; kept for later runs)\n" {
		t.Fatalf("as it starts: %q", got)
	}
	// Nothing more for half a minute, then a line each half minute, with
	// how far a download is once that is known.
	c.add(29 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if got := out.String(); strings.Count(got, "\n") != 1 {
		t.Fatalf("before half a minute: %q", got)
	}
	c.add(time.Second)
	waitFor(t, "the first line", func() bool { return strings.Count(out.String(), "\n") == 2 })
	s.Bytes(16<<20, 38<<20)
	c.add(45 * time.Second)
	waitFor(t, "the second line", func() bool { return strings.Count(out.String(), "\n") == 3 })
	s.Done()
	s.Done() // once is enough
	c.add(time.Hour)
	time.Sleep(20 * time.Millisecond)
	want := "axx: downloading axx 0.8.0 (once; kept for later runs)\n" +
		"axx: still downloading axx 0.8.0 (30s)\n" +
		"axx: still downloading axx 0.8.0 (1m15s, 42% of 38 MB)\n"
	if got := out.String(); got != want {
		t.Errorf("wrote:\n%s\nwant:\n%s", got, want)
	}
	if strings.ContainsAny(out.String(), "\r\x1b") {
		t.Error("a plain line rewrites nothing")
	}
}

func TestLiveStepLeavesTheLinesAPlainOneWrites(t *testing.T) {
	var out screen
	r, c := reporter(&out, Options{Live: true, ASCII: true})
	s := r.Start("downloading axx 0.8.0", "downloading axx 0.8.0")
	waitFor(t, "the live line", func() bool { return strings.Contains(out.String(), "downloading axx 0.8.0 /") })
	if strings.Contains(out.String(), "\n") {
		t.Fatalf("a live line was ended: %q", out.String())
	}
	// It shows how long and how far.
	s.Bytes(5<<20, 12<<20)
	c.add(3 * time.Second)
	waitFor(t, "the details", func() bool {
		l := out.lines()
		return strings.HasSuffix(l[len(l)-1], " 41% of 12 MB, 3s") && strings.HasPrefix(l[len(l)-1], "downloading axx 0.8.0 ")
	})
	// What else is written goes above it.
	_, _ = io.WriteString(r, "warning: the mirror is slow\n")
	waitFor(t, "the line below the warning", func() bool {
		l := out.lines()
		return len(l) == 2 && l[0] == "warning: the mirror is slow" && strings.HasPrefix(l[1], "downloading axx 0.8.0 ")
	})
	s.Done()
	if got := out.lines(); len(got) != 3 || got[0] != "warning: the mirror is slow" || got[1] != "downloading axx 0.8.0" || got[2] != "" {
		t.Errorf("the screen at the end: %q", got)
	}
	// Nothing is written once no step goes on.
	before := out.String()
	c.add(time.Minute)
	time.Sleep(20 * time.Millisecond)
	if out.String() != before {
		t.Error("written to after the step ended")
	}
}

func TestLiveStepInsideAnother(t *testing.T) {
	var out screen
	r, _ := reporter(&out, Options{Live: true, ASCII: true})
	outer := r.Start("axx: preparing rest", "preparing rest")
	inner := r.Start("axx: setting up (once)", "setting up")
	waitFor(t, "the inner line", func() bool { l := out.lines(); return strings.HasPrefix(l[len(l)-1], "axx: setting up (once) ") })
	inner.Done()
	outer.Done()
	// As a plain reporter leaves them: in the order they started.
	if got := out.lines(); len(got) != 3 || got[0] != "axx: preparing rest" || got[1] != "axx: setting up (once)" {
		t.Errorf("the screen at the end: %q", got)
	}
}

func TestLiveLineNeverWraps(t *testing.T) {
	var out screen
	r, _ := reporter(&out, Options{Live: true, ASCII: true, Width: func() int { return 20 }})
	s := r.Start("axx: preparing rest, sql, kafka and mongo", "preparing")
	s.Bytes(1<<20, 4<<20)
	waitFor(t, "the live line", func() bool { return strings.Contains(out.String(), "25% of") })
	for _, l := range out.lines() {
		if n := len([]rune(l)); n > 19 {
			t.Errorf("a line of %d columns on a terminal of 20: %q", n, l)
		}
	}
	// The line gives way, not how far the step is.
	if l := out.lines(); !strings.HasPrefix(l[len(l)-1], "ax… ") || !strings.HasSuffix(l[len(l)-1], " 25% of 4.0 MB") {
		t.Errorf("the live line: %q", l[len(l)-1])
	}
	s.Done()
	if got := out.lines(); got[len(got)-2] != "axx: preparing rest, sql, kafka and mongo" {
		t.Errorf("the line that stays is whole: %q", got)
	}
}

func TestNothingForNoReporter(t *testing.T) {
	var r *Reporter
	s := r.Start("a", "a")
	s.Bytes(1, 2)
	s.Done()
	if n, err := r.Write([]byte("x")); n != 1 || err != nil {
		t.Errorf("write: %d, %v", n, err)
	}
	if On(nil) != nil {
		t.Error("a reporter on no writer")
	}
	var buf bytes.Buffer
	plain := On(&buf)
	if On(plain) != plain {
		t.Error("a reporter is its own")
	}
	st := plain.Start("copying", "copying")
	rd := st.Reader(strings.NewReader("12345"), 5)
	if _, err := io.ReadAll(rd); err != nil || st.done.Load() != 5 || st.total.Load() != 5 {
		t.Errorf("counted %d of %d: %v", st.done.Load(), st.total.Load(), err)
	}
	st.Done()
	if buf.String() != "copying\n" {
		t.Errorf("wrote %q", buf.String())
	}
}

func TestAStepThatIsQuickSaysNothing(t *testing.T) {
	for _, live := range []bool{false, true} {
		var out screen
		r, c := reporter(&out, Options{Live: live, ASCII: true})
		s := r.IfSlow("reading axx's releases", "reading axx's releases")
		c.add(900 * time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		s.Done()
		if got := out.String(); got != "" {
			t.Errorf("live %v: a quick step wrote %q", live, got)
		}
		// One that goes on says what it does, and its line stays.
		s = r.IfSlow("reading axx's releases", "reading axx's releases")
		c.add(time.Second)
		waitFor(t, "its line", func() bool { return strings.Contains(out.String(), "reading axx's releases") })
		s.Done()
		if got := out.lines(); len(got) != 2 || got[0] != "reading axx's releases" {
			t.Errorf("live %v: the screen at the end: %q", live, got)
		}
	}
}

func TestSizes(t *testing.T) {
	for n, want := range map[int64]string{512: "512 B", 860 << 10: "860 KB", 5347738: "5.1 MB", 38 << 20: "38 MB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
}
