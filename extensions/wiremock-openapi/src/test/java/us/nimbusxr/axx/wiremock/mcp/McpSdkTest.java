// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;

import io.modelcontextprotocol.client.McpClient;
import io.modelcontextprotocol.client.McpSyncClient;
import io.modelcontextprotocol.client.transport.HttpClientStreamableHttpTransport;
import io.modelcontextprotocol.spec.McpError;
import io.modelcontextprotocol.spec.McpSchema;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.time.Duration;
import java.util.List;
import java.util.Map;

/**
 * The versions before 2026-07-28, as the official MCP Java SDK speaks them (initialize, then a
 * session), and every answer the mock sent checked against MCP's published schema of 2025-11-25.
 */
class McpSdkTest extends McpMockTest {
    private static final String V = "2025-11-25";

    private McpSyncClient client;

    private static McpSchema.CallToolRequest call(String tool, Map<String, Object> arguments) {
        return McpSchema.CallToolRequest.builder(tool).arguments(arguments).build();
    }

    @BeforeEach
    void connect() {
        client = McpClient.sync(HttpClientStreamableHttpTransport.builder(url()).endpoint("/mcp").build())
                .requestTimeout(Duration.ofSeconds(10))
                .clientInfo(McpSchema.Implementation.builder("parcel-assistant", "3.1.0").build())
                .build();
    }

    @AfterEach
    void disconnect() {
        client.closeGracefully();
    }

    /** Checks every answer the mock sent against the schema of 2025-11-25: its envelope, and its result as what the method returns. */
    @AfterEach
    @SuppressWarnings("unchecked")
    void everyAnswerMatchesTheSchema() {
        Map<String, String> results = Map.of("initialize", "InitializeResult", "tools/list", "ListToolsResult", "tools/call", "CallToolResult",
                "resources/list", "ListResourcesResult", "resources/read", "ReadResourceResult", "prompts/list", "ListPromptsResult",
                "prompts/get", "GetPromptResult", "ping", "EmptyResult");
        for (ServeEvent e : served()) {
            if (!e.getRequest().getMethod().getName().equals("POST") || e.getResponse().getStatus() == 202) {
                continue;
            }
            Map<String, Object> request = Json.read(e.getRequest().getBodyAsString(), Map.class);
            String body = e.getResponse().getBodyAsString();
            Map<String, Object> answer = Json.read(body, Map.class);
            if (answer.containsKey("error")) {
                assertThat(McpContracts.check(V, "JSONRPCErrorResponse", body)).as(body).isEmpty();
                continue;
            }
            assertThat(McpContracts.check(V, "JSONRPCResultResponse", body)).as(body).isEmpty();
            String definition = results.get((String) request.get("method"));
            assertThat(definition).as("a result of " + request.get("method")).isNotNull();
            assertThat(McpContracts.check(V, definition, Json.write(answer.get("result")))).as(body).isEmpty();
        }
    }

    @Test
    void theSdkConnectsAndListsWhatTheDescriptionHas() {
        McpSchema.InitializeResult init = client.initialize();
        assertThat(init.protocolVersion()).isEqualTo("2025-11-25");
        assertThat(init.serverInfo().name()).isEqualTo("carrier-tools");
        assertThat(init.serverInfo().version()).isEqualTo("2.4.0");
        assertThat(init.instructions()).contains("parcel reference");
        assertThat(init.capabilities().tools()).isNotNull();
        assertThat(init.capabilities().prompts()).isNotNull();

        assertThat(client.listTools().tools()).extracting(McpSchema.Tool::name).containsExactly("shipment_status", "book_pickup");
        assertThat(client.listResources().resources()).extracting(McpSchema.Resource::uri).containsExactly("carrier://service-areas", "carrier://tariffs/2026");
        assertThat(client.listPrompts().prompts()).extracting(McpSchema.Prompt::name).containsExactly("delivery_update");
        client.ping();
        // The SDK's session header came with initialize; the answers are the same with it.
        assertThat(served().get(0).getResponse().getHeaders().getHeader("Mcp-Session-Id").isPresent()).isTrue();
    }

    @Test
    @SuppressWarnings("unchecked")
    void toolCallsAreAnsweredByTheirStubs() {
        stubShipment("PX-MCP-7001", "IN_TRANSIT", "Erfurt");
        client.initialize();
        // The SDK checks structured results against the output schema of the tools it listed.
        client.listTools();
        McpSchema.CallToolResult r = client.callTool(call("shipment_status", Map.of("reference", "PX-MCP-7001")));
        assertThat(r.isError()).isNotEqualTo(Boolean.TRUE);
        assertThat((Map<String, Object>) r.structuredContent()).containsEntry("status", "IN_TRANSIT").containsEntry("depot", "Erfurt");
        assertThat(r.content()).singleElement().isInstanceOfSatisfying(McpSchema.TextContent.class,
                t -> assertThat(t.text()).contains("\"status\":\"IN_TRANSIT\""));

        stubTool("book_pickup", "{\"reference\": \"PX-MCP-7002\", \"shop\": \"maple-crafts\", \"date\": \"2026-10-02\"}",
                "{\"content\": [{\"type\": \"text\", \"text\": \"Pickup of PX-MCP-7002 booked at maple-crafts on 2026-10-02.\"}]}");
        r = client.callTool(call("book_pickup", Map.of("reference", "PX-MCP-7002", "shop", "maple-crafts", "date", "2026-10-02")));
        assertThat(r.content()).singleElement().isInstanceOfSatisfying(McpSchema.TextContent.class,
                t -> assertThat(t.text()).isEqualTo("Pickup of PX-MCP-7002 booked at maple-crafts on 2026-10-02."));
        assertThat(r.structuredContent()).isNull();

        stubTool("book_pickup", "{\"reference\": \"PX-MCP-7003\", \"shop\": \"maple-crafts\"}",
                "{\"content\": [{\"type\": \"text\", \"text\": \"maple-crafts is closed for pickups this week.\"}], \"isError\": true}");
        r = client.callTool(call("book_pickup", Map.of("reference", "PX-MCP-7003", "shop", "maple-crafts")));
        assertThat(r.isError()).isTrue();
    }

