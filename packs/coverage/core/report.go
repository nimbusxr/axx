package coveragecore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// reportRuntime is a service that writes its coverage itself, by its own
// tools, as lcov (*.info) or Cobertura (*.xml) in its folder: any language.
type reportRuntime struct{}

func (reportRuntime) Name() string { return "report" }

// Measured: a report can have every metric; Convert says which this one
// has.
func (reportRuntime) Measured() istanbul.Measured {
	return istanbul.Measured{Branches: true, Functions: true}
}

func (reportRuntime) Launch(context.Context, LaunchCall) ([]string, error) { return nil, nil }

func (reportRuntime) Convert(_ context.Context, call ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	return readReports(call.End.Dir, call.Path)
}

// readReports reads the lcov and Cobertura reports in dir and what they
// measure, their paths mapped by path.
func readReports(dir string, path func(string) string) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	var all []*istanbul.FileCoverage
	var measured istanbul.Measured
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		read := istanbul.ReadLcov
		switch strings.ToLower(filepath.Ext(p)) {
		case ".info", ".lcov":
		case ".xml":
			read = istanbul.ReadCobertura
		default:
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		files, m, err := read(f)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		measured.Branches = measured.Branches || m.Branches
		measured.Functions = measured.Functions || m.Functions
		for _, fc := range files {
			fc.Path = path(fc.Path)
		}
		all = append(all, files...)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, measured, nil
	}
	return all, measured, err
}
