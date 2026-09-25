package rest

import "strings"

// packDoc is the pack's Markdown documentation.
var packDoc = buildDoc()

func buildDoc() string {
	var b strings.Builder
	b.WriteString(`Send HTTP requests to REST services, validate them against the services' OpenAPI specifications, and assert on the responses.

**Services.** Register services with ` + "`the <name> service with the following properties:`" + ` (` + "`url`" + `, optional ` + "`openapi`" + `). The first service registered in a scenario is the default one; the other steps name a service with ` + "`on <name>`" + `.

**Requests.** Each service keeps its requests in the order they are added: ` + "`a GET request to /path`" + ` adds the first (default) request and ` + "`a 2nd ordered POST request to /path`" + ` the second. Header, payload, execution and response steps address a request with ` + "`for 2nd ordered request`" + ` / ` + "`for 2nd ordered response`" + ` (default: the first). A request is executed once; its response stays available to every later step.

**Payloads.** A payload starts from an OpenAPI content example (the named one, or the first in document order; ` + "`externalValue`" + ` examples are read relative to the specification) or from an empty template ` + "`{}`" + `, and is edited with the payload property steps (JSONPath, typed values, ` + "`null`" + `/` + "`undefined`" + `). ` + "`application/x-www-form-urlencoded`" + ` payloads are sent form-encoded.

**Execution.** Requests go through one HTTP client shared by the run (connections are reused and closed at the end), honor the step timeout, follow redirects for GET and HEAD, and do not verify TLS certificates unless ` + "`packs.rest.tls.verify: true`" + ` is set in axx.yaml. Without a Content-Type header a payload is sent with its payload step's media type.

**OpenAPI validation.** When a service has an ` + "`openapi`" + ` specification (OpenAPI 3.0 or 3.1, a URL or a file; parsed once per run), each executed request and its response are validated after sending. Every finding has a key in the style of the swagger request validator (the one the WireMock extension uses), and a level:

- ` + "`ERROR`" + ` (alias ` + "`FAIL`" + `, the default for every key) fails the execute step, listing every error with its key;
- ` + "`WARN`" + ` and ` + "`INFO`" + ` are logged on the step;
- ` + "`IGNORE`" + ` drops the finding.

Levels come from ` + "`openapi.levels`" + ` in axx.yaml and are overridden per scenario with ` + "`the OpenAPI validation levels are:`" + `. A key also sets every more specific key (` + "`validation.request.body`" + ` covers ` + "`validation.request.body.schema.required`" + `); the most specific configured key wins.

| Key | Reported when |
| --- | --- |
`)
	for _, k := range knownKeys {
		b.WriteString("| `" + k.key + "` | " + k.when + " |\n")
	}
	b.WriteString(`
Schema keywords use the draft-4 names: ` + "`const`" + ` is reported as ` + "`enum`" + `, ` + "`exclusiveMinimum`" + `/` + "`exclusiveMaximum`" + ` as ` + "`minimum`" + `/` + "`maximum`" + `, ` + "`unevaluatedProperties`" + ` as ` + "`additionalProperties`" + `.

When a scenario fails, its failure context (` + "`rest`" + `) shows the last request and response (headers, bodies truncated to 2 KB) and the OpenAPI findings.
`)
	return b.String()
}
