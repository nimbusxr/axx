package lifecycle

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestRing(t *testing.T) {
	tests := []struct {
		add  int
		want []string
	}{
		{0, []string{}},
		{2, []string{"l1", "l2"}},
		{3, []string{"l1", "l2", "l3"}},
		{7, []string{"l5", "l6", "l7"}},
	}
	for _, tt := range tests {
		r := newRing(3)
		for i := 1; i <= tt.add; i++ {
			r.add(fmt.Sprintf("l%d", i))
		}
		if got := r.snapshot(); !slices.Equal(got, tt.want) {
			t.Errorf("after %d lines: %q, want %q", tt.add, got, tt.want)
		}
	}
}

func TestPump(t *testing.T) {
	long := strings.Repeat("x", maxLineBytes+10)
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"lines", "a\nb\n", []string{"a", "b"}},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}},
		{"final line without newline", "a\nb", []string{"a", "b"}},
		{"empty lines kept", "a\n\nb\n", []string{"a", "", "b"}},
		{"overlong line split", long + "\n", []string{long[:maxLineBytes], long[maxLineBytes:]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			pump(strings.NewReader(tt.in), func(l string) { got = append(got, l) })
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %.20q (len %d), want %.20q (len %d)", i, got[i], len(got[i]), tt.want[i], len(tt.want[i]))
				}
			}
		})
	}
}

func TestConsoleKeepsLinesIntact(t *testing.T) {
	out := &buffer{}
	c := newConsole(out, out)
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c"} {
		w := c.prefixed(c.stdout, name)
		if name == "b" {
			w = c.prefixed(c.stderr, name)
		}
		wg.Go(func() {
			for i := range 200 {
				w(fmt.Sprintf("line %d of %s", i, name))
			}
		})
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 600 {
		t.Fatalf("got %d lines, want 600", len(lines))
	}
	for _, l := range lines {
		var name string
		var i int
		if _, err := fmt.Sscanf(l, "[%1s] line %d of", &name, &i); err != nil || !strings.HasSuffix(l, "of "+name) {
			t.Fatalf("mangled line %q", l)
		}
	}
}

func TestFormatTail(t *testing.T) {
	got := formatTail([]string{"a", "b", "c"}, 2)
	if got != "\n  b\n  c" {
		t.Errorf("formatTail = %q", got)
	}
}
