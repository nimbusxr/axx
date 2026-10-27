package onnx

import (
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
)

// Graph is an ONNX model's main graph, as a format that translates a
// stub's ONNX graph into its own reads it: its nodes in their order, its
// initializers, its inputs' and outputs' names, and its ai.onnx operator
// set.
type Graph struct {
	Opset        int64
	Nodes        []Node
	Initializers map[string]Initializer
	Inputs       []string
	Outputs      []string
}

// Node is a node: its operator, its inputs' and outputs' names (an
// optional input left out is ""), and its attributes, by name.
type Node struct {
	Op              string
	Inputs, Outputs []string
	Attrs           map[string]Attr
}

// Attr is an attribute's value: an int, a float, or ints.
type Attr struct {
	Int   int64
	Float float64
	Ints  []int64
}

// Initializer is a constant: its type, its dimensions and its values' raw
// bytes, little-endian.
type Initializer struct {
	Type contract.DType
	Dims []int64
	Raw  []byte
}

// Values are an initializer's values.
func (i Initializer) Values() []float64 { return i.Type.Decode(i.Raw) }

// ReadGraph reads a model's main graph: as the stubs this package writes
// have them (their initializers' values raw, no subgraphs).
func ReadGraph(model []byte) (*Graph, error) {
	g := &Graph{Initializers: map[string]Initializer{}}
	var graph []byte
	err := pb.Fields(model, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
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
			if domain == "" || domain == "ai.onnx" {
				g.Opset = version
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if graph == nil {
		return nil, errors.New("the model has no graph")
	}
	err = pb.Fields(graph, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case graphNode:
			n, err := readNodeOf(data)
			if err != nil {
				return err
			}
			g.Nodes = append(g.Nodes, n)
		case graphInitializer:
			name, init, err := readInitializer(data)
			if err != nil {
				return err
			}
			g.Initializers[name] = init
		case graphInput, graphOutput:
			t, err := valueInfo(data)
			if err != nil {
				return err
			}
			if f == graphInput {
				g.Inputs = append(g.Inputs, t.Name)
			} else {
				g.Outputs = append(g.Outputs, t.Name)
			}
		}
		return nil
	})
	return g, err
}

func readNodeOf(b []byte) (Node, error) {
	n := Node{Attrs: map[string]Attr{}}
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case nodeInput:
			n.Inputs = append(n.Inputs, string(data))
		case nodeOutput:
			n.Outputs = append(n.Outputs, string(data))
		case nodeOpType:
			n.Op = string(data)
		case nodeDomain:
			if d := string(data); d != "" && d != "ai.onnx" {
				return fmt.Errorf("its node of %s:%s is of another domain than ai.onnx", d, n.Op)
			}
		case nodeAttribute:
			var name string
			var a Attr
			if err := pb.Fields(data, func(f protowire.Number, t protowire.Type, v uint64, data []byte) error {
				switch f {
				case attrName:
					name = string(data)
				case attrI:
					a.Int = int64(v)
				case attrF:
					a.Float = float64(math.Float32frombits(uint32(v)))
				case attrInts:
					a.Ints = append(a.Ints, pb.Varints(t, v, data)...)
				}
				return nil
			}); err != nil {
				return err
			}
			n.Attrs[name] = a
		}
		return nil
	})
	return n, err
}

func readInitializer(b []byte) (string, Initializer, error) {
	var name string
	var init Initializer
	err := pb.Fields(b, func(f protowire.Number, t protowire.Type, v uint64, data []byte) error {
		switch f {
		case tensorName:
			name = string(data)
		case tensorDims:
			init.Dims = append(init.Dims, pb.Varints(t, v, data)...)
		case tensorDataType:
			dt, ok := dtypeOf(v)
			if !ok {
				return fmt.Errorf("an initializer of ONNX type %d", v)
			}
			init.Type = dt
		case tensorRawData:
			init.Raw = data
		}
		return nil
	})
	return name, init, err
}

// TypeOf is the type of ONNX's TensorProto.DataType number elem (a Cast's
// "to").
func TypeOf(elem int64) (contract.DType, bool) { return dtypeOf(uint64(elem)) }
