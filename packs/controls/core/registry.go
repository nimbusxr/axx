package controlscore

import (
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
)

// A Framework is a set of controls that a scenario's tags can name: OWASP's
// ASVS, NIST's SP 800-53, a company's own rules. A pack that carries one
// registers it (RegisterFramework), and the project's scenarios then claim its
// controls with tags of its key: @asvs:6.2.1.
type Framework struct {
	// Key is what its tags start with: "asvs" for @asvs:6.2.1. Lower case
	// letters, digits and hyphens.
	Key string
	// Title and Version name it: "OWASP ASVS", "5.0.0". A list that names a
	// version is of that version, and a report says which it is of.
	Title   string
	Version string
	// Notice says where the framework's text is from and under what terms, when
	// it must be said wherever the text is shown ("OWASP ASVS 5.0.0, CC BY-SA
	// 4.0"). axx prints it with the text.
	Notice string
	// Controls are its controls, in its own order.
	Controls []Control
	// Lists are the lists of its controls a project can require by name: a
	// level of ASVS, a company's baseline.
	Lists []List
	// Same says whether two ways of writing a control's ID name the same
	// control, where a framework has several (NIST's IA-5(1), IA-5.1 and
	// ia-5.1). nil: an ID is written one way, in any case of its letters.
	Same func(a, b string) bool
}

// Control is one control of a framework.
type Control struct {
	// ID is the control's own identifier in its framework: "6.2.1", "IA-5".
	ID string
	// Title is a few words for it, and Group where it is in its framework
	// ("V6 Authentication, Password Security"): what a report lists it by.
	Title string
	Group string
	// Text is what the control requires, in the framework's words. It is
	// shown where a person reads up on a control, and is not written into
	// evidence: a framework's text has its own terms.
	Text string
}

// List is a list of a framework's controls that a project can require.
type List struct {
	// Name is what `require` names it by: "asvs-5.0-level-1".
	Name string
	// Title says what it is: "OWASP ASVS 5.0, level 1".
	Title string
	// Controls are the controls it requires.
	Controls []Required
}

// Required is a control a list requires.
type Required struct {
	// ID is the control's, in the list's framework.
	ID string
	// Scenarios is how many passing scenarios it takes to prove it, when
	// more than one: a control with several sides needs a scenario for
	// each.
	Scenarios int
}

// A Format writes the report of the controls in a format of its own: OSCAL's
// assessment results, say. `axx controls report --format <name>` uses it.
type Format struct {
	// Name is the format's: "oscal". Doc says what it writes, in a line.
	Name string
	Doc  string
	// Write writes the report.
	Write func(w io.Writer, r *Report) error
}

// A Source reads what another tool found of the controls (a scanner's
// results, an auditor's sheet) as evidence, which the report then shows beside
// what the scenarios proved.
type Source struct {
	// Name is the source's: "sarif". Doc says what it reads, in a line.
	Name string
	Doc  string
	// Read reads a file of its kind. ok is false for a file that is not
	// one, which the other sources are then tried on.
	Read func(path string, data []byte) (e *Evidence, ok bool, err error)
}

var (
	mu         sync.Mutex
	frameworks = map[string]*Framework{}
	formats    = map[string]Format{}
	sources    = map[string]Source{}
)

// RegisterFramework adds a framework whose controls scenarios can claim, and
// whose lists a project can require. One registered again replaces the one
// before it.
func RegisterFramework(f Framework) {
	mu.Lock()
	defer mu.Unlock()
	f.Key = strings.ToLower(f.Key)
	frameworks[f.Key] = &f
}

// RegisterFormat adds a format the report can be written in.
func RegisterFormat(f Format) {
	mu.Lock()
	defer mu.Unlock()
	formats[f.Name] = f
}

// RegisterSource adds a kind of file that is read as evidence.
func RegisterSource(s Source) {
	mu.Lock()
	defer mu.Unlock()
	sources[s.Name] = s
}

// registered are the registered frameworks, by key.
func registered() map[string]*Framework {
	mu.Lock()
	defer mu.Unlock()
	return maps.Clone(frameworks)
}

func formatOf(name string) (Format, bool) {
	mu.Lock()
	defer mu.Unlock()
	f, ok := formats[name]
	return f, ok
}

// formatNames are the registered formats' names, in order.
func formatNames() []string {
	mu.Lock()
	defer mu.Unlock()
	return slices.Sorted(maps.Keys(formats))
}

// registeredSources are the registered sources, in the order of their names.
func registeredSources() []Source {
	mu.Lock()
	defer mu.Unlock()
	var out []Source
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		out = append(out, sources[name])
	}
	return out
}

// control finds a framework's control by an ID as a tag or a list writes it.
func (f *Framework) control(id string) (*Control, bool) {
	for i := range f.Controls {
		if f.same(f.Controls[i].ID, id) {
			return &f.Controls[i], true
		}
	}
	return nil, false
}

func (f *Framework) same(a, b string) bool {
	if f.Same != nil {
		return f.Same(a, b)
	}
	return strings.EqualFold(a, b)
}

// nearest is the control of the framework whose ID is closest to one that is
// not in it, "" when none is close: what a mistyped tag probably meant.
func (f *Framework) nearest(id string) string {
	best, bestDist := "", 3
	for _, c := range f.Controls {
		if d := distance(strings.ToLower(c.ID), strings.ToLower(id)); d < bestDist {
			best, bestDist = c.ID, d
		}
	}
	return best
}

// distance is the edit distance of two short strings.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
