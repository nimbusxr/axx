// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import static com.github.tomakehurst.wiremock.client.WireMock.created;
import static com.github.tomakehurst.wiremock.client.WireMock.get;
import static com.github.tomakehurst.wiremock.client.WireMock.okJson;
import static com.github.tomakehurst.wiremock.client.WireMock.post;
import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Metadata;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.net.http.HttpResponse;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.attribute.FileTime;
import java.time.Instant;
import java.util.List;
import java.util.Map;

/** The journal record, the modes, the levels, the default spec and the admin endpoints. */
class FindingsTest {
    static final String SPEC = OpenApiValidatorExtensionTest.JSON_OPENAPI_FILE_PATH;
    static final String VALID_USER = OpenApiValidatorExtensionTest.VALID_USER;

    @TempDir Path dir;

    private WireMockServer wm;

    @AfterEach
    void tearDown() {
        if (wm != null) {
            wm.stop();
        }
    }

    private void start(Map<String, String> env) {
        wm =
                new WireMockServer(
                        options()
                                .dynamicPort()
                                .extensions(new OpenApiValidatorExtension(Settings.fromEnv(env))));
        wm.start();
    }

    private HttpResponse<String> postUser(String body) {
        return Http.request(wm.port(), "POST", "/users")
                .header("Content-Type", "application/json")
                .body(body)
                .send();
    }

    private static Metadata spec(String path) {
        return Metadata.metadata().attr("openApiSpecSource", path).build();
    }

    /** The findings record of the only journaled request, as axx reads it over the admin API. */
    @SuppressWarnings("unchecked")
    private Map<String, Object> journalRecord() {
        Map<String, Object> journal =
                ProblemBody.read(Http.request(wm.port(), "GET", "/__admin/requests").send().body());
        List<Map<String, Object>> requests = (List<Map<String, Object>>) journal.get("requests");
        assertThat(requests).hasSize(1);
        List<Map<String, Object>> subEvents =
                (List<Map<String, Object>>) requests.get(0).get("subEvents");
        return subEvents.stream()
                .filter(e -> OpenApiValidatorExtension.SUB_EVENT.equals(e.get("type")))
                .map(e -> (Map<String, Object>) e.get("data"))
                .findFirst()
                .orElseThrow(() -> new AssertionError("no " + OpenApiValidatorExtension.SUB_EVENT + " sub-event: " + subEvents));
    }

    @SuppressWarnings("unchecked")
    private static List<Map<String, Object>> findings(Map<String, Object> record) {
        return (List<Map<String, Object>>) record.get("findings");
    }

    @Test
    void failModeAnswersProblemJsonAndRecordsTheFindings() {
        start(Map.of());
        wm.stubFor(post("/users").withMetadata(spec(SPEC)).willReturn(created()));

        HttpResponse<String> response = postUser("{}");

        assertThat(response.statusCode()).isEqualTo(500);
        assertThat(response.headers().firstValue("Content-Type")).contains("application/problem+json");
        Map<String, Object> problem = ProblemBody.read(response.body());
        assertThat(problem).containsEntry("title", "OpenAPI validation failed").containsEntry("spec", SPEC);
        assertThat((String) problem.get("detail")).isEqualTo("POST /users does not match " + SPEC + ": 3 errors");

        Map<String, Object> record = journalRecord();
        assertThat(record)
                .containsEntry("format", OpenApiValidatorExtension.FORMAT)
                .containsEntry("spec", SPEC)
                .containsEntry("mode", "fail");
        assertThat(findings(record))
                .extracting(f -> f.get("key"), f -> f.get("level"), f -> f.get("side"))
                .containsOnly(
                        org.assertj.core.groups.Tuple.tuple(
                                "validation.request.body.schema.required", "ERROR", "request"));
        assertThat(findings(record)).hasSize(3);
    }

    @Test
    void reportModeServesTheStubAndRecordsTheFindings() {
        start(Map.of("OPENAPI_VALIDATION_MODE", "report"));
        wm.stubFor(post("/users").withMetadata(spec(SPEC)).willReturn(created()));

        assertThat(postUser("{}").statusCode()).isEqualTo(201);

        Map<String, Object> record = journalRecord();
        assertThat(record).containsEntry("mode", "report");
        assertThat(findings(record)).hasSize(3).allMatch(f -> "ERROR".equals(f.get("level")));
    }

    @Test
    void aValidExchangeRecordsNoFindings() {
        start(Map.of());
        wm.stubFor(post("/users").withMetadata(spec(SPEC)).willReturn(created()));

        assertThat(postUser(VALID_USER).statusCode()).isEqualTo(201);

        assertThat(findings(journalRecord())).isEmpty();
    }

    @Test
    void stubModeOverridesTheDefault() {
        start(Map.of("OPENAPI_VALIDATION_MODE", "fail"));
        wm.stubFor(
                post("/users")
                        .withMetadata(
                                Metadata.metadata()
                                        .attr("openApiSpecSource", SPEC)
                                        .attr("openApiValidationMode", "report")
                                        .build())
                        .willReturn(created()));

        assertThat(postUser("{}").statusCode()).isEqualTo(201);
        assertThat(journalRecord()).containsEntry("mode", "report");
    }

