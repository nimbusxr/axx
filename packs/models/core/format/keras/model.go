package keras

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nimbusxr/axx/packs/models/core/internal/hdf5"
)

// layer is a layer of a model being written: its class and name, its
// config's settings (but its name, trainability and type), the layers whose
// outputs it takes, its output's shape (of no batch; −1 is open) and type,
// and its weights, in its variables' order.
type layer struct {
	class    string
	name     string
	settings map[string]any
	inputs   []*layer
	shape    []int
	dtype    string
	weights  []weight
}

// weight is a layer's variable: its name, and its value.
type weight struct {
	name string
	data *hdf5.Data
}

// model is a functional model being written.
type model struct {
	name    string
	layers  []*layer
	inputs  []*layer
	outputs []*layer
	used    map[string]bool
}

func newModel(name string, reserved ...string) *model {
	m := &model{name: name, used: map[string]bool{}}
	for _, r := range reserved {
		m.used[r] = true
	}
	return m
}

// input adds an input of the name, type and shape.
func (m *model) input(name, dtype string, shape []int) *layer {
	l := &layer{class: "InputLayer", name: name, shape: shape, dtype: dtype}
	m.layers = append(m.layers, l)
	m.inputs = append(m.inputs, l)
	return l
}

// add adds a layer of the class, named as Keras names it (its class's name,
// in snake case, numbered), on the inputs.
func (m *model) add(class string, settings map[string]any, shape []int, weights []weight, inputs ...*layer) *layer {
	base := snake(class)
	name := base
	for i := 1; m.used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	m.used[name] = true
	return m.named(name, class, settings, shape, weights, inputs...)
}

// named adds a layer of the name.
func (m *model) named(name, class string, settings map[string]any, shape []int, weights []weight, inputs ...*layer) *layer {
	l := &layer{class: class, name: name, settings: settings, inputs: inputs, shape: shape, dtype: "float32", weights: weights}
	m.layers = append(m.layers, l)
	return l
}

// snake is a class's name in snake case, as Keras names its layers and
// their weights' groups (naming.to_snake_case).
func snake(class string) string {
	s := wordStart.ReplaceAllString(class, "${1}_${2}")
	return strings.ToLower(lowerUpper.ReplaceAllString(s, "${1}_${2}"))
}

var (
	wordStart  = regexp.MustCompile(`(.)([A-Z][a-z]+)`)
	lowerUpper = regexp.MustCompile(`([a-z])([A-Z])`)
)

// ordered are the model's layers as Keras orders them (map_graph): by
// their depth, from the inputs, then by when a search from the outputs
// first reaches them; an input no output needs is first.
func (m *model) ordered() []*layer {
	index := map[*layer]int{}
	finished := map[*layer]bool{}
	var post []*layer
	var visit func(l *layer)
	visit = func(l *layer) {
		if finished[l] {
			return
		}
		if _, ok := index[l]; !ok {
			index[l] = len(index)
		}
		for _, in := range l.inputs {
			visit(in)
		}
		finished[l] = true
		post = append(post, l)
	}
	for _, o := range m.outputs {
		visit(o)
	}
	depth := map[*layer]int{}
	for i := len(post) - 1; i >= 0; i-- {
		l := post[i]
		d := depth[l]
		for _, in := range l.inputs {
			depth[in] = max(depth[in], d+1)
		}
	}
	order := slices.Clone(post)
	for _, in := range m.inputs {
		if !finished[in] {
			index[in] = -1
			order = append(order, in)
		}
	}
	slices.SortStableFunc(order, func(a, b *layer) int {
		if depth[a] != depth[b] {
			return depth[b] - depth[a]
		}
		return index[a] - index[b]
	})
	return order
}

// dims is a shape of a batch: its batch, then the shape's dimensions (null
// where open).
func dims(shape []int) []any {
	out := []any{nil}
	for _, d := range shape {
		if d < 0 {
			out = append(out, nil)
		} else {
			out = append(out, d)
		}
	}
	return out
}

// The dialects of a model's config: Keras 3's (of a .keras file, and of a
// .h5 file, as Keras 3 writes it there) and Keras 2's (tf.keras, tf_keras).
const (
	keras3 = iota
	keras3Legacy
	keras2
)

// config is a layer's config in the dialect.
func (l *layer) config(dialect int) map[string]any {
	cfg := map[string]any{"name": l.name}
	c := map[string]any{"class_name": l.class, "config": cfg, "name": l.name}
	if dialect == keras3 {
		c["module"], c["registered_name"] = "keras.layers", nil
	}
	if l.class == "InputLayer" {
		cfg["dtype"], cfg["sparse"] = l.dtype, false
		if dialect == keras2 {
			cfg["batch_input_shape"], cfg["ragged"] = dims(l.shape), false
		} else {
			cfg["batch_shape"] = dims(l.shape)
		}
		c["inbound_nodes"] = []any{}
		return c
	}
	cfg["trainable"], cfg["dtype"] = true, "float32"
	for k, v := range l.settings {
		cfg[k] = v
	}
	if dialect == keras2 {
		var node []any
		for _, in := range l.inputs {
			node = append(node, []any{in.name, 0, 0, map[string]any{}})
		}
		c["inbound_nodes"] = []any{node}
		return c
	}
	var args []any
	var shapes []any
	for _, in := range l.inputs {
		args = append(args, map[string]any{"class_name": "__keras_tensor__", "config": map[string]any{
			"shape": dims(in.shape), "dtype": in.dtype, "keras_history": []any{in.name, 0, 0},
		}})
		shapes = append(shapes, dims(in.shape))
	}
	if dialect == keras3 {
		c["build_config"] = map[string]any{"input_shape": shapes[0]}
	}
	if len(l.inputs) > 1 {
		// A merging layer's: a list of its inputs, its one argument.
		args = []any{args}
		if dialect == keras3 {
			c["build_config"] = map[string]any{"input_shape": shapes}
		}
	}
	c["inbound_nodes"] = []any{map[string]any{"args": args, "kwargs": map[string]any{}}}
	return c
}

