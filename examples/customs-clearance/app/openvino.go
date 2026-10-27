//go:build openvino

package main

/*
#cgo CFLAGS: -I/opt/openvino/include
#cgo LDFLAGS: -L/opt/openvino/lib -lopenvino_c -Wl,--disable-new-dtags -Wl,-rpath,/opt/openvino/lib
#include <stdlib.h>
#include "openvino/c/openvino.h"
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

// The contents check's classifier runs with OpenVINO, whose C library the
// image links into the service (-tags openvino), its libraries found where
// the image keeps them (an RPATH, which their own dependencies go by too).

// labels are the classifier's classes, in the order of its scores
// (ImageNet's).
//
//go:embed labels.txt
var labelsFile string

// contentsModel is the classifier, compiled for the CPU.
type contentsModel struct {
	mu      sync.Mutex
	core    *C.ov_core_t
	request *C.ov_infer_request_t
	labels  []string
}

// loadContentsModel reads the model's IR (its model.xml, and model.bin
// beside it) and compiles it for the CPU.
func loadContentsModel(path string) (contentsChecker, error) {
	m := &contentsModel{labels: strings.Split(strings.TrimSpace(labelsFile), "\n")}
	fail := func(step string, status C.ov_status_e) error {
		return fmt.Errorf("the contents check's model %s: OpenVINO cannot %s (status %d)", path, step, int(status))
	}
	if s := C.ov_core_create(&m.core); s != C.OK {
		return nil, fail("start", s)
	}
	cpath, device := C.CString(path), C.CString("CPU")
	defer C.free(unsafe.Pointer(cpath))
	defer C.free(unsafe.Pointer(device))
	var model *C.ov_model_t
	if s := C.ov_core_read_model(m.core, cpath, nil, &model); s != C.OK {
		return nil, fail("read it", s)
	}
	defer C.ov_model_free(model)
	var compiled *C.ov_compiled_model_t
	if s := C.ov_core_compile_model_props(m.core, model, device, 0, nil, &compiled); s != C.OK {
		return nil, fail("compile it", s)
	}
	defer C.ov_compiled_model_free(compiled)
	if s := C.ov_compiled_model_create_infer_request(compiled, &m.request); s != C.OK {
		return nil, fail("make a request of it", s)
	}
	return m, nil
}

// check has the model classify a photo: the likeliest class of its scores.
func (m *contentsModel) check(_ context.Context, photo []byte) (string, float64, error) {
	pixels, err := modelInput(photo)
	if err != nil {
		return "", 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dims := [4]C.int64_t{1, 3, photoSide, photoSide}
	var shape C.ov_shape_t
	if s := C.ov_shape_create(4, &dims[0], &shape); s != C.OK {
		return "", 0, fmt.Errorf("check the contents: OpenVINO cannot shape the photo (status %d)", int(s))
	}
	defer C.ov_shape_free(&shape)
	// The tensor is of the photo's memory, C's: a Go pointer cannot be kept by C.
	data := C.malloc(C.size_t(unsafe.Sizeof(*pixels)))
	defer C.free(data)
	copy(unsafe.Slice((*float32)(data), 3*photoSide*photoSide), unsafe.Slice(&pixels[0][0][0], 3*photoSide*photoSide))
	var input *C.ov_tensor_t
	if s := C.ov_tensor_create_from_host_ptr(C.F32, shape, data, &input); s != C.OK {
		return "", 0, fmt.Errorf("check the contents: OpenVINO cannot take the photo (status %d)", int(s))
	}
	defer C.ov_tensor_free(input)
	if s := C.ov_infer_request_set_input_tensor_by_index(m.request, 0, input); s != C.OK {
		return "", 0, fmt.Errorf("check the contents: OpenVINO cannot give the model the photo (status %d)", int(s))
	}
	if s := C.ov_infer_request_infer(m.request); s != C.OK {
		return "", 0, fmt.Errorf("check the contents: the model cannot classify the photo (status %d)", int(s))
	}
	var output *C.ov_tensor_t
	if s := C.ov_infer_request_get_output_tensor_by_index(m.request, 0, &output); s != C.OK {
		return "", 0, fmt.Errorf("check the contents: the model gave no scores (status %d)", int(s))
	}
	defer C.ov_tensor_free(output)
	var size C.size_t
	var out unsafe.Pointer
	if C.ov_tensor_get_size(output, &size) != C.OK || C.ov_tensor_data(output, &out) != C.OK || int(size) != len(m.labels) {
		return "", 0, fmt.Errorf("check the contents: the model's scores are not its %d classes'", len(m.labels))
	}
	scores := unsafe.Slice((*float32)(out), int(size))
	best := 0
	for i, x := range scores {
		if x > scores[best] {
			best = i
		}
	}
	return m.labels[best], float64(scores[best]), nil
}
