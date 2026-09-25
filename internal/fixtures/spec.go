package fixtures

import (
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Spec is one *.factory.yaml: a fixture family, its governing schema, the
// layered values (defaults, prototype, per-fixture data) and the identities
// whose uniqueness is guaranteed across every fixture it produces.
type Spec struct {
	// SourceName is the factory file relative to the resource root.
	SourceName string
	// SourceDir is its directory ("" at the root).
	SourceDir string
	// BaseName is the file name minus .factory.yaml.
	BaseName string

	Family string
	// Schema is factory.schema as written, relative to the factory file.
	Schema string
	// OutputDir is factory.output.dir ("" when outputs follow fixtures).
	OutputDir string
	// OutputIgnored is output.ignored, or the module default.
	OutputIgnored bool
	// Options are the family-specific factory.options.
	Options *jsonx.Object

	Defaults  *jsonx.Object
	Prototype *jsonx.Object
	Metadata  *jsonx.Object
	Identity  []Identity

	// Fixtures holds every bound fixture: inline ones plus *.fixture.yaml
	// files.
	Fixtures *fixtureSet

	// Overlays of <baseName>.prototype.yaml files in other directories,
	// keyed by directory.
	prototypeOverlays map[string]*jsonx.Object
	metadataOverlays  map[string]*jsonx.Object

	inlineFixtures *jsonx.Object
	ignoredSet     *bool
}

// Fixture is one bound fixture: its data tree, what travels beside it, the
// directory its outputs follow and where it came from.
type Fixture struct {
	Data     *jsonx.Object
	Metadata *jsonx.Object
	Dir      string
	Source   string
}

// fixtureSet is an insertion-ordered set of fixtures by key.
type fixtureSet struct {
	keys []string
	byKy map[string]*Fixture
}

func newFixtureSet() *fixtureSet { return &fixtureSet{byKy: map[string]*Fixture{}} }

// Keys returns the fixture keys in binding order.
func (s *fixtureSet) Keys() []string { return append([]string(nil), s.keys...) }

// SortedKeys returns the fixture keys in Java String order.
func (s *fixtureSet) SortedKeys() []string {
	k := s.Keys()
	sortStrings(k)
	return k
}

// Get returns a fixture by key.
func (s *fixtureSet) Get(key string) *Fixture { return s.byKy[key] }

// Len is the number of fixtures.
func (s *fixtureSet) Len() int { return len(s.keys) }

func (s *fixtureSet) put(key string, f *Fixture) *Fixture {
	if prev, ok := s.byKy[key]; ok {
		return prev
	}
	s.keys = append(s.keys, key)
	s.byKy[key] = f
	return nil
}

// SchemaRef is factory.schema rebased from the factory file onto the
// resource root; a #fragment survives.
func (s *Spec) SchemaRef() string {
	ref := s.Schema
	if strings.TrimSpace(ref) == "" || strings.HasPrefix(ref, "classpath:") || strings.HasPrefix(ref, "class:") {
		return ref
	}
	path, fragment := ref, ""
	if hash := strings.IndexByte(ref, '#'); hash >= 0 {
		path, fragment = ref[:hash], ref[hash:]
	}
	joined := path
	if s.SourceDir != "" {
		joined = s.SourceDir + "/" + path
	}
	return normalizeSegments(joined) + fragment
}

// chain lists the overlay directories that apply to dir, farthest first.
func chain(overlays map[string]*jsonx.Object, dir string) []string {
	var near []string
	for d := dir; ; {
		if _, ok := overlays[d]; ok {
			near = append(near, d)
		}
		if d == "" {
			break
		}
		d = parentDir(d)
	}
	for i, j := 0, len(near)-1; i < j; i, j = i+1, j-1 {
		near[i], near[j] = near[j], near[i]
	}
	return near
}

// PrototypeFor is the effective prototype for a fixture in dir: the base
// prototype deep-merged with every ancestor overlay, nearest winning.
func (s *Spec) PrototypeFor(dir string) *jsonx.Object {
	if len(s.prototypeOverlays) == 0 {
		return s.Prototype
	}
	effective := s.Prototype
	for _, d := range chain(s.prototypeOverlays, dir) {
		effective = deepMerge(effective, s.prototypeOverlays[d])
	}
	return effective
}

// MetadataFor is the effective metadata for a fixture in dir.
func (s *Spec) MetadataFor(dir string) *jsonx.Object {
	if len(s.metadataOverlays) == 0 {
		return s.Metadata
	}
	effective := s.Metadata
	for _, d := range chain(s.metadataOverlays, dir) {
		effective = deepMerge(effective, s.metadataOverlays[d])
	}
	return effective
}

// OutputDirFor is where a fixture's files land: output.dir when declared,
// else the fixture's own directory.
func (s *Spec) OutputDirFor(fixtureKey string) string {
	if s.OutputDir != "" {
		return s.OutputDir
	}
	return s.Fixtures.Get(fixtureKey).Dir
}

// OutputDirs are the distinct output directories, sorted.
func (s *Spec) OutputDirs() []string {
	if s.OutputDir != "" {
		return []string{s.OutputDir}
	}
	seen := map[string]bool{}
	var dirs []string
	for _, k := range s.Fixtures.Keys() {
		d := s.Fixtures.Get(k).Dir
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sortStrings(dirs)
	return dirs
}

func parentDir(dir string) string {
	if i := strings.LastIndexByte(dir, '/'); i >= 0 {
		return dir[:i]
	}
	return ""
}
