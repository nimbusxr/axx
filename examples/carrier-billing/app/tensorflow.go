//go:build tensorflow

package main

/*
#cgo CFLAGS: -I/opt/tensorflow/include
#cgo LDFLAGS: -L/opt/tensorflow/lib -ltensorflow -Wl,--disable-new-dtags -Wl,-rpath,/opt/tensorflow/lib
#include <stdlib.h>
#include <string.h>
#include "tensorflow/c/c_api.h"
*/
import "C"

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

// The repacks' photo check's classifier runs with TensorFlow, whose C
// library the image links into the service (-tags tensorflow).

// labels are the classifier's classes, in the order of its logits
// (ImageNet's, background first).
//
//go:embed labels.txt
var labelsFile string

// The tensors of the SavedModel's serving signature (serving_default): the
// photo, and the classes' logits.
const (
	servingInput  = "serving_default_inputs"
	servingOutput = "StatefulPartitionedCall"
)

// photoModel is the classifier: its SavedModel, loaded in a session.
type photoModel struct {
	mu            sync.Mutex
	session       *C.TF_Session
	graph         *C.TF_Graph
	input, output C.TF_Output
	labels        []string
}

// loadPhotoModel loads the SavedModel in folder (its serve tag's graph).
func loadPhotoModel(folder string) (photoChecker, error) {
	status := C.TF_NewStatus()
	defer C.TF_DeleteStatus(status)
	opts := C.TF_NewSessionOptions()
	defer C.TF_DeleteSessionOptions(opts)
	dir, tag := C.CString(folder), C.CString("serve")
	defer C.free(unsafe.Pointer(dir))
	defer C.free(unsafe.Pointer(tag))
	m := &photoModel{graph: C.TF_NewGraph(), labels: strings.Split(strings.TrimSpace(labelsFile), "\n")}
	m.session = C.TF_LoadSessionFromSavedModel(opts, nil, dir, &tag, 1, m.graph, nil, status)
	if C.TF_GetCode(status) != C.TF_OK {
		return nil, fmt.Errorf("the repack check's model %s: TensorFlow cannot load it: %s", folder, C.GoString(C.TF_Message(status)))
	}
	for _, t := range []struct {
		name string
		out  *C.TF_Output
	}{{servingInput, &m.input}, {servingOutput, &m.output}} {
		name := C.CString(t.name)
		op := C.TF_GraphOperationByName(m.graph, name)
		C.free(unsafe.Pointer(name))
		if op == nil {
			return nil, fmt.Errorf("the repack check's model %s has no %s: it is not the classifier's SavedModel", folder, t.name)
		}
		*t.out = C.TF_Output{oper: op, index: 0}
	}
	return m, nil
}

// check has the model classify a photo: the likeliest class of its logits,
// and its probability.
func (m *photoModel) check(_ context.Context, photo []byte) (string, float64, error) {
	pixels, err := modelInput(photo)
	if err != nil {
		return "", 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dims := [4]C.int64_t{1, photoSide, photoSide, 3}
	size := C.size_t(4 * photoSide * photoSide * 3)
	input := C.TF_AllocateTensor(C.TF_FLOAT, &dims[0], 4, size)
	defer C.TF_DeleteTensor(input)
	data := unsafe.Slice((*float32)(C.TF_TensorData(input)), photoSide*photoSide*3)
	for y := range photoSide {
		for x := range photoSide {
			copy(data[(y*photoSide+x)*3:], pixels[y][x][:])
		}
	}
	status := C.TF_NewStatus()
	defer C.TF_DeleteStatus(status)
	var output *C.TF_Tensor
	C.TF_SessionRun(m.session, nil, &m.input, &input, 1, &m.output, &output, 1, nil, 0, nil, status)
	if C.TF_GetCode(status) != C.TF_OK {
		return "", 0, fmt.Errorf("check the photo: TensorFlow cannot run the model: %s", C.GoString(C.TF_Message(status)))
	}
	defer C.TF_DeleteTensor(output)
	n := int(C.TF_TensorByteSize(output)) / 4
	if n != len(m.labels) {
		return "", 0, fmt.Errorf("check the photo: the model gave %d logits, not its %d classes'", n, len(m.labels))
	}
	raw := unsafe.Slice((*float32)(C.TF_TensorData(output)), n)
	logits := make([]float64, n)
	for i, x := range raw {
		logits[i] = float64(x)
	}
	best, score := top(logits)
	return m.labels[best], score, nil
}