// config is the model's config in the dialect, its layers in order.
func (m *model) config(order []*layer, dialect int) map[string]any {
	var layers []any
	for _, l := range order {
		layers = append(layers, l.config(dialect))
	}
	refs := func(ls []*layer) []any {
		var out []any
		for _, l := range ls {
			out = append(out, []any{l.name, 0, 0})
		}
		return out
	}
	c := map[string]any{
		"class_name": "Functional",
		"config": map[string]any{
			"name": m.name, "trainable": true, "layers": layers,
			"input_layers": refs(m.inputs), "output_layers": refs(m.outputs),
		},
	}
	if dialect == keras3 {
		c["module"], c["registered_name"] = "keras.src.models.functional", "Functional"
		c["build_config"] = map[string]any{"input_shape": nil}
	}
	return c
}

// encodeJSON is a value as JSON, its maps' keys sorted, as encoding/json
// writes them, so that a stub is the same bit for bit.
func encodeJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// weightsFile is the model's weights as a .keras file has them
// (model.weights.h5): each layer's variables in layers/<its group>/vars,
// its group named as Keras names it there: by its class, in the layers'
// order.
func (m *model) weightsFile(order []*layer) ([]byte, error) {
	layers := &hdf5.Object{}
	used := map[string]int{}
	for _, l := range order {
		name := snake(l.class)
		if n, ok := used[name]; ok {
			used[name] = n + 1
			name = fmt.Sprintf("%s_%d", name, n+1)
		} else {
			used[name] = 0
		}
		if len(l.weights) == 0 {
			continue
		}
		vars := &hdf5.Object{}
		for i, w := range l.weights {
			vars.Members = append(vars.Members, hdf5.Member{Name: fmt.Sprint(i), Object: &hdf5.Object{Data: w.data}})
		}
		layers.Members = append(layers.Members, hdf5.Member{Name: name, Object: &hdf5.Object{Members: []hdf5.Member{{Name: "vars", Object: vars}}}})
	}
	return hdf5.Write(&hdf5.Object{Members: []hdf5.Member{{Name: "layers", Object: layers}}})
}

// archive is the model as a .keras file: a zip of its metadata, its config
// and its weights, of no time.
func (m *model) archive(version, saved string) ([]byte, error) {
	order := m.ordered()
	metadata, err := encodeJSON(map[string]any{"keras_version": version, "date_saved": saved})
	if err != nil {
		return nil, err
	}
	config, err := encodeJSON(m.config(order, keras3))
	if err != nil {
		return nil, err
	}
	weights, err := m.weightsFile(order)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, f := range []struct {
		name string
		data []byte
	}{{"metadata.json", metadata}, {"config.json", config}, {"model.weights.h5", weights}} {
		w, err := z.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Store, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// legacy is the model as a .h5 file, as Keras writes one (save_model's
// HDF5 format): its config and version in the root's attributes, each
// layer's weights in model_weights/<its name>, by their names.
func (m *model) legacy(version, backend string, dialect int) ([]byte, error) {
	order := m.ordered()
	config, err := encodeJSON(m.config(order, dialect))
	if err != nil {
		return nil, err
	}
	weights := &hdf5.Object{Attrs: []hdf5.Attr{
		{Name: "backend", Data: hdf5.Text(backend)},
		{Name: "keras_version", Data: hdf5.Text(version)},
	}}
	var names []string
	for _, l := range order {
		names = append(names, l.name)
	}
	weights.Attrs = append([]hdf5.Attr{{Name: "layer_names", Data: hdf5.Strings(names...)}}, weights.Attrs...)
	byName := slices.Clone(order)
	slices.SortFunc(byName, func(a, b *layer) int { return strings.Compare(a.name, b.name) })
	for _, l := range byName {
		group := &hdf5.Object{}
		var weightNames []string
		inner := &hdf5.Object{}
		for _, w := range l.weights {
			name := w.name
			if dialect == keras2 {
				name += ":0"
			}
			weightNames = append(weightNames, l.name+"/"+name)
			inner.Members = append(inner.Members, hdf5.Member{Name: name, Object: &hdf5.Object{Data: w.data}})
		}
		group.Attrs = []hdf5.Attr{{Name: "weight_names", Data: hdf5.Strings(weightNames...)}}
		if len(l.weights) > 0 {
			group.Members = []hdf5.Member{{Name: l.name, Object: inner}}
		}
		weights.Members = append(weights.Members, hdf5.Member{Name: l.name, Object: group})
	}
	return hdf5.Write(&hdf5.Object{
		Attrs: []hdf5.Attr{
			{Name: "backend", Data: hdf5.Text(backend)},
			{Name: "keras_version", Data: hdf5.Text(version)},
			{Name: "model_config", Data: hdf5.Text(string(config))},
		},
		Members: []hdf5.Member{{Name: "model_weights", Object: weights}},
	})
}
