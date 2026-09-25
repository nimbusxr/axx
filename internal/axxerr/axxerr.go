// Package axxerr provides axx's structured error type.
//
// Every user-facing error carries a stable code (AXX-Exxxx), an optional
// source location, a hint and a docs link, so both humans and agents can act
// on it without guessing.
package axxerr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/internal/exitcode"
)

// DocsBase is the root of the documentation site used for error links.
const DocsBase = "https://axx.nimbusxr.us"

// Location points at a position in a source file.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

func (l Location) String() string {
	var b strings.Builder
	b.WriteString(l.File)
	if l.Line > 0 {
		fmt.Fprintf(&b, ":%d", l.Line)
		if l.Column > 0 {
			fmt.Fprintf(&b, ":%d", l.Column)
		}
	}
	return b.String()
}

// Error is a structured, user-facing error.
type Error struct {
	Code     string        `json:"code"`
	Message  string        `json:"message"`
	Location *Location     `json:"location,omitempty"`
	Hint     string        `json:"hint,omitempty"`
	Exit     exitcode.Code `json:"-"`
	Cause    error         `json:"-"`
}

func (e *Error) Error() string {
	var b strings.Builder
	if e.Location != nil {
		b.WriteString(e.Location.String())
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Cause != nil {
		cause := e.Cause.Error()
		if !strings.HasPrefix(cause, "\n") {
			b.WriteString(": ")
		}
		b.WriteString(cause)
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Cause }

// DocsURL links to the reference entry for the error code.
func (e *Error) DocsURL() string {
	if e.Code == "" {
		return ""
	}
	return DocsBase + "/references/error-codes/#" + strings.ToLower(e.Code)
}

// New creates an error with the given code, exit status and message.
func New(code string, exit exitcode.Code, format string, args ...any) *Error {
	return &Error{Code: code, Exit: exit, Message: fmt.Sprintf(format, args...)}
}

// Wrap creates an error that wraps cause.
func Wrap(cause error, code string, exit exitcode.Code, format string, args ...any) *Error {
	e := New(code, exit, format, args...)
	e.Cause = cause
	return e
}

// At attaches a source location and returns e for chaining.
func (e *Error) At(file string, line, column int) *Error {
	e.Location = &Location{File: file, Line: line, Column: column}
	return e
}

// WithHint attaches a remediation hint and returns e for chaining.
func (e *Error) WithHint(format string, args ...any) *Error {
	e.Hint = fmt.Sprintf(format, args...)
	return e
}

// ExitCode returns the exit status implied by err: the status of the first
// *Error in the chain, or exitcode.Failed for any other non-nil error.
func ExitCode(err error) exitcode.Code {
	if err == nil {
		return exitcode.OK
	}
	var ae *Error
	if errors.As(err, &ae) && ae.Exit != exitcode.OK {
		return ae.Exit
	}
	return exitcode.Failed
}
