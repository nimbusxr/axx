// Command genschema writes the axx.yaml JSON Schema generated from the Go types.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/config"
)

func main() {
	out := flag.String("out", "axx.schema.json", "output file")
	flag.Parse()

	// go generate runs in the package directory; comments are read relative
	// to the module root.
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..", "..")
	if err := os.Chdir(root); err != nil {
		fail(err)
	}
	b, err := config.Generate(true)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(wd, *out), b, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "genschema:", err)
	os.Exit(1)
}
