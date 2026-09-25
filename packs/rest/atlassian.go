package rest

import (
	"fmt"
	"strings"

	liberrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
)

// Issue is one OpenAPI validation finding, keyed like the swagger request
// validator in the WireMock extension (validation.request.body.schema.required,
// validation.response.status.unknown, ...), so one set of level keys means the
// same thing for mocks and services.
type Issue struct {
	Key     string `json:"key"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// direction says whether findings come from validating the request or the
// response; some validator errors do not say it themselves.
type direction int

const (
	dirRequest direction = iota
	dirResponse
)

func (d direction) prefix() string {
	if d == dirResponse {
		return "validation.response"
	}
	return "validation.request"
}

// Keys produced by the mapping. The list is the swagger request validator's
// key set that libopenapi-validator can detect; the docs render it.
var knownKeys = []struct{ key, when string }{
	{"validation.request.path.missing", "no path of the specification matches the request path"},
	{"validation.request.operation.notAllowed", "the path exists but not for the request method"},
	{"validation.request.body.missing", "the operation requires a body and the request has none"},
	{"validation.request.body.unexpected", "the request has a body the operation does not declare"},
	{"validation.request.contentType.notAllowed", "the request Content-Type is not declared for the operation"},
	{"validation.request.body.schema.{keyword}", "the request body violates a schema keyword (type, required, enum, format, pattern, minimum, maxLength, additionalProperties, oneOf, ...)"},
	{"validation.request.body.schema.invalidJson", "the request body cannot be parsed"},
	{"validation.request.body.schema.processingError", "the request body schema cannot be compiled or the body cannot be read"},
	{"validation.request.parameter.missing", "a required path parameter is missing"},
	{"validation.request.parameter.query.missing", "a required query parameter is missing"},
	{"validation.request.parameter.header.missing", "a required header parameter is missing"},
	{"validation.request.parameter.cookie.missing", "a required cookie parameter is missing"},
	{"validation.request.parameter.schema.{keyword}", "a parameter value violates its schema (type, enum, format, pattern, minimum, ...)"},
	{"validation.request.parameter.schema.invalidJson", "a JSON (content) parameter cannot be parsed"},
	{"validation.request.parameter.collection.invalidFormat", "an array or object parameter is serialized in the wrong style"},
	{"validation.request.parameter.collection.tooManyItems", "an array parameter has more than maxItems items"},
	{"validation.request.parameter.collection.tooFewItems", "an array parameter has fewer than minItems items"},
	{"validation.request.parameter.collection.duplicateItems", "an array parameter with uniqueItems repeats an item"},
	{"validation.request.parameter.{in}.invalid", "any other parameter problem; {in} is `path`, `query`, `header` or `cookie`"},
	{"validation.request.security.missing", "the credentials a security requirement needs are absent"},
	{"validation.request.security.invalid", "credentials are present but do not match the security scheme"},
	{"validation.response.status.unknown", "the response status is not documented for the operation (and there is no default)"},
	{"validation.response.contentType.notAllowed", "the response Content-Type is not declared for the status"},
	{"validation.response.body.missing", "the response declares a body schema but has no body"},
	{"validation.response.body.unexpected", "a response to HEAD has a body"},
	{"validation.response.body.schema.{keyword}", "the response body violates a schema keyword"},
	{"validation.response.body.schema.invalidJson", "the response body cannot be parsed"},
	{"validation.response.body.schema.processingError", "the response body schema cannot be compiled or the body cannot be read"},
	{"validation.response.header.missing", "a required response header is missing"},
	{"validation.response.header.schema.{keyword}", "a response header value violates its schema"},
	{"validation.request.unknownError", "anything else the validator reports about the request"},
	{"validation.response.unknownError", "anything else the validator reports about the response"},
}

// schemaKeywords are the JSON Schema and OpenAPI keywords a schema failure
// can be keyed by. Newer keywords map to the draft-4 names the swagger
// request validator used.
var schemaKeywords = map[string]string{
	"type": "type", "required": "required", "enum": "enum", "const": "enum", "format": "format",
	"pattern": "pattern", "minLength": "minLength", "maxLength": "maxLength",
	"minimum": "minimum", "maximum": "maximum", "exclusiveMinimum": "minimum", "exclusiveMaximum": "maximum",
	"multipleOf": "multipleOf", "minItems": "minItems", "maxItems": "maxItems", "uniqueItems": "uniqueItems",
	"minProperties": "minProperties", "maxProperties": "maxProperties",
	"additionalProperties": "additionalProperties", "unevaluatedProperties": "additionalProperties",
	"additionalItems": "additionalItems", "unevaluatedItems": "additionalItems",
	"items": "items", "prefixItems": "items", "contains": "contains", "minContains": "contains", "maxContains": "contains",
	"propertyNames": "propertyNames", "dependencies": "dependencies", "dependentRequired": "dependencies",
	"dependentSchemas": "dependencies", "oneOf": "oneOf", "anyOf": "anyOf", "allOf": "allOf", "not": "not",
	"if": "if", "then": "then", "else": "else", "nullable": "nullable", "readOnly": "readOnly",
	"writeOnly": "writeOnly", "discriminator": "discriminator",
	"contentEncoding": "contentEncoding", "contentMediaType": "contentMediaType",
}

// nameContainers are keywords whose next pointer segment is a name, not a
// keyword (a property called "type" must not read as the type keyword).
var nameContainers = map[string]bool{
	"properties": true, "patternProperties": true, "$defs": true, "definitions": true,
	"dependentSchemas": true, "dependentRequired": true, "dependencies": true, "parameters": true,
	"paths": true, "headers": true, "responses": true, "content": true,
}

// schemaKeyword extracts the failing keyword from a JSON pointer such as
// /properties/recipient/required. It returns "unknownError" when the
// pointer names no keyword (a false schema, for example).
func schemaKeyword(pointer string) string {
	segs := strings.Split(strings.TrimPrefix(pointer, "#"), "/")
	for i := len(segs) - 1; i >= 0; i-- {
		seg := strings.ReplaceAll(strings.ReplaceAll(segs[i], "~1", "/"), "~0", "~")
		if i > 0 && nameContainers[segs[i-1]] {
			continue
		}
		if kw, ok := schemaKeywords[seg]; ok {
			return kw
		}
	}
	return "unknownError"
}

// mapIssues converts libopenapi-validator errors to keyed findings (levels
// are applied by the caller). One validator error with several schema
// failures becomes one finding per failure, as the swagger request
// validator reported them.
func mapIssues(dir direction, errs []*liberrors.ValidationError, requestHasBody bool) []Issue {
	var out []Issue
	for _, e := range errs {
		if e == nil {
			continue
		}
		out = append(out, mapIssue(dir, e, requestHasBody)...)
	}
	return out
}

func mapIssue(dir direction, e *liberrors.ValidationError, requestHasBody bool) []Issue {
	msg := strings.TrimSpace(e.Message)
	detail := msg
	if e.Reason != "" && !strings.Contains(msg, e.Reason) {
		detail = msg + ": " + e.Reason
	}
	one := func(key string) []Issue { return []Issue{{Key: key, Message: detail}} }
	lower := strings.ToLower(msg)

	switch e.ValidationType {
	case helpers.PathValidation:
		if e.ValidationSubType == helpers.ValidationMissingOperation {
			return one("validation.request.operation.notAllowed")
		}
		return one("validation.request.path.missing")

	case helpers.RequestValidation:
		if e.ValidationSubType == helpers.ValidationMissingOperation {
			return one("validation.request.operation.notAllowed")
		}
		return one("validation.request.unknownError")

	case helpers.SecurityValidation:
		if strings.Contains(lower, "mismatch") || strings.Contains(lower, "authentication failed") {
			return one("validation.request.security.invalid")
		}
		return one("validation.request.security.missing")

	case helpers.RequestBodyValidation:
		if e.ValidationSubType == helpers.RequestBodyContentType {
			if !requestHasBody && strings.Contains(msg, "content type ''") {
				return one("validation.request.body.missing")
			}
			return one("validation.request.contentType.notAllowed")
		}
		return bodyIssues("validation.request.body", e, detail, lower)

	case helpers.ResponseBodyValidation: // "response"
		switch e.ValidationSubType {
		case helpers.ResponseBodyResponseCode:
			return one("validation.response.status.unknown")
		case helpers.RequestBodyContentType:
			return one("validation.response.contentType.notAllowed")
		case "object":
			return one("validation.response.body.missing")
		case helpers.ParameterValidationHeader:
			return one("validation.response.header.missing")
		case "headers":
			return schemaIssues("validation.response.header.schema", e, detail)
		}
		if strings.Contains(lower, "must not include a body") {
			return one("validation.response.body.unexpected")
		}
		if strings.Contains(lower, "cannot be read, it's empty") {
			return one("validation.response.body.missing")
		}
		return bodyIssues("validation.response.body", e, detail, lower)

	case helpers.ParameterValidation:
		return parameterIssues(e, detail, lower)

	case helpers.URLEncodedValidation, helpers.XmlValidation:
		base := dir.prefix() + ".body"
		switch e.ValidationSubType {
		case helpers.InvalidTypeEncoding:
			return one(base + ".schema.type")
		case helpers.Schema:
			if len(e.SchemaValidationErrors) > 0 {
				return schemaIssues(base+".schema", e, detail)
			}
			return one(base + ".schema.invalidJson")
		}
		return one(base + ".schema.unknownError")

	case "strict":
		return one(dir.prefix() + ".body.schema.additionalProperties")
	}
	return one(dir.prefix() + ".unknownError")
}

// bodyIssues maps request or response body errors under base
// (validation.request.body or validation.response.body).
func bodyIssues(base string, e *liberrors.ValidationError, detail, lower string) []Issue {
	one := func(key string) []Issue { return []Issue{{Key: key, Message: detail}} }
	switch {
	case len(e.SchemaValidationErrors) > 0:
		return schemaIssues(base+".schema", e, detail)
	case strings.Contains(lower, "is empty for"):
		return one(base + ".missing")
	case strings.Contains(lower, "is not declared"):
		return one(base + ".unexpected")
	case strings.Contains(lower, "could not be decoded") || strings.Contains(strings.ToLower(e.Reason), "cannot be decoded"):
		return one(base + ".schema.invalidJson")
	case strings.Contains(lower, "schema compilation") || strings.Contains(lower, "schema is nil") ||
		strings.Contains(lower, "cannot be rendered") || strings.Contains(lower, "no registered decoder") ||
		strings.Contains(lower, "could not be read") || strings.Contains(lower, "could not be inspected"):
		return one(base + ".schema.processingError")
	}
	return one(base + ".schema.unknownError")
}

// parameterIssues maps request parameter errors.
func parameterIssues(e *liberrors.ValidationError, detail, lower string) []Issue {
	one := func(key string) []Issue { return []Issue{{Key: key, Message: detail}} }
	in := e.ValidationSubType // path, query, header, cookie
	switch {
	case strings.HasSuffix(lower, " is missing"):
		if in == helpers.ParameterValidationPath {
			return one("validation.request.parameter.missing")
		}
		return one("validation.request.parameter." + in + ".missing")
	case len(e.SchemaValidationErrors) > 0:
		return schemaIssues("validation.request.parameter.schema", e, detail)
	case strings.Contains(lower, "is not a valid integer") || strings.Contains(lower, "is not a valid number") ||
		strings.Contains(lower, "is not a valid boolean"):
		return one("validation.request.parameter.schema.type")
	case strings.Contains(lower, "does not match allowed values"):
		return one("validation.request.parameter.schema.enum")
	case strings.Contains(lower, "has too many items"):
		return one("validation.request.parameter.collection.tooManyItems")
	case strings.Contains(lower, "does not have enough items"):
		return one("validation.request.parameter.collection.tooFewItems")
	case strings.Contains(lower, "contains non-unique items"):
		return one("validation.request.parameter.collection.duplicateItems")
	case strings.Contains(lower, "not exploded correctly") || strings.Contains(lower, "delimited incorrectly") ||
		strings.Contains(lower, "not a valid deepobject"):
		return one("validation.request.parameter.collection.invalidFormat")
	case strings.Contains(lower, "is not valid json") || strings.Contains(lower, "cannot be decoded") ||
		strings.Contains(lower, "could not be decoded"):
		return one("validation.request.parameter.schema.invalidJson")
	case strings.Contains(lower, "schema compilation"):
		return one("validation.request.parameter.schema.processingError")
	case strings.Contains(lower, "failed to validate"):
		return one("validation.request.parameter.schema.unknownError")
	}
	if in == "" {
		return one("validation.request.parameter.invalid")
	}
	return one("validation.request.parameter." + in + ".invalid")
}

// schemaIssues returns one finding per schema failure, keyed
// <base>.<keyword>.
func schemaIssues(base string, e *liberrors.ValidationError, detail string) []Issue {
	if len(e.SchemaValidationErrors) == 0 {
		return []Issue{{Key: base + ".unknownError", Message: detail}}
	}
	out := make([]Issue, 0, len(e.SchemaValidationErrors))
	for _, f := range e.SchemaValidationErrors {
		if f == nil {
			continue
		}
		where := f.FieldPath
		if where == "" && e.ParameterName != "" {
			where = e.ParameterName
		}
		text := f.Reason
		if where != "" && where != "$" {
			text = where + ": " + text
		}
		out = append(out, Issue{Key: base + "." + schemaKeyword(f.KeywordLocation), Message: fmt.Sprintf("%s (%s)", text, strings.TrimSpace(e.Message))})
	}
	return out
}
