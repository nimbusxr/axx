package video

import (
	"fmt"
	"syscall"
)

// open loads OpenH264 from path.
func open(path string) (*library, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("cannot load OpenH264 (%s): %w", path, err)
	}
	var l library
	for name, fn := range map[string]*uintptr{"WelsCreateSVCEncoder": &l.create, "WelsDestroySVCEncoder": &l.destroy} {
		proc, err := dll.FindProc(name)
		if err != nil {
			return nil, fmt.Errorf("OpenH264 (%s) has no %s: %w", path, name, err)
		}
		*fn = proc.Addr()
	}
	return &l, nil
}

// call calls a C function of OpenH264's, which returns an int.
func call(fn uintptr, args ...uintptr) int32 {
	r, _, _ := syscall.SyscallN(fn, args...)
	return int32(r) //nolint:gosec // the function returns a C int
}
