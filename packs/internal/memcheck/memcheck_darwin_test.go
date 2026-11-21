// SPDX-License-Identifier: Apache-2.0

package memcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A library built as the packs build theirs when checked, and loaded as they load theirs (into a
// program that was not built checked), has its overflow reported to the folder, and the program
// ended; one without a fault runs to its end and reports nothing. That is the whole mechanism:
// what the packs' own tests then run under it is their libraries.
func TestALibrarysOverflowIsReported(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("needs Xcode's compiler")
	}
	work := t.TempDir()
	reports := t.TempDir()
	t.Setenv(Env, reports)

	write := func(name, text string) string {
		path := filepath.Join(work, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	run := func(name string, args ...string) {
		t.Helper()
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, out)
		}
	}
	host := filepath.Join(work, "host")
	run("xcrun", "clang", write("host.c", "#include <stdio.h>\nint main(void) { puts(\"the host ran\"); return 0; }\n"), "-o", host)
	library := write("library.m", `#import <Foundation/Foundation.h>
#include <stdlib.h>
__attribute__((constructor)) static void loaded(void) {
  char *bytes = malloc(8);
  if (getenv("AXX_MEMCHECK_TEST_OVERFLOW")) {
    volatile int past = 8;
    bytes[past] = 1;
  }
  free(bytes);
}
`)
	options := write(SourceName, string(Source()))
	built := filepath.Join(work, "library.dylib")
	run("xcrun", append([]string{"clang", "-dynamiclib", "-fobjc-arc", "-framework", "Foundation", library, options, "-o", built}, Clang()...)...)

	start := func(overflow bool) (string, error) {
		cmd := exec.Command(host)
		cmd.Env = append(os.Environ(), "DYLD_INSERT_LIBRARIES="+built)
		if overflow {
			cmd.Env = append(cmd.Env, "AXX_MEMCHECK_TEST_OVERFLOW=1")
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := start(false)
	if err != nil || !strings.Contains(out, "the host ran") {
		t.Fatalf("with the checked library and no fault, the host: %v: %s", err, out)
	}
	if found, _ := Reports(); len(found) != 0 {
		t.Fatalf("no fault, and yet reports: %v", found)
	}

	out, err = start(true)
	if err == nil {
		t.Fatalf("the host ran to its end past an overflow: %s", out)
	}
	found, err := Reports()
	if err != nil || len(found) != 1 {
		t.Fatalf("Reports() = %v, %v, want the overflow's one report (the host said: %s)", found, err, out)
	}
	report, err := os.ReadFile(found[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"heap-buffer-overflow", "library.m"} {
		if !strings.Contains(string(report), want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
}
