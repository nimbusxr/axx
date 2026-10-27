package modelscore

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// tensorSyntax shows how --input and --output give a tensor.
const tensorSyntax = "'name=type[dims]', like 'images=float32[1,3,224,224]' (? for a dimension of any size; quoted, as shells read [ and ?)"

// giveTensors gives the contract's tensors the types and shapes --input and
// --output say (name=type[dims]): those the model does not say (a
// TorchScript module's), or others than it says.
func giveTensors(c *contract.Contract, inputs, outputs []string) error {
	for _, g := range []struct {
		kind   string
		specs  []string
		tensor []contract.Tensor
	}{{"input", inputs, c.Inputs}, {"output", outputs, c.Outputs}} {
		for _, spec := range g.specs {
			name, typ, shape, err := parseTensor(spec)
			if err != nil {
				return axxerr.New(contract.CodeContract, exitcode.Usage, "--%s %s: %v", g.kind, spec, err).WithHint("--%s gives a tensor as %s", g.kind, tensorSyntax)
			}
			i := slices.IndexFunc(g.tensor, func(t contract.Tensor) bool { return t.Name == name })
			if i < 0 {
				names := make([]string, len(g.tensor))
				for k, t := range g.tensor {
					names[k] = t.Name
				}
				return axxerr.New(contract.CodeContract, exitcode.Usage, "--%s %s: the model has no %s %s", g.kind, spec, g.kind, name).
					WithHint("its %ss are %s", g.kind, strings.Join(names, ", "))
			}
			t := &g.tensor[i]
			t.Type, t.Shape = typ, shape
			if g.kind == "input" {
				t.Image = nil
				t.GuessImage()
			}
		}
	}
	return nil
}

// parseTensor parses name=type[dims].
func parseTensor(spec string) (string, contract.DType, []contract.Dim, error) {
	name, rest, ok := strings.Cut(spec, "=")
	open := strings.Index(rest, "[")
	if !ok || name == "" || open < 0 || !strings.HasSuffix(rest, "]") {
		return "", "", nil, fmt.Errorf("it is not %s", tensorSyntax)
	}
	typ := contract.DType(rest[:open])
	if !typ.Known() {
		return "", "", nil, fmt.Errorf("%q is not a type a tensor is of", typ)
	}
	var shape []contract.Dim
	if dims := strings.TrimSpace(rest[open+1 : len(rest)-1]); dims != "" {
		for _, d := range strings.Split(dims, ",") {
			d = strings.TrimSpace(d)
			if n, err := strconv.ParseInt(d, 10, 64); err == nil && n > 0 {
				shape = append(shape, contract.Dim{Size: n})
			} else if d == "?" || err != nil && d != "" {
				shape = append(shape, contract.Dim{Name: d})
			} else {
				return "", "", nil, fmt.Errorf("its dimension %q is not a size (1 or more) or ?", d)
			}
		}
	}
	return name, typ, shape, nil
}

// untyped are the tensors of the contract whose types the model does not
// say, as the --input and --output that give them, of example shapes.
func untyped(c *contract.Contract) []string {
	var out []string
	for _, t := range c.Inputs {
		if t.Type == "" {
			out = append(out, "--input '"+t.Name+"=float32[1,3,224,224]'")
		}
	}
	for _, t := range c.Outputs {
		if t.Type == "" {
			out = append(out, "--output '"+t.Name+"=float32[1,1000]'")
		}
	}
	return out
}
