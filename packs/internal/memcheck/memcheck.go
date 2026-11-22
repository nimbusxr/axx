// SPDX-License-Identifier: Apache-2.0

// Package memcheck builds axx's C, C++ and Objective-C under a memory checker, for axx's own
// tests: the libraries the packs load into apps (the simulators' library, the AR library, the
// Apple model adapter) are the code of axx that Go does not keep memory-safe.
//
// With AXX_MEMCHECK naming a folder, each of them is built with AddressSanitizer, and with where
// its reports go: a file in that folder for each process that had one, which the checker ends as
// it writes it. The folder also lists the libraries that were built checked (Record), so that a
// run can show it checked something. The library brings the checker's runtime along (Xcode's, by the library's own
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
	"strings"
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
// and the checked one never share a place, nor do two that report to different folders, nor two
// of different options (a library has its options built in).
func Key() []byte {
	if !Enabled() {
		return nil
	}
	return []byte("memcheck:" + options())
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
	return []byte("const char *__asan_default_options(void) { return " + strconv.Quote(options()) + "; }\n")
}

// options are the checker's, as it reads them: apart by spaces, the path in quotes, as a path
// has colons on Windows (the checker's other separator) and may have spaces anywhere.
//
// The checker keeps no record of where memory was allocated and freed (malloc_context_size=0).
// A library the iOS pack loads into a simulator is in every process the simulator starts from
// then on, a hundred of them, and each keeps that record for all of its own allocations: on one
// simulator the processes took 6.6 GB more with it and 3.1 GB more without, of 37 GB. A report
// still has the fault and where axx's code was, which is what fails a job; to see where the
// memory came from as well, run the test again with ASAN_OPTIONS=malloc_context_size=30 in the
// process's environment, which the checker reads over these.
func options() string {
	path := filepath.Join(Dir(), "report")
	quote := "'"
	if strings.Contains(path, quote) {
		quote = `"`
	}
	return "log_path=" + quote + path + quote + " abort_on_error=1 malloc_context_size=0"
}

// LibrariesName is the file in Dir that lists the libraries built checked.
const LibrariesName = "libraries"

// Record notes a library that was built checked, or taken checked from axx's cache, as a line of
// Dir's list of them, and returns what it is given: a builder's result goes through it. A run
// that was to check memory and lists none checked nothing, and the list says so: that every
// scenario passed says nothing of it.
func Record(library string, err error) (string, error) {
	if err != nil || !Enabled() {
		return library, err
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return "", err
	}
	list, err := os.OpenFile(filepath.Join(Dir(), LibrariesName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	_, err = list.WriteString(library + "\n")
	if closed := list.Close(); err == nil {
		err = closed
	}
	if err != nil {
		return "", err
	}
	return library, nil
}

// Libraries are the libraries Record noted, each once, in the order they were first noted.
func Libraries() ([]string, error) {
	if !Enabled() {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(Dir(), LibrariesName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var libraries []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && !seen[line] {
			seen[line] = true
			libraries = append(libraries, line)
		}
	}
	return libraries, nil
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
