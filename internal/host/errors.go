package host

import (
	"errors"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Error codes of the axx host and of runs that use one.
const (
	CodeRefused      = "AXX-E0500"
	CodeNotApproved  = "AXX-E0501"
	CodeLost         = "AXX-E0502"
	CodeRunning      = "AXX-E0503"
	CodeDefinition   = "AXX-E0504"
	CodeInterrupted  = "AXX-E0505"
	CodeNotListening = "AXX-E0506"
	CodeNotAnswered  = "AXX-E0507"
)

func hostErr(code, format string, args ...any) *axxerr.Error {
	return axxerr.New(code, exitcode.Environment, format, args...)
}

// failure is how the host reports err to a run.
func failure(err error) *Failure {
	f := &Failure{Message: err.Error(), Exit: int(exitcode.Environment)}
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		f.Code, f.Hint = ae.Code, ae.Hint
		if ae.Exit != 0 {
			f.Exit = int(ae.Exit)
		}
	}
	return f
}

// err is the error a run reports for f.
func (f *Failure) err() *axxerr.Error {
	code := f.Code
	if code == "" {
		code = CodeRefused
	}
	exit := exitcode.Code(f.Exit)
	if exit == 0 {
		exit = exitcode.Environment
	}
	e := axxerr.New(code, exit, "%s", f.Message)
	if f.Hint != "" {
		e = e.WithHint("%s", f.Hint)
	}
	return e
}
