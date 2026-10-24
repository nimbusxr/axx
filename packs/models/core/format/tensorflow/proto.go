package tensorflow

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
)

// DataTypes are TensorFlow's DataType numbers of the contract's types.
var DataTypes = map[contract.DType]int64{
	"float32": 1, "float64": 2, "int32": 3, "uint8": 4, "int16": 5, "int8": 6,
	"int64": 9, "bool": 10, "uint16": 17, "float16": 19, "uint32": 22, "uint64": 23,
}

// TypeOf is the contract's type of a DataType number.
func TypeOf(n int64) (contract.DType, bool) {
	for t, v := range DataTypes {
		if v == n {
			return t, true
		}
	}
	return "", false
}

// Fields of graph.proto, node_def.proto, attr_value.proto, tensor.proto,
// tensor_shape.proto, versions.proto, meta_graph.proto and
// saved_model.proto.
const (
	graphNode, graphLibrary, graphVersions     = 1, 2, 4
	libraryFunction, functionNodes             = 1, 3
	nodeNameField, nodeOp, nodeInput, nodeAttr = 1, 2, 3, 5
	mapKey, mapValue                           = 1, 2

	attrList, attrS, attrI, attrB, attrType, attrShape, attrTensor = 1, 2, 3, 5, 6, 7, 8
	listI, listType                                                = 3, 6

	tensorDtype, tensorShape, tensorContent = 1, 2, 4
	shapeDim, shapeUnknownRank, dimSize     = 2, 3, 1
	versionsProducer, versionsMinConsumer   = 1, 2

	savedModelVersion, savedModelMetaGraphs                         = 1, 2
	metaInfo, metaGraph, metaSignatures                             = 1, 2, 5
	infoTags, infoTensorflowVersion, infoTensorflowGitVersion       = 4, 5, 6
	signatureInputs, signatureOutputs, signatureMethod              = 1, 2, 3
	tensorInfoName, tensorInfoDtype, tensorInfoShape                = 1, 2, 3
	predictMethod                                                   = "tensorflow/serving/predict"
	defaultProducer, defaultMinConsumer                       int64 = 1286, 12
)

// encodeShape is a TensorShapeProto: of its dimensions, −1 for one of any
// size.
func encodeShape(dims []int64) []byte {
	var m pb.Msg
	for _, d := range dims {
		m = m.Bytes(shapeDim, pb.Msg(nil).Varint(dimSize, uint64(d)))
	}
	return m
}

// encodeAttr is an AttrValue.
func encodeAttr(a Attr) []byte {
	var m pb.Msg
	switch a.Kind {
	case AttrString:
		m = m.Str(attrS, a.S)
	case AttrInt:
		m = m.Varint(attrI, uint64(a.I))
	case AttrBool:
		v := uint64(0)
		if a.B {
			v = 1
		}
		m = m.Varint(attrB, v)
	case AttrType:
		m = m.Varint(attrType, uint64(DataTypes[a.Type]))
	case AttrTypes:
		types := make([]int64, len(a.Types))
		for i, t := range a.Types {
			types[i] = DataTypes[t]
		}
		m = m.Bytes(attrList, pb.Msg(nil).Packed(listType, types))
	case AttrInts:
		m = m.Bytes(attrList, pb.Msg(nil).Packed(listI, a.Ints))
	case AttrShape:
		m = m.Bytes(attrShape, encodeShape(a.Shape))
	case AttrTensor:
		t := pb.Msg(nil).Varint(tensorDtype, uint64(DataTypes[a.Tensor.Type])).Bytes(tensorShape, encodeShape(a.Tensor.Dims))
		if len(a.Tensor.Raw) > 0 {
			t = t.Bytes(tensorContent, a.Tensor.Raw)
		}
		m = m.Bytes(attrTensor, t)
	}
	return m
}

