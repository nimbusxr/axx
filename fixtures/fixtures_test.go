package fixtures_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/fixtures"
	"github.com/nimbusxr/axx/internal/axxerr"
)

// stamp is a pure function that shows what it received: its arguments, the
// instance's discriminator and its run.id attribute.
type stamp struct{}

func (stamp) Name() string    { return "stamp" }
func (stamp) Version() string { return "1" }
func (stamp) Recorded() bool  { return false }
func (stamp) Apply(args []string, scope *fixtures.InstanceScope) (string, error) {
	return strings.Join(args, "+") + "@" + scope.Discriminator + "/" + scope.Attributes["run.id"], nil
}

// tokenize is a recorded function: its results come from the pairings lock,
// never from a call.
type tokenize struct{ t *testing.T }

func (tokenize) Name() string    { return "tokenize" }
func (tokenize) Version() string { return "1" }
func (tokenize) Recorded() bool  { return true }
func (f tokenize) Apply([]string, *fixtures.InstanceScope) (string, error) {
	f.t.Error("a recorded function was called")
	return "", errors.New("called")
}

// library writes a module of one factory (a prototype, two fixtures, an
// identity authored by one of them and derived for the other, a uuid
// identity) and its pairings lock, and loads it.
func library(t *testing.T, extra map[string]string) *fixtures.Library {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"orders/orders.factory.yaml": `factory:
  family: json
  schema: order.schema.json
identity:
  - path: orderId
    prefix: ORD-
  - path: trackingId
    format: uuid-name-based
metadata:
  topic: orders
`,
		"orders/orders.prototype.yaml": `data:
  status: CREATED
  currency: EUR
  customer:
    tier: SILVER
  label: ${ stamp(status, currency) }
`,
		"orders/created.fixture.yaml": `data:
  amount: 12.5
  quantity: 3
  customer:
    name: Ada
metadata:
  key: ${ stamp($data.customer.name, "key") }
`,
		"orders/shipped.fixture.yaml": `data:
  orderId: ORD-FIXED-1
  status: SHIPPED
  buyer:
    $ref: ./created.fixture.yaml#/customer/name
  echo:
    $ref: "#/status"
  token: ${ tokenize(status) }
`,
		"axx-fixtures.pairings.yaml": `tokenize:
  version: "1"
  pairings:
  - args:
    - SHIPPED
    value: tok-shipped
`,
	}
	for name, body := range extra {
		files[name] = body
	}
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib, err := fixtures.Load(fixtures.DefaultConfig(dir), stamp{}, tokenize{t})
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func fixture(t *testing.T, lib *fixtures.Library, ref string) *fixtures.Fixture {
	t.Helper()
	f, err := lib.Fixture(ref)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func scope(discriminator string) fixtures.InstanceScope {
	return fixtures.InstanceScope{Discriminator: discriminator, Attributes: map[string]string{"run.id": "run-7"}}
}

func instance(t *testing.T, lib *fixtures.Library, f *fixtures.Fixture, in fixtures.Input) *fixtures.Instance {
	t.Helper()
	x, err := lib.Instance(f, in)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestAFixtureIsFoundByItsFactoryAndKey(t *testing.T) {
	lib := library(t, nil)
	f := fixture(t, lib, "orders#created")
	if got := fixture(t, lib, "orders/orders#created"); got.Ref() != f.Ref() {
		t.Errorf("by path: %s, by base name: %s", got.Ref(), f.Ref())
	}
	if f.Ref() != "orders/orders#created" || f.Factory() != "orders/orders" || f.Key() != "created" || f.Dir() != "orders" ||
		f.Document() != "orders/created.fixture.yaml" || f.Family() != "json" || f.SchemaRef() != "orders/order.schema.json" {
		t.Errorf("fixture: %s %s %s %s %s %s %s", f.Ref(), f.Factory(), f.Key(), f.Dir(), f.Document(), f.Family(), f.SchemaRef())
	}
	ids := f.Identities()
	if len(ids) != 2 || ids[0].Path != "orderId" || ids[0].Prefix != "ORD-" || ids[0].Derive != "fixture-key" ||
		ids[1].Path != "trackingId" || ids[1].Format != "uuid-name-based" {
		t.Errorf("identities: %+v", ids)
	}
	if n := len(lib.Fixtures()); n != 2 {
		t.Errorf("%d fixtures", n)
	}
	for ref, want := range map[string]string{
		"orders":             "is not <factory>#<fixture>",
		"parcels#created":    "names no factory (factories: [orders/orders])",
		"orders#create":      "did you mean created",
		"other/orders#ships": "names no factory",
	} {
		_, err := lib.Fixture(ref)
		var ae *axxerr.Error
		if !errors.As(err, &ae) || ae.Code != "AXX-E0911" || !strings.Contains(ae.Message, want) {
			t.Errorf("%s: %v", ref, err)
		}
	}
}

func TestAFactoryNameTwoFactoriesShareIsAmbiguous(t *testing.T) {
	lib := library(t, map[string]string{
		"returns/orders.factory.yaml": "factory:\n  family: json\nfixtures:\n  refund: {}\n",
	})
	_, err := lib.Fixture("orders#refund")
	if err == nil || !strings.Contains(err.Error(), "names 2 factories: [orders/orders, returns/orders]") {
		t.Fatal(err)
	}
	if f := fixture(t, lib, "returns/orders#refund"); f.Document() != "inline in returns/orders.factory.yaml" {
		t.Errorf("document: %s", f.Document())
	}
}

func TestTheSameScopeGivesTheSameBytes(t *testing.T) {
	lib := library(t, nil)
	f := fixture(t, lib, "orders#created")
	in := fixtures.Input{Scope: scope("42|run-7|orders|0")}
	a, b := instance(t, lib, f, in), instance(t, lib, f, in)
	if !bytes.Equal(a.Data.JSON(), b.Data.JSON()) || !bytes.Equal(a.Metadata.JSON(), b.Metadata.JSON()) ||
		!bytes.Equal(a.Data.CompactJSON(), b.Data.CompactJSON()) {
		t.Errorf("two instances of one scope differ:\n%s\n%s", a.Data.JSON(), b.Data.JSON())
	}
	// The prototype under the fixture, keys in the order they were written,
	// the derived identities after them.
	want := `{
  "status": "CREATED",
  "currency": "EUR",
  "customer": {
    "tier": "SILVER",
    "name": "Ada"
  },
  "label": "CREATED+EUR@42|run-7|orders|0/run-7",
  "amount": 12.5,
  "quantity": 3,
  "orderId": "ORD-42|run-7|orders|0|orderId",
  "trackingId": "` + a.Identities["trackingId"] + `"
}
`
	if string(a.Data.JSON()) != want {
		t.Errorf("data:\n%s\nwant:\n%s", a.Data.JSON(), want)
	}
	if string(a.Metadata.CompactJSON()) != `{"topic":"orders","key":"Ada+key@42|run-7|orders|0/run-7"}` {
		t.Errorf("metadata: %s", a.Metadata.CompactJSON())
	}
	v := a.Data.Value()
	if v["quantity"] != 3 || v["amount"] != 12.5 || a.Data.Get("customer.name") != "Ada" || a.Data.Get("customer.nope") != nil {
		t.Errorf("plain values: %#v", v)
	}
}

func TestEachInstanceDerivesItsOwnIdentities(t *testing.T) {
	lib := library(t, nil)
	created, shipped := fixture(t, lib, "orders#created"), fixture(t, lib, "orders#shipped")
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first := instance(t, lib, created, fixtures.Input{Scope: scope("42|run-7|orders|0")})
	second := instance(t, lib, created, fixtures.Input{Scope: scope("42|run-7|orders|1")})
	for _, path := range []string{"orderId", "trackingId"} {
		if first.Identities[path] == second.Identities[path] {
			t.Errorf("%s: both instances have %s", path, first.Identities[path])
		}
		if first.Data.Get(path) != first.Identities[path] {
			t.Errorf("%s: the data has %v, the identities %s", path, first.Data.Get(path), first.Identities[path])
		}
	}
	if !strings.HasPrefix(first.Identities["orderId"], "ORD-") || !uuid.MatchString(first.Identities["trackingId"]) {
		t.Errorf("identities: %v", first.Identities)
	}
	docs := func(string) (*fixtures.Tree, error) { return first.Data, nil }
	for _, d := range []string{"42|run-7|orders|0", "42|run-7|orders|1"} {
		x := instance(t, lib, shipped, fixtures.Input{Scope: scope(d), Documents: docs})
		if x.Identities["orderId"] != "ORD-FIXED-1" {
			t.Errorf("an authored identity changed: %s", x.Identities["orderId"])
		}
	}
}

func TestOverridesApplyBeforeAnythingResolves(t *testing.T) {
	lib := library(t, nil)
	x := instance(t, lib, fixture(t, lib, "orders#created"), fixtures.Input{
		Scope: scope("s"),
		Overrides: map[string]any{
			"customer.tier": "GOLD",
			"currency":      "USD",
			"quantity":      int64(9),
			"notes.first":   []string{"fragile"},
			"orderId":       "ORD-OVERRIDDEN",
		},
		MetadataOverrides: map[string]any{"partition": 2},
	})
	if x.Data.Get("customer.tier") != "GOLD" || x.Data.Get("quantity") != 9 || x.Data.Get("label") != "CREATED+USD@s/run-7" ||
		x.Identities["orderId"] != "ORD-OVERRIDDEN" || x.Metadata.Get("partition") != 2 {
		t.Errorf("data %s metadata %s identities %v", x.Data.CompactJSON(), x.Metadata.CompactJSON(), x.Identities)
	}
	if notes, ok := x.Data.Get("notes.first").([]any); !ok || len(notes) != 1 || notes[0] != "fragile" {
		t.Errorf("notes: %#v", x.Data.Get("notes"))
	}
	_, err := lib.Instance(fixture(t, lib, "orders#created"), fixtures.Input{Overrides: map[string]any{"customer.name.first": "A"}})
	if err == nil || !strings.Contains(err.Error(), "orders/orders.factory.yaml -> fixtures.created: override path 'customer.name.first' traverses a non-map value") {
		t.Error(err)
	}
	_, err = lib.Instance(fixture(t, lib, "orders#created"), fixtures.Input{Overrides: map[string]any{"when": make(chan int)}})
	if err == nil || !strings.Contains(err.Error(), "override path 'when': a value of type chan int has no place in a fixture") {
		t.Error(err)
	}
}

func TestRefsTakeValuesFromTheDocumentsOfTheInstance(t *testing.T) {
	lib := library(t, nil)
	created, shipped := fixture(t, lib, "orders#created"), fixture(t, lib, "orders#shipped")
	earlier := instance(t, lib, created, fixtures.Input{Scope: scope("s"), Overrides: map[string]any{"customer.name": "Grace"}})
	var asked []string
	x := instance(t, lib, shipped, fixtures.Input{Scope: scope("s"), Documents: func(document string) (*fixtures.Tree, error) {
		asked = append(asked, document)
		return earlier.Data, nil
	}})
	if x.Data.Get("buyer") != "Grace" || x.Data.Get("echo") != "SHIPPED" || len(asked) != 1 || asked[0] != created.Document() {
		t.Errorf("data %s, documents asked %v", x.Data.CompactJSON(), asked)
	}
	if earlier.Data.Get("customer.name") != "Grace" {
		t.Error("the instance changed a document it was given")
	}
	for name, docs := range map[string]func(string) (*fixtures.Tree, error){
		"none given":   nil,
		"not produced": func(string) (*fixtures.Tree, error) { return nil, nil },
	} {
		_, err := lib.Instance(shipped, fixtures.Input{Scope: scope("s"), Documents: docs})
		if err == nil || !strings.Contains(err.Error(), "orders/orders.factory.yaml -> fixtures.shipped: orders/created.fixture.yaml is not produced for this instance") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRecordedFunctionsAnswerFromThePairingsLockOnly(t *testing.T) {
	lib := library(t, nil)
	shipped := fixture(t, lib, "orders#shipped")
	earlier := instance(t, lib, fixture(t, lib, "orders#created"), fixtures.Input{Scope: scope("s")})
	docs := func(string) (*fixtures.Tree, error) { return earlier.Data, nil }
	x := instance(t, lib, shipped, fixtures.Input{Scope: scope("s"), Documents: docs})
	if x.Data.Get("token") != "tok-shipped" {
		t.Errorf("token: %v", x.Data.Get("token"))
	}
	_, err := lib.Instance(shipped, fixtures.Input{Scope: scope("s"), Documents: docs, Overrides: map[string]any{"status": "LOST"}})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != "AXX-E0902" ||
		!strings.Contains(ae.Message, "orders/orders.factory.yaml -> fixtures.shipped: no recorded pairing for tokenize(LOST)") {
		t.Errorf("%v", err)
	}
}

func TestAHandEditedPairingsLockIsRefused(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("orders/orders.factory.yaml", "factory:\n  family: json\nfixtures:\n  created: {}\n")
	write("axx-fixtures.pairings.yaml", "tokenize:\n  version: \"1\"\n  pairings: []\n")
	write("axx-fixtures.manifest.yaml", "files:\n- path: axx-fixtures.pairings.yaml\n  sha256: 0000\n  factory: <axx-fixtures>\n  fixture: <recorded pairings>\n")
	_, err := fixtures.Load(fixtures.DefaultConfig(dir))
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != "AXX-E0903" {
		t.Errorf("%v", err)
	}
}
