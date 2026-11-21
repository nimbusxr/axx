// SPDX-License-Identifier: Apache-2.0

// Package memcheck builds axx's C, C++ and Objective-C under a memory checker, for axx's own
// tests: the libraries the packs load into apps (the simulators' library, the AR library, the
// Apple model adapter) are the code of axx that Go does not keep memory-safe.
//
// With AXX_MEMCHECK naming a folder, each of them is built with AddressSanitizer, and with where
// its reports go: a file in that folder for each process that had one, which the checker ends as
// it writes it. The library brings the checker's runtime along (Xcode's, by the library's own
// search path), so nothing changes in how an app is started, and the app itself needs no build of
// its own. A checked build is another entry in axx's cache, beside the usual one.
//
// It is not a setting of axx: a project's runs never set it.
package memcheck

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// Env names the folder the reports go to. Empty, nothing is checked.
const Env = "AXX_MEMCHECK"

// Dir is the folder the reports go to, or "" when nothing is checked.
func Dir() string {
	dir := os.Getenv(Env)
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// Enabled says whether the libraries are built checked.
func Enabled() bool { return Dir() != "" }

// Key is what a checked build adds to what its place in the cache is made from: the usual build
// and the checked one never share a place, nor do two that report to different folders.
func Key() []byte {
	if !Enabled() {
		return nil
	}
	return []byte("memcheck:" + Dir())
}

// Clang is what clang (and clang++) gets more for a checked build: of a source, or of the link.
func Clang() []string {
	if !Enabled() {
		return nil
	}
	return []string{"-fsanitize=address", "-fno-omit-frame-pointer", "-g"}
}

// Swift is what swiftc gets more when it links objects that Clang built checked.
func Swift() []string {
	if !Enabled() {
		return nil
	}
	return []string{"-sanitize=address"}
}

// SourceName is the name Source is written under, beside a library's own sources.
const SourceName = "axx-memcheck.c"

// Source is one more C source of a checked library: the checker's options, which it asks the
// process for as it starts. Reports go to files in Dir (report.<pid>), not to an app's standard
// error, which no test reads.
func Source() []byte {
	options := "log_path=" + filepath.Join(Dir(), "report") + ":abort_on_error=1"
	return []byte("const char *__asan_default_options(void) { return " + strconv.Quote(options) + "; }\n")
}

// Reports are the files the checker wrote, one for each process it found a problem in.
func Reports() ([]string, error) {
	if !Enabled() {
		return nil, nil
	}
	found, err := filepath.Glob(filepath.Join(Dir(), "report.*"))
	sort.Strings(found)
	return found, err
}
