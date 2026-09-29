// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.graphql;

import java.util.Map;

/**
 * The GraphQL mock's settings, from the environment.
 *
 * @param schemaSource {@code GRAPHQL_SCHEMA_SOURCE}: the SDL file of the mocked service or
 *     subgraph; without it the mock is off
 * @param path {@code GRAPHQL_PATH}: where it answers, {@code /graphql} by default
 */
record GraphqlSettings(String schemaSource, String path) {
    static final String DEFAULT_PATH = "/graphql";

    static GraphqlSettings fromEnv(Map<String, String> env) {
        String source = env.get("GRAPHQL_SCHEMA_SOURCE");
        String path = env.getOrDefault("GRAPHQL_PATH", DEFAULT_PATH);
        if (source != null && source.isBlank()) {
            source = null;
        }
        if (path.isBlank()) {
            path = DEFAULT_PATH;
        }
        return new GraphqlSettings(source, path.startsWith("/") ? path : "/" + path);
    }

    boolean enabled() {
        return schemaSource != null;
    }
}
