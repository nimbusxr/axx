package jsonx

import (
	"errors"
	"fmt"
)

// ErrorKind identifies the Java exception class Jayway JsonPath and
// json-smart raise for an operation. Kinds form the same hierarchy as the Java classes; see
// [ErrorKind.Is].
type ErrorKind int

// Error kinds, named after the Java exception classes they stand for.
const (
	// JsonPathError is com.jayway.jsonpath.JsonPathException, the root of
	// the JsonPath exception hierarchy.
	JsonPathError ErrorKind = iota + 1
	// InvalidPath is com.jayway.jsonpath.InvalidPathException (a JsonPathError).
	InvalidPath
	// PathNotFound is com.jayway.jsonpath.PathNotFoundException (an InvalidPath).
	PathNotFound
	// InvalidModification is com.jayway.jsonpath.InvalidModificationException
	// (a JsonPathError).
	InvalidModification
	// InvalidJSON is com.jayway.jsonpath.InvalidJsonException (a JsonPathError).
	InvalidJSON
	// IllegalArgument is java.lang.IllegalArgumentException.
	IllegalArgument
	// NumberFormat is java.lang.NumberFormatException (an IllegalArgument).
	NumberFormat
	// IndexOutOfBounds is java.lang.IndexOutOfBoundsException.
	IndexOutOfBounds
	// StringIndexOutOfBounds is java.lang.StringIndexOutOfBoundsException
	// (an IndexOutOfBounds).
	StringIndexOutOfBounds
	// UnsupportedOperation is java.lang.UnsupportedOperationException.
	UnsupportedOperation
	// NullPointer is java.lang.NullPointerException.
	NullPointer
	// ClassCast is java.lang.ClassCastException.
	ClassCast
	// IllegalState is java.lang.IllegalStateException.
	IllegalState
	// Timeout is not a Java exception: a regular expression in a filter ran
	// longer than the match timeout (Java would have kept running).
	Timeout
	// PatternSyntax is java.util.regex.PatternSyntaxException (an
	// IllegalArgument).
	PatternSyntax
	// JSONParse is Jackson's JsonProcessingException (a malformed JSON text
	// read strictly).
	JSONParse
)

var kindInfo = map[ErrorKind]struct {
	class  string
	parent ErrorKind
}{
	JsonPathError:          {"JsonPathException", 0},
	InvalidPath:            {"InvalidPathException", JsonPathError},
	PathNotFound:           {"PathNotFoundException", InvalidPath},
	InvalidModification:    {"InvalidModificationException", JsonPathError},
	InvalidJSON:            {"InvalidJsonException", JsonPathError},
	IllegalArgument:        {"IllegalArgumentException", 0},
	NumberFormat:           {"NumberFormatException", IllegalArgument},
	IndexOutOfBounds:       {"IndexOutOfBoundsException", 0},
	StringIndexOutOfBounds: {"StringIndexOutOfBoundsException", IndexOutOfBounds},
	UnsupportedOperation:   {"UnsupportedOperationException", 0},
	NullPointer:            {"NullPointerException", 0},
	ClassCast:              {"ClassCastException", 0},
	IllegalState:           {"IllegalStateException", 0},
	Timeout:                {"RegexTimeoutException", 0},
	PatternSyntax:          {"PatternSyntaxException", IllegalArgument},
	JSONParse:              {"JsonProcessingException", 0},
}

// JavaClass returns the simple name of the Java exception class.
func (k ErrorKind) JavaClass() string { return kindInfo[k].class }

// String returns the Java class name.
func (k ErrorKind) String() string { return k.JavaClass() }

// Is reports whether k is target or one of its subclasses, as Java's
// instanceof would.
func (k ErrorKind) Is(target ErrorKind) bool {
	for c := k; c != 0; c = kindInfo[c].parent {
		if c == target {
			return true
		}
	}
	return false
}

// Error is the exception Jayway JsonPath or json-smart throws. Message
// is Java's getMessage(); NoMessage records that it was null.
type Error struct {
	Kind      ErrorKind
	Message   string
	NoMessage bool
}

func (e *Error) Error() string {
	if e.NoMessage {
		return e.Kind.JavaClass()
	}
	return e.Kind.JavaClass() + ": " + e.Message
}

// JavaMessage returns the message as Java string concatenation renders it:
// "null" when Java's getMessage() returned null.
func (e *Error) JavaMessage() string {
	if e.NoMessage {
		return "null"
	}
	return e.Message
}

// Is makes errors.Is(err, &jsonx.Error{Kind: k}) match k and its subclasses.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && e.Kind.Is(t.Kind)
}

// Sentinels for errors.Is.
var (
	ErrJsonPath            = &Error{Kind: JsonPathError}
	ErrInvalidPath         = &Error{Kind: InvalidPath}
	ErrPathNotFound        = &Error{Kind: PathNotFound}
	ErrInvalidModification = &Error{Kind: InvalidModification}
	ErrInvalidJSON         = &Error{Kind: InvalidJSON}
)

// IsJsonPathException reports whether err is (a subclass of) Jayway's
// JsonPathException, the class json-path-assert matchers treat as "no match".
func IsJsonPathException(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind.Is(JsonPathError)
}

func newErr(kind ErrorKind, format string, args ...any) *Error {
	if len(args) == 0 {
		return &Error{Kind: kind, Message: format}
	}
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// throw panics with a Java-equivalent exception; catch recovers it at API
// boundaries. The engine is a port of Java code that relies on exceptions for
// control flow, and keeping that shape keeps the port reviewable against the
// original.
func throw(kind ErrorKind, format string, args ...any) {
	panic(newErr(kind, format, args...))
}

func throwNoMessage(kind ErrorKind) {
	panic(&Error{Kind: kind, NoMessage: true})
}

// wrapCause builds the exception Java's `new X(cause)` constructor creates:
// its message is cause.toString(), i.e. "<fully.qualified.Class>: <message>".
func wrapCause(kind ErrorKind, cause *Error) *Error {
	return &Error{Kind: kind, Message: qualifiedClass(cause.Kind) + causeSuffix(cause)}
}

func causeSuffix(cause *Error) string {
	if cause.NoMessage {
		return ""
	}
	return ": " + cause.Message
}

func qualifiedClass(k ErrorKind) string {
	switch k {
	case JsonPathError, InvalidPath, PathNotFound, InvalidModification, InvalidJSON:
		return "com.jayway.jsonpath." + k.JavaClass()
	case Timeout:
		return k.JavaClass()
	case PatternSyntax:
		return "java.util.regex." + k.JavaClass()
	case JSONParse:
		return "com.fasterxml.jackson.core." + k.JavaClass()
	default:
		return "java.lang." + k.JavaClass()
	}
}

// catch runs fn and returns the Java-equivalent exception it threw, if any.
// Other panics propagate.
func catch(fn func()) (err *Error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			err = e
		}
	}()
	fn()
	return nil
}

// catchKind runs fn and returns an exception of the given kind (or a
// subclass); other exceptions propagate.
func catchKind(kind ErrorKind, fn func()) *Error {
	err := catch(fn)
	if err != nil && !err.Kind.Is(kind) {
		panic(err)
	}
	return err
}
