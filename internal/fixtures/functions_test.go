package fixtures

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// axx's own expression functions, and a pack's (core.FixtureFunction).

const tokenSchema = `{"type": "object", "properties": {"id": {"type": "string"}, "token": {"type": "string"}, "value": {"type": "string"}}}`

// tokensWorkspace is a factory of one fixture whose value is expr, beside a
// token field.
func tokensWorkspace(t *testing.T, expr string) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.write("tokens/token.schema.json", tokenSchema)
	w.write("tokens/tokens.factory.yaml", "factory:\n  family: json\n  schema: token.schema.json\nidentity:\n  - path: id\n")
	w.write("tokens/one.fixture.yaml", "data:\n  token: parcel-key\n  value: "+expr+"\n")
	return w
}

func (w *workspace) value() string {
	w.t.Helper()
	return str(at(jsonOf(w.t, w.expand()["tokens/one.json"]), "value"))
}

// sha256Hex is what shasum -a 256 gives for the text parcel-key.
const sha256Hex = "b159b218218d8f225ff054163fe85ee4c85be82f8c9b53144b54056a357e1cef"

// The digests and encodings are those shasum, md5, base64 and xxd give for
// the same text.
func TestAxxsOwnFunctionsHashAndEncode(t *testing.T) {
	for expr, want := range map[string]string{
		`${ sha256("parcel-key") }`:         sha256Hex,
		`${ sha256(token) }`:                sha256Hex, // a field of the fixture
		`${ sha256("parcel", "-", "key") }`: sha256Hex, // its arguments, one after another
		`${ sha512(token) }`:                "0abb62d9d10ccde5c242ed47022891b66c52e63f432843a38846971dec5500ed2b7021e9c316b81952e26941b272d5fc700fd0e1053ce9941850568d6456f11f",
		`${ sha1(token) }`:                  "5f805ede32b5fd290aaac52c9bc9a1f2f5d64236",
		`${ md5(token) }`:                   "53487ca58a1a536e7054df6db7a6cff0",
		`${ base64("depot:", token) }`:      "ZGVwb3Q6cGFyY2VsLWtleQ==",
		`${ base64url("depot?", token) }`:   "ZGVwb3Q_cGFyY2VsLWtleQ",
		`${ hex(token) }`:                   "70617263656c2d6b6579",
	} {
		t.Run(expr, func(t *testing.T) {
			if got := tokensWorkspace(t, expr).value(); got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

// They are pure: check computes them again, so a fixture whose file was
// written with another value is drift.
func TestAxxsOwnFunctionsAreComputedAgainByCheck(t *testing.T) {
	w := tokensWorkspace(t, `${ sha256(token) }`)
	w.generate()
	if w.exists(PairingsFile) {
		t.Errorf("a pure function's results were recorded:\n%s", w.read(PairingsFile))
	}
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace("tokens/one.fixture.yaml", "token: parcel-key", "token: depot-key")
	if f := w.check(); len(f) != 1 || !strings.Contains(f[0], "tokens/one.json") {
		t.Fatalf("check after the token changed: %v", f)
	}
}

func TestAxxsOwnFunctionsErrors(t *testing.T) {
	mustErrContain(t, tokensWorkspace(t, `${ sha256() }`).expandErr(), "sha256 needs a value", "fixtures.one")
	mustErrContain(t, tokensWorkspace(t, `${ sha3(token) }`).expandErr(), "unknown expression function 'sha3'", "sha256", "a pack of the project adds its own")
}

// A dataset's column is computed alike: the hash of a token no fixture
// writes out.
func TestADatasetColumnIsAHash(t *testing.T) {
	w := sessionsWorkspace(t)
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - token_hash: ${ sha256(\"parcel-key\") }\n")
	mustContain(t, string(w.expand()["seeds/alpha.yaml"]), "token_hash: "+sha256Hex)
}

// packFunctions are a pack's functions as the factory calls them.
func packFunctions(t *testing.T, fns ...core.FixtureFunction) []Function {
	t.Helper()
	var out []Function
	for _, f := range fns {
		fn, err := PackFunction("parcels", f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, fn)
	}
	return out
}

// A pack's pure function is called with its arguments and the fixture it
// computes for, on every expansion.
func TestAPacksFunctionComputesAValue(t *testing.T) {
	w := tokensWorkspace(t, `${ checkDigit(token, "mod10") }`)
	var calls []core.FixtureCall
	w.opts.Functions = packFunctions(t, core.FixtureFunction{Name: "checkDigit", Apply: func(call *core.FixtureCall) (string, error) {
		calls = append(calls, *call)
		return call.Args[0] + "-7", nil
	}})
	if got := w.value(); got != "parcel-key-7" {
		t.Errorf("value: %s", got)
	}
	if len(calls) != 1 || strings.Join(calls[0].Args, ",") != "parcel-key,mod10" || !strings.Contains(calls[0].Instance, "fixtures.one") {
		t.Errorf("calls: %+v", calls)
	}
}

// A pack's recorded function is called by generate, between its Before and
// After, and its results are read back from the pairings lock: check and a
// second generate never call it.
func TestAPacksRecordedFunctionIsCalledOnceAndReadBack(t *testing.T) {
	w := tokensWorkspace(t, `${ issue(token) }`)
	var events []string
	w.opts.Functions = packFunctions(t, core.FixtureFunction{
		Name: "issue", Recorded: true, Version: "2",
		Before: func(context.Context) error { events = append(events, "before"); return nil },
		After:  func(context.Context) error { events = append(events, "after"); return nil },
		Apply: func(call *core.FixtureCall) (string, error) {
			events = append(events, "apply "+call.Args[0])
			return "tok-" + call.Args[0], nil
		},
	})
	w.generate()
	mustContain(t, w.read("tokens/one.json"), `"value": "tok-parcel-key"`)
	mustContain(t, w.read(PairingsFile), "issue:", `version: "2"`, "tok-parcel-key")
	if got := strings.Join(events, ", "); got != "before, apply parcel-key, after" {
		t.Errorf("generate: %s", got)
	}
	events = nil
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	if len(events) != 0 {
		t.Errorf("called again: %v", events)
	}
}

func TestAPacksFunctionThatFails(t *testing.T) {
	w := tokensWorkspace(t, `${ issue(token) }`)
	w.opts.Functions = packFunctions(t, core.FixtureFunction{Name: "issue", Apply: func(*core.FixtureCall) (string, error) {
		return "", errors.New("the tokens service refused parcel-key")
	}})
	mustErrContain(t, w.expandErr(), "expression function 'issue' failed", "the tokens service refused parcel-key", "fixtures.one")
}

// A pack's function axx cannot use says which pack's it is, and why; one
// named as one of axx's own is refused.
func TestAPacksFunctionAxxCannotUse(t *testing.T) {
	apply := func(*core.FixtureCall) (string, error) { return "", nil }
	for name, tc := range map[string]struct {
		f     core.FixtureFunction
		parts []string
	}{
		"no name":               {core.FixtureFunction{Apply: apply}, []string{"parcels pack", "no name an expression can call"}},
		"a name with a space":   {core.FixtureFunction{Name: "check digit", Apply: apply}, []string{"check digit", "no name an expression can call"}},
		"nothing to apply":      {core.FixtureFunction{Name: "issue"}, []string{"issue", "nothing to apply"}},
		"recorded, unversioned": {core.FixtureFunction{Name: "issue", Recorded: true, Apply: apply}, []string{"issue", "recorded and has no version"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := PackFunction("parcels", tc.f)
			mustErrContain(t, err, tc.parts...)
		})
	}
	w := tokensWorkspace(t, `${ sha256(token) }`)
	w.opts.Functions = packFunctions(t, core.FixtureFunction{Name: "sha256", Apply: apply})
	_, err := w.generator()
	mustErrContain(t, err, "named 'sha256', as one of axx's own is", "base64")
}
