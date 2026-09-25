// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import static com.github.tomakehurst.wiremock.client.WireMock.aResponse;
import static com.github.tomakehurst.wiremock.client.WireMock.any;
import static com.github.tomakehurst.wiremock.client.WireMock.urlEqualTo;
import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.client.ResponseDefinitionBuilder;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;

import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.MethodSource;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.TreeSet;

/**
 * Checks that this extension reports the keys the axx REST steps report for the same exchanges
 * (packs/rest's TestValidatorsAgree reads the same cases), so one vocabulary of keys and levels
 * means the same on both sides of a contract.
 */
class FindingsAgreementTest {
    static final Path DIR = Path.of("../../testdata/openapi-agreement");

    static WireMockServer wm;

    @BeforeAll
    static void start() {
        Settings settings =
                Settings.fromEnv(
                        Map.of(
                                "OPENAPI_VALIDATION_MODE", "report",
                                "OPENAPI_SPEC_SOURCE", DIR.resolve("spec.yaml").toString()));
        wm = new WireMockServer(options().dynamicPort().extensions(new OpenApiValidatorExtension(settings)));
        wm.start();
    }

    @AfterAll
    static void stop() {
        wm.stop();
    }

    @SuppressWarnings("unchecked")
    static List<Map<String, Object>> cases() throws IOException {
        Map<String, Object> doc = Json.read(Files.readString(DIR.resolve("cases.json")), Map.class);
        return (List<Map<String, Object>>) doc.get("cases");
    }

    /** A case body as sent: a JSON string's text, anything else as JSON. */
    static String body(Object value) {
        if (value == null) {
            return null;
        }
        return value instanceof String s ? s : Json.write(value);
    }

    @ParameterizedTest(name = "{0}")
    @MethodSource("namedCases")
    @SuppressWarnings("unchecked")
    void validatorsAgree(String name, Map<String, Object> c) {
        Map<String, Object> request = (Map<String, Object>) c.get("request");
        Map<String, Object> response = (Map<String, Object>) c.get("response");
        wm.resetAll();

        ResponseDefinitionBuilder stub = aResponse().withStatus(((Number) response.get("status")).intValue());
        ((Map<String, String>) response.getOrDefault("headers", Map.of())).forEach(stub::withHeader);
        String responseBody = body(response.get("body"));
        if (responseBody != null) {
            stub.withBody(responseBody);
        }
        String path = (String) request.get("path");
        wm.stubFor(any(urlEqualTo(path)).willReturn(stub));

        Http http = Http.request(wm.port(), (String) request.get("method"), path);
        ((Map<String, String>) request.getOrDefault("headers", Map.of())).forEach(http::header);
        String requestBody = body(request.get("body"));
        if (requestBody != null) {
            http.body(requestBody);
        }
        http.send();

        List<ServeEvent> events = wm.getAllServeEvents();
        assertThat(events).hasSize(1);
        TreeSet<String> keys = new TreeSet<>();
        events.get(0).getSubEvents().stream()
                .filter(e -> OpenApiValidatorExtension.SUB_EVENT.equals(e.getType()))
                .forEach(
                        e ->
                                ((List<Map<String, Object>>) e.getData().get("findings"))
                                        .forEach(f -> keys.add((String) f.get("key"))));
        assertThat(keys).containsExactlyElementsOf(new TreeSet<>((List<String>) c.get("keys")));
    }

    static List<Object[]> namedCases() throws IOException {
        return cases().stream().map(c -> new Object[] {c.get("name"), c}).toList();
    }
}
