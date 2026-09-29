// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * What the mock remembers of its answers: the tools it called, by call id, and the conversations
 * of the Responses API's responses, which a request continues by naming the response ({@code
 * previous_response_id}) instead of sending it again. Both keep their latest entries only.
 */
final class Memory {
    private static final int SIZE = 10_000;

    private final Map<String, String> tools = lru();
    private final Map<String, Conversation> responses = lru();

    void called(String callId, String tool) {
        tools.put(callId, tool);
    }

    String toolName(String callId) {
        return tools.get(callId);
    }

    void responded(String responseId, Conversation conversation) {
        responses.put(responseId, conversation);
    }

    Conversation response(String responseId) {
        return responses.get(responseId);
    }

    private static <V> Map<String, V> lru() {
        return Collections.synchronizedMap(new LinkedHashMap<>(256, 0.75f, true) {
            @Override
            protected boolean removeEldestEntry(Map.Entry<String, V> eldest) {
                return size() > SIZE;
            }
        });
    }
}
