// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import com.networknt.schema.Error;
import com.networknt.schema.InputFormat;
import com.networknt.schema.Schema;
import com.networknt.schema.SchemaRegistry;
import com.networknt.schema.SpecificationVersion;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * MCP's published JSON schemas of the protocol versions 2026-07-28 and 2025-11-25, which the build
 * fetches pinned (build.gradle.kts): the definitions the mock's answers must match.
 *
 * <p>The validator is networknt's, the version the MCP SDK brings, not the mock's own (which the
 * mock's jar carries in a package of its own).
 */
final class McpContracts {
    private McpContracts() {}

    private static final SchemaRegistry REGISTRY = SchemaRegistry.withDefaultDialect(SpecificationVersion.DRAFT_2020_12);
    private static final Map<String, Schema> SCHEMAS = new ConcurrentHashMap<>();

    /** The problems of a JSON value as the definition of a version's schema, or none. */
    static List<String> check(String version, String definition, String json) {
        Schema schema = SCHEMAS.computeIfAbsent(version + "#" + definition, k -> {
            String text;
            try {
                text = Files.readString(Path.of(System.getProperty("axx.contracts"), "mcp-" + version + ".json"), StandardCharsets.UTF_8).strip();
            } catch (IOException e) {
                throw new UncheckedIOException(e);
            }
            if (!text.contains("\"" + definition + "\":")) {
                throw new IllegalArgumentException("MCP " + version + "'s schema has no definition " + definition);
            }
            // The whole schema, as a reference to one of its definitions.
            return REGISTRY.getSchema("{\"$ref\": \"#/$defs/" + definition + "\", " + text.substring(1), InputFormat.JSON);
        });
        return schema.validate(json, InputFormat.JSON).stream().map(Error::toString).toList();
    }
}
