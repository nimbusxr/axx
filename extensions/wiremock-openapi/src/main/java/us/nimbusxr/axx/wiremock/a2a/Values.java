// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import com.github.tomakehurst.wiremock.common.Json;

import us.nimbusxr.axx.wiremock.json.JsonText;

import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** JSON values as maps and lists (in their order, nulls kept), and the A2A conventions for them. */
final class Values {
    private Values() {}

    /** Parses JSON text; IllegalArgumentException when it is not JSON. */
    static Object parse(String text) {
        try {
            return Json.read(text, Object.class);
        } catch (RuntimeException e) {
            throw new IllegalArgumentException(e.getMessage(), e);
        }
    }

    static String write(Object v) {
        return JsonText.write(v);
    }

    /** An object of keys and values, in their order, nulls kept. */
    static Map<String, Object> obj(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> map(Object v) {
        return v instanceof Map<?, ?> m ? (Map<String, Object>) m : null;
    }

    static List<Object> list(Object v) {
        return v instanceof List<?> l ? new ArrayList<>(l) : null;
    }

    /** A string that says something: null for none and for the empty string, which ProtoJSON writes for an unset field. */
    static String text(Object v) {
        return v instanceof String s && !s.isEmpty() ? s : null;
    }

    /** Now, as A2A's timestamps are: ISO 8601 in UTC, to the millisecond. */
    static String now() {
        return Instant.now().truncatedTo(ChronoUnit.MILLIS).toString();
    }
}
