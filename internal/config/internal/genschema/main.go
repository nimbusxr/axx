// Command genschema writes the axx.yaml JSON Schema generated from the Go types,
// and copies every schema beside it to the IntelliJ plugin's resources (-copy):
// the plugin bundles them, and its own folder is what its releases are made of.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/config"
)

func main() {
	out := flag.String("out", "axx.schema.json", "output file")
	copyTo := flag.String("copy", "", "a folder to copy every schema beside the output file to")
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
	if *copyTo != "" {
		if err := copySchemas(filepath.Dir(filepath.Join(wd, *out)), filepath.Join(wd, *copyTo)); err != nil {
			fail(err)
		}
	}
}

// copySchemas makes to's schemas the same as from's: every .schema.json file
// copied, and none that from has not.
func copySchemas(from, to string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	isSchema := func(e os.DirEntry) bool { return !e.IsDir() && strings.HasSuffix(e.Name(), ".schema.json") }
	have, err := os.ReadDir(to)
	if err != nil {
		return err
	}
	for _, e := range have {
		if _, err := os.Stat(filepath.Join(from, e.Name())); isSchema(e) && os.IsNotExist(err) {
			if err := os.Remove(filepath.Join(to, e.Name())); err != nil {
				return err
			}
		}
	}
	want, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range want {
		if !isSchema(e) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "genschema:", err)
	os.Exit(1)
}
