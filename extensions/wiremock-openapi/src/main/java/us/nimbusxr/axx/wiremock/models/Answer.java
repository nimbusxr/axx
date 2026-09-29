// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * A stub's answer, written the same way for every provider: the mock renders it in the format of
 * the request it answers.
 *
 * @param text the answer's text
 * @param json the answer's structured output, sent as its JSON text
 * @param toolCalls the tools the model calls
 * @param reasoning the model's reasoning, as the provider shows it
 * @param refusal the model's refusal, in the provider's refusal field where it has one
 * @param stop why the answer stopped early: at its length, or for safety
 * @param usage the tokens the answer reports, which are estimated from the text when not given
 * @param malformed whether the answer breaks off in the middle of its JSON
 * @param cutOffAfter the events a stream ends after, without its end
 * @param error the provider's error the request gets instead of an answer
 * @param embedding the vector every text of an embeddings request gets
 * @param dimensions the dimensions of the vectors of an embeddings request that asks for none
 * @param models the models a models request lists
 */
record Answer(
        String text,
        Object json,
        List<ToolCall> toolCalls,
        String reasoning,
        String refusal,
        Stop stop,
        Usage usage,
        boolean malformed,
        Integer cutOffAfter,
        Failure error,
        List<Double> embedding,
        Integer dimensions,
        List<String> models) {

    /** Why an answer stopped before its end. */
    enum Stop {
        LENGTH,
        SAFETY
    }

    /** The tokens an answer reports. */
    record Usage(Integer inputTokens, Integer outputTokens, Integer reasoningTokens) {}

    /**
     * A provider's error, in its own shape: its kind, its message (the provider's usual one when
     * not given), the seconds to wait before retrying, and for a stream, the events it sends
     * first.
     */
    record Failure(Type type, String message, Integer retryAfter, Integer afterEvents) {
        enum Type {
            RATE_LIMIT("rate_limit"),
            OVERLOADED("overloaded"),
            CONTEXT_LENGTH("context_length"),
            AUTH("auth"),
            SERVER("server");

            final String key;

            Type(String key) {
                this.key = key;
            }
        }
    }

    static final Set<String> KEYS = Set.of("text", "json", "toolCalls", "reasoning", "refusal", "stop", "usage", "malformed",
            "cutOffAfter", "error", "embedding", "dimensions", "models");

    /** The text the answer sends: its text, or its structured output's JSON. */
    String content() {
        if (json != null) {
            return Values.write(json);
        }
        return text == null ? "" : text;
    }

    /** Reads a stub's answer: a JSON object, or nothing for an empty answer. */
    static Answer read(String body) {
        if (body == null || body.isBlank()) {
            return new Answer(null, null, List.of(), null, null, null, null, false, null, null, null, null, List.of());
        }
        Object parsed = Values.parse(body);
        if (!(parsed instanceof Map<?, ?>)) {
            throw new IllegalArgumentException("the model answer is a JSON object, with " + keys() + ", not: " + body);
        }
        Map<String, Object> m = Values.map(parsed);
        for (String k : m.keySet()) {
            if (!KEYS.contains(k)) {
                throw new IllegalArgumentException("the model answer has no key \"" + k + "\": its keys are " + keys());
            }
        }
        String text = string(m, "text");
        Object json = m.get("json");
        String refusal = string(m, "refusal");
        if (text != null && json != null) {
            throw new IllegalArgumentException("the model answer has text or json, not both");
        }
        if (refusal != null && (text != null || json != null)) {
            throw new IllegalArgumentException("the model answer's refusal is its text: it has no text or json besides");
        }
        List<ToolCall> calls = new ArrayList<>();
        for (Object o : list(m, "toolCalls")) {
            if (!(o instanceof Map<?, ?> c) || !(c.get("name") instanceof String name)) {
                throw new IllegalArgumentException("each of the model answer's toolCalls has a name, and its arguments: " + Values.write(o));
            }
            for (Object k : c.keySet()) {
                if (!List.of("name", "arguments", "id").contains(k)) {
                    throw new IllegalArgumentException("a tool call of the model answer has no key \"" + k + "\": its keys are name, arguments and id");
                }
            }
            Object args = c.get("arguments");
            if (args != null && !(args instanceof Map<?, ?>)) {
                throw new IllegalArgumentException("the arguments of the model answer's " + name + " call are a JSON object");
            }
            calls.add(new ToolCall(c.get("id") instanceof String id ? id : null, name, args == null ? Map.of() : args));
        }
        Stop stop = null;
        if (m.get("stop") != null) {
            stop = switch (String.valueOf(m.get("stop"))) {
                case "length" -> Stop.LENGTH;
                case "safety" -> Stop.SAFETY;
                default -> throw new IllegalArgumentException("the model answer's stop is length or safety, not \"" + m.get("stop") + "\"");
            };
        }
        Usage usage = null;
        if (m.get("usage") != null) {
            Map<String, Object> u = object(m, "usage", List.of("inputTokens", "outputTokens", "reasoningTokens"));
            usage = new Usage(integer(u, "inputTokens"), integer(u, "outputTokens"), integer(u, "reasoningTokens"));
        }
        Failure error = null;
        if (m.get("error") != null) {
            Map<String, Object> e = object(m, "error", List.of("type", "message", "retryAfter", "afterEvents"));
            Failure.Type type = null;
            for (Failure.Type t : Failure.Type.values()) {
                if (t.key.equals(e.get("type"))) {
                    type = t;
                }
            }
            if (type == null) {
                throw new IllegalArgumentException("the model answer's error type is rate_limit, overloaded, context_length, auth or server, not \""
                        + e.get("type") + "\"");
            }
            error = new Failure(type, string(e, "message"), integer(e, "retryAfter"), integer(e, "afterEvents"));
        }
        List<Double> embedding = null;
        if (m.get("embedding") != null) {
            embedding = new ArrayList<>();
            for (Object v : list(m, "embedding")) {
                if (!(v instanceof Number n)) {
                    throw new IllegalArgumentException("the model answer's embedding is a list of numbers");
                }
                embedding.add(n.doubleValue());
            }
        }
        List<String> models = new ArrayList<>();
        for (Object v : list(m, "models")) {
            if (!(v instanceof String s)) {
                throw new IllegalArgumentException("the model answer's models are the names of models");
            }
            models.add(s);
        }
        Object malformed = m.getOrDefault("malformed", false);
        if (!(malformed instanceof Boolean)) {
            throw new IllegalArgumentException("the model answer's malformed is true or false");
        }
        return new Answer(text, json, calls, string(m, "reasoning"), refusal, stop, usage, (Boolean) malformed,
                integer(m, "cutOffAfter"), error, embedding, integer(m, "dimensions"), models);
    }

    private static String keys() {
        return "text, json, toolCalls, reasoning, refusal, stop, usage, malformed, cutOffAfter, error, embedding, dimensions and models";
    }

    private static String string(Map<String, Object> m, String key) {
        Object v = m.get(key);
        if (v != null && !(v instanceof String)) {
            throw new IllegalArgumentException("the model answer's " + key + " is text");
        }
        return (String) v;
    }

    private static Integer integer(Map<String, Object> m, String key) {
        Object v = m.get(key);
        if (v == null) {
            return null;
        }
        if (!(v instanceof Number n) || n.doubleValue() != Math.floor(n.doubleValue()) || n.intValue() < 0) {
            throw new IllegalArgumentException("the model answer's " + key + " is a whole number, not " + Values.write(v));
        }
        return n.intValue();
    }

    private static List<Object> list(Map<String, Object> m, String key) {
        Object v = m.get(key);
        if (v != null && !(v instanceof List<?>)) {
            throw new IllegalArgumentException("the model answer's " + key + " is a list");
        }
        return Values.list(v);
    }

    private static Map<String, Object> object(Map<String, Object> m, String key, List<String> keys) {
        if (!(m.get(key) instanceof Map<?, ?>)) {
            throw new IllegalArgumentException("the model answer's " + key + " is an object, with " + String.join(", ", keys));
        }
        Map<String, Object> o = Values.map(m.get(key));
        for (String k : o.keySet()) {
            if (!keys.contains(k)) {
                throw new IllegalArgumentException("the model answer's " + key + " has no key \"" + k + "\": its keys are " + String.join(", ", keys));
            }
        }
        return o;
    }
}
