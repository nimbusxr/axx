package lint

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/fixtures"
)

// Project runs everything `axx lint` checks for a loaded configuration: the
// test-data isolation rules of its lint section and the builtin feature-file
// checks over run.paths. Configuration errors are returned; findings are in
// the report.
func Project(ctx context.Context, cfg *config.Config, opts Options) (*Report, error) {
	set, err := Load(cfg)
	if err != nil {
		return nil, err
	}
	rep := set.Run(opts)
	e, err := engine.New(engine.Options{Config: cfg})
	if err != nil {
		rep.Notes = append(rep.Notes, "feature files were not checked: "+firstLine(err))
		return rep, nil
	}
	paths, _, err := e.FeaturePaths(nil)
	if err != nil {
		return rep, nil //nolint:nilerr // no feature paths, nothing to check
	}
	set2, err := e.LoadFeatures(paths)
	if err != nil {
		var ae *axxerr.Error
		if errors.As(err, &ae) && ae.Code == feature.CodeNotFound {
			return rep, nil // no features yet
		}
		rep.Notes = append(rep.Notes, "some feature files were not checked (run `axx validate`): "+firstLine(err))
	}
	if set2 != nil && len(set2.Pickles) > 0 {
		for _, rr := range FeatureChecks(e.Registry, set2.Pickles, opts.WorkDir) {
			rep.Add(rr)
		}
		rep.Filter(opts)
		rep.Hints = FixtureHints(e.Registry, set2.Pickles, FixtureSources{Resolve: e.ResolvePath, Generated: generated(cfg)}, opts)
	}
	return rep, nil
}

// generated reports whether a fixture factory generates a file: whether
// axx-fixtures.manifest.yaml records it.
func generated(cfg *config.Config) func(abs string) bool {
	base := cfg.FixturesDir()
	m, err := fixtures.LoadManifest(base)
	if err != nil {
		return nil
	}
	return func(abs string) bool {
		rel, err := filepath.Rel(base, abs)
		return err == nil && m.Managed(filepath.ToSlash(rel))
	}
}

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSuffix(s, ":")
}
