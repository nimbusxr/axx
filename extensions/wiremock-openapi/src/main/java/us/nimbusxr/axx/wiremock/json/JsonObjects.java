// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.json;

import java.util.LinkedHashMap;
import java.util.Map;

/** JSON objects as maps, written as keys and values. */
public final class JsonObjects {
    private JsonObjects() {}

    /** An object of keys and values, in their order, nulls kept. */
    public static Map<String, Object> of(Object... kv) {
        if (kv.length % 2 != 0) {
            throw new IllegalArgumentException("an object is keys and values, in pairs: " + kv.length + " is odd");
        }
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }
}
