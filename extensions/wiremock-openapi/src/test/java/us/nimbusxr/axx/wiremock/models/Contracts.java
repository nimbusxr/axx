// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import com.atlassian.oai.validator.OpenApiInteractionValidator;
import com.atlassian.oai.validator.report.LevelResolver;
import com.atlassian.oai.validator.model.SimpleRequest;
import com.atlassian.oai.validator.model.SimpleResponse;
import com.atlassian.oai.validator.report.MessageResolver;
import com.atlassian.oai.validator.report.ValidationReport;
import com.atlassian.oai.validator.schema.SchemaValidator;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;

import io.swagger.v3.oas.models.OpenAPI;
import io.swagger.v3.oas.models.media.Schema;
import io.swagger.v3.parser.OpenAPIV3Parser;
import io.swagger.v3.parser.core.models.ParseOptions;

import java.net.URI;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * The providers' published contracts, which the build fetches pinned (build.gradle.kts):
 * OpenAI's OpenAPI document ({@code openai.json}) and Ollama's ({@code ollama.yaml}).
 *
 * <p>The validator refuses the properties a schema does not list, unless it allows them. That
 * holds OpenAI's answers to every property OpenAI lists; Ollama's document lists only some of
 * what Ollama's server sends (a tool call's {@code id} and {@code index}, which Ollama's own Go
 * types have), so its unlisted properties are allowed, as JSON Schema allows them.
 */
final class Contracts {
    private Contracts() {}

    private static final Map<String, OpenAPI> APIS = new ConcurrentHashMap<>();
    private static final Map<String, OpenApiInteractionValidator> VALIDATORS = new ConcurrentHashMap<>();

    static OpenAPI api(String spec) {
        return APIS.computeIfAbsent(spec, s -> {
            ParseOptions options = new ParseOptions();
            options.setResolve(true);
            OpenAPI api = new OpenAPIV3Parser().read(Path.of(System.getProperty("axx.contracts"), s).toString(), null, options);
            if (api == null) {
                throw new IllegalStateException("cannot read the contract " + s);
            }
            merge(api);
            return api;
        });
    }

    /**
     * The contract's findings on the mock's answer to a request it served. The requests are the
     * SDKs', which the validator misreads where the contract combines schemas.
     */
    static List<String> interaction(String spec, ServeEvent e) {
        OpenApiInteractionValidator validator = VALIDATORS.computeIfAbsent(spec,
                s -> OpenApiInteractionValidator.createFor(api(s)).withLevelResolver(levels(s)).build());
        URI uri = URI.create(e.getRequest().getUrl());
        SimpleRequest.Builder request = new SimpleRequest.Builder(e.getRequest().getMethod().toString(), uri.getRawPath())
                .withBody(e.getRequest().getBody());
        e.getRequest().getHeaders().all().forEach(h -> request.withHeader(h.key(), h.values()));
        SimpleResponse.Builder response = new SimpleResponse.Builder(e.getResponse().getStatus()).withBody(e.getResponse().getBody());
        e.getResponse().getHeaders().all().forEach(h -> response.withHeader(h.key(), h.values()));
        return messages(validator.validate(request.build(), response.build())).stream().filter(m -> m.startsWith("validation.response")).toList();
    }

    /** The findings of a schema of the contract on a JSON value. */
    static List<String> schema(String spec, String name, String json) {
        Schema<?> schema = api(spec).getComponents().getSchemas().get(name);
        if (schema == null) {
            throw new IllegalArgumentException(spec + " has no schema " + name);
        }
        return messages(new SchemaValidator(api(spec), new MessageResolver(levels(spec))).validate(json, schema, "body"));
    }

    private static LevelResolver levels(String spec) {
        LevelResolver.Builder b = LevelResolver.create();
        if (spec.startsWith("ollama")) {
            b.withLevel("validation.schema.additionalProperties", ValidationReport.Level.IGNORE)
                    .withLevel("validation.response.body.schema.additionalProperties", ValidationReport.Level.IGNORE);
        }
        return b.build();
    }

    /**
     * The findings of the schema of a Responses stream event on it: the schema of its type, as
     * the contract's ResponseStreamEvent lists them.
     */
    static List<String> responseEvent(String spec, String json) {
        String type = String.valueOf(com.github.tomakehurst.wiremock.common.Json.read(json, Map.class).get("type"));
        for (Object o : api(spec).getComponents().getSchemas().get("ResponseStreamEvent").getAnyOf()) {
            String ref = ((Schema<?>) o).get$ref();
            String name = ref.substring(ref.lastIndexOf('/') + 1);
            Schema<?> event = api(spec).getComponents().getSchemas().get(name);
            Schema<?> t = event.getProperties() == null ? null : event.getProperties().get("type");
            if (t != null && t.getEnum() != null && t.getEnum().contains(type)) {
                return schema(spec, name, json);
            }
        }
        return List.of("no event of the contract has the type " + type);
    }

