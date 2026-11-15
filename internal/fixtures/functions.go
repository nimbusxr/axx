package fixtures

import (
	"context"
	"crypto/md5"  //nolint:gosec // a fixture's value in a format a system under test has, not a secret's protection
	"crypto/sha1" //nolint:gosec // the same
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"regexp"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// The functions every factory has: hashing and encoding, which a fixture's
// value is often of another one (a token's hash, a header's Base64). A
// project's own come from its packs (core.FixtureFunction).

// builtinFunctions are axx's own expression functions, all pure.
func builtinFunctions() []Function {
	digest := func(name, label string, h func() hash.Hash) Function {
		return pureFunction{
			name: name, doc: "the " + label + " hash of its arguments, one after another, in lowercase hexadecimal",
			apply: func(args []string) (string, error) {
				sum := h()
				for _, a := range args {
					sum.Write([]byte(a))
				}
				return hex.EncodeToString(sum.Sum(nil)), nil
			},
		}
	}
	joined := func(args []string) []byte { return []byte(strings.Join(args, "")) }
	return []Function{
		digest("sha256", "SHA-256", sha256.New),
		digest("sha512", "SHA-512", sha512.New),
		digest("sha1", "SHA-1", sha1.New),
		digest("md5", "MD5", md5.New),
		pureFunction{
			name: "base64", doc: "its arguments, one after another, in Base64",
			apply: func(args []string) (string, error) { return base64.StdEncoding.EncodeToString(joined(args)), nil },
		},
		pureFunction{
			name: "base64url", doc: "its arguments, one after another, in URL-safe Base64 without padding, as a JWT's parts are",
			apply: func(args []string) (string, error) { return base64.RawURLEncoding.EncodeToString(joined(args)), nil },
		},
		pureFunction{
			name: "hex", doc: "its arguments, one after another, in lowercase hexadecimal",
			apply: func(args []string) (string, error) { return hex.EncodeToString(joined(args)), nil },
		},
	}
}

// pureFunction is one of axx's own functions.
type pureFunction struct {
	name, doc string
	apply     func(args []string) (string, error)
}

func (f pureFunction) Name() string    { return f.name }
func (f pureFunction) Version() string { return "1" }
func (f pureFunction) Recorded() bool  { return false }
func (f pureFunction) Apply(args []string, _ *InstanceScope) (string, error) {
	if len(args) == 0 {
		return "", genError("%s needs a value: a literal in quotes, or a field's name", f.name)
	}
	return f.apply(args)
}

// FunctionDoc describes an expression function, for the commands and the
// docs that list them.
type FunctionDoc struct {
	Name string `json:"name"`
	Doc  string `json:"doc,omitempty"`
	// Recorded says its results are recorded in the pairings lock.
	Recorded bool `json:"recorded,omitempty"`
	// Pack is the pack that adds it; none for axx's own.
	Pack string `json:"pack,omitempty"`
}

// BuiltinFunctions describes axx's own expression functions.
func BuiltinFunctions() []FunctionDoc {
	var out []FunctionDoc
	for _, f := range builtinFunctions() {
		out = append(out, FunctionDoc{Name: f.Name(), Doc: f.(pureFunction).doc})
	}
	return out
}

var functionName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// PackFunction is a pack's function (core.FixtureFunction) as the factory
// calls one. pack names the pack in what it says of a function it cannot
// use: one without a name an expression can call, or without an Apply.
func PackFunction(pack string, f core.FixtureFunction) (Function, error) {
	switch {
	case !functionName.MatchString(f.Name):
		return nil, configError("the %s pack's fixture function %q has no name an expression can call: letters, digits, _ and -, a letter first", pack, f.Name)
	case f.Apply == nil:
		return nil, configError("the %s pack's fixture function %s has nothing to apply", pack, f.Name)
	case f.Recorded && strings.TrimSpace(f.Version) == "":
		return nil, configError("the %s pack's fixture function %s is recorded and has no version: its recorded results are kept by it", pack, f.Name)
	}
	fn := packFunction{f}
	if f.Before == nil && f.After == nil {
		return fn, nil
	}
	return packLifecycle{fn}, nil
}

type packFunction struct{ f core.FixtureFunction }

func (p packFunction) Name() string   { return p.f.Name }
func (p packFunction) Recorded() bool { return p.f.Recorded }
func (p packFunction) Version() string {
	if p.f.Version == "" {
		return "1"
	}
	return p.f.Version
}

func (p packFunction) Apply(args []string, scope *InstanceScope) (string, error) {
	call := &core.FixtureCall{Args: args}
	if scope != nil {
		call.Instance, call.Attributes = scope.Discriminator, scope.Attributes
	}
	return p.f.Apply(call)
}

// packLifecycle is a pack's function that starts something for a generate
// run.
type packLifecycle struct{ packFunction }

func (p packLifecycle) BeforeAll() error {
	if p.f.Before == nil {
		return nil
	}
	return p.f.Before(context.Background())
}

func (p packLifecycle) AfterAll() error {
	if p.f.After == nil {
		return nil
	}
	return p.f.After(context.Background())
}
