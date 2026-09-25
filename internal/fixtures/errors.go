package fixtures

import (
	"errors"
	"fmt"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Error codes. Every failure of the fixture factory is an *axxerr.Error
// carrying one of these.
const (
	// CodeConfig: the fixtures section of axx.yaml is unusable (a sources
	// root that does not exist or overlaps, a malformed glob, a conformance
	// rule naming an unknown family).
	CodeConfig = "AXX-E0900"
	// CodeSpec: a *.factory.yaml, *.fixture.yaml or *.prototype.yaml file is
	// malformed or cannot be bound to a factory.
	CodeSpec = "AXX-E0901"
	// CodeGenerate: expanding the specs failed (an unresolved required
	// field, a value of the wrong type, a schema oracle rejecting the output,
	// an identity collision, a broken $ref or expression).
	CodeGenerate = "AXX-E0902"
	// CodeHandEdit: generate refused to overwrite managed files whose
	// content no longer matches the manifest.
	CodeHandEdit = "AXX-E0903"
	// CodeCheck: `axx fixtures check` found drift, a manifest mismatch or a
	// conformance failure.
	CodeCheck = "AXX-E0904"
	// CodeAdopt: adoption refused (managed files, an existing factory, key
	// collisions, a failed round trip).
	CodeAdopt = "AXX-E0905"
	// CodeGit: `axx fixtures untrack` could not run git.
	CodeGit = "AXX-E0906"
	// CodeSchema: a governing schema (avsc, JSON Schema, OpenAPI, XSD,
	// proto, DDL) cannot be read or compiled.
	CodeSchema = "AXX-E0907"
	// CodeIO: a file could not be read or written.
	CodeIO = "AXX-E0908"
)

const hintGenerate = "fix the factory sources (defaults:, prototype, fixture data) and run `axx fixtures generate`"

func specError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeSpec, exitcode.Usage, format, args...).
		WithHint("`axx schema --kind factory|fixture|prototype` prints the file formats")
}

func configError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeConfig, exitcode.Usage, format, args...).
		WithHint("check the fixtures section of axx.yaml")
}

func genError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeGenerate, exitcode.Failed, format, args...).WithHint(hintGenerate)
}

func checkError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeCheck, exitcode.Failed, format, args...).WithHint(hintGenerate)
}

// codeOf returns the error code of err ("" when it has none).
func codeOf(err error) string {
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func schemaError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeSchema, exitcode.Usage, format, args...).
		WithHint("factory.schema is relative to the factory file; conformance schemaRef to fixtures.baseDir")
}

func adoptError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeAdopt, exitcode.Failed, format, args...).
		WithHint("adoption writes nothing when it refuses; fix the cause and run it again")
}

func ioError(err error, format string, args ...any) *axxerr.Error {
	e := axxerr.New(CodeIO, exitcode.Failed, format, args...)
	e.Message = fmt.Sprintf("%s: %v", e.Message, err)
	return e.WithHint("check that the path exists and is readable/writable")
}

// message returns the text of a failure without its code, for embedding one
// failure in another.
func errText(err error) string {
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		return ae.Message
	}
	return err.Error()
}

// withCode keeps err when it is already an *axxerr.Error and wraps anything
// else as a generation failure.
func withCode(err error) error {
	if err == nil {
		return nil
	}
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		return err
	}
	return genError("%s", err.Error())
}
