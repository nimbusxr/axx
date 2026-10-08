//go:build darwin || linux

package video

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// open loads OpenH264 from path, with no cgo.
func open(path string) (*library, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("cannot load OpenH264 (%s): %w", path, err)
	}
	var l library
	for name, fn := range map[string]*uintptr{"WelsCreateSVCEncoder": &l.create, "WelsDestroySVCEncoder": &l.destroy} {
		if *fn, err = purego.Dlsym(h, name); err != nil {
			return nil, fmt.Errorf("OpenH264 (%s) has no %s: %w", path, name, err)
		}
	}
	return &l, nil
}

// call calls a C function of OpenH264's, which returns an int.
func call(fn uintptr, args ...uintptr) int32 {
	r, _, _ := purego.SyscallN(fn, args...)
	return int32(r) //nolint:gosec // the function returns a C int
}
