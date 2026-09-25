package fixtures

// File names the factory owns, relative to the resource root.
const (
	// ManifestFile is the committed ownership list.
	ManifestFile = "axx-fixtures.manifest.yaml"
	// PairingsFile is the committed record of recorded-function results.
	PairingsFile = "axx-fixtures.pairings.yaml"
	// DefaultLintOutput is where generated lint rules are written.
	DefaultLintOutput = "axx-lint.generated.yaml"
	// Owner marks manifest entries the tool writes about itself.
	Owner = "<axx-fixtures>"
)

// DefaultFactories locates factory specs when fixtures.factories is unset.
var DefaultFactories = []string{"**.factory.yaml"}

// Config is the fixtures section of axx.yaml with every default applied and
// BaseDir made absolute.
type Config struct {
	// BaseDir is the resource root every relative path resolves against.
	BaseDir string
	// Factories are globs (relative to BaseDir) locating factory specs.
	Factories []string
	// Sources are additional roots, relative to BaseDir ("../" reaches
	// outside it), walked for specs exactly like BaseDir itself.
	Sources []string
	// Conformance rules validate files no factory manages.
	Conformance []ConformanceRule
	// LintEmit writes generated lint rules for declared identities.
	LintEmit bool
	// LintOutput is where they go, relative to BaseDir.
	LintOutput string
	// OutputIgnored is the default for factory.output.ignored.
	OutputIgnored bool
}

// ConformanceRule validates matched files through a family's oracle.
type ConformanceRule struct {
	Name         string   `json:"name"`
	FilePatterns []string `json:"filePatterns"`
	SchemaType   string   `json:"schemaType"`
	SchemaRef    string   `json:"schemaRef"`
}

// withDefaults fills unset fields.
func (c Config) withDefaults() Config {
	if len(c.Factories) == 0 {
		c.Factories = DefaultFactories
	}
	if c.LintOutput == "" {
		c.LintOutput = DefaultLintOutput
	}
	return c
}

// DefaultConfig is the configuration used when axx.yaml has no fixtures
// section: every convention, rooted at baseDir.
func DefaultConfig(baseDir string) Config {
	return Config{BaseDir: baseDir, LintEmit: true}.withDefaults()
}
