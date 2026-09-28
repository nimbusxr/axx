package rest

import (
	"net/http"
	"os"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/signing"
	"github.com/nimbusxr/axx/internal/tokens"
)

// authSince is the axx version that introduced tokens and signing.
const authSince = "0.1.5"

// signer signs a request about to be sent: path is its path with the query.
type signer func(h http.Header, method, path string, body []byte, now time.Time) error

// authSteps are the steps that authorize and sign requests: tokens a
// scenario registers, and webhook signatures.
func authSteps() []core.StepDef {
	out := []core.StepDef{{
		ID: "rest.token", Keyword: "Given", Arg: core.ArgTable, Since: authSince,
		Expr: "the {word} token with the following properties:",
		Doc: docList("Register a bearer token under a name: a JSON Web Token axx signs, or an OAuth 2.0 client credentials token axx gets from a token endpoint.",
			"A request uses it with `the request is authorized with the {word} token`; the steps of other packs that take header rows, "+
				"like the websocket and sse packs, with `${token:<name>}`.",
			"A JSON Web Token is signed each time it is used, with `iat` and `exp` from then; the claims of the table add to them or replace them.",
			"A client credentials token is got once for the run, and again once it expires.",
			"Values expand `${env:..}` and `${sys:..}`; the token and its secrets are masked in logs and failures."),
		Table: &core.TableDoc{
			Columns: []string{"property", "value"}, Rows: tokens.Rows,
			Note: "Give a `key` for a token axx signs, or a `token url`, `client id` and `client secret` for one it gets, not both.",
		},
		Examples: []string{
			"Given the shop token with the following properties:\n" +
				"  | key        | ${env:SHOP_TOKEN_KEY} |\n" +
				"  | claim.shop | maple-crafts          |",
			"Given the shop-system token with the following properties:\n" +
				"  | token url     | http://localhost:8400/oauth/token |\n" +
				"  | client id     | maple-crafts                      |\n" +
				"  | client secret | ${env:SHOP_CLIENT_SECRET}         |\n" +
				"  | scope         | parcels:read                      |",
		},
		Run: func(sc *core.Scenario, a core.Args) error {
			pairs, err := a.Table.Pairs()
			if err != nil {
				return err
			}
			t, err := tokens.Parse(a.String(0), pairs,
				func(v string) (string, error) { return secrets.Expand(sc, v), nil },
				func(p string) ([]byte, error) {
					path, err := sc.Suite().ResolvePath(p)
					if err != nil {
						return nil, err
					}
					return os.ReadFile(path)
				})
			if err != nil {
				return err
			}
			secrets.Keep(sc, t.Secrets()...)
			return tokens.Register(sc, t)
		},
	}}
	for _, f := range []family{
		{
			id: "rest.request.token", keyword: "Given", noun: "request", since: authSince,
			head: "the request is authorized with the {word} token", nHead: 1,
			doc: "Send the request with the token of that name: `Authorization: Bearer <token>`.",
			details: []string{
				"The token is had when the request is executed: a JSON Web Token is signed then.",
			},
			example:      "Given the request is authorized with the shop token",
			namedExample: "Given the request is authorized with the shop token for 2nd ordered request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return withRequest(sc, a, t, func(r *Request) error {
					r.token = a.String(0)
					return nil
				})
			},
		},
		{
			id: "rest.request.signed", keyword: "Given", arg: core.ArgTable, noun: "request", since: authSince,
			head: "the request is signed in the {word} header", tail: " with the following properties:", nHead: 1,
			doc: "Sign the request as webhooks are signed: an HMAC of its body, or of what the `signs` template gives, in the header.",
			details: []string{
				"The signature is made when the request is executed, over the body as it is sent.",
				"`sha256={signature}` is GitHub's form; `t={timestamp},v1={signature}` with `{timestamp}.{body}` signed, Stripe's.",
				"The key expands `${env:..}`, and is masked.",
			},
			table: &core.TableDoc{Columns: []string{"property", "value"}, Rows: signing.Rows},
			example: "Given the request is signed in the X-Courier-Signature header with the following properties:\n" +
				"  | key   | ${env:COURIER_WEBHOOK_KEY} |\n" +
				"  | value | sha256={signature}         |",
			namedExample: "Given the request is signed in the Stripe-Signature header for request on parcels with the following properties:\n" +
				"  | key   | ${env:PAYMENTS_WEBHOOK_KEY}  |\n" +
				"  | signs | {timestamp}.{body}           |\n" +
				"  | value | t={timestamp},v1={signature} |",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				s, err := signing.Parse(a.String(0), pairs, func(v string) (string, error) { return secrets.Expand(sc, v), nil })
				if err != nil {
					return err
				}
				secrets.Keep(sc, string(s.Key))
				return withRequest(sc, a, t, func(r *Request) error {
					r.signers = append(r.signers, func(h http.Header, method, path string, body []byte, now time.Time) error {
						s.Sign(h, method, path, body, now)
						return nil
					})
					return nil
				})
			},
		},
		{
			id: "rest.request.webhook", keyword: "Given", noun: "request", since: authSince,
			head: "the request is signed as a standard webhook with the key {string}", nHead: 1,
			doc: "Sign the request as a Standard Webhook (standardwebhooks.com): its `webhook-id`, `webhook-timestamp` and `webhook-signature` headers.",
			details: []string{
				"The key is `whsec_` and the key in base64, as Standard Webhooks give it; it expands `${env:..}`, and is masked.",
				"The signature is made when the request is executed, over the body as it is sent.",
			},
			example:      "Given the request is signed as a standard webhook with the key '${env:SHOP_WEBHOOK_KEY}'",
			namedExample: "Given the request is signed as a standard webhook with the key '${env:SHOP_WEBHOOK_KEY}' for request on shops",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				key := secrets.Expand(sc, a.String(0))
				if err := signing.CheckStandardKey(key); err != nil {
					return err
				}
				secrets.Keep(sc, key)
				return withRequest(sc, a, t, func(r *Request) error {
					r.signers = append(r.signers, func(h http.Header, _, _ string, body []byte, now time.Time) error {
						return signing.SignStandard(h, key, body, now)
					})
					return nil
				})
			},
		},
	} {
		out = append(out, f.defs()...)
	}
	return out
}
