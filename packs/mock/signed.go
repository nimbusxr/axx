package mock

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/signing"
)

// signedSince is the axx version that introduced the signature checks.
const signedSince = "0.1.5"

// signedStep checks that the requests a service sent are signed as its
// webhooks promise, in a header.
func signedStep() core.StepDef {
	return core.StepDef{
		ID: "mock.signed", Keyword: "Then", Arg: core.ArgTable, Since: signedSince,
		Expr: "the mocked request named {word}[[ on {mockedService}]] is signed in the {word} header with the following properties:",
		Doc: "Check that the named request was received, and that every request it names is signed in the header: an HMAC of its body, " +
			"or of what the `signs` template gives, made with the key.\n\n" +
			"- axx signs what the mock received, byte for byte, its method and its path with the query, and compares the signatures.\n" +
			"- `{timestamp}` is read from the header's value, or from the timestamp header.\n" +
			"- The key expands `${env:..}`, and is masked.",
		Table: &core.TableDoc{Columns: []string{"property", "value"}, Rows: signing.Rows},
		Examples: []string{"Then the mocked request named status-callback on shops is signed in the X-Parcels-Signature header with the following properties:\n" +
			"  | key   | ${env:SHOP_WEBHOOK_KEY} |\n" +
			"  | value | sha256={signature}      |"},
		Run: func(sc *core.Scenario, a core.Args) error {
			pairs, err := a.Table.Pairs()
			if err != nil {
				return err
			}
			s, err := signing.Parse(a.String(2), pairs, func(v string) (string, error) { return secrets.Expand(sc, v), nil })
			if err != nil {
				return err
			}
			secrets.Keep(sc, string(s.Key))
			return checkSigned(sc, a, func(l logged) error { return s.Verify(l.header(), l.Method, l.URL, l.body()) })
		},
	}
}

// webhookStep checks that the requests a service sent are Standard
// Webhooks signed with the key.
func webhookStep() core.StepDef {
	return core.StepDef{
		ID: "mock.webhook", Keyword: "Then", Since: signedSince,
		Expr: "the mocked request named {word}[[ on {mockedService}]] is signed as a standard webhook with the key {string}",
		Doc: "Check that the named request was received, and that every request it names is a Standard Webhook (standardwebhooks.com) " +
			"signed with the key: its `webhook-signature` has the signature of its `webhook-id`, `webhook-timestamp` and body.\n\n" +
			"- The key is `whsec_` and the key in base64, as Standard Webhooks give it; it expands `${env:..}`, and is masked.",
		Examples: []string{"Then the mocked request named delivered on shops is signed as a standard webhook with the key '${env:SHOP_WEBHOOK_KEY}'"},
		Run: func(sc *core.Scenario, a core.Args) error {
			key := secrets.Expand(sc, a.String(2))
			if err := signing.CheckStandardKey(key); err != nil {
				return err
			}
			secrets.Keep(sc, key)
			return checkSigned(sc, a, func(l logged) error { return signing.VerifyStandard(l.header(), key, l.body()) })
		},
	}
}

// checkSigned checks every request the named request matches.
func checkSigned(sc *core.Scenario, a core.Args, verify func(logged) error) error {
	svc, err := service(sc, a, 1)
	if err != nil {
		return err
	}
	name := a.String(0)
	p, err := named(svc, name)
	if err != nil {
		return err
	}
	found, err := svc.c.find(sc.Context(), p)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return core.Failf("%s received no request named %s (%s), so none is signed", svc.Name, name, p.describe())
	}
	var bad []string
	for _, l := range found {
		if err := verify(l); err != nil {
			bad = append(bad, fmt.Sprintf("%s %s: %v", l.Method, l.URL, err))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return secrets.Hide(sc, core.Failf("%d of the %d requests named %s on %s are not signed as they should be:\n  - %s",
		len(bad), len(found), name, svc.Name, strings.Join(bad, "\n  - ")))
}
