// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import static org.assertj.core.api.Assertions.assertThat;

import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.http.RequestMethod;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;

import org.junit.jupiter.api.Test;

import java.net.URI;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.List;
import java.util.Map;

/**
 * MCP 2026-07-28, which has no sessions and no initialize: requests written by hand, as a client of
 * that version sends them, and every answer checked against MCP's published schema of the version.
 */
class McpModernTest extends McpMockTest {
    private static final String V = "2026-07-28";

    /** Checks the request against the schema too, so the tests send what a client of the version sends. */
    private HttpResponse<String> call(Object id, String method, Map<String, Object> params, String requestDefinition) throws Exception {
        assertThat(McpContracts.check(V, requestDefinition, modernRequest(id, method, params))).isEmpty();
        return modern(id, method, params);
    }

    private static void matches(String definition, HttpResponse<String> res) {
        assertThat(McpContracts.check(V, definition, res.body())).as(res.body()).isEmpty();
    }

    @Test
    void theSchemaRefusesWhatIsNotTheProtocols() {
        // The checks below mean something: the schema refuses an answer off the contract.
        assertThat(McpContracts.check(V, "CallToolResult", "{\"content\": \"PX-MCP-7100 is in transit\"}")).isNotEmpty();
        assertThat(McpContracts.check(V, "DiscoverResult", "{\"resultType\": \"complete\", \"supportedVersions\": [], \"capabilities\": {}}")).isNotEmpty();
        assertThat(McpContracts.check("2025-11-25", "InitializeResult", "{\"protocolVersion\": \"2025-11-25\"}")).isNotEmpty();
    }

    @Test
    @SuppressWarnings("unchecked")
    void discoverSaysWhoTheServerIsAndWhatItSpeaks() throws Exception {
        HttpResponse<String> res = call("discover-1", "server/discover", Map.of(), "DiscoverRequest");
        assertThat(res.statusCode()).isEqualTo(200);
        assertThat(res.headers().firstValue("Content-Type")).hasValue("application/json");
        assertThat(res.headers().firstValue("Mcp-Session-Id")).isEmpty();
        matches("DiscoverResultResponse", res);
        Map<String, Object> result = result(res);
        assertThat(json(res)).containsEntry("id", "discover-1");
        assertThat(result).containsEntry("resultType", "complete");
        assertThat((List<String>) result.get("supportedVersions")).containsExactly("2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26");
        assertThat((Map<String, Object>) result.get("capabilities")).containsOnlyKeys("tools", "resources", "prompts");
        assertThat(result).containsEntry("instructions", "Look up the partner carrier's shipments by their parcel reference (PX-...).");
        assertThat(result).containsEntry("ttlMs", 0).containsEntry("cacheScope", "private");
        assertThat((Map<String, Object>) result.get("_meta")).containsEntry("io.modelcontextprotocol/serverInfo",
                Map.of("name", "carrier-tools", "title", "Partner carrier", "version", "2.4.0"));
    }

    @Test
    @SuppressWarnings("unchecked")
    void theListsAreTheDescriptions() throws Exception {
        HttpResponse<String> res = call(1, "tools/list", Map.of(), "ListToolsRequest");
        matches("ListToolsResultResponse", res);
        List<Map<String, Object>> tools = (List<Map<String, Object>>) result(res).get("tools");
        assertThat(tools).extracting(t -> t.get("name")).containsExactly("shipment_status", "book_pickup");
        assertThat(tools.get(0)).containsEntry("annotations", Map.of("readOnlyHint", true)).containsKey("outputSchema");

        res = call(2, "resources/list", Map.of(), "ListResourcesRequest");
        matches("ListResourcesResultResponse", res);
        List<Map<String, Object>> resources = (List<Map<String, Object>>) result(res).get("resources");
        assertThat(resources).extracting(r -> r.get("uri")).containsExactly("carrier://service-areas", "carrier://tariffs/2026");
        // The inline content is what a read answers, not part of the listing.
        assertThat(resources.get(0)).doesNotContainKey("text").containsEntry("mimeType", "text/plain");

        res = call(3, "resources/templates/list", Map.of(), "ListResourceTemplatesRequest");
        matches("ListResourceTemplatesResultResponse", res);
        assertThat((List<Map<String, Object>>) result(res).get("resourceTemplates")).extracting(t -> t.get("uriTemplate"))
                .containsExactly("carrier://labels/{reference}");

        res = call(4, "prompts/list", Map.of(), "ListPromptsRequest");
        matches("ListPromptsResultResponse", res);
        assertThat((List<Map<String, Object>>) result(res).get("prompts")).extracting(p -> p.get("name")).containsExactly("delivery_update");
    }

