// Package onnx reads ONNX models' contracts and writes stubs as ONNX models,
// with the protobuf wire format of onnx.proto
// (https://github.com/onnx/onnx/blob/main/onnx/onnx.proto) and no generated
// code: a model and its graph are a handful of fields.
package onnx

import (
	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// The fields of onnx.proto that stubs and contracts use.
const (
	modelIRVersion       = 1
	modelProducerName    = 2
	modelProducerVersion = 3
	modelDocString       = 6
	modelGraph           = 7
	modelOpsetImport     = 8
	modelMetadataProps   = 14

	opsetDomain  = 1
	opsetVersion = 2

	entryKey   = 1
	entryValue = 2

	graphNode        = 1
	graphName        = 2
	graphInitializer = 5
	graphInput       = 11
	graphOutput      = 12

	nodeInput     = 1
	nodeOutput    = 2
	nodeName      = 3
	nodeOpType    = 4
	nodeAttribute = 5
	nodeDomain    = 7

	attrName   = 1
	attrF      = 2
	attrI      = 3
	attrG      = 6
	attrInts   = 8
	attrGraphs = 11
	attrType   = 20

	attrTypeFloat = 1
	attrTypeInt   = 2
	attrTypeInts  = 7

	tensorDims     = 1
	tensorDataType = 2
	tensorName     = 8
	tensorRawData  = 9

	tensorExternalData = 13

	valueName = 1
	valueType = 2

	typeTensor    = 1
	tensorElem    = 1
	tensorShape   = 2
	shapeDim      = 1
	dimValue      = 1
	dimParam      = 2
	typeSequence  = 4
	typeMap       = 5
	typeOptional  = 9
	typeSparseTen = 8
)

// elemTypes are ONNX's TensorProto.DataType numbers of the types stubs have.
var elemTypes = map[contract.DType]uint64{
	"float32": 1, "uint8": 2, "int8": 3, "uint16": 4, "int16": 5, "int32": 6, "int64": 7,
	"bool": 9, "float16": 10, "float64": 11, "uint32": 12, "uint64": 13,
}

func dtypeOf(elem uint64) (contract.DType, bool) {
	for t, n := range elemTypes {
		if n == elem {
			return t, true
		}
	}
	return "", false
}
