package mock

import "github.com/nimbusxr/axx/internal/oaslevel"

// levelKeys are the keys the OpenAPI validation levels of a mocked service
// can be set on: the keys of the findings the axx WireMock extension records,
// and their prefixes. The extension validates with the Atlassian swagger
// request validator (openapi-request-validator-core, see
// extensions/wiremock-openapi/build.gradle.kts): its message keys, a schema
// failure keyed by the JSON Schema validator's message key (networknt
// json-schema-validator), and the extension's own validation.spec, for a
// specification it cannot use.
var levelKeys = oaslevel.NewKeys([]string{
	"validation.request.path.missing",
	"validation.request.operation.notAllowed",
	"validation.request.webhook.missing",
	"validation.request.body.missing",
	"validation.request.body.unexpected",
	"validation.request.contentType.invalid",
	"validation.request.contentType.notAllowed",
	"validation.request.accept.invalid",
	"validation.request.accept.notAllowed",
	"validation.request.security.missing",
	"validation.request.security.invalid",
	"validation.request.parameter.missing",
	"validation.request.parameter.query.missing",
	"validation.request.parameter.query.unexpected",
	"validation.request.parameter.header.missing",
	"validation.request.parameter.cookie.missing",
	"validation.request.parameter.enum.invalid",
	"validation.request.parameter.collection.invalid",
	"validation.request.parameter.collection.invalidFormat",
	"validation.request.parameter.collection.tooManyItems",
	"validation.request.parameter.collection.tooFewItems",
	"validation.request.parameter.collection.duplicateItems",
	"validation.response.status.unknown",
	"validation.response.body.missing",
	"validation.response.body.unexpected",
	"validation.response.contentType.invalid",
	"validation.response.contentType.notAllowed",
	"validation.response.header.missing",
	"validation.{schema}.schema.{keyword}",
	"validation.spec",
}, map[string][]string{
	"{schema}":  {"request.body", "request.parameter", "response.body", "response.header"},
	"{keyword}": schemaMessageKeys,
}, nil)

// schemaMessageKeys are what a schema failure is keyed by, after
// validation.<part>.schema.: the JSON Schema validator's message keys (its
// keywords, some with a detail, like format.date-time), the validator's own
// formats (format.int32, ...), and the failures to read the value
// (invalidJson) or to use the schema (processingError).
var schemaMessageKeys = []string{
	"invalidJson", "processingError",
	"$ref", "additionalItems", "additionalProperties", "allOf", "anyOf", "const",
	"contains", "contains.max", "contains.min", "contentEncoding", "contentMediaType",
	"dependencies", "dependentRequired", "dependentSchemas",
	"discriminator.anyOf.no_match_found", "discriminator.missing_discriminating_value", "discriminator.oneOf.no_match_found",
	"enum", "exclusiveMaximum", "exclusiveMinimum", "false",
	"format", "format.date", "format.date-time", "format.double", "format.duration", "format.email", "format.float",
	"format.hostname", "format.idn-email", "format.idn-hostname", "format.int32", "format.int64", "format.ipv4",
	"format.ipv6", "format.iri", "format.iri-reference", "format.json-pointer", "format.regex",
	"format.relative-json-pointer", "format.time", "format.unknown", "format.uri", "format.uri-reference",
	"format.uri-template", "format.uuid",
	"id", "items", "maxContains", "maxItems", "maxLength", "maxProperties", "maximum",
	"minContains", "minContainsVsMaxContains", "minItems", "minLength", "minProperties", "minimum",
	"multipleOf", "not", "notAllowed", "oneOf", "oneOf.indexes", "pattern", "patternProperties",
	"prefixItems", "properties", "propertyNames", "readOnly", "required", "type",
	"unevaluatedItems", "unevaluatedProperties", "unionType", "uniqueItems", "writeOnly",
}
