package hdf5

import (
	"os"
	"slices"
	"testing"
)

// A file written is read back: its groups' members, attributes and
// datasets.
func TestAFileIsReadAsWritten(t *testing.T) {
	kernel := Float32s([]uint64{2, 3}, []float32{1, 2, 3, 4, 5, 6.5})
	root := &Object{
		Attrs: []Attr{{Name: "model_config", Data: Text(`{"class_name": "Functional"}`)}, {Name: "layer_names", Data: Strings("dense", "scores")}},
		Members: []Member{
			{Name: "layers", Object: &Object{Members: []Member{
				{Name: "dense", Object: &Object{Members: []Member{
					{Name: "vars", Object: &Object{Members: []Member{{Name: "0", Object: &Object{Data: kernel}}}}},
				}}},
			}}},
			{Name: "empty", Object: &Object{}},
		},
	}
	b, err := Write(root)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Root()
	if err != nil {
		t.Fatal(err)
	}
	if names, err := r.Members(); err != nil || !slices.Equal(names, []string{"empty", "layers"}) {
		t.Errorf("members %v, %v", names, err)
	}
	if a, ok, err := r.Attr("model_config"); err != nil || !ok || a.Strings[0] != `{"class_name": "Functional"}` {
		t.Errorf("model_config %+v, %v, %v", a, ok, err)
	}
	if a, ok, err := r.Attr("layer_names"); err != nil || !ok || !slices.Equal(a.Strings, []string{"dense", "scores"}) {
		t.Errorf("layer_names %+v, %v, %v", a, ok, err)
	}
	n := r
	for _, name := range []string{"layers", "dense", "vars", "0"} {
		if n, err = n.Member(name); err != nil {
			t.Fatal(err)
		}
	}
	d, err := n.Data()
	if err != nil {
		t.Fatal(err)
	}
	vs, err := d.Float32s()
	if err != nil || !slices.Equal(vs, []float32{1, 2, 3, 4, 5, 6.5}) || !slices.Equal(d.Dims, []uint64{2, 3}) {
		t.Errorf("data %v %v, %v", d.Dims, vs, err)
	}
}

// A file of h5py's (HDF5's earliest structures: a superblock of version 0,
// symbol tables, object headers of version 1, strings of variable length)
// is read: testdata/h5py.h5, of make_h5py.py.
func TestAFileOfH5pyIsRead(t *testing.T) {
	b, err := os.ReadFile("testdata/h5py.h5")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Root()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{"keras_version": {"2.15.0"}, "model_config": {`{"class_name": "Sequential"}`}, "layer_names": {"dense", "dense_1"}} {
		if a, ok, err := r.Attr(name); err != nil || !ok || !slices.Equal(a.Strings, want) {
			t.Errorf("%s: %+v, %v, %v", name, a, ok, err)
		}
	}
	n, err := r.Member("model_weights")
	if err == nil {
		n, err = n.Member("dense")
	}
	if err == nil {
		n, err = n.Member("kernel:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	d, err := n.Data()
	if err != nil {
		t.Fatal(err)
	}
	if vs, err := d.Float32s(); err != nil || !slices.Equal(vs, []float32{0.5, -1, 2, 3}) {
		t.Errorf("kernel %v, %v", vs, err)
	}
}
