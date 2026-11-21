// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * A stub's answer to a message, written simply: a reply ({@code reply}, and {@code data}), a task
 * ({@code state}, {@code message}, {@code artifacts}, {@code updates}), or an {@code error}. The
 * mock renders it in A2A 1.0's shapes.
 *
 * @param reply the text of a message the agent answers with, instead of a task
 * @param data the data of that message, as a data part after its text
 * @param state the task's state, in A2A's short form ({@code completed}, {@code input-required}...)
 * @param message the text of the task's status message
 * @param artifacts the task's artifacts
 * @param updates the texts of the working updates a stream sends before the task's state
 * @param error the error the message gets instead
 */
record AgentAnswer(String reply, Object data, String state, String message, List<Artifact> artifacts, List<String> updates, Failure error) {
    /** An artifact: its name and description, and one part: text, data or a URL, with its media type. */
    record Artifact(String name, String description, String text, Object data, String url, String mediaType) {}

    /** An A2A or JSON-RPC error. */
    record Failure(int code, String message) {}

    static final Set<String> KEYS = Set.of("reply", "data", "state", "message", "artifacts", "updates", "error");
    static final List<String> STATES = List.of("completed", "input-required", "failed", "rejected", "auth-required", "working", "canceled");
    private static final Set<String> ARTIFACT_KEYS = Set.of("name", "description", "text", "data", "url", "mediaType");

    boolean isTask() {
        return state != null;
    }

    /** The proto name of a short state: {@code input-required} is {@code TASK_STATE_INPUT_REQUIRED}. */
    static String protoState(String state) {
        return "TASK_STATE_" + state.toUpperCase(java.util.Locale.ROOT).replace('-', '_');
    }

    /** The short name of a proto state. */
    static String shortState(String protoState) {
        return protoState.substring("TASK_STATE_".length()).toLowerCase(java.util.Locale.ROOT).replace('_', '-');
    }

    /** Reads a stub's answer; IllegalArgumentException says what is wrong with it. */
    static AgentAnswer read(String body) {
        Map<String, Object> m;
        try {
            m = body == null || body.isBlank() ? null : Values.map(Values.parse(body));
        } catch (IllegalArgumentException e) {
            m = null;
        }
        if (m == null) {
            throw new IllegalArgumentException("an agent's answer is a JSON object with a reply, a state or an error, not: " + body);
        }
        for (String k : m.keySet()) {
            if (!KEYS.contains(k)) {
                throw new IllegalArgumentException("an agent's answer has no key \"" + k + "\": its keys are reply and data (a message), "
                        + "state, message, artifacts and updates (a task), or error");
            }
        }
        if (m.get("error") != null) {
            if (m.size() > 1) {
                throw new IllegalArgumentException("an agent's answer is an error or an answer, not both");
            }
            Map<String, Object> e = Values.map(m.get("error"));
            if (e == null || !(e.get("code") instanceof Integer code) || !(e.get("message") instanceof String message)
                    || !Set.of("code", "message").containsAll(e.keySet())) {
                throw new IllegalArgumentException("an agent's error is {\"code\": <a whole number>, \"message\": \"...\"}, not " + Values.write(m.get("error")));
            }
            return new AgentAnswer(null, null, null, null, List.of(), List.of(), new Failure(code, message));
        }
        if (m.get("reply") != null) {
            if (!(m.get("reply") instanceof String reply)) {
                throw new IllegalArgumentException("an agent's reply is text");
            }
            for (String k : List.of("state", "message", "artifacts", "updates")) {
                if (m.containsKey(k)) {
                    throw new IllegalArgumentException("an agent's answer is a reply (reply, data) or a task (state, message, artifacts, updates), "
                            + "not both: it has a reply and " + k);
                }
            }
            return new AgentAnswer(reply, m.get("data"), null, null, List.of(), List.of(), null);
        }
        if (m.get("state") == null) {
            throw new IllegalArgumentException("an agent's answer has a reply (a message), a state (a task) or an error");
        }
        if (!(m.get("state") instanceof String state) || !STATES.contains(state)) {
            throw new IllegalArgumentException("a task's state is one of " + String.join(", ", STATES) + ", not " + Values.write(m.get("state")));
        }
        if (m.containsKey("data")) {
            throw new IllegalArgumentException("data is a reply's: a task's data is an artifact's, {\"artifacts\": [{\"name\": ..., \"data\": ...}]}");
        }
        if (m.get("message") != null && !(m.get("message") instanceof String)) {
            throw new IllegalArgumentException("a task's message is the text of its status message");
        }
        List<Artifact> artifacts = new ArrayList<>();
        if (m.get("artifacts") != null) {
            List<Object> list = Values.list(m.get("artifacts"));
            if (list == null) {
                throw new IllegalArgumentException("a task's artifacts are a list of {\"name\", \"description\", \"text\" | \"data\" | \"url\", \"mediaType\"}");
            }
            for (Object o : list) {
                artifacts.add(artifact(o));
            }
        }
        List<String> updates = new ArrayList<>();
        if (m.get("updates") != null) {
            List<Object> list = Values.list(m.get("updates"));
            if (list == null || !list.stream().allMatch(u -> u instanceof String)) {
                throw new IllegalArgumentException("a task's updates are the texts of its working updates, a list of strings");
            }
            list.forEach(u -> updates.add((String) u));
        }
        return new AgentAnswer(null, null, state, (String) m.get("message"), artifacts, updates, null);
    }

    private static Artifact artifact(Object o) {
        Map<String, Object> a = Values.map(o);
        if (a == null || !(a.get("name") instanceof String name)) {
            throw new IllegalArgumentException("each artifact of a task has a name: " + Values.write(o));
        }
        for (String k : a.keySet()) {
            if (!ARTIFACT_KEYS.contains(k)) {
                throw new IllegalArgumentException("the artifact " + name + " has no key \"" + k + "\": its keys are name, description, text, data, url "
                        + "and mediaType");
            }
        }
        long parts = List.of("text", "data", "url").stream().filter(a::containsKey).count();
        if (parts != 1) {
            throw new IllegalArgumentException("the artifact " + name + " has one of text, data or url");
        }
        for (String k : List.of("description", "text", "url", "mediaType")) {
            if (a.get(k) != null && !(a.get(k) instanceof String)) {
                throw new IllegalArgumentException("the artifact " + name + "'s " + k + " is text");
            }
        }
        return new Artifact(name, (String) a.get("description"), (String) a.get("text"), a.get("data"), (String) a.get("url"), (String) a.get("mediaType"));
    }
}
