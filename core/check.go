package core

import "context"

// A Check is what `axx doctor` checks for a pack: that what its steps need
// is on the machine, like an SDK or a tool.
type Check struct {
	// Name names what is checked, like "Android SDK".
	Name string
	// Run checks it.
	Run func(ctx context.Context) CheckResult
}

// CheckResult is what a check found: ok, warn or fail, what it saw, and what
// fixes a problem.
type CheckResult struct {
	Status CheckStatus
	Detail string
	Hint   string
}

// CheckStatus is a check's verdict.
type CheckStatus string

const (
	CheckOK   CheckStatus = "ok"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)