    @Test
    @SuppressWarnings("unchecked")
    void aToolCallIsAnsweredByItsStub() throws Exception {
        stubShipment("PX-MCP-7101", "OUT_FOR_DELIVERY", "Leipzig");
        HttpResponse<String> res = call(7, "tools/call", Map.of("name", "shipment_status", "arguments", Map.of("reference", "PX-MCP-7101")),
                "CallToolRequest");
        assertThat(res.statusCode()).isEqualTo(200);
        matches("CallToolResultResponse", res);
        Map<String, Object> result = result(res);
        assertThat(result).containsEntry("resultType", "complete")
                .containsEntry("structuredContent", Map.of("reference", "PX-MCP-7101", "status", "OUT_FOR_DELIVERY", "depot", "Leipzig"))
                .doesNotContainKey("isError");
        // The structured content as text too, as MCP asks of servers.
        List<Map<String, Object>> content = (List<Map<String, Object>>) result.get("content");
        assertThat(content).hasSize(1);
        assertThat(content.get(0)).containsEntry("type", "text");
        assertThat(Json.read((String) content.get(0).get("text"), Map.class)).isEqualTo(result.get("structuredContent"));
        assertThat((Map<String, Object>) result.get("_meta")).containsKey("io.modelcontextprotocol/serverInfo");
    }

    @Test
    @SuppressWarnings("unchecked")
    void argumentsThatBreakTheInputSchemaAreAToolError() throws Exception {
        HttpResponse<String> res = call(8, "tools/call", Map.of("name", "shipment_status", "arguments", Map.of("reference", "7102")),
                "CallToolRequest");
        assertThat(res.statusCode()).isEqualTo(200);
        matches("CallToolResultResponse", res);
        Map<String, Object> result = result(res);
        assertThat(result).containsEntry("isError", true);
        String text = (String) ((List<Map<String, Object>>) result.get("content")).get(0).get("text");
        assertThat(text).startsWith("Invalid arguments for the tool shipment_status: ").contains("/reference").contains("^PX-");

        res = call(9, "tools/call", Map.of("name", "shipment_status", "arguments", Map.of()), "CallToolRequest");
        assertThat((String) ((List<Map<String, Object>>) result(res).get("content")).get(0).get("text")).contains("reference");
    }

    @Test
    void protocolErrorsAreJsonRpcErrors() throws Exception {
        HttpResponse<String> res = call(10, "tools/call", Map.of("name", "track_parcel", "arguments", Map.of()), "CallToolRequest");
        assertThat(res.statusCode()).isEqualTo(200);
        matches("JSONRPCErrorResponse", res);
        assertThat(McpContracts.check(V, "InvalidParamsError", Json.write(error(res)))).isEmpty();
        assertThat(error(res)).containsEntry("code", -32602).containsEntry("message", "Unknown tool: track_parcel");

        // No stub answers: a setup problem, which the answer names.
        res = call(11, "tools/call", Map.of("name", "shipment_status", "arguments", Map.of("reference", "PX-MCP-7103")), "CallToolRequest");
        matches("JSONRPCErrorResponse", res);
        assertThat(McpContracts.check(V, "InternalError", Json.write(error(res)))).isEmpty();
        assertThat((String) error(res).get("message")).isEqualTo("no stub answers the tool shipment_status with these arguments: {\"reference\":\"PX-MCP-7103\"}");
    }

    @Test
    void aStubCanAnswerAToolErrorOrAProtocolError() throws Exception {
        stubTool("book_pickup", "{\"reference\": \"PX-MCP-7104\", \"shop\": \"maple-crafts\"}",
                "{\"content\": [{\"type\": \"text\", \"text\": \"No pickups on Sundays: choose another date.\"}], \"isError\": true}");
        HttpResponse<String> res = call(12, "tools/call", Map.of("name", "book_pickup", "arguments",
                Map.of("reference", "PX-MCP-7104", "shop", "maple-crafts")), "CallToolRequest");
        matches("CallToolResultResponse", res);
        assertThat(result(res)).containsEntry("isError", true)
                .containsEntry("content", List.of(Map.of("type", "text", "text", "No pickups on Sundays: choose another date.")));

        stubTool("book_pickup", "{\"reference\": \"PX-MCP-7105\", \"shop\": \"maple-crafts\"}",
                "{\"error\": {\"code\": -32050, \"message\": \"the carrier's booking system is down\", \"data\": {\"retryAfter\": 30}}}");
        res = call(13, "tools/call", Map.of("name", "book_pickup", "arguments", Map.of("reference", "PX-MCP-7105", "shop", "maple-crafts")),
                "CallToolRequest");
        matches("JSONRPCErrorResponse", res);
        assertThat(error(res)).isEqualTo(Map.of("code", -32050, "message", "the carrier's booking system is down", "data", Map.of("retryAfter", 30)));
    }

