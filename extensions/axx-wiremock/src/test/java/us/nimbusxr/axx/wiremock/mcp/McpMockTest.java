// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** A WireMock server with the MCP mock of a partner carrier's server, and stubs written as mapping files write them. */
abstract class McpMockTest {
    static final String CARRIER = Path.of("src/test/resources/mcp/carrier.yaml").toAbsolutePath().toString();
    static final HttpClient HTTP = HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();

    WireMockServer wm;

    @BeforeEach
    void startWireMock() {
        // On the loopback address, which the tests call: a server on every address can be given a
        // port another process listens on at 127.0.0.1, which then answers the tests.
        wm = new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort()
                .extensions(new McpExtensionFactory(new McpSettings(CARRIER, "/mcp"))));
        wm.start();
    }

    @AfterEach
    void stopWireMock() {
        wm.stop();
    }

    String url() {
        return "http://127.0.0.1:" + wm.port();
    }

    /** Adds a stub, from the JSON of a mapping file. */
    void stub(String mapping) {
        wm.addStubMapping(StubMapping.buildFrom(mapping));
    }

    /** Adds a stub of a tool's answer to the calls with these arguments. */
    void stubTool(String tool, String arguments, String answer) {
        stub("{\"request\": {\"method\": \"POST\", \"urlPath\": \"/mcp/tools/" + tool + "\", \"bodyPatterns\": [{\"equalToJson\": " + arguments
                + "}]}, \"response\": {\"status\": 200, \"jsonBody\": " + answer + "}}");
    }

    /** The carrier's answer about a shipment: where it is. */
    void stubShipment(String reference, String status, String depot) {
        stubTool("shipment_status", "{\"reference\": \"" + reference + "\"}",
                "{\"structuredContent\": {\"reference\": \"" + reference + "\", \"status\": \"" + status + "\", \"depot\": \"" + depot + "\"}}");
    }

    /** Posts a body to the MCP endpoint, with headers as name and value pairs. */
    HttpResponse<String> post(String body, String... headers) throws IOException, InterruptedException {
        HttpRequest.Builder b = HttpRequest.newBuilder(URI.create(url() + "/mcp"))
                .header("Content-Type", "application/json")
                .header("Accept", "application/json, text/event-stream")
                .POST(HttpRequest.BodyPublishers.ofString(body));
        if (headers.length % 2 != 0) {
            throw new IllegalArgumentException("headers are names and values, in pairs: " + headers.length + " is odd");
        }
        for (int i = 0; i < headers.length; i += 2) {
            b.header(headers[i], headers[i + 1]);
        }
        return HTTP.send(b.build(), HttpResponse.BodyHandlers.ofString());
    }

    /** The _meta of a request of MCP 2026-07-28. */
    static Map<String, Object> meta() {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("io.modelcontextprotocol/protocolVersion", "2026-07-28");
        m.put("io.modelcontextprotocol/clientInfo", Map.of("name", "parcel-assistant", "version", "3.1.0"));
        m.put("io.modelcontextprotocol/clientCapabilities", Map.of());
        return m;
    }

    /** A request of MCP 2026-07-28, as its JSON. */
    static String modernRequest(Object id, String method, Map<String, Object> params) {
        Map<String, Object> p = new LinkedHashMap<>(params);
        p.put("_meta", meta());
        Map<String, Object> r = new LinkedHashMap<>();
        r.put("jsonrpc", "2.0");
        r.put("id", id);
        r.put("method", method);
        r.put("params", p);
        return Json.write(r);
    }

    /** Sends a request of MCP 2026-07-28 with the headers the transport asks for. */
    HttpResponse<String> modern(Object id, String method, Map<String, Object> params) throws IOException, InterruptedException {
        Object name = params.containsKey("name") ? params.get("name") : params.get("uri");
        return name == null
                ? post(modernRequest(id, method, params), "MCP-Protocol-Version", "2026-07-28", "Mcp-Method", method)
                : post(modernRequest(id, method, params), "MCP-Protocol-Version", "2026-07-28", "Mcp-Method", method, "Mcp-Name", name.toString());
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> json(HttpResponse<String> res) {
        return Json.read(res.body(), Map.class);
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> result(HttpResponse<String> res) {
        return (Map<String, Object>) json(res).get("result");
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> error(HttpResponse<String> res) {
        return (Map<String, Object>) json(res).get("error");
    }

    /** The requests served, the first first. */
    List<ServeEvent> served() {
        return wm.getAllServeEvents().reversed();
    }
}
