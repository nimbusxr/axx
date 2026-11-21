// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;

/** An answer as the mock sends it: its status, its headers and its body. */
record Rendered(int status, Map<String, String> headers, byte[] body) {
    static Rendered json(int status, Object body) {
        return of(status, "application/json", Values.write(body).getBytes(StandardCharsets.UTF_8));
    }

    static Rendered of(int status, String contentType, byte[] body) {
        Map<String, String> headers = new LinkedHashMap<>();
        headers.put("Content-Type", contentType);
        return new Rendered(status, headers, body);
    }

    Rendered header(String name, String value) {
        Map<String, String> h = new LinkedHashMap<>(headers);
        h.put(name, value);
        return new Rendered(status, h, body);
    }

    /** The answer broken off in the middle of its body. */
    Rendered broken() {
        byte[] half = new byte[body.length / 2];
        System.arraycopy(body, 0, half, 0, half.length);
        return new Rendered(status, headers, half);
    }
}
