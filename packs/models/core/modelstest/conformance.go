package modelstest

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// The scenes every format's stubs are held to: two mappings' keys, and two
// pictures of neither.
var (
	Door  = Scene{W: 320, H: 240, X: 40, Y: 120, Box: Brown}
	Step  = Scene{W: 320, H: 240, X: 200, Y: 20, Box: Brown}
	White = Scene{W: 320, H: 240, X: 120, Y: 60, Box: color.RGBA{R: 245, G: 245, B: 240, A: 255}}
	Empty = Scene{W: 320, H: 240, X: -1000, Y: -1000}
)

// Noisy is the scene drawn larger, as another camera would take it, with
// noise.
func Noisy(s Scene) image.Image {
	big := Scene{W: s.W * 2, H: s.H * 2, X: s.X * 2, Y: s.Y * 2, Box: s.Box}.Image()
	rnd := rand.New(rand.NewPCG(7, 11))
	for i := range big.Pix {
		if i%4 != 3 {
			big.Pix[i] = uint8(max(0, min(255, int(big.Pix[i])+rnd.IntN(17)-8)))
		}
	}
	return big
}

// AppInput prepares a picture as an app would for the input: laid out, then
// scaled as its model expects (ImageNet's means and deviations for floats,
// centered for int8).
func AppInput(r *match.Reducer, in *contract.Tensor, img image.Image) []float64 {
	px := match.Picture(r, in, img)
	switch in.Type {
	case "float32":
		mean, sd := [3]float64{0.485, 0.456, 0.406}, [3]float64{0.229, 0.224, 0.225}
		ci, _, _ := in.ImageDims()
		out := make([]float64, len(px))
		for i, v := range px {
			c := i / (r.H * r.W)
			if ci == len(in.Shape)-1 {
				c = i % r.C
			}
			out[i] = (v/255 - mean[c%3]) / sd[c%3]
		}
		return out
	case "int8":
		out := make([]float64, len(px))
		for i, v := range px {
			out[i] = v - 128
		}
		return out
	}
	return px
}

// Variant is a contract every format's stubs are written for.
type Variant struct {
	Name     string
	Contract func() *contract.Contract
	// Pictures says the runtime takes the image input as a picture (a Core
	// ML pixel buffer), in RGB, and lays it out for the model itself.
	Pictures bool
	// Answers are the answers to the door's key, the step's, and the
	// default's, in the contract's task's terms: a classifier's, unless
	// given.
	Answers *[3]map[string]any
	// Half says the runtime stores and computes in half precision (ncnn's
	// default on a CPU of ARM's): an output is the reference's as half
	// precision holds it, within two of its steps, and one under its least
	// normal value may be 0.
	Half bool
}

// Tensor is an input or output's type, dimensions and values. A
// dictionary's values (a classifier's probabilities, by label) come with
// their keys.
type Tensor struct {
	Type string    `json:"type"`
	Dims []int64   `json:"dims"`
	Data []float64 `json:"data"`
	Keys []string  `json:"keys,omitempty"`
}

// Run is a run of a model, by its path, on its inputs, by name.
type Run struct {
	Model string            `json:"model"`
	Feeds map[string]Tensor `json:"feeds"`
}

// Runner runs models in a format's runtime, and returns their outputs, by
// name, in the runs' order.
type Runner func(ctx context.Context, runs []Run) ([]map[string]Tensor, error)

