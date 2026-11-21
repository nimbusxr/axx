// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import com.github.tomakehurst.wiremock.common.Json;

import java.util.List;
import java.util.Map;

/** Reads the extension's problem+json body and journal records in tests. */
final class ProblemBody {
    private ProblemBody() {}

    @SuppressWarnings("unchecked")
    static Map<String, Object> read(String json) {
        return Json.read(json, Map.class);
    }

    @SuppressWarnings("unchecked")
    static List<Map<String, Object>> findings(String json) {
        return (List<Map<String, Object>>) read(json).get("findings");
    }
}