    @Test
    void aStubThatBreaksTheOutputSchemaIsLoud() throws Exception {
        stubTool("shipment_status", "{\"reference\": \"PX-MCP-7106\"}",
                "{\"structuredContent\": {\"reference\": \"PX-MCP-7106\", \"status\": \"LOST\"}}");
        HttpResponse<String> res = call(14, "tools/call", Map.of("name", "shipment_status", "arguments", Map.of("reference", "PX-MCP-7106")),
                "CallToolRequest");
        matches("JSONRPCErrorResponse", res);
        assertThat(error(res)).containsEntry("code", -32603);
        assertThat((String) error(res).get("message")).contains("of the tool shipment_status answers structuredContent that does not match the tool's outputSchema")
                .contains("/status");

        stubTool("shipment_status", "{\"reference\": \"PX-MCP-7107\"}", "{\"content\": [{\"type\": \"text\", \"text\": \"in transit\"}]}");
        res = call(15, "tools/call", Map.of("name", "shipment_status", "arguments", Map.of("reference", "PX-MCP-7107")), "CallToolRequest");
        assertThat((String) error(res).get("message")).contains("answers no structuredContent, which the tool's outputSchema describes");
    }

    @Test
    @SuppressWarnings("unchecked")
    void aResourceIsReadFromItsStubOrItsInlineContent() throws Exception {
        HttpResponse<String> res = call(20, "resources/read", Map.of("uri", "carrier://service-areas"), "ReadResourceRequest");
        matches("ReadResourceResultResponse", res);
        assertThat(result(res)).containsEntry("contents",
                List.of(Map.of("uri", "carrier://service-areas", "mimeType", "text/plain", "text", "DE 01067-99998, AT 1010-9992")));
        assertThat(result(res)).containsEntry("ttlMs", 0).containsEntry("cacheScope", "private");

        stub("{\"request\": {\"method\": \"POST\", \"urlPath\": \"/mcp/resources\", \"bodyPatterns\": [{\"equalToJson\": {\"uri\": \"carrier://labels/PX-MCP-7108\"}}]},"
                + " \"response\": {\"jsonBody\": {\"contents\": [{\"mimeType\": \"application/pdf\", \"blob\": \"JVBERi0xLjcK\"}]}}}");
        res = call(21, "resources/read", Map.of("uri", "carrier://labels/PX-MCP-7108"), "ReadResourceRequest");
        matches("ReadResourceResultResponse", res);
        assertThat((List<Map<String, Object>>) result(res).get("contents")).containsExactly(
                Map.of("uri", "carrier://labels/PX-MCP-7108", "mimeType", "application/pdf", "blob", "JVBERi0xLjcK"));

        // A resource without content or stub does not exist: Invalid Params since 2026-07-28.
        res = call(22, "resources/read", Map.of("uri", "carrier://tariffs/2026"), "ReadResourceRequest");
        matches("JSONRPCErrorResponse", res);
        assertThat(error(res)).containsEntry("code", -32602).containsEntry("data", Map.of("uri", "carrier://tariffs/2026"));
    }

    @Test
    @SuppressWarnings("unchecked")
    void aPromptIsAnsweredByItsStub() throws Exception {
        stub("{\"request\": {\"method\": \"POST\", \"urlPath\": \"/mcp/prompts/delivery_update\", \"bodyPatterns\": [{\"matchesJsonPath\": \"$[?(@.reference == 'PX-MCP-7109')]\"}]},"
                + " \"response\": {\"jsonBody\": {\"description\": \"A delivery update\", \"messages\": [{\"role\": \"user\", \"content\": {\"type\": \"text\","
                + " \"text\": \"Tell the recipient that PX-MCP-7109 arrives tomorrow.\"}}]}}}");
        HttpResponse<String> res = call(30, "prompts/get", Map.of("name", "delivery_update", "arguments", Map.of("reference", "PX-MCP-7109")),
                "GetPromptRequest");
        matches("GetPromptResultResponse", res);
        assertThat(result(res)).containsEntry("description", "A delivery update");
        assertThat((List<Object>) result(res).get("messages")).hasSize(1);

        res = call(31, "prompts/get", Map.of("name", "delivery_update", "arguments", Map.of("tone", "friendly")), "GetPromptRequest");
        matches("JSONRPCErrorResponse", res);
        assertThat(error(res)).containsEntry("code", -32602).containsEntry("message", "Missing required argument of the prompt delivery_update: reference");

        res = call(32, "prompts/get", Map.of("name", "pickup_reminder"), "GetPromptRequest");
        assertThat(error(res)).containsEntry("code", -32602).containsEntry("message", "Unknown prompt: pickup_reminder");
    }

