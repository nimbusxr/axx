// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.graphql;

import com.github.tomakehurst.wiremock.extension.Extension;
import com.github.tomakehurst.wiremock.extension.ExtensionFactory;
import com.github.tomakehurst.wiremock.extension.WireMockServices;

import java.util.List;

/**
 * Makes the GraphQL mock when {@code GRAPHQL_SCHEMA_SOURCE} names a schema. WireMock finds it
 * through {@link java.util.ServiceLoader} scanning.
 */
public class GraphqlExtensionFactory implements ExtensionFactory {
    private final GraphqlSettings settings;

    /** Reads the settings from the environment; WireMock instantiates it reflectively. */
    public GraphqlExtensionFactory() {
        this(GraphqlSettings.fromEnv(System.getenv()));
    }

    GraphqlExtensionFactory(GraphqlSettings settings) {
        this.settings = settings;
    }

    @Override
    public List<Extension> create(WireMockServices services) {
        if (!settings.enabled()) {
            return List.of();
        }
        return List.of(new GraphqlMock(settings, services.getFiles()));
    }
}
