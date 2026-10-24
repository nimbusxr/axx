package onnx

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/internal/textenc"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
)

func init() {
	format.Register(format.Format{Type: "onnx", Name: "ONNX", Extensions: []string{".onnx"}, Contract: ReadContract, Write: writeModel, Operators: operators})
}

// operators reads the operators of the ONNX model at path.
func operators(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Contract(b)
	if err != nil {
		return nil, err
	}
	return c.Format.Operators, nil
}

// ReadContract reads the contract of the ONNX model at path: its inputs and
// outputs, its IR version and operator sets, the operators it uses and its
// metadata. An input shaped like an image is given its layout, and how its
// pictures are fitted, when its folder has its image processor's
// configuration (preprocessor_config.json).
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	c, err := Contract(b)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an ONNX model axx reads: %v", path, err).
			WithHint("axx reads ONNX models whose inputs and outputs are tensors (onnx.proto's ModelProto)")
	}
	switch {
	case isDecoder(c):
		if err := decoderContract(c, path); err != nil {
			return nil, err
		}
	case isSpeechEncoder(c):
		if err := speechContract(c, path); err != nil {
			return nil, err
		}
	case textenc.Is(c):
		if err := textenc.Read(c, path); err != nil {
			return nil, err
		}
	default:
		if err := readProcessor(c, path); err != nil {
			return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model's image processor cannot be read: %v", err).
				WithHint("keep the model in its folder as it comes (onnx/model.onnx below preprocessor_config.json, as Hugging Face's repositories have it)")
		}
	}
	return c, nil
}

// Contract reads the contract of an ONNX model.
func Contract(model []byte) (*contract.Contract, error) {
	c := &contract.Contract{Format: contract.Format{Type: "onnx", Opsets: map[string]int64{}}}
	var graph []byte
	err := pb.Fields(model, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
		switch f {
		case modelIRVersion:
			c.Format.IRVersion = int64(v)
		case modelProducerName:
			c.Format.Producer = string(data)
		case modelGraph:
			graph = data
		case modelOpsetImport:
			var domain string
			var version int64
			if err := pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
				switch f {
				case opsetDomain:
					domain = string(data)
				case opsetVersion:
					version = int64(v)
				}
				return nil
			}); err != nil {
				return err
			}
			if domain == "ai.onnx" {
				domain = ""
			}
			c.Format.Opsets[domain] = version
		case modelMetadataProps:
			k, val, err := entry(data)
			if err != nil {
				return err
			}
			if c.Metadata == nil {
				c.Metadata = map[string]string{}
			}
			c.Metadata[k] = val
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if graph == nil || c.Format.IRVersion == 0 {
		return nil, errors.New("it has no graph or IR version")
	}
	ops := map[string]bool{}
	initializers, locations := map[string]bool{}, map[string]bool{}
	var inputs, outputs [][]byte
	if err := readGraph(graph, ops, initializers, locations, &inputs, &outputs); err != nil {
		return nil, err
	}
	for loc := range locations {
		c.Format.ExternalData = append(c.Format.ExternalData, loc)
	}
	sort.Strings(c.Format.ExternalData)
	for op := range ops {
		c.Format.Operators = append(c.Format.Operators, op)
	}
	sort.Strings(c.Format.Operators)
	for _, in := range inputs {
		t, err := valueInfo(in)
		if err != nil {
			return nil, err
		}
		if initializers[t.Name] {
			continue // an initializer an older IR lists as an input too
		}
		t.GuessImage()
		c.Inputs = append(c.Inputs, t)
	}
	for _, out := range outputs {
		t, err := valueInfo(out)
		if err != nil {
			return nil, err
		}
		c.Outputs = append(c.Outputs, t)
	}
	if len(c.Inputs) == 0 || len(c.Outputs) == 0 {
		return nil, errors.New("its graph has no inputs or no outputs")
	}
	return c, nil
}

func entry(b []byte) (key, value string, err error) {
	err = pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case entryKey:
			key = string(data)
		case entryValue:
			value = string(data)
		}
		return nil
	})
	return key, value, err
}

// readGraph collects a graph's operators (those of its subgraphs too), its
// initializers' names and the files their data is in (external data's
// locations), and its inputs' and outputs' value infos.
func readGraph(b []byte, ops, initializers, locations map[string]bool, inputs, outputs *[][]byte) error {
	return pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case graphNode:
			return readNode(data, ops)
		case graphInitializer:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				switch f {
				case tensorName:
					initializers[string(data)] = true
				case tensorExternalData:
					k, v, err := entry(data)
					if err == nil && k == "location" && locations != nil {
						locations[v] = true
					}
					return err
				}
				return nil
			})
		case graphInput:
			if inputs != nil {
				*inputs = append(*inputs, data)
			}
		case graphOutput:
			if outputs != nil {
				*outputs = append(*outputs, data)
			}
		}
		return nil
	})
}

func readNode(b []byte, ops map[string]bool) error {
	var op, domain string
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case nodeOpType:
			op = string(data)
		case nodeDomain:
			domain = string(data)
		case nodeAttribute:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				if f == attrG || f == attrGraphs {
					return readGraph(data, ops, map[string]bool{}, nil, nil, nil)
				}
				return nil
			})
		}
		return nil
	})
	if domain != "" && domain != "ai.onnx" {
		op = domain + ":" + op
	}
	ops[op] = true
	return err
}

func valueInfo(b []byte) (contract.Tensor, error) {
	var t contract.Tensor
	var typ []byte
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case valueName:
			t.Name = string(data)
		case valueType:
			typ = data
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	var tensor []byte
	var other string
	err = pb.Fields(typ, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case typeTensor:
			tensor = data
		case typeSequence:
			other = "a sequence"
		case typeMap:
			other = "a map"
		case typeOptional:
			other = "an optional value"
		case typeSparseTen:
			other = "a sparse tensor"
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	if tensor == nil {
		if other == "" {
			other = "of no type"
		}
		return t, fmt.Errorf("%s is %s: stubs have tensors only (a classifier's ZipMap output is a sequence of maps: export the model without it)", t.Name, other)
	}
	var elem uint64
	err = pb.Fields(tensor, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
		switch f {
		case tensorElem:
			elem = v
		case tensorShape:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				if f != shapeDim {
					return nil
				}
				var d contract.Dim
				err := pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
					switch f {
					case dimValue:
						d.Size = int64(v)
					case dimParam:
						d.Name = string(data)
					}
					return nil
				})
				if d.Name == "" && d.Size == 0 {
					d.Name = "?" // a dimension the model leaves open, unnamed
				}
				t.Shape = append(t.Shape, d)
				return err
			})
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	dt, ok := dtypeOf(elem)
	if !ok {
		return t, fmt.Errorf("%s holds elements of ONNX type %d, which stubs do not have", t.Name, elem)
	}
	t.Type = dt
	return t, nil
}
