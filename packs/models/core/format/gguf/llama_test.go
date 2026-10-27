package gguf

import (
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
)

// A small plan: a vocabulary of 300 tokens, the end of generation 2, and
// tokens that stand for a chat template's (1 begins a turn, 3 is "user",
// 4 "assistant", 5 a newline).
func testPlan() *lm.Plan {
	return &lm.Plan{
		Vocab: 300,
		EOG:   2,
		Keys: []lm.KeyPattern{
			// "leave it with the neighbour": as it tokenizes after a space, or
			// not.
			{Variants: [][]int32{{100, 101, 102, 103, 104}, {110, 101, 102, 103, 104}}, Core: [][]int32{{101, 102, 103}}},
			// "fragile glass", once as two tokens, once as one.
			{Variants: [][]int32{{120, 121}, {122}}},
		},
		Answers: [][]int32{
			{200, 201, 202, 203, 201, 204}, // "201" repeats: its context tells which follows
			{210, 211, 212},
			{220, 221},
		},
		Context: 4096,
	}
}

func chat(user ...int32) []int32 {
	p := []int32{1, 3, 5}
	p = append(p, user...)
	return append(p, 2, 5, 1, 4, 5)
}

func TestStubAnswersItsMappings(t *testing.T) {
	p := testPlan()
	m, err := compile(p)
	if err != nil {
		t.Fatal(err)
	}
	f := m.file(nil, p.Context, "test")
	fw := newForward(t, f)
	t.Logf("E %d, K %d, heads %d×%d, ffn %d, codes %d dims", m.l.E, m.l.K, m.l.heads, m.l.headDim, fw.ffn, m.l.codeDims)
	cases := []struct {
		name   string
		prompt []int32
		want   []int32
	}{
		{"the first key, after a space", chat(50, 51, 100, 101, 102, 103, 104, 52), p.Answers[0]},
		{"the first key, at the start", chat(110, 101, 102, 103, 104), p.Answers[0]},
		{"the second, in two tokens", chat(60, 120, 121, 62), p.Answers[1]},
		{"the second, in one", chat(60, 61, 122), p.Answers[1]},
		{"half the second", chat(120, 60, 61, 62), p.Answers[2]},
		{"both keys: the first wins", chat(122, 100, 101, 102, 103, 104), p.Answers[0]},
		{"a key cut short", chat(100, 101, 102, 104), p.Answers[2]},
		{"nothing", chat(70, 71, 72), p.Answers[2]},
	}
	for _, c := range cases {
		got, lead := fw.generate(t, c.prompt, p.EOG, 12)
		want := append(slices.Clone(c.want), p.EOG)
		if !slices.Equal(got, want) {
			t.Errorf("%s: generated %v, want %v", c.name, got, want)
		}
		if lead < 50 {
			t.Errorf("%s: a token led by %.1f only", c.name, lead)
		}
	}
}
