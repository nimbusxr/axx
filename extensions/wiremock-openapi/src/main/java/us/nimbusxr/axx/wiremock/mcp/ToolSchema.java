// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import com.networknt.schema.Error;
import com.networknt.schema.Schema;
import com.networknt.schema.SchemaRegistry;
import com.networknt.schema.SpecificationVersion;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * A tool's input or output schema: JSON Schema 2020-12 unless its {@code $schema} names another
 * draft, as MCP says. It lists what is wrong with a value, one problem per line: the place in the
 * value (a JSON pointer) and what is wrong there.
 */
final class ToolSchema {
    private static final SchemaRegistry REGISTRY = SchemaRegistry.withDefaultDialect(SpecificationVersion.DRAFT_2020_12);

    private final Schema schema;

    private ToolSchema(Schema schema) {
        this.schema = schema;
    }

    /** Compiles a schema; IllegalArgumentException when the validator cannot read it. */
    static ToolSchema of(Map<String, Object> schema) {
        try {
            Schema s = REGISTRY.getSchema(Values.tree(schema));
            s.initializeValidators();
            return new ToolSchema(s);
        } catch (RuntimeException e) {
            throw new IllegalArgumentException("it is not a JSON Schema the validator reads: " + e.getMessage(), e);
        }
    }

    /** What is wrong with a value, or nothing. */
    List<String> problems(Object value) {
        List<String> out = new ArrayList<>();
        for (Error e : schema.validate(Values.tree(value))) {
            String at = e.getInstanceLocation() == null ? "" : e.getInstanceLocation().toString();
            out.add(at.isEmpty() ? e.getMessage() : at + ": " + e.getMessage());
        }
        return out;
    }
}
