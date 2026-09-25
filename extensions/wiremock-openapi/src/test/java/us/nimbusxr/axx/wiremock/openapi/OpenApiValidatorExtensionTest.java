// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import static com.github.tomakehurst.wiremock.client.WireMock.aResponse;
import static com.github.tomakehurst.wiremock.client.WireMock.created;
import static com.github.tomakehurst.wiremock.client.WireMock.delete;
import static com.github.tomakehurst.wiremock.client.WireMock.equalTo;
import static com.github.tomakehurst.wiremock.client.WireMock.get;
import static com.github.tomakehurst.wiremock.client.WireMock.noContent;
import static com.github.tomakehurst.wiremock.client.WireMock.okJson;
import static com.github.tomakehurst.wiremock.client.WireMock.post;
import static com.github.tomakehurst.wiremock.client.WireMock.urlPathEqualTo;
import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Metadata;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.net.http.HttpResponse;
import java.util.stream.Collectors;

class OpenApiValidatorExtensionTest {
    static final String GET_USERS_URL = "/users";
    static final String ADD_USER_URL = "/users";
    static final String DELETE_USER_URL = "/users/123";
    static final String JSON_OPENAPI_FILE_PATH = "src/test/resources/openapi/users_v1.json";
    static final String OPENAPI_31_FILE_PATH = "src/test/resources/openapi/users_v31.json";
    static final String OPENAPI_31_LEGACY_NULLABLE_FILE_PATH =
            "src/test/resources/openapi/users_v31_legacy_nullable.json";
    static final String OPENAPI_301_NULLABLE_FILE_PATH =
            "src/test/resources/openapi/users_v301_nullable.json";
    static final String YAML_OPENAPI_FILE_PATH = "src/test/resources/openapi/users_v2.yaml";
    static final String INVALID_OPENAPI_FILE_PATH = "src/test/resources/openapi/invalid.json";

    static final String CORRELATION_ID = "1b57b0db-bde2-4c5f-83bf-cf1e47ce5639";
    static final String VALID_USER =
            """
            {"id":"72c1004d-716a-4b89-b77d-a3cb53d17eb7","role":"admin","username":"root"}""";

    private final Metadata metadata =
            Metadata.metadata().attr("openApiSpecSource", JSON_OPENAPI_FILE_PATH).build();

    private WireMockServer wm;

    @BeforeEach
    void setUp() {
        // A dynamic port: a fixed one collides with anything else listening locally.
        wm =
                new WireMockServer(
                        options().dynamicPort().extensions(new OpenApiValidatorExtension()));
        wm.start();
    }

    @AfterEach
    void tearDown() {
        wm.stop();
    }

    private Http request(String method, String path) {
        return Http.request(wm.port(), method, path);
    }

    private static void assertStatus(HttpResponse<String> response, int status) {
        assertThat(response.statusCode())
                .as("status (body: %s)", response.body())
                .isEqualTo(status);
    }

    /** The findings of a problem+json body, one "key (side): message" line each. */
    static String findingLines(String body) {
        return ProblemBody.findings(body).stream()
                .map(f -> f.get("key") + " (" + f.get("side") + "): " + f.get("message"))
                .collect(Collectors.joining("\n"));
    }

    private static Metadata specSource(String path) {
        return Metadata.metadata().attr("openApiSpecSource", path).build();
    }

    /** A /v31/users response whose single user has the given JSON value as nickname. */
    private static String usersWithNickname(String nickname) {
        return """
                [{"id":"u-1","username":"neil","nickname":%s}]"""
                .formatted(nickname);
    }

    /** A users_v1.json user that violates every constraint, plus the given extra JSON fields. */
    private static String invalidUser(String extraFields) {
        return """
                {"id":"test","role":"unknown","username":"toolongusername","name":"x",\
                "dob":"invalid date string"%s}"""
                .formatted(extraFields);
    }

    private void stubV31Users(String specPath, String nickname) {
        wm.stubFor(
                get("/v31/users")
                        .withMetadata(specSource(specPath))
                        .willReturn(okJson(usersWithNickname(nickname))));
    }

    @Test
    void testOpenApi31UnionTypeAcceptsNull() {
        // The 2.x validator checked only the first type of a union, so a null value for
        // "type": ["string", "null"] failed; OpenAPI 3.1 specs used to need rewriting to 3.0.1.
        stubV31Users(OPENAPI_31_FILE_PATH, "null");

        assertStatus(request("GET", "/v31/users").send(), 200);
    }

