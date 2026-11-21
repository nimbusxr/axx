// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * One OpenAPI validation finding.
 *
 * @param key the validation key, e.g. {@code validation.request.body.schema.required}
 * @param level the level after the configured levels were applied
 * @param side whose part broke the contract: {@code request} (the caller), {@code response} (the
 *     stub) or {@code spec} (the spec could not be used)
 * @param message what is wrong
 * @param details further information from the validator
 */
record Finding(String key, Level level, String side, String message, List<String> details) {
    Finding withLevel(Level l) {
        return new Finding(key, l, side, message, details);
    }

    /** The JSON form recorded on the request journal (findings format 1). */
    Map<String, Object> toMap() {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("key", key);
        m.put("level", level.name());
        m.put("side", side);
        m.put("message", message);
        if (!details.isEmpty()) {
            m.put("details", details);
        }
        return m;
    }
}
