// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import com.github.tomakehurst.wiremock.extension.StubLifecycleListener;
import com.github.tomakehurst.wiremock.matching.RequestPattern;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

import us.nimbusxr.axx.wiremock.stubs.InProcessStubs;

import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.Map;

/**
 * Refuses a wrong stub of a tool when it is added, so a mistake in a mapping file stops WireMock
 * at startup instead of answering nothing: a stub under {@code <MCP_PATH>/tools/<tool>} for a
 * tool the server's description does not list, and a tool stub whose answer is not one (a key
 * other than {@code content}, {@code structuredContent}, {@code isError} and {@code error}, or
 * one of them of the wrong shape).
 */
public class McpStubs implements StubLifecycleListener {
    private final McpSettings settings;
    private final McpServer server;

    McpStubs(McpSettings settings, McpServer server) {
        this.settings = settings;
        this.server = server;
    }

    @Override
    public String getName() {
        return "mcp-stubs";
    }

    @Override
    public void beforeStubCreated(StubMapping stub) {
        check(stub);
    }

    @Override
    public void beforeStubEdited(StubMapping oldStub, StubMapping newStub) {
        check(newStub);
    }

    void check(StubMapping stub) {
        String prefix = settings.stubsBase() + "/tools/";
        RequestPattern request = stub.getRequest();
        String path = InProcessStubs.exactPath(request);
        boolean tool;
        if (path != null) {
            tool = path.startsWith(prefix);
            if (tool) {
                String name = URLDecoder.decode(path.substring(prefix.length()).replace("+", "%2B"), StandardCharsets.UTF_8);
                if (!server.hasTool(name)) {
                    throw new IllegalArgumentException("the stub " + InProcessStubs.describe(stub) + " answers the tool " + name
                            + ", which the MCP server description " + server.source + " does not list: its tools are " + server.toolNames());
                }
            }
        } else {
            String pattern = request.getUrlPathPattern() != null ? request.getUrlPathPattern() : request.getUrlPattern();
            tool = pattern != null && pattern.replace("\\", "").startsWith(prefix);
        }
        if (!tool || stub.getResponse() == null) {
            return;
        }
        String body = InProcessStubs.inlineBody(stub.getResponse());
        if (body == null || body.isBlank()) {
            return;
        }
        Map<String, Object> answer;
        try {
            answer = Values.map(Values.parse(body));
        } catch (IllegalArgumentException e) {
            answer = null;
        }
        String problem = answer == null ? "a tool's answer is a JSON object with content, structuredContent and isError, or error"
                : McpMock.toolAnswerProblem(answer);
        if (problem != null) {
            throw new IllegalArgumentException("the stub " + InProcessStubs.describe(stub) + " is not an MCP tool stub: " + problem);
        }
    }
}