    @Test
    void testOpenApi301NullableKeywordAcceptsNull() {
        // The dialect differential to the 3.1 test below: 'nullable' IS a valid OpenAPI 3.0
        // keyword, so the same document under openapi: 3.0.1 accepts null values.
        stubV31Users(OPENAPI_301_NULLABLE_FILE_PATH, "null");

        assertStatus(request("GET", "/v31/users").send(), 200);
    }

    @Test
    void testOpenApi31LegacyNullableKeywordRejectsNull() {
        // OpenAPI 3.1 removed the 3.0 'nullable' keyword; JSON Schema 2020-12 ignores it,
        // so a 3.1 document relying on 'nullable: true' does NOT permit null values.
        // The 2.x validator tolerated this spec bug; 3.x reports it. Consumers hitting
        // this must convert to union types: "type": ["string", "null"].
        stubV31Users(OPENAPI_31_LEGACY_NULLABLE_FILE_PATH, "null");

        assertStatus(request("GET", "/v31/users").send(), 500);
    }

    @Test
    void testOpenApi31UnionTypeAcceptsString() {
        stubV31Users(OPENAPI_31_FILE_PATH, "\"buzz\"");

        assertStatus(request("GET", "/v31/users").send(), 200);
    }

    @Test
    void testOpenApi31UnionTypeRejectsWrongType() {
        stubV31Users(OPENAPI_31_FILE_PATH, "42");

        assertStatus(request("GET", "/v31/users").send(), 500);
    }

    @Test
    void testRequestBodyHasRequiredProperties() {
        wm.stubFor(post(ADD_USER_URL).withMetadata(metadata).willReturn(created()));

        HttpResponse<String> response =
                request("POST", ADD_USER_URL)
                        .header("Content-Type", "application/json")
                        .body(VALID_USER)
                        .send();

        assertStatus(response, 201);
    }