    @Test
    void defaultLevelsRelaxFindingsButKeepThemRecorded() {
        start(Map.of("OPENAPI_VALIDATION_LEVELS", "validation.request.body=WARN"));
        wm.stubFor(post("/users").withMetadata(spec(SPEC)).willReturn(created()));

        assertThat(postUser("{}").statusCode()).isEqualTo(201);
        assertThat(findings(journalRecord())).hasSize(3).allMatch(f -> "WARN".equals(f.get("level")));
    }

    @Test
    void stubLevelsTakePrecedenceOverDefaultLevels() {
        start(Map.of("OPENAPI_VALIDATION_LEVELS", "validation.request.body.schema.required=IGNORE"));
        wm.stubFor(
                post("/users")
                        .withMetadata(
                                Metadata.metadata()
                                        .attr("openApiSpecSource", SPEC)
                                        .attr(
                                                "openApiValidationLevels",
                                                Map.of("validation.request.body", "ERROR"))
                                        .build())
                        .willReturn(created()));

        // The stub's broader key wins over the more specific default: stub settings are closer
        // to the exchange.
        assertThat(postUser("{}").statusCode()).isEqualTo(500);
        assertThat(findings(journalRecord())).allMatch(f -> "ERROR".equals(f.get("level")));
    }

    @Test
    void invalidStubLevelsAreReportedAsAFinding() {
        start(Map.of());
        wm.stubFor(
                post("/users")
                        .withMetadata(
                                Metadata.metadata()
                                        .attr("openApiSpecSource", SPEC)
                                        .attr("openApiValidationLevels", Map.of("validation.request", "LOUD"))
                                        .build())
                        .willReturn(created()));

        HttpResponse<String> response = postUser(VALID_USER);

        assertThat(response.statusCode()).isEqualTo(500);
        assertThat(OpenApiValidatorExtensionTest.findingLines(response.body()))
                .contains("validation.spec (spec)", "invalid level \"LOUD\"");
    }

    @Test
    void theDefaultSpecAppliesUnlessAStubOptsOut() {
        start(Map.of("OPENAPI_SPEC_SOURCE", SPEC));
        wm.stubFor(post("/users").willReturn(created()));
        wm.stubFor(
                post("/opted-out")
                        .withMetadata(Metadata.metadata().attr("openApiValidation", false).build())
                        .willReturn(created()));

        assertThat(postUser("{}").statusCode()).isEqualTo(500);
        assertThat(
                        Http.request(wm.port(), "POST", "/opted-out")
                                .header("Content-Type", "application/json")
                                .body("{}")
                                .send()
                                .statusCode())
                .isEqualTo(201);
    }

    @Test
    void adminInfoReportsVersionFormatAndSettings() {
        start(Map.of("OPENAPI_VALIDATION_MODE", "report", "OPENAPI_VALIDATION_LEVELS", "validation.response=WARN"));

        HttpResponse<String> response = Http.request(wm.port(), "GET", "/__admin/openapi-validation").send();

        assertThat(response.statusCode()).isEqualTo(200);
        Map<String, Object> info = ProblemBody.read(response.body());
        assertThat(info)
                .containsEntry("name", "axx-wiremock-openapi")
                .containsEntry("format", OpenApiValidatorExtension.FORMAT)
                .containsEntry("mode", "report")
                .containsKey("version");
        assertThat(info.get("levels"))
                .isEqualTo(
                        Map.of(
                                "validation.request.parameter.query.unexpected", "IGNORE",
                                "validation.response", "WARN"));
    }

    @Test
    void aChangedSpecFileIsLoadedAgain() throws IOException {
        Path spec = dir.resolve("users.json");
        Files.copy(Path.of(SPEC), spec);
        start(Map.of());
        wm.stubFor(get("/users").withMetadata(spec(spec.toString())).willReturn(okJson("[{}]")));
        assertThat(Http.request(wm.port(), "GET", "/users").header("x-correlation-id", OpenApiValidatorExtensionTest.CORRELATION_ID).send().statusCode())
                .isEqualTo(500);

        // Loosen the spec: a user no longer requires any property.
        Files.writeString(
                spec,
                Files.readString(spec).replace("\"required\": [\"id\", \"username\", \"role\"]", "\"required\": []"));
        Files.setLastModifiedTime(spec, FileTime.from(Instant.now().plusSeconds(5)));

        HttpResponse<String> loosened =
                Http.request(wm.port(), "GET", "/users").header("x-correlation-id", OpenApiValidatorExtensionTest.CORRELATION_ID).send();
        assertThat(loosened.statusCode()).as(loosened.body()).isEqualTo(200);
    }

    @Test
    void resetForgetsCachedSpecs() {
        start(Map.of());
        wm.stubFor(post("/users").withMetadata(spec(SPEC)).willReturn(created()));
        postUser(VALID_USER);

        HttpResponse<String> response = Http.request(wm.port(), "POST", "/__admin/openapi-validation/reset").send();

        assertThat(response.statusCode()).isEqualTo(200);
        assertThat(ProblemBody.read(response.body())).containsEntry("cleared", 1);
    }
}
