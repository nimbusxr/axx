// Package mutant lists the deliberate bugs the evals app (the parcels service)
// can be started with. A task's verifier runs the agent's features against the
// correct app and against each mutant the task targets: a good acceptance test
// passes on the first and fails on every targeted mutant.
//
// The app reads the active mutants from EVALS_MUTANT (comma-separated). The
// variable is set only by the verifier, never in the agent's environment.
package mutant

import (
	"sort"
	"strings"
)

// EnvVar selects the active mutants.
const EnvVar = "EVALS_MUTANT"

// Mutant is one deliberate bug.
type Mutant struct {
	Name string
	// Area is the part of the service it breaks (for docs and reports).
	Area string
	// Bug describes what goes wrong, in product terms.
	Bug string
}

// All is every mutant the app implements.
var All = []Mutant{
	{"wrong-status-on-create", "rest", "registering a parcel answers 200 instead of 201"},
	{"wrong-status-on-duplicate", "rest", "registering an existing reference answers 200 with the existing parcel instead of 409"},
	{"update-not-persisted", "rest", "PATCH answers 200 with the changed parcel but does not store the change"},
	{"delete-not-removed", "rest", "DELETE answers 204 but the parcel can still be fetched"},
	{"list-ignores-sender-filter", "rest", "listing parcels by sender returns every sender's parcels"},
	{"no-weight-limit", "validation", "parcels heavier than 30000 g are accepted"},
	{"no-service-level-check", "validation", "unknown service levels are accepted"},
	{"skips-address-check", "address", "the address service is never called; the zone is UNKNOWN"},
	{"address-check-without-api-key", "address", "the address service is called without the X-Api-Key header"},
	{"ignores-undeliverable", "address", "parcels to undeliverable postcodes are registered anyway"},
	{"import-drops-postcode", "import", "imported parcels lose the recipient postcode"},
	{"import-leaves-line-pending", "import", "imported manifest lines stay PENDING"},
	{"import-wrong-source", "import", "imported parcels are recorded with source api instead of manifest"},
	{"import-accepts-overweight", "import", "overweight manifest lines are imported instead of rejected"},
	{"tracking-status-from-first-scan", "tracking", "the tracking view shows the earliest scan instead of the latest"},
	{"tracking-counts-duplicate-scans", "tracking", "duplicate scans (same scan id) are counted twice"},
	{"event-not-published", "events", "no ParcelRegistered event is published"},
	{"event-weight-in-kilograms", "events", "the event's weightGrams carries kilograms"},
}

// Known reports whether name is a mutant.
func Known(name string) bool {
	for _, m := range All {
		if m.Name == name {
			return true
		}
	}
	return false
}

// Names returns every mutant name, sorted.
func Names() []string {
	out := make([]string, len(All))
	for i, m := range All {
		out[i] = m.Name
	}
	sort.Strings(out)
	return out
}

// Set is the set of active mutants.
type Set map[string]bool

// Parse reads a comma-separated EVALS_MUTANT value. Unknown names are
// returned separately so the app can refuse to start with a typo.
func Parse(v string) (Set, []string) {
	s := Set{}
	var unknown []string
	for _, n := range strings.Split(v, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if !Known(n) {
			unknown = append(unknown, n)
			continue
		}
		s[n] = true
	}
	return s, unknown
}

// On reports whether the named mutant is active.
func (s Set) On(name string) bool { return s[name] }
