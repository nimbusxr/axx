// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.mcp;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;

import us.nimbusxr.axx.wiremock.json.JsonObjects;
import us.nimbusxr.axx.wiremock.json.JsonText;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * JSON values as maps and lists (in their order, nulls kept), read from JSON or YAML, and as
 * Jackson trees for the JSON Schema validator.
 */
final class Values {
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final ObjectMapper YAML = new ObjectMapper(new YAMLFactory());

    private Values() {}

    /** Parses JSON text; throws IllegalArgumentException when it is not JSON. */
    static Object parse(String text) {
        try {
            return JSON.readValue(text, Object.class);
        } catch (JsonProcessingException e) {
            throw new IllegalArgumentException(e.getOriginalMessage(), e);
        }
    }

    /** Parses YAML text, which JSON text is too. */
    static Object parseYaml(String text) {
        try {
            return YAML.readValue(text, Object.class);
        } catch (JsonProcessingException e) {
            throw new IllegalArgumentException(e.getOriginalMessage(), e);
        }
    }

    static JsonNode tree(Object value) {
        return JSON.valueToTree(value);
    }

    static String write(Object value) {
        return JsonText.write(value);
    }

    /** An object of keys and values, in their order, nulls kept. */
    static Map<String, Object> obj(Object... kv) {
        return JsonObjects.of(kv);
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> map(Object v) {
        return v instanceof Map<?, ?> m ? (Map<String, Object>) m : null;
    }

    static List<Object> list(Object v) {
        return v instanceof List<?> l ? new ArrayList<>(l) : null;
    }

    static String string(Object v) {
        return v instanceof String s ? s : null;
    }
}
