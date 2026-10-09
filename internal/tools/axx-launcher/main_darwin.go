//go:build darwin

// axx-launcher is the program macOS holds responsible for axx's desktop runs
// (ADR 0013). Signed with NimbusXR's Developer ID in an app of its own, it is
// the one identity a person allows Accessibility, Screen Recording and
// Automation once: axx starts it responsible for itself, and it runs axx as
// its child, which runs the apps under test as its own.
//
//	axx-launcher program [arguments...]
//
// Its standard input and output are the program's, the stop signals it gets
// pass on, and its exit status is the program's.
package main

import (
	"fmt"
	"os"

	"github.com/nimbusxr/axx/internal/launcher"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: axx-launcher program [arguments...]")
		os.Exit(64)
	}
	code, err := launcher.Run(os.Args[1], os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "axx-launcher: cannot run %s: %v\n", os.Args[1], err)
		os.Exit(127)
	}
	os.Exit(code)
}
