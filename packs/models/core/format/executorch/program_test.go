package executorch

import (
	"reflect"
	"testing"
)

// A program's methods of settings, and its inputs of dynamic shapes, read
// back as they were written.
func TestProgramReadsSettingsAndBoundsBack(t *testing.T) {
	n, yes, schema := int64(2048), true, `{"forward": {}}`
	settings := []method{
		{Name: "get_max_seq_len", Values: []constant{{Int: &n}}},
		{Name: "use_kv_cache", Values: []constant{{Bool: &yes}}},
		{Name: "get_model_schema", Values: []constant{{String: &schema}}},
		{Name: "get_eos_ids", Values: []constant{{Ints: []int64{2, 7}}}},
	}
	p := &program{}
	for _, m := range settings {
		p.plans = append(p.plans, settingsOf(m))
	}
	l := &lowering{p: p, pl: &plan{name: "forward"}, ops: map[[2]string]int{}}
	tokens := l.alloc("int64", []int64{1, 16})
	l.pl.values[tokens].tensor.bounded = true
	pos := l.alloc("int64", []int64{1})
	l.pl.inputs = []int{tokens, pos}
	l.pl.outputs = []int{l.kernel("aten::add", "out", "int64", []int64{1}, pos, pos, l.integer(1))}
	l.pl.inMeta, l.pl.outMeta = treeSpecs(2, 1)
	p.plans = append(p.plans, l.pl)

	methods, ops, err := readProgram(p.encode())
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != len(settings)+1 {
		t.Fatalf("%d methods, want %d", len(methods), len(settings)+1)
	}
	for i, m := range settings {
		if got := methods[i]; got.Name != m.Name || !reflect.DeepEqual(got.Values, m.Values) {
			t.Errorf("%s reads back as %s, %+v", m.Name, got.Name, got.Values)
		}
	}
	forward := methods[len(settings)]
	if want := [][]bool{{false, true}, {false}}; !reflect.DeepEqual(forward.Bounded, want) {
		t.Errorf("forward's bounded dimensions read back %v, want %v", forward.Bounded, want)
	}
	if !reflect.DeepEqual(ops, []string{"aten::add.out"}) {
		t.Errorf("its operators read back %v", ops)
	}
	if !isLanguageModel(methods) {
		t.Error("a program of forward of tokens and their position, and get_max_seq_len, is not read as a language model's")
	}
}