    @Test
    void testRequestBodyMissesRequiredProperties() {
        wm.stubFor(post(ADD_USER_URL).withMetadata(metadata).willReturn(created()));

        HttpResponse<String> response =
                request("POST", ADD_USER_URL)
                        .header("Content-Type", "application/json")
                        .body("{}")
                        .send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "required property 'id' not found",
                        "required property 'role' not found",
                        "required property 'username' not found");
    }

    @Test
    void testRequestBodyHasInvalidProperties() {
        wm.stubFor(post(ADD_USER_URL).withMetadata(metadata).willReturn(created()));

        HttpResponse<String> response =
                request("POST", ADD_USER_URL)
                        .header("Content-Type", "application/json")
                        .body(invalidUser(",\"extra\":\"extra\""))
                        .send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "does not match the uuid pattern",
                        "must be at most 10 characters long",
                        "must be at least 2 characters long",
                        "does not match the date pattern",
                        "does not have a value in the enumeration [\"user\", \"admin\"]",
                        "property 'extra' is not defined in the schema and the schema does not"
                                + " allow additional properties");
    }

    @Test
    void testRequestHasRequiredQueryStringParameter() {
        wm.stubFor(
                delete(urlPathEqualTo(DELETE_USER_URL))
                        .withMetadata(metadata)
                        .withQueryParam("soft", equalTo("true"))
                        .willReturn(noContent()));

        assertStatus(request("DELETE", DELETE_USER_URL + "?soft=true").send(), 204);
    }

    @Test
    void testRequestHasInvalidQueryStringParameter() {
        wm.stubFor(
                delete(urlPathEqualTo(DELETE_USER_URL))
                        .withMetadata(metadata)
                        .willReturn(noContent()));

        HttpResponse<String> response = request("DELETE", DELETE_USER_URL).send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "validation.request.parameter.query.missing (request): Query parameter"
                                + " 'soft' is required on path '/users/{userId}' but not found in"
                                + " request.");
    }

    @Test
    void testResponseHasRequiredProperties() {
        wm.stubFor(
                get(GET_USERS_URL)
                        .withMetadata(metadata)
                        .willReturn(okJson("[" + VALID_USER + "]")));

        HttpResponse<String> response =
                request("GET", GET_USERS_URL).header("x-correlation-id", CORRELATION_ID).send();

        assertStatus(response, 200);
    }

    @Test
    void testResponseMissesRequiredProperties() {
        wm.stubFor(get(GET_USERS_URL).withMetadata(metadata).willReturn(okJson("[{}]")));

        HttpResponse<String> response =
                request("GET", GET_USERS_URL).header("x-correlation-id", CORRELATION_ID).send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "required property 'id' not found",
                        "required property 'role' not found",
                        "required property 'username' not found");
    }

    @Test
    void testResponseHasInvalidProperties() {
        wm.stubFor(
                get(GET_USERS_URL)
                        .withMetadata(metadata)
                        .willReturn(okJson("[" + invalidUser("") + "]")));

        HttpResponse<String> response =
                request("GET", GET_USERS_URL).header("x-correlation-id", CORRELATION_ID).send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "does not match the uuid pattern",
                        "must be at most 10 characters long",
                        "must be at least 2 characters long",
                        "does not match the date pattern",
                        "does not have a value in the enumeration [\"user\", \"admin\"]");
    }

    @Test
    void testResponseHasInvalidStatusCode() {
        wm.stubFor(
                get(GET_USERS_URL).withMetadata(metadata).willReturn(aResponse().withStatus(404)));

        HttpResponse<String> response =
                request("GET", GET_USERS_URL).header("x-correlation-id", CORRELATION_ID).send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "validation.response.status.unknown (response): Response status 404 not"
                                + " defined for path '/users'.");
    }

    @Test
    void testUnknownPath() {
        wm.stubFor(get(urlPathEqualTo("/unknown")).withMetadata(metadata).willReturn(noContent()));

        HttpResponse<String> response = request("GET", "/unknown").send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "validation.request.path.missing (request): No API path found that"
                                + " matches request '/unknown'.");
    }

    @Test
    void testUnmatchedResponse() {
        wm.stubFor(get("/test").withMetadata(metadata).willReturn(noContent()));

        assertStatus(request("GET", "/not-test").send(), 404);
    }

    @Test
    void testInvalidOpenApiFileThrowsException() {
        wm.stubFor(
                post(ADD_USER_URL)
                        .withMetadata(specSource(INVALID_OPENAPI_FILE_PATH))
                        .willReturn(created()));

        HttpResponse<String> response =
                request("POST", ADD_USER_URL)
                        .header("Content-Type", "application/json")
                        .body("{}")
                        .send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains("Unable to load API spec from provided URL or payload");
    }

    @Test
    void testRequestBodyMissesRequiredPropertiesWhenUsingYamlOpenapiFile() {
        wm.stubFor(
                post(ADD_USER_URL)
                        .withMetadata(specSource(YAML_OPENAPI_FILE_PATH))
                        .willReturn(created()));

        HttpResponse<String> response =
                request("POST", ADD_USER_URL)
                        .header("Content-Type", "application/json")
                        .body("{}")
                        .send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body())).contains("required property");
    }

    @Test
    void testRequestWithContentTypeText() {
        wm.stubFor(post(ADD_USER_URL).withMetadata(metadata).willReturn(created()));

        HttpResponse<String> response =
                request("POST", ADD_USER_URL)
                        .header("Content-Type", "text/plain; charset=ISO-8859-1")
                        .body(VALID_USER)
                        .send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "Request Content-Type header '[text/plain; charset=ISO-8859-1]'"
                                + " does not match any allowed types. Must be one of:"
                                + " [application/json].");
    }

    @Test
    void testResponseWithContentTypeText() {
        wm.stubFor(
                get(GET_USERS_URL)
                        .withMetadata(metadata)
                        .willReturn(
                                aResponse()
                                        .withStatus(200)
                                        .withHeader("Content-Type", "text/plain")
                                        .withBody("[" + VALID_USER + "]")));

        HttpResponse<String> response =
                request("GET", GET_USERS_URL).header("x-correlation-id", CORRELATION_ID).send();

        assertStatus(response, 500);
        assertThat(findingLines(response.body()))
                .contains(
                        "Response Content-Type header 'text/plain' does not match any"
                                + " allowed types. Must be one of: [application/json].");
    }

    @Test
    void testStubWithoutSpecSourceIsNotValidated() {
        wm.stubFor(post("/no-metadata").willReturn(created()));
        wm.stubFor(
                post("/other-metadata")
                        .withMetadata(Metadata.metadata().attr("owner", "tests").build())
                        .willReturn(created()));

        assertStatus(request("POST", "/no-metadata").body("not json").send(), 201);
        assertStatus(request("POST", "/other-metadata").body("not json").send(), 201);
    }
}
