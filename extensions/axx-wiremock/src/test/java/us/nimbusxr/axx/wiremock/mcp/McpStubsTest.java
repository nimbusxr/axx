// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.github.tomakehurst.wiremock.WireMockServer;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;

/** A mistake in a tool stub or in the server's description is loud: WireMock refuses it. */
class McpStubsTest extends McpMockTest {
    @Test
    void aStubOfAToolTheServerDoesNotListIsRefused() {
        assertThatThrownBy(() -> stubTool("track_parcel", "{\"reference\": \"PX-MCP-7201\"}", "{\"structuredContent\": {}}"))
                .hasMessageContaining("answers the tool track_parcel, which the MCP server description")
                .hasMessageContaining("its tools are shipment_status, book_pickup");
    }

    @Test
    void aToolStubThatIsNotAnAnswerIsRefused() {
        assertThatThrownBy(() -> stubTool("shipment_status", "{\"reference\": \"PX-MCP-7202\"}", "{\"structured\": {\"status\": \"DELIVERED\"}}"))
                .hasMessageContaining("is not an MCP tool stub: a tool's answer has no key \"structured\"");
        assertThatThrownBy(() -> stubTool("shipment_status", "{\"reference\": \"PX-MCP-7203\"}", "{\"error\": {\"code\": \"busy\"}}"))
                .hasMessageContaining("an error is {\"code\": <a whole number>, \"message\": \"...\"");
        assertThatThrownBy(() -> stubTool("book_pickup", "{\"reference\": \"PX-MCP-7204\"}", "{\"content\": \"booked\"}"))
                .hasMessageContaining("a tool's content is a list of content blocks");
    }

    @Test
    void aWrongMappingFileStopsWireMock(@TempDir Path root) throws Exception {
        Files.createDirectories(root.resolve("mappings"));
        Files.writeString(root.resolve("mappings/carrier.json"), "{\"request\": {\"method\": \"POST\", \"urlPath\": \"/mcp/tools/shipment_status\"},"
                + " \"response\": {\"jsonBody\": {\"structuredContent\": {}, \"isErorr\": true}}}");
        assertThatThrownBy(() -> new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort().usingFilesUnderDirectory(root.toString())
                .extensions(new McpExtensionFactory(new McpSettings(CARRIER, "/mcp")))))
                .hasMessageContaining("a tool's answer has no key \"isErorr\"");
    }

    @Test
    void aWrongDescriptionIsRefusedWithWhatIsWrong() {
        assertThatThrownBy(() -> McpServer.read("carrier.yaml", "serverInfo: {name: carrier-tools, version: 1.0.0}\ntool: []"))
                .hasMessageContaining("it has no key \"tool\"");
        assertThatThrownBy(() -> McpServer.read("carrier.yaml", "serverInfo: {name: carrier-tools}"))
                .hasMessageContaining("its serverInfo names the server and its version");
        assertThatThrownBy(() -> McpServer.read("carrier.yaml", "serverInfo: {name: carrier-tools, version: 1.0.0}\ntools: [{name: shipment_status}]"))
                .hasMessageContaining("the tool shipment_status has an inputSchema");
        assertThatThrownBy(() -> McpServer.read("carrier.yaml", "serverInfo: {name: c, version: '1'}\n"
                + "tools: [{name: a, inputSchema: {type: object}}, {name: a, inputSchema: {type: object}}]"))
                .hasMessageContaining("two tools named a");
        assertThatThrownBy(() -> McpServer.load("/nowhere/carrier.yaml")).hasMessageContaining("cannot read the MCP server description /nowhere/carrier.yaml");
    }

    @Test
    void theSettingsComeFromTheEnvironment() {
        assertThat(McpSettings.fromEnv(Map.of()).enabled()).isFalse();
        assertThat(McpSettings.fromEnv(Map.of("MCP_SERVER_SOURCE", " ")).enabled()).isFalse();
        assertThat(McpSettings.fromEnv(Map.of("MCP_SERVER_SOURCE", "/var/mcp/carrier.yaml"))).isEqualTo(new McpSettings("/var/mcp/carrier.yaml", "/mcp"));
        assertThat(McpSettings.fromEnv(Map.of("MCP_SERVER_SOURCE", "/var/mcp/carrier.yaml", "MCP_PATH", "carrier/mcp/")).path()).isEqualTo("/carrier/mcp");
    }
}