// Conform holds a format's stubs to the reference: for each variant, a stub
// of a carton at the door and a packet on the step (an envelope or a tube
// otherwise) is written, run in the format's runtime on each key, a noisy
// picture of each, and pictures of neither, and every output is the
// reference's answer. A runtime that runs batches gets every picture of a
// variant with an open batch in one run.
func Conform(t *testing.T, variants []Variant, ext string, write func(*stub.Stub) (format.Model, error), run Runner, batches bool) {
	t.Helper()
	type check struct {
		variant, picture string
		want             int
		item             []float64
		stub             *stub.Stub
		run, of, at      int // the run, how many pictures it has, and which this is
		half             bool
	}
	var runs []Run
	var checks []check
	for _, v := range variants {
		answers := [3]map[string]any{
			{"label": "carton", "score": 0.94},
			{"label": "packet", "score": 0.81},
			{"scores": map[string]any{"envelope": 0.4, "tube": 0.35}},
		}
		if v.Answers != nil {
			answers = *v.Answers
		}
		dir := Dir(t, v.Contract(),
			Mapping("A carton at the door", "../inputs/door.png", answers[0]),
			Mapping("A packet on the step", "../inputs/step.png", answers[1]),
			Mapping("Anything else", "", answers[2]),
		)
		Door.Write(t, filepath.Join(dir, "inputs", "door.png"))
		Step.Write(t, filepath.Join(dir, "inputs", "step.png"))
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		m, err := write(s)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		model := filepath.Join(dir, "stub"+ext)
		if err := save(m, model); err != nil {
			t.Fatal(err)
		}
		in := s.Contract.MatchedInput()
		pictures := []struct {
			name string
			img  image.Image
			want int
		}{
			{"the door's key", Door.Image(), 0},
			{"the step's key", Step.Image(), 1},
			{"the door, noisy", Noisy(Door), 0},
			{"the step, noisy", Noisy(Step), 1},
			{"an envelope", White.Image(), 2},
			{"an empty porch", Empty.Image(), 2},
		}
		dims := make([]int64, len(in.Shape))
		for i, d := range in.Shape {
			dims[i] = max(d.Size, 1)
		}
		together := batches && in.Shape[0].Name != ""
		var batch []float64
		for k, p := range pictures {
			item := AppInput(s.Nearest.Reducer, in, p.img)
			feed := item
			if v.Pictures && in.Image.Channels != "gray" {
				rgb := *in
				rgb.Image = &contract.Image{Layout: in.Image.Layout, Resize: in.Image.Resize}
				feed = match.Picture(s.Nearest.Reducer, &rgb, p.img)
			}
			c := check{variant: v.Name, picture: p.name, want: p.want, item: item, stub: s, run: len(runs), of: 1, half: v.Half}
			if together {
				c.of, c.at = len(pictures), k
				batch = append(batch, feed...)
			} else {
				runs = append(runs, Run{Model: model, Feeds: map[string]Tensor{in.Name: {Type: string(in.Type), Dims: dims, Data: feed}}})
			}
			checks = append(checks, c)
		}
		if together {
			dims[0] = int64(len(pictures))
			runs = append(runs, Run{Model: model, Feeds: map[string]Tensor{in.Name: {Type: string(in.Type), Dims: dims, Data: batch}}})
		}
	}

	results, err := run(context.Background(), runs)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		idx, answer, err := c.stub.Answer(c.item)
		if err != nil {
			t.Fatal(err)
		}
		if idx != c.want {
			t.Errorf("%s, %s: the reference answers with %s, want %s", c.variant, c.picture, c.stub.Names[idx], c.stub.Names[c.want])
		}
		for _, out := range c.stub.Contract.Outputs {
			res, ok := results[c.run][out.Name]
			if !ok {
				t.Errorf("%s, %s: the runtime gave no output %s: %v", c.variant, c.picture, out.Name, keys(results[c.run]))
				continue
			}
			data := valuesOf(res, &out)
			size := len(data) / c.of
			got := data[c.at*size : (c.at+1)*size]
			want := out.Type.Decode(out.Type.Encode(answer[out.Name]))
			if err := equal(got, want, c.half); err != nil {
				t.Errorf("%s, %s: output %s: %v (the runtime %v, the reference %v)", c.variant, c.picture, out.Name, err, got, want)
			}
		}
	}
}

// valuesOf is an output's values as the runtime gave them: a dictionary's
// (a classifier's probabilities, by label) in its labels' order.
func valuesOf(res Tensor, out *contract.Tensor) []float64 {
	if len(res.Keys) == 0 {
		return res.Data
	}
	byKey := map[string]float64{}
	for i, k := range res.Keys {
		byKey[k] = res.Data[i]
	}
	data := make([]float64, len(out.Labels))
	for i, l := range out.Labels {
		data[i] = byKey[l]
	}
	return data
}

func keys(m map[string]Tensor) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func equal(got, want []float64, half bool) error {
	if len(got) != len(want) {
		return fmt.Errorf("%d values, want %d", len(got), len(want))
	}
	for i := range got {
		w, tolerance := want[i], 1e-6*math.Max(1, math.Abs(want[i]))
		if half {
			// Half precision's step at w: 11 bits of it.
			_, exp := math.Frexp(w)
			tolerance = 2 * math.Ldexp(1, exp-11)
			if math.Abs(w) < halfNormal {
				tolerance = halfNormal
			}
		}
		if math.Abs(got[i]-w) > tolerance {
			return fmt.Errorf("value %d is %v, want %v", i, got[i], w)
		}
	}
	return nil
}

// halfNormal is half precision's least normal value.
const halfNormal = 1.0 / (1 << 14)

// save writes a model to path: its file, or its folder's files.
func save(m format.Model, path string) error {
	if m.Files == nil {
		for name, data := range m.Beside {
			p := filepath.Join(filepath.Dir(path), filepath.FromSlash(format.BesideName(path, name)))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, data, 0o644); err != nil {
				return err
			}
		}
		return os.WriteFile(path, m.File, 0o644)
	}
	for name, data := range m.Files {
		p := filepath.Join(path, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
