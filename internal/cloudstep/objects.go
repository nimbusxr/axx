package cloudstep

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/filecontent"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

// ObjectStore is an object storage service (S3, Cloud Storage, Blob
// Storage), or a folder, as the object steps use it.
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
	// Since is the axx version that introduced the pack, when it came
	// after the object steps (0.1.0): its steps are as old as it is.
	Since string
}

// since is the version that introduced a step of the pack: the step's, or
// the pack's when that is later.
func (o Objects) since(step string) string {
	if o.Since == "" || compareVersions(o.Since, step) < 0 {
		return step
	}
	return o.Since
}

// compareVersions compares two versions such as 0.1.10 and 0.1.9.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// Steps are the pack's object steps: upload a file, and the checks.
func (o Objects) Steps() []core.StepDef {
	c := o.Container
	return append([]core.StepDef{
		{
			ID: o.Pack + ".upload", Keyword: "When", Since: o.since("0.1.0"),
			Expr: "the {filepath} file is uploaded to the {word} " + c + "[[ as {path}]]",
			Doc: fmt.Sprintf("Upload a file to the %s, under the file's name or the name given. Its content type follows the "+
				"file's extension.", c),
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
	}, o.Checks()...)
}

// Checks are the pack's object checks: they wait for an object, its exact
// content, its JSON properties, its text or a row of its table.
func (o Objects) Checks() []core.StepDef {
	c, obj := o.Container, o.Object
	return []core.StepDef{
		{
			ID: o.Pack + ".has", Keyword: "Then", Since: o.since("0.1.0"),
			Expr: "[[within {duration} ]]the {word} " + c + " has a(n) " + obj + " named {path}",
			Doc: fmt.Sprintf("Check that the %s has %s %s with that name. The check waits for it: 10 seconds, or `within {duration}`.",
				c, article(obj), obj),
			Examples: []string{fmt.Sprintf("Then within 30s the %s %s has %s named disputes/kestrel-2026-09.csv", o.Example, c, article(obj)+" "+obj)},
			Run: func(sc *core.Scenario, a core.Args) error {
				container, name := a.String(1), a.String(2)
				return o.await(sc, Wait(a, 0), container, name, func([]byte) (bool, string, error) { return true, "", nil })
			},
		},
		{
			ID: o.Pack + ".identical", Keyword: "Then", Since: o.since("0.1.0"),
			Expr: "[[within {duration} ]]the {path} " + obj + " in the {word} " + c + " is identical to the {filepath} file",
			Doc: fmt.Sprintf("Check that the %s exists with exactly the content of the file. The check waits for it: 10 seconds, "+
				"or `within {duration}`.", obj),
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
			ID: o.Pack + ".properties", Keyword: "Then", Arg: core.ArgTable, Since: o.since("0.1.0"),
			Expr: "[[within {duration} ]]the {path} " + obj + " in the {word} " + c + " has the following properties:",
			Doc: fmt.Sprintf("Check that the %s holds JSON with those values at those paths. The check waits for it: 10 seconds, "+
				"or `within {duration}`.", obj),
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note: "Each row is a path into the JSON (a property name, a dotted path or a JSONPath) and the value it has, compared " +
					"as text: `null` for null and `undefined` for absent, as in the other JSON property steps.",
			},
			Examples: []string{fmt.Sprintf("Then the summaries/kestrel-2026-09.json %s in the %s %s has the following properties:", obj, o.Example, c) +
				exampleTable([][2]string{{"carrier", "KESTREL"}, {"lines", "14"}, {"totals.disputed", "5.25"}})},
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
		{
			ID: o.Pack + ".contains", Keyword: "Then", Since: o.since("0.1.1"),
			Expr: "[[within {duration} ]]the {path} " + obj + " in the {word} " + c + " contains {string}",
			Doc: fmt.Sprintf("Check that the text of the %s contains the text.\n\n"+
				"- The check waits for it: 10 seconds, or `within {duration}`.\n"+
				"- Case matters; runs of spaces and line breaks count as one space.\n"+
				"- The text is read by the %s's type: the text of a PDF's pages, the paragraphs and tables of a Word document "+
				"(.docx), the cells of every sheet of an Excel workbook (.xlsx), the text content (or the markup) of XML and HTML, "+
				"or the text itself.", obj, obj),
			Examples: []string{fmt.Sprintf(`Then the invoices/kestrel-2026-09.pdf %s in the %s %s contains "Total due: 1284.50 EUR"`, obj, o.Example, c)},
			Run: func(sc *core.Scenario, a core.Args) error {
				name, want := a.String(1), a.String(3)
				return o.await(sc, Wait(a, 0), a.String(2), name, unchanged(func(got []byte) (bool, string, error) {
					f := filecontent.File{Name: name, Body: got}
					if k := f.Kind(); k == filecontent.Binary {
						return false, "", fmt.Errorf("the %s %s is %s (%d bytes), not a file whose text axx reads (%s)",
							name, obj, k.Noun(), len(got), filecontent.Readable)
					}
					ok, text, err := f.Contains(want)
					switch {
					case err != nil:
						return false, err.Error(), nil //nolint:nilerr // a file still being written, say: wait
					case ok:
						return true, "", nil
					}
					return false, fmt.Sprintf("its text does not contain %q. %s", want, filecontent.Nearest(text, want)), nil
				}))
			},
		},
		{
			ID: o.Pack + ".row", Keyword: "Then", Arg: core.ArgTable, Since: o.since("0.1.1"),
			Expr: "[[within {duration} ]]the {path} " + obj + " in the {word} " + c + " has a row where:",
			Doc: fmt.Sprintf("Check that the table of the %s has a row with those values in those columns.\n\n"+
				"- The check waits for it: 10 seconds, or `within {duration}`.\n"+
				"- The %s is a CSV or TSV file, or an Excel workbook (.xlsx; its first sheet), whose first row names the columns.\n"+
				"- Cells compare as text, as the workbook shows them, with runs of spaces as one space; an empty value matches an "+
				"empty cell.", obj, obj),
			Table: &core.TableDoc{
				Columns: []string{"column", "value"},
				Note:    fmt.Sprintf("Each row names a column, as the %s's first row names it, and the value in that column.", obj),
			},
			Examples: []string{fmt.Sprintf("Then within 30s the disputes/kestrel-2026-09.csv %s in the %s %s has a row where:", obj, o.Example, c) +
				exampleTable([][2]string{{"parcel", "PX-5199"}, {"status", "UNKNOWN_SHIPMENT"}, {"billed", "4.10"}})},
			Run: func(sc *core.Scenario, a core.Args) error {
				if a.Table == nil {
					return fmt.Errorf("the step needs a table of the row's columns and values (| column | value |)")
				}
				want, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				name := a.String(1)
				return o.await(sc, Wait(a, 0), a.String(2), name, unchanged(func(got []byte) (bool, string, error) {
					f := filecontent.File{Name: name, Body: got}
					if k := f.Kind(); !k.HasTable() {
						return false, "", fmt.Errorf("the %s %s is %s, not a table axx reads (CSV, TSV, Excel .xlsx)", name, obj, k.Noun())
					}
					t, err := f.Table("")
					if err != nil {
						return false, err.Error(), nil //nolint:nilerr // a file still being written, say: wait
					}
					ok, why := t.FindRow(want)
					return ok, why, nil
				}))
			},
		},
	}
}

// unchanged gives content that did not change since the last poll the
// same verdict, without reading it again.
func unchanged(check func([]byte) (bool, string, error)) func([]byte) (bool, string, error) {
	var last []byte
	var done, seen bool
	var why string
	return func(b []byte) (bool, string, error) {
		if seen && bytes.Equal(b, last) {
			return done, why, nil
		}
		d, w, err := check(b)
		if err == nil {
			last, done, why, seen = b, d, w, true
		}
		return d, w, err
	}
}

// Await waits until the object exists and check accepts its content: for a
// pack's own check of its objects. A name with a * names the one object it
// matches.
func (o Objects) Await(sc *core.Scenario, d time.Duration, container, name string, check func([]byte) (bool, string, error)) error {
	return o.await(sc, d, container, name, check)
}

// await waits until the object exists and check accepts its content.
func (o Objects) await(sc *core.Scenario, d time.Duration, container, name string, check func([]byte) (bool, string, error)) error {
	st, err := o.Store(sc)
	if err != nil {
		return err
	}
	return Poll(sc, d, func() (bool, string, error) {
		name := name
		if strings.Contains(name, "*") {
			found, why, err := o.match(sc, st, container, name, d)
			if err != nil || found == "" {
				return false, why, err
			}
			name = found
		}
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

// maxMatched is how many names a pattern is matched against.
const maxMatched = 10000

// match is the one object a name with a * names: the * stands for any
// characters but a slash (snap-*.json). None or several is why the check
// waits.
func (o Objects) match(sc *core.Scenario, st ObjectStore, container, pattern string, d time.Duration) (string, string, error) {
	names, err := st.List(sc.Context(), container, maxMatched)
	if err != nil {
		return "", "", fmt.Errorf("cannot list the %s %s: %w", container, o.Container, err)
	}
	var found []string
	for _, n := range names {
		if matches(pattern, n) {
			found = append(found, n)
		}
	}
	switch len(found) {
	case 1:
		return found[0], "", nil
	case 0:
		return "", fmt.Sprintf("The %s %s has no %s named %s after %s. It has %s", container, o.Container, o.Object, pattern, d, listing(names, o.Object)), nil
	}
	return "", fmt.Sprintf("%d %ss in the %s %s are named %s after %s, and the name stands for one: %s", len(found), o.Object, container, o.Container, pattern, d, listing(found, o.Object)), nil
}

// matches is whether a name matches a pattern whose * stand for any
// characters but a slash; its other characters are themselves (a ? or a [
// in a file's name is no pattern).
func matches(pattern, name string) bool {
	escaped := strings.NewReplacer(`\`, `\\`, "?", `\?`, "[", `\[`).Replace(pattern)
	ok, err := path.Match(escaped, name)
	return err == nil && ok
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
