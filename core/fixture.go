package core

import "context"

// FixtureFunction is an expression function a pack adds to the project's
// fixture factories. A value of a fixture spec that is "${ name(args) }"
// calls it when `axx fixtures generate` expands the fixtures, and the
// generated file has what it returns. axx has functions of its own for
// hashing and encoding; a project's pack adds what its data needs: a token
// in the service's format, a checksum, a value a local service issues.
//
// A function is pure or recorded. A pure function's value depends only on
// its call: axx calls it again on every `axx fixtures generate` and `axx
// fixtures check`, so a change of the function shows as fixtures to
// generate again. A recorded function may ask something that runs (a
// tokenization service of the local stack): `axx fixtures generate` calls
// it and writes each result to axx-fixtures.pairings.yaml, which is
// committed, and `axx fixtures check` and packs' instances read that file
// and never call it.
type FixtureFunction struct {
	// Name is what expressions call: letters, digits, "_" and "-", a
	// letter first. Two functions of one name stop the command.
	Name string `json:"name"`
	// Doc says what it returns and what its arguments are, in a line.
	Doc string `json:"doc,omitempty"`
	// Recorded says its results are recorded and read back, not computed
	// again.
	Recorded bool `json:"recorded,omitempty"`
	// Version is the version of a recorded function's behavior: results
	// recorded under another are recorded again. Change it when the same
	// arguments give another value.
	Version string `json:"version,omitempty"`
	// Apply computes the value.
	Apply func(call *FixtureCall) (string, error) `json:"-"`
	// Before runs once before the first Apply of an `axx fixtures
	// generate`, and After once at its end, when Before ran: for a recorded
	// function that starts what it asks. Neither runs when every result is
	// recorded already.
	Before func(ctx context.Context) error `json:"-"`
	After  func(ctx context.Context) error `json:"-"`
}

// FixtureCall is one call of a fixture's expression.
type FixtureCall struct {
	// Args are the arguments as text: a literal as written, without its
	// quotes, and a field's name as that field's value.
	Args []string
	// Instance identifies what the value is for: a fixture of a factory,
	// or a pack's instance of one. A value that must differ from one to
	// the next derives from it, so that it is the same on every run.
	Instance string
	// Attributes are what a pack that produces instances says of this one.
	Attributes map[string]string
}
