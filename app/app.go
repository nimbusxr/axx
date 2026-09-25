// Package app runs the axx command line. The axx binary calls Main with no
// packs: axx builds a copy of itself for the packs a project lists in
// axx-packs.yaml, whose main registers them here.
package app

import (
	"os"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cli"
	"github.com/nimbusxr/axx/internal/engine"
)

// Main registers packs, keyed by their axx-packs.yaml entry (rest, ./steps,
// a module path), runs the axx command line and exits with its code.
func Main(packs map[string]core.Pack) {
	for key, p := range packs {
		engine.Register(key, p)
	}
	os.Exit(cli.Main(os.Args[1:]))
}
