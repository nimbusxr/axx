package fixtures

import (
	"bytes"
	"strings"
	"testing"
)

// The json family's OpenAPI 3.0 backend on mock response bodies:
// schema-ordered output, required/optional semantics, schema defaults,
// nullable, enums, the typo guard, the serve-time oracle and adoption.

const tokens = "mocks/tokens"

func openapiWorkspace(t *testing.T) *workspace {
	t.Helper()
	return newWorkspace(t).seed("openapi-corpus")
}

func TestOpenAPIWildcardDefaultsFillArrayElements(t *testing.T) {
	w := openapiWorkspace(t)
	f := tokens + "/card-tokens.factory.yaml"
	w.replace(f, "identity:", "defaults:\n  auditTrail[].step: \"recorded\"\nidentity:")
	w.replace(f, "fixtures:", "fixtures:\n  token-audited:\n    auditTrail: [ { }, { } ]")
	doc := jsonOf(t, w.expand()[tokens+"/token-audited.json"])
	expect(t, str(at(doc, "auditTrail", 0, "step")), "recorded")
	expect(t, str(at(doc, "auditTrail", 1, "step")), "recorded")
}

func TestOpenAPIBodyResolution(t *testing.T) {
	files := openapiWorkspace(t).expand()
	s := jsonOf(t, files[tokens+"/token-success.json"])
	for path, want := range map[string]string{
		"responseCode": "00000", "cardIdentifier": "1111222233-token-success", "nonFinancialToken": "9991234567789", "encryptionFlag": "3",
	} {
		expect(t, str(at(s, path)), want)
	}
	expect(t, str(at(s, "card", "network")), "VISA")
	if s.Has("expiryDate") {
		t.Error("optional unresolved: omitted")
	}
	if jsonOf(t, files[tokens+"/token-success-no-nf.json"]).Has("nonFinancialToken") {
		t.Error("optional field intentionally absent")
	}
	d := jsonOf(t, files[tokens+"/token-declined.json"])
	expect(t, str(at(d, "responseText")), "DECLINE")
	if !d.Has("expiryDate") || at(d, "expiryDate") != nil {
		t.Error("explicit null on a nullable field")
	}
	expect(t, str(at(d, "card", "network")), "MC")
	expect(t, str(at(d, "card", "type")), "VIC")
}

func TestOpenAPIOrderAndStability(t *testing.T) {
	w := openapiWorkspace(t)
	first, second := w.expand(), w.expand()
	for p, b := range first {
		if !bytes.Equal(b, second[p]) {
			t.Errorf("%s not byte-stable", p)
		}
	}
	body := string(first[tokens+"/token-success.json"])
	if strings.Index(body, "responseCode") > strings.Index(body, "cardIdentifier") || strings.Index(body, "cardIdentifier") > strings.Index(body, "encryptionFlag") {
		t.Errorf("schema order:\n%s", body)
	}
}

func TestOpenAPIErrors(t *testing.T) {
	tests := []struct {
		name, file, old, new string
		parts                []string
	}{
		{"typo", "card-tokens.factory.yaml", `nonFinancialToken: "9991234567789"`, `nonFinancialTokn: "999"`, []string{"nonFinancialTokn", "CardTokenResponse"}},
		{"enum", "card-tokens.factory.yaml", `responseText: "DECLINE"`, `responseText: "MAYBE"`, []string{"MAYBE", "APPROVAL"}},
		{"unresolved required", "card-tokens.prototype.yaml", "  responseCode: \"00000\"\n", "", []string{"responseCode", "required by CardTokenResponse", "token-success", "defaults:"}},
		{"null on non-nullable", "card-tokens.factory.yaml", `nonFinancialToken: "9991234567789"`, "nonFinancialToken: null", []string{"nonFinancialToken", "not nullable"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := openapiWorkspace(t)
			w.replace(tokens+"/"+tc.file, tc.old, tc.new)
			mustErrContain(t, w.expandErr(), tc.parts...)
		})
	}
}

func TestOpenAPILifecycleAndConformance(t *testing.T) {
	w := openapiWorkspace(t)
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace(tokens+"/token-declined.json", "DECLINE", "APPROVAL")
	if f := w.check(); len(f) != 1 || !strings.Contains(f[0], "FIXTURE DRIFT") {
		t.Fatalf("failures: %v", f)
	}

	w = openapiWorkspace(t)
	w.write("mocks/legacy/good.json", `{"responseCode": "00000", "responseText": "APPROVAL", "cardIdentifier": "legacy-1"}`)
	w.write("mocks/legacy/bad.json", `{"responseCode": "00000", "responseText": "NOT_AN_ENUM", "cardIdentifier": "legacy-2"}`)
	w.conformance(ConformanceRule{
		Name: "legacy token bodies", FilePatterns: []string{"mocks/legacy/*.json"}, SchemaType: "json",
		SchemaRef: "openapi/payments.yaml#/components/schemas/CardTokenResponse",
	})
	f := filtered(w.check(), "conformance")
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "bad.json", "CardTokenResponse")
}

func TestOpenAPIBodyAdoptionRegeneratesByteForByte(t *testing.T) {
	w := openapiWorkspace(t)
	w.generate()
	for _, p := range []string{tokens + "/card-tokens.factory.yaml", tokens + "/card-tokens.prototype.yaml", ManifestFile, DefaultLintOutput} {
		w.remove(p)
	}
	before := w.read(tokens + "/token-success.json")
	res, err := w.adopter().Adopt("json", "openapi/payments.yaml#/components/schemas/CardTokenResponse", tokens+"/*.json", "adopted-bodies", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "3/3 deep-equal")
	w.generate()
	expect(t, w.read(tokens+"/token-success.json"), before)
}