// EncodeGraph is a GraphDef of the graph, of TensorFlow's producer and
// min_consumer versions.
func EncodeGraph(g *Graph, producer, minConsumer int64) []byte {
	var m pb.Msg
	for _, n := range g.Nodes {
		nd := pb.Msg(nil).Str(nodeNameField, n.Name).Str(nodeOp, n.Op)
		for _, in := range n.Inputs {
			nd = nd.Str(nodeInput, in)
		}
		for _, k := range slices.Sorted(maps.Keys(n.Attrs)) {
			nd = nd.Bytes(nodeAttr, pb.Msg(nil).Str(mapKey, k).Bytes(mapValue, encodeAttr(n.Attrs[k])))
		}
		m = m.Bytes(graphNode, nd)
	}
	return m.Bytes(graphVersions, pb.Msg(nil).Varint(versionsProducer, uint64(producer)).Varint(versionsMinConsumer, uint64(minConsumer)))
}

// tensorInfo is a TensorInfo of a tensor of the graph.
func tensorInfo(name string, t *contract.Tensor) []byte {
	dims := make([]int64, len(t.Shape))
	for i, d := range t.Shape {
		dims[i] = -1
		if d.Fixed() {
			dims[i] = d.Size
		}
	}
	return pb.Msg(nil).Str(tensorInfoName, name).Varint(tensorInfoDtype, uint64(DataTypes[t.Type])).Bytes(tensorInfoShape, encodeShape(dims))
}

// encodeSavedModel is a SavedModel of one meta graph: the graph, its tags,
// and its signature of the contract's inputs and outputs, by their names.
func encodeSavedModel(graph []byte, d *details, c *contract.Contract) []byte {
	info := pb.Msg(nil)
	for _, tag := range d.Tags {
		info = info.Str(infoTags, tag)
	}
	if d.TensorflowVersion != "" {
		info = info.Str(infoTensorflowVersion, d.TensorflowVersion)
	}
	sig := pb.Msg(nil)
	for _, in := range c.Inputs {
		sig = sig.Bytes(signatureInputs, pb.Msg(nil).Str(mapKey, in.Name).Bytes(mapValue, tensorInfo(d.Signature.Inputs[in.Name], &in)))
	}
	for _, out := range c.Outputs {
		sig = sig.Bytes(signatureOutputs, pb.Msg(nil).Str(mapKey, out.Name).Bytes(mapValue, tensorInfo(d.Signature.Outputs[out.Name], &out)))
	}
	sig = sig.Str(signatureMethod, predictMethod)
	meta := pb.Msg(nil).Bytes(metaInfo, info).Bytes(metaGraph, graph).
		Bytes(metaSignatures, pb.Msg(nil).Str(mapKey, d.SignatureKey).Bytes(mapValue, sig))
	return pb.Msg(nil).Varint(savedModelVersion, 1).Bytes(savedModelMetaGraphs, meta)
}

// savedModel is what a SavedModel's contract is read from: its meta graph
// for serving's tags, its signatures, its graph's versions and operators.
type savedModel struct {
	tags              []string
	tensorflowVersion string
	signatures        map[string]signature
	producer          int64
	ops               []string
}

// signature is a SignatureDef: its inputs' and outputs' tensors, by their
// keys.
type signature struct {
	inputs, outputs map[string]contract.Tensor
	names           Signature
	// err says why a stub cannot be of it: a tensor of it is not one a
	// stub has (sparse, of unknown rank...).
	err error
}

// readSavedModel reads a SavedModel's meta graph of the tag "serve" (or
// its first).
func readSavedModel(b []byte) (*savedModel, error) {
	var metas [][]byte
	var version uint64
	err := pb.Fields(b, func(f protowire.Number, t protowire.Type, v uint64, data []byte) error {
		switch {
		case f == savedModelVersion && t == protowire.VarintType:
			version = v
		case f == savedModelMetaGraphs && t == protowire.BytesType:
			metas = append(metas, data)
		case f == savedModelVersion:
			return errors.New("it is a GraphDef (a frozen graph), not a SavedModel")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if version != 1 || len(metas) == 0 {
		return nil, fmt.Errorf("it is not a SavedModel (its schema version is %d, and it has %d meta graphs)", version, len(metas))
	}
	var out *savedModel
	for _, meta := range metas {
		m, err := readMetaGraph(meta)
		if err != nil {
			return nil, err
		}
		if out == nil || slices.Contains(m.tags, "serve") && !slices.Contains(out.tags, "serve") {
			out = m
		}
	}
	return out, nil
}

func readMetaGraph(b []byte) (*savedModel, error) {
	m := &savedModel{signatures: map[string]signature{}, producer: defaultProducer}
	ops := map[string]bool{}
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case metaInfo:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				switch f {
				case infoTags:
					m.tags = append(m.tags, string(data))
				case infoTensorflowVersion:
					m.tensorflowVersion = string(data)
				}
				return nil
			})
		case metaGraph:
			return readGraphOps(data, ops, &m.producer)
		case metaSignatures:
			key, sig, err := readSignatureEntry(data)
			if err != nil {
				return err
			}
			if key != "__saved_model_init_op" {
				m.signatures[key] = sig
			}
		}
		return nil
	})
	m.ops = slices.Sorted(maps.Keys(ops))
	return m, err
}

