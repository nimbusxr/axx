package mobilecore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// Documents are the files a registration's documents property names, like
// "declarations/cn23.pdf, scans/pod.jpg", resolved as a step resolves a file
// (next to the feature file first). The phone has them in one folder, by
// their names: two files of one name are refused, as is a folder.
func Documents(sc *core.Scenario, value string) ([]string, error) {
	var out []string
	named := map[string]string{}
	for _, f := range strings.Split(value, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		path, err := sc.ResolvePath(f)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", f, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", f, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%q is a folder: name the files in it, as the phone has them in one folder", f)
		}
		name := filepath.Base(path)
		if other, ok := named[name]; ok {
			return nil, fmt.Errorf("%q and %q are both %s: the phone has its documents in one folder, by their names", other, f, name)
		}
		named[name] = f
		out = append(out, path)
	}
	return out, nil
}
