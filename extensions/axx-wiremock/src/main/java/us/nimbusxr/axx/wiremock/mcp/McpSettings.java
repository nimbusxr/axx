// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import java.util.Map;

/**
 * The MCP mock's settings, from the environment.
 *
 * @param serverSource {@code MCP_SERVER_SOURCE}: the file (JSON or YAML) that describes the mocked
 *     server: its identity, tools, resources and prompts; without it the mock is off
 * @param path {@code MCP_PATH}: the MCP endpoint, {@code /mcp} by default
 */
record McpSettings(String serverSource, String path) {
    static final String DEFAULT_PATH = "/mcp";

    static McpSettings fromEnv(Map<String, String> env) {
        String source = env.get("MCP_SERVER_SOURCE");
        String path = env.getOrDefault("MCP_PATH", DEFAULT_PATH);
        if (source != null && source.isBlank()) {
            source = null;
        }
        if (path == null || path.isBlank()) {
            path = DEFAULT_PATH;
        }
        path = path.startsWith("/") ? path : "/" + path;
        if (path.length() > 1 && path.endsWith("/")) {
            path = path.substring(0, path.length() - 1);
        }
        return new McpSettings(source, path);
    }

    boolean enabled() {
        return serverSource != null;
    }

    /** Where the stubs' paths start: the endpoint, or nothing when the endpoint is {@code /}. */
    String stubsBase() {
        return path.equals("/") ? "" : path;
    }
}
