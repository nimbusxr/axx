package core

import (
	"errors"
	"fmt"
)

// AssertionError reports an expectation that did not hold. Reporters show
// Expected and Actual side by side; agents receive them as structured data.
type AssertionError struct {
	Message  string `json:"message"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
}

func (e *AssertionError) Error() string {
	if e.Expected == nil && e.Actual == nil {
		return e.Message
	}
	return fmt.Sprintf("%s\n  expected: %s\n  actual:   %s", e.Message, render(e.Expected), render(e.Actual))
}

func render(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%v", v)
}

// Fail returns an assertion failure with expected and actual values.
func Fail(message string, expected, actual any) error {
	return &AssertionError{Message: message, Expected: expected, Actual: actual}
}

// Failf returns an assertion failure without expected/actual values.
func Failf(format string, args ...any) error {
	return &AssertionError{Message: fmt.Sprintf(format, args...)}
}

// ErrPending marks a step as not yet implemented.
var ErrPending = errors.New("pending")

// ErrSkip skips the rest of the scenario without failing it.
var ErrSkip = errors.New("skipped")

// IsAssertion reports whether err is (or wraps) an assertion failure.
func IsAssertion(err error) bool {
	var ae *AssertionError
	return errors.As(err, &ae)
}