    @Test
    @SuppressWarnings("unchecked")
    void anUnsupportedVersionIsRefusedWithTheSupportedOnes() throws Exception {
        String body = modernRequest(40, "tools/list", Map.of()).replace("2026-07-28", "2027-01-01");
        HttpResponse<String> res = post(body, "MCP-Protocol-Version", "2027-01-01", "Mcp-Method", "tools/list");
        assertThat(res.statusCode()).isEqualTo(400);
        matches("UnsupportedProtocolVersionError", res);
        Map<String, Object> data = (Map<String, Object>) error(res).get("data");
        assertThat(data).containsEntry("requested", "2027-01-01");
        assertThat((List<String>) data.get("supported")).contains("2026-07-28", "2025-11-25");
    }

    @Test
    void theVersionHeaderMustMatchTheBody() throws Exception {
        HttpResponse<String> res = post(modernRequest(41, "tools/list", Map.of()), "MCP-Protocol-Version", "2025-11-25", "Mcp-Method", "tools/list");
        assertThat(res.statusCode()).isEqualTo(400);
        matches("HeaderMismatchError", res);
        assertThat(error(res)).containsEntry("code", -32020);
    }

    @Test
    void aRequestWithoutTheClientsCapabilitiesIsMalformed() throws Exception {
        String body = "{\"jsonrpc\": \"2.0\", \"id\": 42, \"method\": \"tools/list\", \"params\": {\"_meta\": {\"io.modelcontextprotocol/protocolVersion\": \"2026-07-28\"}}}";
        HttpResponse<String> res = post(body, "MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list");
        assertThat(res.statusCode()).isEqualTo(400);
        matches("JSONRPCErrorResponse", res);
        assertThat(error(res)).containsEntry("code", -32602);
    }

    @Test
    void anUnknownMethodIsNotFound() throws Exception {
        HttpResponse<String> res = call(43, "completion/complete", Map.of("ref", Map.of("type", "ref/prompt", "name", "delivery_update"),
                "argument", Map.of("name", "tone", "value", "f")), "CompleteRequest");
        assertThat(res.statusCode()).isEqualTo(404);
        matches("JSONRPCErrorResponse", res);
        assertThat(McpContracts.check(V, "MethodNotFoundError", Json.write(error(res)))).isEmpty();
        // ping is gone in 2026-07-28.
        assertThat(modern(44, "ping", Map.of()).statusCode()).isEqualTo(404);
    }

    @Test
    void theTransportAnswersOnlyPosts() throws Exception {
        for (String method : List.of("GET", "DELETE")) {
            HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create(url() + "/mcp")).method(method, HttpRequest.BodyPublishers.noBody())
                    .header("Accept", "text/event-stream").build(), HttpResponse.BodyHandlers.ofString());
            assertThat(res.statusCode()).as(method).isEqualTo(405);
        }
        HttpResponse<String> res = post("{\"jsonrpc\": \"2.0\", \"method\": \"notifications/cancelled\", \"params\": {\"requestId\": 7}}");
        assertThat(res.statusCode()).isEqualTo(202);
        assertThat(res.body()).isEmpty();
    }

    @Test
    @SuppressWarnings("unchecked")
    void theJournalRecordsTheCallsNotTheLookups() throws Exception {
        stubShipment("PX-MCP-7110", "DELIVERED", "Leipzig");
        call(50, "server/discover", Map.of(), "DiscoverRequest");
        call(51, "tools/call", Map.of("name", "shipment_status", "arguments", Map.of("reference", "PX-MCP-7110")), "CallToolRequest");
        List<ServeEvent> served = served();
        assertThat(served).hasSize(2);
        assertThat(served).allSatisfy(e -> {
            assertThat(e.getRequest().getMethod()).isEqualTo(RequestMethod.POST);
            assertThat(e.getRequest().getUrl()).isEqualTo("/mcp");
        });
        Map<String, Object> call = Json.read(served.get(1).getRequest().getBodyAsString(), Map.class);
        assertThat(call).containsEntry("method", "tools/call");
        Map<String, Object> params = (Map<String, Object>) call.get("params");
        assertThat(params).containsEntry("name", "shipment_status").containsEntry("arguments", Map.of("reference", "PX-MCP-7110"));
        assertThat(served.get(1).getResponse().getBodyAsString()).contains("DELIVERED");
    }
}
