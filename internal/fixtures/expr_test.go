package fixtures

import (
	"errors"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// The expressions contract: whole-scalar ${ fn(args) } with field
// references; pure functions re-evaluated everywhere (drift catches
// implementation changes); recorded functions invoked only by generate, their
// results kept in the committed pairings lock, which check reads alone.

const cardSchema = `{
  "type": "record",
  "name": "CardEvent",
  "namespace": "us.nimbusxr.space",
  "fields": [
    {"name": "event_id", "type": "string"},
    {"name": "card_number", "type": "string"},
    {"name": "last_four", "type": "string"},
    {"name": "crm_token", "type": "string"},
    {"name": "nf_token", "type": "string"}
  ]
}`

func cardsWorkspace(t *testing.T) (*workspace, *tokenize) {
	t.Helper()
	withoutAvroOracle(t)
	w := newWorkspace(t)
	tok := &tokenize{version: "1"}
	w.opts = Options{Functions: []Function{last4, join, tok}}
	w.write("schemas/card.avsc", cardSchema)
	w.write("kafka/cards/card-events.factory.yaml", `factory:
  family: avro
  schema: ../../schemas/card.avsc
identity:
  - path: event_id
    prefix: evt-
`)
	w.write("kafka/cards/card-events.prototype.yaml", `data:
  last_four: ${ last4(card_number) }
  crm_token: ${ tokenize(card_number, "CRM") }
  nf_token: ${ tokenize(card_number, "NF") }
`)
	w.write("kafka/cards/visa-payment.fixture.yaml", "data: { card_number: \"4111111111111144\" }\n")
	w.write("kafka/cards/mc-payment.fixture.yaml", "data: { card_number: \"5500000000001177\" }\n")
	return w, tok
}

func TestExpressionsResolvePerFixtureReferences(t *testing.T) {
	w, _ := cardsWorkspace(t)
	w.generate()
	visa := jsonOf(t, []byte(w.read("kafka/cards/visa-payment.json")))
	if str(at(visa, "last_four")) != "1144" || str(at(visa, "crm_token")) != "4411111111111114-CRM" || str(at(visa, "nf_token")) != "4411111111111114-NF" {
		t.Errorf("visa: %s", w.read("kafka/cards/visa-payment.json"))
	}
	mc := jsonOf(t, []byte(w.read("kafka/cards/mc-payment.json")))
	if str(at(mc, "last_four")) != "1177" {
		t.Errorf("mc: %s", w.read("kafka/cards/mc-payment.json"))
	}
}

func TestRecordedFunctionsWriteThePairingsLockAndAreNeverReinvoked(t *testing.T) {
	w, tok := cardsWorkspace(t)
	w.generate()
	if tok.calls.Load() != 4 { // 2 cards x 2 variants, generate only
		t.Fatalf("calls: %d", tok.calls.Load())
	}
	lock := w.read(PairingsFile)
	mustContain(t, lock, "tokenize", `"4111111111111144"`, "4411111111111114-CRM")
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	if tok.calls.Load() != 4 {
		t.Error("check must not invoke recorded functions")
	}
	if second := w.generate(); len(second.Written) != 0 || tok.calls.Load() != 4 {
		t.Errorf("regenerate: %+v, calls %d", second, tok.calls.Load())
	}
}

func TestPairingsLockBytes(t *testing.T) {
	w, _ := cardsWorkspace(t)
	w.generate()
	want := pairingsHeader + `tokenize:
  version: "1"
  pairings:
  - args:
    - "4111111111111144"
    - CRM
    value: 4411111111111114-CRM
  - args:
    - "4111111111111144"
    - NF
    value: 4411111111111114-NF
  - args:
    - "5500000000001177"
    - CRM
    value: 7711000000000055-CRM
  - args:
    - "5500000000001177"
    - NF
    value: 7711000000000055-NF
`
	if got := w.read(PairingsFile); got != want {
		t.Errorf("lock:\n%s\nwant:\n%s", got, want)
	}
}

func TestVerifyWithoutRecordedPairingsFails(t *testing.T) {
	w, _ := cardsWorkspace(t)
	failures := w.check()
	if len(failures) == 0 {
		t.Fatal("expected failures")
	}
	mustContain(t, failures[0], "no recorded pairing", "axx fixtures generate")
}

func TestVersionBumpInvalidatesRecordedPairings(t *testing.T) {
	w, tok := cardsWorkspace(t)
	w.generate()
	tok.version = "2"
	failures := w.check()
	if len(failures) == 0 {
		t.Fatal("expected failures")
	}
	mustContain(t, failures[0], "recorded with version 1", "version 2")
	tok.calls.Store(0)
	w.generate()
	if tok.calls.Load() != 4 {
		t.Errorf("re-record calls: %d", tok.calls.Load())
	}
	if f := w.check(); len(f) != 0 {
		t.Errorf("check: %v", f)
	}
}

func TestHandEditedPairingsLockIsRefused(t *testing.T) {
	w, _ := cardsWorkspace(t)
	w.generate()
	w.replace(PairingsFile, "-CRM", "-TAMPERED")
	mustErrContain(t, w.generateErr(), "hand-edited", PairingsFile)
}

func TestRemovingExpressionsPrunesThePairingsLock(t *testing.T) {
	w, _ := cardsWorkspace(t)
	w.generate()
	w.replace("kafka/cards/card-events.prototype.yaml", `nf_token: ${ tokenize(card_number, "NF") }`, `nf_token: "none"`)
	w.generate()
	lock := w.read(PairingsFile)
	mustContain(t, lock, "CRM")
	if strings.Contains(lock, "NF\n") {
		t.Errorf("unused pairings must be pruned:\n%s", lock)
	}
}

func TestIdentityPinsMayBeExpressions(t *testing.T) {
	w, _ := cardsWorkspace(t)
	w.write("kafka/cards/visa-payment.fixture.yaml", "data:\n  event_id: ${ last4(card_number) }\n  card_number: \"4111111111111144\"\n")
	w.generate()
	if got := str(at(jsonOf(t, []byte(w.read("kafka/cards/visa-payment.json"))), "event_id")); got != "1144" {
		t.Errorf("event_id: %s", got)
	}
	w.write("kafka/cards/mc-payment.fixture.yaml", "data:\n  event_id: ${ last4(card_number) }\n  card_number: \"5500000000001144\"\n")
	_, err := w.mustGenerator().Expand(ModeGenerate)
	mustErrContain(t, err, "identity collision", "1144")
}

func TestUnknownFunctionAndBadReferences(t *testing.T) {
	w, _ := cardsWorkspace(t)
	proto := "kafka/cards/card-events.prototype.yaml"
	w.replace(proto, "${ last4(card_number) }", "${ nope(card_number) }")
	mustErrContain(t, w.generateErr(), "unknown expression function 'nope'", "last4")
	w.replace(proto, "${ nope(card_number) }", "${ last4(missing_field) }")
	mustErrContain(t, w.generateErr(), "missing_field", "references no field")
	w.replace(proto, "${ last4(missing_field) }", "${ last4(crm_token) }")
	mustErrContain(t, w.generateErr(), "chaining is not supported")
}

func TestLifecycleRunsOncePerGenerateAroundActualInvocations(t *testing.T) {
	w, tok := cardsWorkspace(t)
	w.generate()
	if tok.beforeAll.Load() != 1 || tok.afterAll.Load() != 1 {
		t.Fatalf("lifecycle: %d/%d", tok.beforeAll.Load(), tok.afterAll.Load())
	}
	w.generate() // every pairing recorded: nothing boots
	w.check()    // verify mode never boots
	if tok.beforeAll.Load() != 1 || tok.afterAll.Load() != 1 {
		t.Errorf("lifecycle: %d/%d", tok.beforeAll.Load(), tok.afterAll.Load())
	}
}

func TestAfterAllRunsEvenWhenTheFunctionFails(t *testing.T) {
	w, tok := cardsWorkspace(t)
	tok.failApply = true
	if err := w.generateErr(); err == nil || errors.Is(err, errUnexpectedSuccess) {
		t.Fatal("expected a failure")
	}
	if tok.beforeAll.Load() != 1 || tok.afterAll.Load() != 1 {
		t.Errorf("lifecycle: %d/%d", tok.beforeAll.Load(), tok.afterAll.Load())
	}
}

// Scope-addressed arguments ($data.x, $metadata.x).

func scopedEvaluator() *evaluator {
	fns, _ := functionMap([]Function{join})
	return newEvaluator(fns, ModeGenerate, newPairingsLock())
}

func TestScopedArguments(t *testing.T) {
	payload := newObject("checkoutId", "CHK-1")
	metadata := newObject("key", "CHK-1", "trace", `${ join($metadata.key, "created") }`, "fromPayload", `${ join($data.checkoutId, "seen") }`)
	if err := scopedEvaluator().evaluateTree(metadata, "checkout -> metadata", map[string]*jsonx.Object{ScopeData: payload, ScopeMetadata: metadata}); err != nil {
		t.Fatal(err)
	}
	if str(get(metadata, "trace")) != "CHK-1|created" || str(get(metadata, "fromPayload")) != "CHK-1|seen" {
		t.Errorf("metadata: %s", str(metadata))
	}
	for _, expr := range []string{`${ join(sku, "bare") }`, `${ join($data.sku, "scoped") }`} {
		data := newObject("sku", "SKU-11", "label", expr)
		if err := scopedEvaluator().evaluateTree(data, "checkout -> fixtures.created", nil); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(str(get(data, "label")), "SKU-11|") {
			t.Errorf("%s -> %s", expr, str(get(data, "label")))
		}
	}
}

func TestScopedArgumentErrors(t *testing.T) {
	tests := []struct {
		expr  string
		parts []string
	}{
		{`${ join($metadata.key, "early") }`, []string{"$metadata", "not resolved where this expression lives", "[$data]"}},
		{`${ join($cargo.key, "x") }`, []string{"names no scope", "[$data, $metadata]"}},
		{`${ join($data, "x") }`, []string{"names a scope but no field in it"}},
		{`${ join($data.nope, "x") }`, []string{"references no field in $data"}},
	}
	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			data := newObject("label", tc.expr)
			mustErrContain(t, scopedEvaluator().evaluateTree(data, "checkout -> fixtures.created", nil), tc.parts...)
		})
	}
	payload := newObject("checkoutId", `${ join("a", "b") }`)
	metadata := newObject("trace", `${ join($data.checkoutId, "created") }`)
	err := scopedEvaluator().evaluateTree(metadata, "checkout -> metadata", map[string]*jsonx.Object{ScopeData: payload, ScopeMetadata: metadata})
	mustErrContain(t, err, "chaining is not supported")
}
