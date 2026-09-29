// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import com.github.tomakehurst.wiremock.extension.Extension;
import com.github.tomakehurst.wiremock.extension.ExtensionFactory;
import com.github.tomakehurst.wiremock.extension.WireMockServices;

import java.util.List;

/**
 * Makes the MCP server mock when {@code MCP_SERVER_SOURCE} names a server description: the mock
 * itself and the check of its tool stubs. The description is read when WireMock starts, and a
 * description that is wrong stops it. WireMock finds the factory through {@link
 * java.util.ServiceLoader} scanning.
 */
public class McpExtensionFactory implements ExtensionFactory {
    private final McpSettings settings;

    /** Reads the settings from the environment; WireMock instantiates it reflectively. */
    public McpExtensionFactory() {
        this(McpSettings.fromEnv(System.getenv()));
    }

    McpExtensionFactory(McpSettings settings) {
        this.settings = settings;
    }

    @Override
    public List<Extension> create(WireMockServices services) {
        if (!settings.enabled()) {
            return List.of();
        }
        McpServer server = McpServer.load(settings.serverSource());
        return List.of(new McpMock(settings, server, services.getFiles()), new McpStubs(settings, server));
    }
}