    /**
     * Merges the schemas made of others ({@code allOf}) into one: the validator checks each part
     * on its own, and a part refuses the properties of the others. (The parser can merge them
     * while it resolves every reference, which OpenAI's document is too large for.)
     */
    @SuppressWarnings({"rawtypes", "unchecked"})
    private static void merge(OpenAPI api) {
        Map<String, Schema> schemas = api.getComponents().getSchemas();
        for (String name : List.copyOf(schemas.keySet())) {
            merged(schemas, schemas.get(name));
        }
        java.util.Set<Schema> seen = java.util.Collections.newSetFromMap(new java.util.IdentityHashMap<>());
        for (Schema schema : schemas.values()) {
            map(schemas, schema, seen);
        }
    }

    /**
     * Gives the discriminators that name no schemas their mapping: the value of each alternative's
     * property. Most of OpenAI's discriminators have none, and their values ({@code "function"})
     * name no schema, so without it the validator matches no alternative, for any answer.
     */
    @SuppressWarnings({"rawtypes", "unchecked"})
    private static void map(Map<String, Schema> schemas, Schema s, java.util.Set<Schema> seen) {
        if (s == null || !seen.add(s)) {
            return;
        }
        List<Object> alternatives = s.getOneOf() != null ? s.getOneOf() : s.getAnyOf();
        if (s.getDiscriminator() != null && s.getDiscriminator().getMapping() == null && alternatives != null) {
            String property = s.getDiscriminator().getPropertyName();
            for (Object o : alternatives) {
                String ref = ((Schema) o).get$ref();
                Schema alternative = ref == null ? null : schemas.get(ref.substring(ref.lastIndexOf('/') + 1));
                Schema p = alternative == null || alternative.getProperties() == null ? null : (Schema) alternative.getProperties().get(property);
                if (p != null && p.getEnum() != null && p.getEnum().size() == 1) {
                    s.getDiscriminator().mapping(String.valueOf(p.getEnum().get(0)), ref);
                } else if (p != null && p.getConst() != null) {
                    s.getDiscriminator().mapping(String.valueOf(p.getConst()), ref);
                }
            }
        }
        if (s.getProperties() != null) {
            s.getProperties().values().forEach(v -> map(schemas, (Schema) v, seen));
        }
        map(schemas, s.getItems(), seen);
        if (s.getAdditionalProperties() instanceof Schema a) {
            map(schemas, a, seen);
        }
        for (List<Object> list : java.util.Arrays.asList(s.getOneOf(), s.getAnyOf(), s.getAllOf())) {
            if (list != null) {
                list.forEach(v -> map(schemas, (Schema) v, seen));
            }
        }
    }

    @SuppressWarnings({"rawtypes", "unchecked"})
    private static Schema merged(Map<String, Schema> schemas, Schema s) {
        if (s.get$ref() != null) {
            return merged(schemas, schemas.get(s.get$ref().substring(s.get$ref().lastIndexOf('/') + 1)));
        }
        if (s.getAllOf() == null) {
            return s;
        }
        List<Object> parts = s.getAllOf();
        s.setAllOf(null);
        for (Object o : parts) {
            Schema part = merged(schemas, (Schema) o);
            if (part.getProperties() != null) {
                part.getProperties().forEach((k, v) -> s.addProperty((String) k, (Schema) v));
            }
            if (part.getRequired() != null) {
                for (Object r : part.getRequired()) {
                    if (s.getRequired() == null || !s.getRequired().contains(r)) {
                        s.addRequiredItem((String) r);
                    }
                }
            }
            if (s.getType() == null && part.getType() != null) {
                s.setType(part.getType());
            }
            if (s.getTypes() == null && part.getTypes() != null) {
                s.setTypes(part.getTypes());
            }
        }
        return s;
    }

    private static List<String> messages(ValidationReport report) {
        List<String> out = new ArrayList<>();
        for (ValidationReport.Message m : report.getMessages()) {
            if (m.getLevel() == ValidationReport.Level.ERROR) {
                out.add(m.getKey() + ": " + m.getMessage() + (m.getNestedMessages().isEmpty() ? "" : " " + m.getNestedMessages().stream()
                        .map(n -> n.getKey() + ": " + n.getMessage()).toList()));
            }
        }
        return out;
    }

    /** The data of the events of a server-sent event stream, [DONE] left out. */
    static List<String> sseData(String body) {
        List<String> out = new ArrayList<>();
        for (String line : body.split("\n")) {
            if (line.startsWith("data: ") && !line.equals("data: [DONE]")) {
                out.add(line.substring(6));
            }
        }
        return out;
    }
}
