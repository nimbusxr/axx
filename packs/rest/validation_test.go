package rest

import (
	"slices"
	"sort"
	"strings"
	"testing"

	liberrors "github.com/pb33f/libopenapi-validator/errors"
)

// lastIssues returns the keys of the findings of the last executed request.
func lastIssues(h *harness) []string {
	st := stateKey.Of(h.sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.last == nil {
		return nil
	}
	var keys []string
	for _, is := range st.last.ex.Issues {
		keys = append(keys, is.Key)
	}
	sort.Strings(keys)
	return slices.Compact(keys)
}

type validationCase struct {
	name  string
	steps [][]string // step text, then optional table rows as "a|b"
	want  []string   // expected finding keys (sorted, unique)
}

func runValidationCases(t *testing.T, spec string, cases []validationCase) {
	t.Helper()
	_, srv := newAPI(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.service("space", srv.URL, spec)
			var err error
			for _, s := range c.steps {
				var rows [][]string
				for _, r := range s[1:] {
					rows = append(rows, strings.Split(r, "|"))
				}
				if err = h.step(s[0], rows...); err != nil {
					break
				}
			}
			got := lastIssues(h)
			if !slices.Equal(got, c.want) {
				t.Fatalf("findings %v, want %v (step error: %v)", got, c.want, err)
			}
			if len(c.want) > 0 {
				if err == nil {
					t.Fatal("findings at level ERROR must fail the step")
				}
				for _, k := range c.want {
					if !strings.Contains(err.Error(), k) {
						t.Errorf("error does not list %s:\n%v", k, err)
					}
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidationKeysOpenAPI30(t *testing.T) {
	jsonHeaders := []string{"the request headers are:", "Content-Type|application/json", "Accept|application/json"}
	runValidationCases(t, "space30.yaml", []validationCase{
		{"valid", [][]string{{"a GET request to /api/launches"}, {"the request is executed"}}, nil},
		{
			"path missing",
			[][]string{{"a GET request to /api/nowhere"}, {"the request is executed"}},
			[]string{"validation.request.path.missing"},
		},
		{
			"operation not allowed",
			[][]string{{"a DELETE request to /api/launches"}, {"the request is executed"}},
			[]string{"validation.request.operation.notAllowed"},
		},
		{
			"body missing",
			[][]string{{"a POST request to /api/launches"}, {"the request is executed"}},
			[]string{"validation.request.body.missing"},
		},
		{"body required", [][]string{
			{"a POST request to /api/launches"},
			jsonHeaders,
			{"a request payload using an application/json empty content template"},
			{"the request is executed"},
		}, []string{"validation.request.body.schema.required", "validation.response.body.schema.required"}},
		{"body type enum format minLength", [][]string{
			{"a POST request to /api/launches"},
			jsonHeaders,
			{"a request payload using an application/json content example named 'Alpha Launch'"},
			{"the request payload properties are:", `flight_number|"one"`, "status|bogus", "date_utc|yesterday", `name|""`},
			{"the request is executed"},
		}, []string{
			"validation.request.body.schema.enum", "validation.request.body.schema.format", "validation.request.body.schema.minLength",
			"validation.request.body.schema.type", "validation.response.body.schema.enum", "validation.response.body.schema.format",
			"validation.response.body.schema.minLength", "validation.response.body.schema.type",
		}},
		{"nested required in array item", [][]string{
			{"a POST request to /api/launches"},
			jsonHeaders,
			{"a request payload using an application/json content example"},
			{"the request payload property crew is '[{\"last_name\": \"Doe\"}]'"},
			{"the request is executed"},
		}, []string{"validation.request.body.schema.required", "validation.response.body.schema.required"}},
		{"content type not allowed", [][]string{
			{"a POST request to /api/launches"},
			{"the request header Content-Type is 'text/plain'"},
			{"a request payload using an application/json empty content template"},
			{"the request is executed"},
		}, []string{"validation.request.contentType.notAllowed", "validation.response.body.schema.required"}},
		{
			"query and header parameters",
			[][]string{{"a GET request to /api/missions?limit=abc"}, {"the request is executed"}},
			[]string{"validation.request.parameter.header.missing", "validation.request.parameter.query.missing", "validation.request.parameter.schema.type"},
		},
		{
			"query enum",
			[][]string{{"a GET request to /api/missions?status=bogus"}, {"the request header X-Trace is 't1'"}, {"the request is executed"}},
			[]string{"validation.request.parameter.schema.enum"},
		},
		{"parameters valid", [][]string{{"a GET request to /api/missions?status=planned&limit=5"}, {"the request header X-Trace is 't1'"}, {"the request is executed"}}, nil},
		{
			"path parameter type",
			[][]string{{"a GET request to /api/launches/abc"}, {"the request is executed"}},
			[]string{"validation.request.parameter.schema.type"},
		},
		{
			"security missing",
			[][]string{{"a GET request to /api/secure"}, {"the request is executed"}},
			[]string{"validation.request.security.missing"},
		},
		{"security present", [][]string{{"a GET request to /api/secure"}, {"the request header X-API-Key is 'k'"}, {"the request is executed"}}, nil},
		{
			"response status unknown",
			[][]string{{"a GET request to /api/launches/418"}, {"the request is executed"}},
			[]string{"validation.response.status.unknown"},
		},
		{
			"response header missing",
			[][]string{{"a GET request to /api/launches/2"}, {"the request is executed"}},
			[]string{"validation.response.header.missing"},
		},
		{
			"response body types",
			[][]string{{"a GET request to /api/launches/3"}, {"the request is executed"}},
			[]string{"validation.response.body.schema.type"},
		},
		{
			"response content type",
			[][]string{{"a GET request to /api/launches/4"}, {"the request is executed"}},
			[]string{"validation.response.contentType.notAllowed"},
		},
		{"documented error response", [][]string{
			{"a POST request to /api/launches?fail=1"},
			jsonHeaders,
			{"a request payload using an application/json content example"},
			{"the request is executed"},
		}, nil},
		{"form body", [][]string{
			{"a POST request to /api/launches/search"},
			{"a request payload using an application/x-www-form-urlencoded empty content template"},
			{"the request payload property name is 'Falcon'"},
			{"the request is executed"},
		}, nil},
	})
}

func TestValidationKeysOpenAPI31(t *testing.T) {
	jsonHeader := []string{"the request header Content-Type is 'application/json'"}
	runValidationCases(t, "space31.json", []validationCase{
		{"valid", [][]string{
			{"a POST request to /api/launches"},
			jsonHeader,
			{"a request payload using an application/json content example named 'First'"},
			{"the request payload properties are:", "note|x", "kind|rocket"},
			{"the request payload property note is null"},
			{"the request is executed"},
		}, nil},
		{"const exclusiveMinimum additionalProperties pattern", [][]string{
			{"a POST request to /api/launches"},
			jsonHeader,
			{"a request payload using an application/json content example named 'First'"},
			{"the request payload properties are:", "kind|plane", "flight_number|0", "extra|1", "name|forbidden-name"},
			{"the request is executed"},
		}, []string{
			"validation.request.body.schema.additionalProperties", "validation.request.body.schema.enum",
			"validation.request.body.schema.minimum", "validation.request.body.schema.pattern",
			"validation.response.body.schema.additionalProperties",
			"validation.response.body.schema.enum", "validation.response.body.schema.minimum", "validation.response.body.schema.pattern",
		}},
		{"type array", [][]string{
			{"a POST request to /api/launches"},
			jsonHeader,
			{"a request payload using an application/json content example named 'First'"},
			{"the request payload property note is '5'"},
			{"the request is executed"},
		}, []string{"validation.request.body.schema.type", "validation.response.body.schema.type"}},
		{
			"path parameter and undocumented 404",
			[][]string{{"a GET request to /api/launches/x1"}, {"the request is executed"}},
			[]string{"validation.request.parameter.schema.type", "validation.response.status.unknown"},
		},
	})
}

func TestLevelsInScenario(t *testing.T) {
	_, srv := newAPI(t)
	body := func(h *harness) {
		h.ok("a POST request to /api/launches")
		h.ok("the request header Content-Type is 'application/json'")
		h.ok("a request payload using an application/json empty content template")
	}

	t.Run("ignore request body, response still fails", func(t *testing.T) {
		h := newHarness(t)
		h.service("space", srv.URL, "space30.yaml")
		h.ok("the OpenAPI validation levels are:", []string{"validation.request.body", "IGNORE"})
		body(h)
		err := h.assertion("the request is executed", "validation.response.body.schema.required")
		if strings.Contains(err.Error(), "validation.request.body") {
			t.Fatalf("ignored key reported: %v", err)
		}
		if got := lastIssues(h); !slices.Equal(got, []string{"validation.response.body.schema.required"}) {
			t.Fatalf("kept findings %v", got)
		}
	})

	t.Run("warn and info are logged", func(t *testing.T) {
		h := newHarness(t)
		h.service("space", srv.URL, "space30.yaml")
		h.ok("the OpenAPI validation levels on space are:",
			[]string{"validation.request", "warn"}, []string{"validation.response.body.schema", "INFO"})
		body(h)
		h.ok("the request is executed")
		logs := strings.Join(h.sink.logs, "\n")
		if !strings.Contains(logs, "OpenAPI WARN validation.request.body.schema.required") ||
			!strings.Contains(logs, "OpenAPI INFO validation.response.body.schema.required") {
			t.Fatalf("logs:\n%s", logs)
		}
	})

	t.Run("FAIL is an alias of ERROR and the most specific key wins", func(t *testing.T) {
		h := newHarness(t)
		h.service("space", srv.URL, "space30.yaml")
		h.ok("the OpenAPI validation levels are:",
			[]string{"validation", "IGNORE"}, []string{"validation.response.body.schema.required", "FAIL"})
		body(h)
		h.assertionFails("the request is executed", "validation.response.body.schema.required")
	})

	t.Run("invalid level", func(t *testing.T) {
		h := newHarness(t)
		h.service("space", srv.URL, "space30.yaml")
		err := h.failure("the OpenAPI validation levels are:", "Supported levels", []string{"validation.request.body", "LOUD"})
		if !strings.Contains(err.Error(), `"LOUD"`) || !strings.Contains(err.Error(), "FAIL") {
			t.Fatal(err)
		}
	})

	t.Run("axx.yaml levels with scenario overrides", func(t *testing.T) {
		h := newHarness(t, withPackConfig("rest", `{"openapi":{"levels":{"validation.request":"IGNORE","validation.response":"WARN"}}}`))
		h.service("space", srv.URL, "space30.yaml")
		body(h)
		h.ok("the request is executed")

		h2 := newHarness(t, withPackConfig("rest", `{"openapi":{"levels":{"validation.request":"IGNORE","validation.response":"WARN"}}}`))
		h2.service("space", srv.URL, "space30.yaml")
		h2.ok("the OpenAPI validation levels are:", []string{"validation.request.body.schema.required", "ERROR"})
		body(h2)
		err := h2.assertion("the request is executed", "validation.request.body.schema.required")
		if strings.Contains(err.Error(), "validation.response") {
			t.Fatalf("response findings are WARN: %v", err)
		}
	})

	t.Run("invalid configured level", func(t *testing.T) {
		h := newHarness(t, withPackConfig("rest", `{"openapi":{"levels":{"validation.request":"NOPE"}}}`))
		h.service("space", srv.URL, "space30.yaml")
		h.ok("a GET request to /api/launches")
		h.fails("the request is executed", "openapi.levels")
		if err := (pack{}).Init(t.Context(), h.suite); err == nil {
			t.Fatal("Init must report the invalid level")
		}
	})
}

func TestParseAndResolveLevels(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Level
	}{{"ERROR", LevelError}, {"fail", LevelError}, {" Warn ", LevelWarn}, {"INFO", LevelInfo}, {"ignore", LevelIgnore}} {
		got, err := ParseLevel(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseLevel(%q) = %v, %v", c.in, got, err)
		}
	}
	if _, err := ParseLevel("DEBUG"); err == nil {
		t.Error("DEBUG is not a level")
	}
	l := Levels{"validation.request": LevelIgnore, "validation.request.body.schema": LevelWarn, "validation": LevelInfo}
	for key, want := range map[string]Level{
		"validation.request.body.schema.required": LevelWarn,
		"validation.request.body.missing":         LevelIgnore,
		"validation.request":                      LevelIgnore,
		"validation.response.status.unknown":      LevelInfo,
		"other.key":                               LevelError,
		"validation.requestX":                     LevelInfo, // not a dotted prefix of validation.request
	} {
		if got := l.Resolve(key); got != want {
			t.Errorf("Resolve(%s) = %v, want %v", key, got, want)
		}
	}
	merged := Levels{"a": LevelWarn, "b": LevelInfo}.Merge(Levels{"a": LevelIgnore})
	if merged["a"] != LevelIgnore || merged["b"] != LevelInfo {
		t.Errorf("Merge = %v", merged)
	}
}

func TestSchemaKeyword(t *testing.T) {
	for ptr, want := range map[string]string{
		"/required":                                        "required",
		"/properties/crew/items/required":                  "required",
		"/properties/type/type":                            "type",
		"/properties/required":                             "unknownError", // a property named "required"
		"/$ref/properties/name/minLength":                  "minLength",
		"/allOf/0/properties/kind/const":                   "enum",
		"/properties/n/exclusiveMinimum":                   "minimum",
		"/unevaluatedProperties":                           "additionalProperties",
		"/paths/~1api~1x/get/parameters/id/schema/maximum": "maximum",
		"": "unknownError",
	} {
		if got := schemaKeyword(ptr); got != want {
			t.Errorf("schemaKeyword(%q) = %s, want %s", ptr, got, want)
		}
	}
}

// TestMapIssuesTable pins the mapping of every validator error family to
// swagger request validator keys, independent of the spec that produces it.
func TestMapIssuesTable(t *testing.T) {
	sv := func(ptr string) []*liberrors.SchemaValidationFailure {
		return []*liberrors.SchemaValidationFailure{{Reason: "r", FieldPath: "$.a", KeywordLocation: ptr}}
	}
	cases := []struct {
		dir     direction
		hasBody bool
		e       liberrors.ValidationError
		want    []string
	}{
		{dirRequest, false, liberrors.ValidationError{ValidationType: "path", ValidationSubType: "missing", Message: "GET Path '/x' not found"}, []string{"validation.request.path.missing"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "path", ValidationSubType: "missingOperation"}, []string{"validation.request.operation.notAllowed"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "request", ValidationSubType: "missingOperation"}, []string{"validation.request.operation.notAllowed"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "contentType", Message: "POST operation request content type '' does not exist"}, []string{"validation.request.body.missing"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "contentType", Message: "POST operation request content type '' does not exist"}, []string{"validation.request.contentType.notAllowed"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "contentType", Message: "POST operation request content type 'text/plain' does not exist"}, []string{"validation.request.contentType.notAllowed"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "schema", Message: "POST request body is empty for '/x'"}, []string{"validation.request.body.missing"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "schema", Message: "POST request body for '/x' is not declared"}, []string{"validation.request.body.unexpected"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "schema", Message: "POST request body for '/x' could not be decoded"}, []string{"validation.request.body.schema.invalidJson"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "schema", Message: "POST request body for '/x' failed to validate schema", Reason: "The request body cannot be decoded: x"}, []string{"validation.request.body.schema.invalidJson"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "schema", Message: "POST request body for '/x' failed schema compilation"}, []string{"validation.request.body.schema.processingError"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "schema", SchemaValidationErrors: append(sv("/required"), sv("/properties/a/type")...)}, []string{"validation.request.body.schema.required", "validation.request.body.schema.type"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "requestBody", ValidationSubType: "schema", Message: "something new"}, []string{"validation.request.body.schema.unknownError"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query parameter 'q' is missing"}, []string{"validation.request.parameter.query.missing"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "header", Message: "Header parameter 'h' is missing"}, []string{"validation.request.parameter.header.missing"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "cookie", Message: "Cookie parameter 'c' is missing"}, []string{"validation.request.parameter.cookie.missing"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "path", Message: "Path parameter 'id' is missing"}, []string{"validation.request.parameter.missing"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query parameter 'q' is not a valid integer"}, []string{"validation.request.parameter.schema.type"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "header", Message: "Header parameter 'h' does not match allowed values"}, []string{"validation.request.parameter.schema.enum"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query array parameter 'q' has too many items"}, []string{"validation.request.parameter.collection.tooManyItems"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query array parameter 'q' does not have enough items"}, []string{"validation.request.parameter.collection.tooFewItems"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query array parameter 'q' contains non-unique items"}, []string{"validation.request.parameter.collection.duplicateItems"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query parameter 'q' delimited incorrectly"}, []string{"validation.request.parameter.collection.invalidFormat"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query parameter 'q' is not valid JSON"}, []string{"validation.request.parameter.schema.invalidJson"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query parameter 'q' failed to validate", SchemaValidationErrors: sv("/paths/~1x/get/parameters/q/schema/maxLength")}, []string{"validation.request.parameter.schema.maxLength"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "parameter", ValidationSubType: "query", Message: "Query parameter 'q' value contains reserved values"}, []string{"validation.request.parameter.query.invalid"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "security", Message: "API Key X not found in header"}, []string{"validation.request.security.missing"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "security", Message: "Authorization header for 'bearer' scheme"}, []string{"validation.request.security.missing"}},
		{dirRequest, false, liberrors.ValidationError{ValidationType: "security", Message: "Authorization header scheme 'bearer' mismatch"}, []string{"validation.request.security.invalid"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "statusCode"}, []string{"validation.response.status.unknown"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "contentType"}, []string{"validation.response.contentType.notAllowed"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "object", Message: "GET response object is missing for '/x'"}, []string{"validation.response.body.missing"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "header", Message: "Missing required header"}, []string{"validation.response.header.missing"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "headers", SchemaValidationErrors: sv("/type")}, []string{"validation.response.header.schema.type"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "schema", Message: "HEAD response for '/x' must not include a body"}, []string{"validation.response.body.unexpected"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "schema", Message: "GET response body for '/x' failed to validate schema", Reason: "The response body cannot be decoded: bad"}, []string{"validation.response.body.schema.invalidJson"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "response", ValidationSubType: "schema", SchemaValidationErrors: sv("/items/properties/a/enum")}, []string{"validation.response.body.schema.enum"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "urlEncodedValidation", ValidationSubType: "schema", Message: "Unable to parse form-urlencoded body"}, []string{"validation.request.body.schema.invalidJson"}},
		{dirRequest, true, liberrors.ValidationError{ValidationType: "urlEncodedValidation", ValidationSubType: "invalidTypeEncoding"}, []string{"validation.request.body.schema.type"}},
		{dirResponse, false, liberrors.ValidationError{ValidationType: "somethingElse"}, []string{"validation.response.unknownError"}},
	}
	for _, c := range cases {
		e := c.e
		got := mapIssues(c.dir, []*liberrors.ValidationError{&e}, c.hasBody)
		var keys []string
		for _, is := range got {
			keys = append(keys, is.Key)
		}
		if !slices.Equal(keys, c.want) {
			t.Errorf("%s/%s %q: keys %v, want %v", e.ValidationType, e.ValidationSubType, e.Message, keys, c.want)
		}
	}
}
