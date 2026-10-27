package pickle

import "testing"

// A pickle is read as it is written: a dict of a dict of numbers and of an
// OrderedDict of tensors (torch.save's of a checkpoint), its tensors'
// storages persistent ids.
func TestAPickleIsReadAsItIsWritten(t *testing.T) {
	var p Writer
	p.Op(0x80, 2, '}', '(')
	p.Str("dims")
	p.Op('}', '(')
	p.Str("n_mels")
	p.Int(80)
	p.Str("n_vocab")
	p.Int(51865)
	p.Str("big")
	p.Int(1 << 40)
	p.Op('u')
	p.Str("model_state_dict")
	p.Global("collections", "OrderedDict")
	p.Op(')', 'R', '(')
	p.Str("encoder.conv1.weight")
	p.Tensor("HalfStorage", "0", 384*80*3, []int64{384, 80, 3})
	p.Op('u', 'u', '.')
	v, err := Read(p.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	top, ok := v.(*Dict)
	if !ok {
		t.Fatalf("read a %T", v)
	}
	dims, _ := top.Get("dims")
	d, ok := dims.(*Dict)
	if !ok {
		t.Fatalf("dims are a %T", dims)
	}
	for name, want := range map[string]int64{"n_mels": 80, "n_vocab": 51865, "big": 1 << 40} {
		if got, _ := d.Get(name); got != want {
			t.Errorf("%s is %v, want %d", name, got, want)
		}
	}
	sd, _ := top.Get("model_state_dict")
	o, ok := sd.(*Object)
	if !ok {
		t.Fatalf("the state dict is a %T", sd)
	}
	items, ok := o.Args.(*Dict)
	if !ok || len(items.Keys) != 1 || items.Keys[0] != "encoder.conv1.weight" {
		t.Fatalf("the state dict's items: %+v", o.Args)
	}
	tensor, ok := items.Values[0].(*Object)
	if !ok || tensor.Callable != (Global{"torch._utils", "_rebuild_tensor_v2"}) {
		t.Errorf("a tensor is %+v", items.Values[0])
	}
}