// readGraphOps reads the operators of a GraphDef's nodes and its
// functions', and its producer version.
func readGraphOps(b []byte, ops map[string]bool, producer *int64) error {
	nodeOps := func(data []byte) error {
		return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
			if f == nodeOp {
				ops[string(data)] = true
			}
			return nil
		})
	}
	return pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case graphNode:
			return nodeOps(data)
		case graphLibrary:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				if f != libraryFunction {
					return nil
				}
				return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
					if f == functionNodes {
						return nodeOps(data)
					}
					return nil
				})
			})
		case graphVersions:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, _ []byte) error {
				if f == versionsProducer {
					*producer = int64(v)
				}
				return nil
			})
		}
		return nil
	})
}

func readSignatureEntry(b []byte) (string, signature, error) {
	var key string
	sig := signature{inputs: map[string]contract.Tensor{}, outputs: map[string]contract.Tensor{}, names: Signature{Inputs: map[string]string{}, Outputs: map[string]string{}}}
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case mapKey:
			key = string(data)
		case mapValue:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				if f != signatureInputs && f != signatureOutputs {
					return nil
				}
				// A tensor a stub cannot have is the signature's error,
				// and the model's only when it is called by it.
				name, t, err := readTensorEntry(data)
				switch {
				case err != nil:
					sig.err = err
				case f == signatureInputs:
					sig.inputs[t.Name], sig.names.Inputs[t.Name] = t, name
				default:
					sig.outputs[t.Name], sig.names.Outputs[t.Name] = t, name
				}
				return nil
			})
		}
		return nil
	})
	return key, sig, err
}

// readTensorEntry reads a signature's map entry of a TensorInfo: the
// tensor of its key, and its name in the graph.
func readTensorEntry(b []byte) (string, contract.Tensor, error) {
	var name string
	var t contract.Tensor
	var dtype int64
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case mapKey:
			t.Name = string(data)
		case mapValue:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
				switch f {
				case tensorInfoName:
					name = string(data)
				case tensorInfoDtype:
					dtype = int64(v)
				case tensorInfoShape:
					shape, err := readShape(data)
					if err != nil {
						return fmt.Errorf("its tensor %s is %w", t.Name, err)
					}
					t.Shape = shape
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return "", t, err
	}
	if name == "" {
		return "", t, fmt.Errorf("its tensor %s is not a dense tensor (a sparse or composite tensor's)", t.Name)
	}
	typ, ok := TypeOf(dtype)
	if !ok {
		return "", t, fmt.Errorf("its tensor %s is of TensorFlow's type %d, which stubs do not have", t.Name, dtype)
	}
	t.Type = typ
	return name, t, nil
}

// readShape reads a TensorShapeProto: its dimensions, those of any size
// open.
func readShape(b []byte) ([]contract.Dim, error) {
	var dims []contract.Dim
	unknown := false
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
		switch f {
		case shapeUnknownRank:
			unknown = v != 0
		case shapeDim:
			var size int64
			if err := pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, _ []byte) error {
				if f == dimSize {
					size = int64(v)
				}
				return nil
			}); err != nil {
				return err
			}
			if size < 0 {
				dims = append(dims, contract.Dim{Name: "?"})
			} else {
				dims = append(dims, contract.Dim{Size: size})
			}
		}
		return nil
	})
	if unknown {
		return nil, errors.New("of unknown rank")
	}
	return dims, err
}
