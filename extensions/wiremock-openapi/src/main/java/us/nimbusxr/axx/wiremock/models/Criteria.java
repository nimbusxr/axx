// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import us.nimbusxr.axx.wiremock.models.ModelCall.Endpoint;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * What a stub's {@code model-request} matcher asks of a request: the texts it is about, the tool
 * whose result the model was just given, and its endpoint.
 */
record Criteria(List<String> about, String afterTool, Endpoint endpoint) {
    static final List<String> PARAMETERS = List.of("about", "afterTool", "endpoint");

    static Criteria of(Map<String, Object> parameters) {
        for (String k : parameters.keySet()) {
            if (!PARAMETERS.contains(k)) {
                throw new IllegalArgumentException("the model-request matcher has no parameter \"" + k + "\": its parameters are about, afterTool and endpoint");
            }
        }
        List<String> about = new ArrayList<>();
        Object a = parameters.get("about");
        if (a instanceof String s) {
            about.add(s);
        } else if (a instanceof List<?> l && l.stream().allMatch(String.class::isInstance)) {
            l.forEach(s -> about.add((String) s));
        } else if (a != null) {
            throw new IllegalArgumentException("the model-request matcher's about is a text, or a list of texts");
        }
        Object after = parameters.get("afterTool");
        if (after != null && !(after instanceof String)) {
            throw new IllegalArgumentException("the model-request matcher's afterTool is the name of a tool");
        }
        Object e = parameters.getOrDefault("endpoint", "chat");
        if (!(e instanceof String name)) {
            throw new IllegalArgumentException("the model-request matcher's endpoint is chat, embeddings or models");
        }
        Endpoint endpoint = Endpoint.of(name);
        if (after != null && endpoint != Endpoint.CHAT) {
            throw new IllegalArgumentException("the model-request matcher's afterTool is for chat requests");
        }
        if (!about.isEmpty() && endpoint == Endpoint.MODELS) {
            throw new IllegalArgumentException("the model-request matcher's about is for chat and embeddings requests");
        }
        return new Criteria(about, (String) after, endpoint);
    }

    /** Whether the call is one the stub answers. */
    boolean matches(ModelCall call) {
        if (call.kind().endpoint != endpoint) {
            return false;
        }
        String text = call.askedAbout();
        for (String s : about) {
            if (!text.contains(s)) {
                return false;
            }
        }
        if (endpoint != Endpoint.CHAT) {
            return true;
        }
        return afterTool == null ? call.conversation().lastTool().isEmpty() : call.conversation().lastTool().equals(Optional.of(afterTool));
    }
}
