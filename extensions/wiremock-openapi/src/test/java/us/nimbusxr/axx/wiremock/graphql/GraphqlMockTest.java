// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.graphql;

import static com.github.tomakehurst.wiremock.client.WireMock.aResponse;
import static com.github.tomakehurst.wiremock.client.WireMock.containing;
import static com.github.tomakehurst.wiremock.client.WireMock.equalToJson;
import static com.github.tomakehurst.wiremock.client.WireMock.matchingJsonPath;
import static com.github.tomakehurst.wiremock.client.WireMock.okJson;
import static com.github.tomakehurst.wiremock.client.WireMock.post;
import static com.github.tomakehurst.wiremock.client.WireMock.urlPathEqualTo;
import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Json;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

/** The GraphQL mock: operations answered field by field from stubs. */
class GraphqlMockTest {
    private static final HttpClient CLIENT = HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();

    private WireMockServer wm;

    @AfterEach
    void tearDown() {
        if (wm != null) {
            wm.stop();
        }
    }

    private void start(String schema) {
        String source = Path.of("src/test/resources/graphql", schema).toAbsolutePath().toString();
        wm = new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort().extensions(new GraphqlExtensionFactory(new GraphqlSettings(source, "/graphql"))));
        wm.start();
    }

    /** Posts a GraphQL request, and returns its status and its body as JSON. */
    @SuppressWarnings("unchecked")
    private Map<String, Object> graphql(int wantStatus, String query, Map<String, Object> variables) throws Exception {
        Map<String, Object> body = variables == null ? Map.of("query", query) : Map.of("query", query, "variables", variables);
        HttpResponse<String> res =
                CLIENT.send(
                        HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + wm.port() + "/graphql"))
                                .header("Content-Type", "application/json")
                                .POST(HttpRequest.BodyPublishers.ofString(Json.write(body)))
                                .build(),
                        HttpResponse.BodyHandlers.ofString());
        assertThat(res.statusCode()).as(res.body()).isEqualTo(wantStatus);
        return res.body().startsWith("{") ? Json.read(res.body(), Map.class) : Map.of();
    }

    private void stubParcel() {
        wm.stubFor(
                post(urlPathEqualTo("/graphql/Query/parcel"))
                        .withRequestBody(equalToJson("{\"arguments\": {\"reference\": \"PX-GQL-7201\"}}", true, true))
                        .willReturn(okJson("{\"reference\": \"PX-GQL-7201\", \"status\": \"IN_TRANSIT\", \"weightGrams\": 1200, \"lastScan\": {\"location\": \"Leipzig\", \"scannedAt\": \"2026-09-28T07:30:00Z\"}}")));
    }

    @Test
    @SuppressWarnings("unchecked")
    void aQueryIsAnsweredFromItsStubs() throws Exception {
        start("parcels.graphql");
        stubParcel();
        Map<String, Object> res =
                graphql(200, "query Parcel($ref: ID!) { parcel(reference: $ref) { reference status weightGrams lastScan { location scannedAt } shop { name } } }",
                        Map.of("ref", "PX-GQL-7201"));
        assertThat(res).doesNotContainKey("errors");
        Map<String, Object> parcel = (Map<String, Object>) ((Map<String, Object>) res.get("data")).get("parcel");
        assertThat(parcel)
                .containsEntry("reference", "PX-GQL-7201")
                .containsEntry("status", "IN_TRANSIT")
                .containsEntry("weightGrams", 1200)
                .containsEntry("lastScan", Map.of("location", "Leipzig", "scannedAt", "2026-09-28T07:30:00Z"))
                .containsEntry("shop", null);
        // Another parcel has no stub: null.
        res = graphql(200, "{ parcel(reference: \"PX-GQL-7299\") { reference } }", null);
        assertThat(res.get("data")).isEqualTo(java.util.Collections.singletonMap("parcel", null));
    }

    @Test
    @SuppressWarnings("unchecked")
    void aFieldTheParentLacksHasAStubOfItsOwn() throws Exception {
        start("parcels.graphql");
        stubParcel();
        wm.stubFor(
                post(urlPathEqualTo("/graphql/Parcel/shop"))
                        .withRequestBody(equalToJson("{\"source\": {\"reference\": \"PX-GQL-7201\"}}", true, true))
                        .willReturn(okJson("{\"id\": \"maple-crafts\", \"name\": \"Maple Home\"}")));
        Map<String, Object> res = graphql(200, "{ parcel(reference: \"PX-GQL-7201\") { shop { id name } } }", null);
        Map<String, Object> parcel = (Map<String, Object>) ((Map<String, Object>) res.get("data")).get("parcel");
        assertThat(parcel.get("shop")).isEqualTo(Map.of("id", "maple-crafts", "name", "Maple Home"));
    }

    @Test
    @SuppressWarnings("unchecked")
    void aStubCanAnswerAnError() throws Exception {
        start("parcels.graphql");
        wm.stubFor(
                post(urlPathEqualTo("/graphql/Mutation/holdParcel"))
                        .withRequestBody(equalToJson("{\"arguments\": {\"reference\": \"PX-GQL-7203\"}}", true, true))
                        .willReturn(aResponse().withHeader("graphql-error", "PX-GQL-7203 is out for delivery").withHeader("graphql-error-code", "NOT_HOLDABLE")));
        Map<String, Object> res = graphql(200, "mutation { holdParcel(reference: \"PX-GQL-7203\", until: \"2026-10-05\") { status } }", null);
        List<Map<String, Object>> errors = (List<Map<String, Object>>) res.get("errors");
        assertThat(errors).hasSize(1);
        assertThat(errors.get(0)).containsEntry("message", "PX-GQL-7203 is out for delivery").containsEntry("path", List.of("holdParcel"));
        assertThat(errors.get(0).get("extensions")).isEqualTo(Map.of("code", "NOT_HOLDABLE"));
        assertThat(res.get("data")).isNull();
    }

    @Test
    @SuppressWarnings("unchecked")
    void theSchemaIsTheContract() throws Exception {
        start("parcels.graphql");
        // An unknown field is a validation error, as a real server answers.
        Map<String, Object> res = graphql(200, "{ parcel(reference: \"PX-GQL-7204\") { signedBy } }", null);
        assertThat(((List<Map<String, Object>>) res.get("errors")).get(0).get("message").toString()).contains("signedBy");
        // A non-null field without a value is an error, which nulls its parent.
        res = graphql(200, "{ parcels(shop: \"maple-crafts\") { reference } }", null);
        assertThat(res.get("data")).isNull();
        assertThat((List<?>) res.get("errors")).isNotEmpty();
        // Introspection works, for clients that read the schema from the mock.
        res = graphql(200, "{ __schema { queryType { name } } }", null);
        assertThat(res.get("data")).isEqualTo(Map.of("__schema", Map.of("queryType", Map.of("name", "Query"))));
        // An abstract type's value says which type it is.
        wm.stubFor(post(urlPathEqualTo("/graphql/Query/node")).willReturn(okJson("{\"__typename\": \"Depot\", \"id\": \"LEJ\", \"city\": \"Leipzig\"}")));
        res = graphql(200, "{ node(id: \"LEJ\") { id ... on Depot { city } } }", null);
        assertThat(res.get("data")).isEqualTo(Map.of("node", Map.of("id", "LEJ", "city", "Leipzig")));
    }

    @Test
    void theJournalHasTheOperationsNotTheLookups() throws Exception {
        start("parcels.graphql");
        stubParcel();
        graphql(200, "{ parcel(reference: \"PX-GQL-7201\") { reference lastScan { location } shop { name } } }", null);
        assertThat(wm.getAllServeEvents()).hasSize(1);
        assertThat(wm.getAllServeEvents().get(0).getRequest().getUrl()).isEqualTo("/graphql");
        assertThat(wm.getAllServeEvents().get(0).getRequest().getBodyAsString()).contains("PX-GQL-7201");
    }

    @Test
    void aStubOfAWholeOperationWins() throws Exception {
        start("parcels.graphql");
        wm.stubFor(
                post(urlPathEqualTo("/graphql"))
                        .withRequestBody(matchingJsonPath("$.query", containing("Outage")))
                        .willReturn(aResponse().withStatus(503)));
        graphql(503, "query Outage { parcel(reference: \"PX-GQL-7205\") { reference } }", Map.of());
    }

    @Test
    void aRequestThatIsNotGraphQL() throws Exception {
        start("parcels.graphql");
        HttpResponse<String> res =
                CLIENT.send(
                        HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + wm.port() + "/graphql"))
                                .POST(HttpRequest.BodyPublishers.ofString("<parcel/>"))
                                .build(),
                        HttpResponse.BodyHandlers.ofString());
        assertThat(res.statusCode()).isEqualTo(400);
        assertThat(res.body()).contains("its body is not JSON");
    }

    @Test
    @SuppressWarnings("unchecked")
    void aSubgraphServesItsSchemaAndItsEntities() throws Exception {
        start("shops.graphql");
        // A stub's value can be its jsonBody, as mapping files write it.
        wm.stubFor(
                post(urlPathEqualTo("/graphql/_entities/Shop"))
                        .withRequestBody(equalToJson("{\"id\": \"maple-crafts\"}", true, true))
                        .willReturn(aResponse().withJsonBody(Json.node("{\"name\": \"Maple Home\", \"tier\": \"GOLD\"}"))));
        Map<String, Object> res = graphql(200, "{ _service { sdl } }", null);
        String sdl = (String) ((Map<String, Object>) ((Map<String, Object>) res.get("data")).get("_service")).get("sdl");
        assertThat(sdl).contains("type Shop").contains("@key");
        res =
                graphql(200, "query ($representations: [_Any!]!) { _entities(representations: $representations) { ... on Shop { id name tier } } }",
                        Map.of("representations", List.of(
                                Map.of("__typename", "Shop", "id", "maple-crafts"),
                                Map.of("__typename", "Shop", "id", "hawthorn-home"))));
        assertThat(res).doesNotContainKey("errors");
        List<Object> entities = (List<Object>) ((Map<String, Object>) res.get("data")).get("_entities");
        assertThat(entities.get(0)).isEqualTo(Map.of("id", "maple-crafts", "name", "Maple Home", "tier", "GOLD"));
        assertThat(entities.get(1)).isNull();
    }

    @Test
    void withoutASchemaTheMockIsOff() throws Exception {
        wm = new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort().extensions(new GraphqlExtensionFactory(new GraphqlSettings(null, "/graphql"))));
        wm.start();
        graphql(404, "{ parcel(reference: \"PX-GQL-7206\") { reference } }", null);
    }
}
