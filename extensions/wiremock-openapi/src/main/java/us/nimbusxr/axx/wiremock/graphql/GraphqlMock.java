// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.graphql;

import static com.github.tomakehurst.wiremock.client.WireMock.urlPathEqualTo;
import static com.github.tomakehurst.wiremock.matching.RequestPatternBuilder.newRequestPattern;

import com.github.tomakehurst.wiremock.client.ResponseDefinitionBuilder;
import com.github.tomakehurst.wiremock.common.FileSource;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.common.Metadata;
import com.github.tomakehurst.wiremock.extension.MappingsLoaderExtension;
import com.github.tomakehurst.wiremock.extension.ResponseTransformerV2;
import com.github.tomakehurst.wiremock.http.HttpHeader;
import com.github.tomakehurst.wiremock.http.HttpHeaders;
import com.github.tomakehurst.wiremock.http.ImmutableRequest;
import com.github.tomakehurst.wiremock.http.Request;
import com.github.tomakehurst.wiremock.http.RequestMethod;
import com.github.tomakehurst.wiremock.http.Response;
import com.github.tomakehurst.wiremock.http.ResponseDefinition;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;
import com.github.tomakehurst.wiremock.stubbing.StubMappings;

import graphql.ExecutionInput;
import graphql.ExecutionResult;
import graphql.GraphQL;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;

/**
 * A GraphQL service, or a federated subgraph, mocked field by field from its schema.
 *
 * <p>It answers operations on {@code POST <path>} by running them against the schema ({@code
 * GRAPHQL_SCHEMA_SOURCE}), with graphql-java. A field's value comes from its parent's value when
 * that has it, and otherwise from a stub: an ordinary WireMock stub of a POST to {@code
 * <path>/<Type>/<field>}, whose body is {@code {"arguments": {...}, "source": {...}}}. An entity
 * of a subgraph ({@code _entities}) comes from a stub of a POST to {@code
 * <path>/_entities/<Type>}, whose body is the entity's representation. A stub's JSON body is the
 * value; a {@code graphql-error} header makes the field an error instead, with the {@code
 * graphql-error-code} header as its {@code extensions.code}. A field without a value is null.
 *
 * <p>Stubs are matched in-process with WireMock's own matchers and priorities, and those lookups
 * are not requests: the journal records the operations the mock received, as it records any
 * request. The endpoint is a stub of its own, at a low priority, which a stub of a whole
 * operation on the same path overrides.
 */
public class GraphqlMock implements MappingsLoaderExtension, ResponseTransformerV2 {
    static final String NAME = "graphql";
    /** Metadata that marks the endpoint's own stub, which field lookups skip. */
    static final String ENDPOINT_KEY = "axxGraphqlEndpoint";

    static final String ERROR_HEADER = "graphql-error";
    static final String ERROR_CODE_HEADER = "graphql-error-code";

    private static final Logger log = LoggerFactory.getLogger(GraphqlMock.class);

    private final GraphqlSettings settings;
    private final FileSource files;
    private volatile StubMappings stubs;
    private volatile GraphQL graphql;

    GraphqlMock(GraphqlSettings settings, FileSource files) {
        this.settings = settings;
        this.files = files;
    }

    @Override
    public String getName() {
        return NAME;
    }

    @Override
    public boolean applyGlobally() {
        return false;
    }

    /** Adds the endpoint's stub, at startup and whenever WireMock resets to its mappings. */
    @Override
    public void loadMappingsInto(StubMappings stubMappings) {
        this.stubs = stubMappings;
        StubMapping endpoint =
                new StubMapping(
                        newRequestPattern(RequestMethod.POST, urlPathEqualTo(settings.path())).build(),
                        ResponseDefinitionBuilder.responseDefinition()
                                .withStatus(200)
                                .withTransformers(NAME)
                                .build());
        endpoint.setId(UUID.nameUUIDFromBytes(("axx-graphql:" + settings.path()).getBytes(StandardCharsets.UTF_8)));
        endpoint.setName("GraphQL: " + settings.schemaSource());
        endpoint.setPriority(10);
        endpoint.setMetadata(Metadata.metadata().attr(ENDPOINT_KEY, true).build());
        stubMappings.addMapping(endpoint);
    }

