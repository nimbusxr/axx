package cloudstep

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

// ObjectStore is an object storage service (S3, Cloud Storage, Blob
// Storage) as the object steps use it.
type ObjectStore interface {
	// Put stores an object.
	Put(ctx context.Context, container, name string, body []byte, contentType string) error
	// Get returns an object's content; ok is false when there is none.
	Get(ctx context.Context, container, name string) (body []byte, ok bool, err error)
	// List returns up to max object names of a container, for failure
	// reports.
	List(ctx context.Context, container string, max int) ([]string, error)
}

// Objects describes the object steps of one storage pack.
type Objects struct {
	// Pack is the pack name, the prefix of the step IDs.
	Pack string
	// Container is how steps name a container, e.g. "s3 bucket".
	Container string
	// Object is how steps name what it holds: "object", "blob".
	Object string
	// Example is a container name for the step examples.
	Example string
	// Store returns the scenario's store.
	Store func(sc *core.Scenario) (ObjectStore, error)
}

// Steps are the pack's object steps: upload a file, and wait for an
// object, its exact content or its JSON properties.
func (o Objects) Steps() []core.StepDef {
	c, obj := o.Container, o.Object
	return []core.StepDef{
		{
			ID: o.Pack + ".upload", Keyword: "When", Since: "0.1.0",
			Expr: "the {filepath} file is uploaded to the {word} " + c + "[[ as {word}]]",
			Doc: fmt.Sprintf("Upload a file (resolved against `resources`) to a %s, named after the file or as given. "+
				"The content type follows the file's extension.", c),
			Examples: []string{
				fmt.Sprintf("When the invoices/kestrel-2026-09.csv file is uploaded to the %s %s", o.Example, c),
				fmt.Sprintf("When the invoices/kestrel-2026-09.csv file is uploaded to the %s %s as incoming/kestrel-2026-09.csv", o.Example, c),
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				st, err := o.Store(sc)
				if err != nil {
					return err
				}
				file := a.String(0)
				p, err := sc.Suite().ResolvePath(file)
				if err != nil {
					return err
				}
				body, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				name := path.Base(filepath.ToSlash(file))
				if a.Present(2) {
					name = a.String(2)
				}
				ct := mime.TypeByExtension(filepath.Ext(p))
				if ct == "" {
					ct = "application/octet-stream"
				}
				if err := st.Put(sc.Context(), a.String(1), name, body, ct); err != nil {
					return fmt.Errorf("cannot upload %s to the %s %s: %w", file, a.String(1), c, err)
				}
				sc.Log("uploaded %s to the %s %s as %s (%d bytes)", file, a.String(1), c, name, len(body))
				return nil
			},
		},
		{
			ID: o.Pack + ".has", Keyword: "Then", Since: "0.1.0",
			Expr:     "[[within {duration} ]]the {word} " + c + " has a(n) " + obj + " named {word}",
			Doc:      fmt.Sprintf("Wait (10s, or the given time) until the %s has an %s with that name.", c, obj),
			Examples: []string{fmt.Sprintf("Then within 30s the %s %s has a(n) %s named disputes/kestrel-2026-09.csv", o.Example, c, obj)},
			Run: func(sc *core.Scenario, a core.Args) error {
				container, name := a.String(1), a.String(2)
				return o.await(sc, Wait(a, 0), container, name, func([]byte) (bool, string, error) { return true, "", nil })
			},
		},
		{
			ID: o.Pack + ".identical", Keyword: "Then", Since: "0.1.0",
			Expr: "[[within {duration} ]]the {word} " + obj + " in the {word} " + c + " is identical to the {filepath} file",
			Doc: fmt.Sprintf("Wait (10s, or the given time) until the %s exists with exactly the content of the file "+
				"(resolved against `resources`).", obj),
			Examples: []string{fmt.Sprintf("Then the disputes/kestrel-2026-09.csv %s in the %s %s is identical to the expected/kestrel-disputes.csv file", obj, o.Example, c)},
			Run: func(sc *core.Scenario, a core.Args) error {
				p, err := sc.Suite().ResolvePath(a.String(3))
				if err != nil {
					return err
				}
				want, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				return o.await(sc, Wait(a, 0), a.String(2), a.String(1), func(got []byte) (bool, string, error) {
					if bytes.Equal(got, want) {
						return true, "", nil
					}
					return false, fmt.Sprintf("its content differs from %s.\nExpected:\n%s\nActual:\n%s", a.String(3), excerpt(want), excerpt(got)), nil
				})
			},
		},
		{
			ID: o.Pack + ".properties", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "[[within {duration} ]]the {word} " + obj + " in the {word} " + c + " has the following properties:",
			Doc: fmt.Sprintf("Wait (10s, or the given time) until the %s exists and its JSON content has the properties: "+
				"`path | value` rows compared as text, `null` for null and `undefined` for absent, as in the other JSON property steps.", obj),
			Examples: []string{fmt.Sprintf("Then the summaries/kestrel-2026-09.json %s in the %s %s has the following properties:", obj, o.Example, c)},
			Run: func(sc *core.Scenario, a core.Args) error {
				return o.await(sc, Wait(a, 0), a.String(2), a.String(1), func(got []byte) (bool, string, error) {
					err := jsonassert.Properties(string(got), a.Table, false)
					switch {
					case err == nil:
						return true, "", nil
					case core.IsAssertion(err):
						return false, err.Error(), nil
					}
					return false, "", err
				})
			},
		},
	}
}

// await waits until the object exists and check accepts its content.
func (o Objects) await(sc *core.Scenario, d time.Duration, container, name string, check func([]byte) (bool, string, error)) error {
	st, err := o.Store(sc)
	if err != nil {
		return err
	}
	return Poll(sc, d, func() (bool, string, error) {
		body, ok, err := st.Get(sc.Context(), container, name)
		if err != nil {
			return false, "", fmt.Errorf("cannot read %s from the %s %s: %w", name, container, o.Container, err)
		}
		if !ok {
			names, err := st.List(sc.Context(), container, 20)
			if err != nil {
				return false, "", fmt.Errorf("cannot list the %s %s: %w", container, o.Container, err)
			}
			return false, fmt.Sprintf("The %s %s has no %s named %s after %s. It has %s", container, o.Container, o.Object, name, d, listing(names, o.Object)), nil
		}
		done, why, err := check(body)
		if done || err != nil {
			return done, "", err
		}
		return false, fmt.Sprintf("The %s %s in the %s %s did not meet the expectation within %s: %s", name, o.Object, container, o.Container, d, why), nil
	})
}

func listing(names []string, object string) string {
	if len(names) == 0 {
		return "no " + object + "s"
	}
	return strings.Join(names, ", ")
}

// excerpt is up to 2 KB of content for a failure report.
func excerpt(b []byte) string {
	const max = 2048
	if len(b) > max {
		return string(b[:max]) + fmt.Sprintf("\n... (%d bytes in all)", len(b))
	}
	return string(b)
}
