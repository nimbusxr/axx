//go:build darwin

package ax

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	askOnce sync.Once
	askErr  error

	axIsProcessTrustedWithOptions func(options ref) bool
	cfDictionaryCreate            func(alloc uintptr, keys, values *ref, n int, keyCallbacks, valueCallbacks uintptr) ref
	askPromptKey                  ref
	askKeyCallbacks               uintptr
	askValueCallbacks             uintptr
)

// AskTrust asks macOS to let this process use the accessibility tree, as an
// app does the first time: macOS lists the app it asks for (axx's launcher,
// or the terminal that runs axx) in System Settings, turned off, and shows a
// prompt that opens them. It reports whether macOS lets it already.
func AskTrust() (bool, error) {
	if err := load(); err != nil {
		return false, err
	}
	askOnce.Do(func() {
		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			askErr = fmt.Errorf("cannot load ApplicationServices: %w", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			askErr = fmt.Errorf("cannot load CoreFoundation: %w", err)
			return
		}
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, as, "AXIsProcessTrustedWithOptions")
		purego.RegisterLibFunc(&cfDictionaryCreate, cf, "CFDictionaryCreate")
		key, err := purego.Dlsym(as, "kAXTrustedCheckOptionPrompt")
		if err != nil {
			askErr = fmt.Errorf("cannot find kAXTrustedCheckOptionPrompt: %w", err)
			return
		}
		// key is the global's address, which holds the CFString.
		askPromptKey = **(**ref)(unsafe.Pointer(&key))
		for name, v := range map[string]*uintptr{"kCFTypeDictionaryKeyCallBacks": &askKeyCallbacks, "kCFTypeDictionaryValueCallBacks": &askValueCallbacks} {
			// These are the callbacks' structs themselves: their addresses are what CFDictionaryCreate takes.
			if *v, err = purego.Dlsym(cf, name); err != nil {
				askErr = fmt.Errorf("cannot find %s: %w", name, err)
				return
			}
		}
	})
	if askErr != nil {
		return false, askErr
	}
	key, value := askPromptKey, cfTrue
	options := cfDictionaryCreate(0, &key, &value, 1, askKeyCallbacks, askValueCallbacks)
	if options == 0 {
		return false, fmt.Errorf("cannot ask macOS for the accessibility tree")
	}
	defer cfRelease(options)
	return axIsProcessTrustedWithOptions(options), nil
}