    @Override
    public Response transform(Response response, ServeEvent serveEvent) {
        Map<String, Object> result;
        try {
            result = execute(serveEvent.getRequest());
        } catch (BadRequest e) {
            return json(response, 400, Map.of("errors", List.of(Map.of("message", e.getMessage()))));
        } catch (RuntimeException | IOException e) {
            log.error("the GraphQL mock of {} failed", settings.schemaSource(), e);
            return json(response, 500, Map.of("errors", List.of(Map.of("message", "the GraphQL mock of " + settings.schemaSource() + " failed: " + e.getMessage()))));
        }
        return json(response, 200, result);
    }

    @SuppressWarnings("unchecked")
    private Map<String, Object> execute(Request request) throws IOException {
        Map<String, Object> body;
        try {
            body = Json.read(request.getBodyAsString(), Map.class);
        } catch (RuntimeException e) {
            throw new BadRequest("the request is not a GraphQL request: its body is not JSON");
        }
        if (body == null || !(body.get("query") instanceof String query)) {
            throw new BadRequest("the request is not a GraphQL request: it has no query");
        }
        ExecutionInput.Builder input = ExecutionInput.newExecutionInput().query(query);
        if (body.get("variables") instanceof Map<?, ?> variables) {
            input.variables((Map<String, Object>) variables);
        }
        if (body.get("operationName") instanceof String operation) {
            input.operationName(operation);
        }
        ExecutionResult result = graphql().execute(input.build());
        return result.toSpecification();
    }

    private GraphQL graphql() throws IOException {
        GraphQL g = graphql;
        if (g == null) {
            synchronized (this) {
                if (graphql == null) {
                    String sdl = Files.readString(Path.of(settings.schemaSource()), StandardCharsets.UTF_8);
                    graphql = GraphQL.newGraphQL(StubbedSchema.build(sdl, this)).build();
                }
                g = graphql;
            }
        }
        return g;
    }

    /**
     * The stub of a field or an entity: the first, by WireMock's order, whose request matches a
     * POST of body to path.
     */
    Optional<ResponseDefinition> lookup(String path, Object body) {
        StubMappings all = stubs;
        if (all == null) {
            return Optional.empty();
        }
        Request probe =
                new ImmutableRequest.Builder()
                        .withAbsoluteUrl("http://localhost" + path)
                        .withMethod(RequestMethod.POST)
                        .withHeader("Content-Type", "application/json")
                        .withBody(Json.write(body).getBytes(StandardCharsets.UTF_8))
                        .build();
        for (StubMapping m : all.getAll()) {
            Metadata md = m.getMetadata();
            if (md != null && md.containsKey(ENDPOINT_KEY)) {
                continue;
            }
            if (m.getRequest().match(probe).isExactMatch()) {
                return Optional.of(m.getResponse());
            }
        }
        return Optional.empty();
    }

    /** The field path of a type's field, or of an entity type, that stubs are posted to. */
    String fieldPath(String type, String field) {
        return settings.path() + "/" + type + "/" + field;
    }

    String entityPath(String type) {
        return settings.path() + "/_entities/" + type;
    }

    /** A stub's value: its body ({@code jsonBody} or {@code body}), or its file's. */
    Object value(ResponseDefinition stub) throws IOException {
        String text = stub.getJsonBody() != null ? stub.getJsonBody().toString() : stub.getBody();
        if (text == null && stub.getBodyFileName() != null) {
            text = new String(files.child("__files").getBinaryFileNamed(stub.getBodyFileName()).readContents(), StandardCharsets.UTF_8);
        }
        if (text == null || text.isBlank()) {
            return null;
        }
        return Json.read(text, Object.class);
    }

    static Optional<StubError> error(ResponseDefinition stub) {
        HttpHeaders headers = stub.getHeaders();
        if (headers == null || !headers.getHeader(ERROR_HEADER).isPresent()) {
            return Optional.empty();
        }
        HttpHeader code = headers.getHeader(ERROR_CODE_HEADER);
        return Optional.of(new StubError(headers.getHeader(ERROR_HEADER).firstValue(), code.isPresent() ? code.firstValue() : null));
    }

    /** An error a stub answers for its field. */
    record StubError(String message, String code) {}

    private static Response json(Response response, int status, Object body) {
        return Response.Builder.like(response)
                .but()
                .status(status)
                .headers(new HttpHeaders(new HttpHeader("Content-Type", "application/json")))
                .body(JsonText.write(body))
                .build();
    }

    private static final class BadRequest extends RuntimeException {
        BadRequest(String message) {
            super(message);
        }
    }
}