    @Test
    void wrongCallsGetToolErrorsOrProtocolErrors() {
        client.initialize();
        McpSchema.CallToolResult r = client.callTool(call("shipment_status", Map.of("reference", 7004)));
        assertThat(r.isError()).isTrue();
        assertThat(((McpSchema.TextContent) r.content().get(0)).text()).contains("Invalid arguments for the tool shipment_status").contains("/reference");

        assertThatThrownBy(() -> client.callTool(call("track_parcel", Map.of("reference", "PX-MCP-7005"))))
                .isInstanceOfSatisfying(McpError.class, e -> {
                    assertThat(e.getJsonRpcError().code()).isEqualTo(-32602);
                    assertThat(e.getJsonRpcError().message()).isEqualTo("Unknown tool: track_parcel");
                });
        assertThatThrownBy(() -> client.callTool(call("shipment_status", Map.of("reference", "PX-MCP-7006"))))
                .isInstanceOfSatisfying(McpError.class, e -> {
                    assertThat(e.getJsonRpcError().code()).isEqualTo(-32603);
                    assertThat(e.getJsonRpcError().message()).contains("no stub answers the tool shipment_status");
                });
    }

    @Test
    void resourcesAndPromptsAreAnswered() {
        stub("{\"request\": {\"method\": \"POST\", \"urlPath\": \"/mcp/resources\", \"bodyPatterns\": [{\"equalToJson\": {\"uri\": \"carrier://tariffs/2026\"}}]},"
                + " \"response\": {\"jsonBody\": {\"contents\": [{\"mimeType\": \"application/json\", \"text\": \"{\\\"domestic\\\": 590}\"}]}}}");
        stub("{\"request\": {\"method\": \"POST\", \"urlPath\": \"/mcp/prompts/delivery_update\", \"bodyPatterns\": [{\"equalToJson\": {\"reference\": \"PX-MCP-7007\"},"
                + " \"ignoreExtraElements\": true}]}, \"response\": {\"jsonBody\": {\"messages\": [{\"role\": \"user\", \"content\": {\"type\": \"text\","
                + " \"text\": \"Tell the recipient that PX-MCP-7007 is at the Leipzig depot.\"}}]}}}");
        client.initialize();

        McpSchema.ReadResourceResult inline = client.readResource(McpSchema.ReadResourceRequest.builder("carrier://service-areas").build());
        assertThat(inline.contents()).singleElement().isInstanceOfSatisfying(McpSchema.TextResourceContents.class,
                c -> assertThat(c.text()).isEqualTo("DE 01067-99998, AT 1010-9992"));
        McpSchema.ReadResourceResult stubbed = client.readResource(McpSchema.ReadResourceRequest.builder("carrier://tariffs/2026").build());
        assertThat(stubbed.contents()).singleElement().isInstanceOfSatisfying(McpSchema.TextResourceContents.class, c -> {
            assertThat(c.uri()).isEqualTo("carrier://tariffs/2026");
            assertThat(c.text()).isEqualTo("{\"domestic\": 590}");
        });
        // The versions before 2026-07-28 answer a missing resource with -32002.
        assertThatThrownBy(() -> client.readResource(McpSchema.ReadResourceRequest.builder("carrier://tariffs/2019").build()))
                .isInstanceOfSatisfying(McpError.class, e -> assertThat(e.getJsonRpcError().code()).isEqualTo(-32002));

        McpSchema.GetPromptResult prompt = client.getPrompt(McpSchema.GetPromptRequest.builder("delivery_update").arguments(Map.of("reference", "PX-MCP-7007", "tone", "friendly")).build());
        assertThat(prompt.messages()).singleElement().satisfies(m -> {
            assertThat(m.role()).isEqualTo(McpSchema.Role.USER);
            assertThat(((McpSchema.TextContent) m.content()).text()).contains("PX-MCP-7007");
        });
    }

    @Test
    @SuppressWarnings("unchecked")
    void theJournalRecordsTheSdksMessages() {
        stubShipment("PX-MCP-7008", "DELIVERED", "Leipzig");
        client.initialize();
        client.callTool(call("shipment_status", Map.of("reference", "PX-MCP-7008")));
        List<Map<String, Object>> posted = served().stream().filter(e -> e.getRequest().getMethod().getName().equals("POST"))
                .map(e -> (Map<String, Object>) Json.read(e.getRequest().getBodyAsString(), Map.class)).toList();
        assertThat(posted).extracting(m -> m.get("method")).containsExactly("initialize", "notifications/initialized", "tools/call");
        assertThat((Map<String, Object>) posted.get(2).get("params")).containsEntry("name", "shipment_status")
                .containsEntry("arguments", Map.of("reference", "PX-MCP-7008"));
        assertThat(served()).noneMatch(e -> e.getRequest().getUrl().startsWith("/mcp/tools"));
    }
}
