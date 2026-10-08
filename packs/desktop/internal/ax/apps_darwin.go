//go:build darwin

package ax

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	appsOnce sync.Once
	appsErr  error

	lsCopyApplicationURLsForBundleIdentifier func(id ref, outError uintptr) ref
	cfURLCopyFileSystemPath                  func(url ref, style int) ref
)

const posixPathStyle = 0 // kCFURLPOSIXPathStyle

func loadApps() error {
	appsOnce.Do(func() {
		if err := load(); err != nil {
			appsErr = err
			return
		}
		cs, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/CoreServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			appsErr = fmt.Errorf("cannot load CoreServices: %w", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			appsErr = fmt.Errorf("cannot load CoreFoundation: %w", err)
			return
		}
		purego.RegisterLibFunc(&lsCopyApplicationURLsForBundleIdentifier, cs, "LSCopyApplicationURLsForBundleIdentifier")
		purego.RegisterLibFunc(&cfURLCopyFileSystemPath, cf, "CFURLCopyFileSystemPath")
	})
	return appsErr
}

// AppPath is the .app of the bundle identifier, as LaunchServices has it:
// the app `open -b` opens. It is "" when no app has the identifier.
func AppPath(bundleID string) (string, error) {
	if err := loadApps(); err != nil {
		return "", err
	}
	id := cfString(bundleID)
	defer cfRelease(id)
	urls := lsCopyApplicationURLsForBundleIdentifier(id, 0)
	if urls == 0 {
		return "", nil
	}
	defer cfRelease(urls)
	var paths []string
	for i := range cfArrayGetCount(urls) {
		p := cfURLCopyFileSystemPath(cfArrayGetValueAtIndex(urls, i), posixPathStyle)
		if p == 0 {
			continue
		}
		if s := strings.TrimRight(goString(p), "/"); strings.HasSuffix(s, ".app") {
			paths = append(paths, s)
		}
		cfRelease(p)
	}
	// The system's copy of an app, not one inside another app's bundle.
	slices.SortStableFunc(paths, func(a, b string) int { return cmp.Compare(len(a), len(b)) })
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}
