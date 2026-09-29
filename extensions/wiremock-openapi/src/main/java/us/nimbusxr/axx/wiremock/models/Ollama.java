// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static us.nimbusxr.axx.wiremock.models.Values.arr;
import static us.nimbusxr.axx.wiremock.models.Values.obj;

import us.nimbusxr.axx.wiremock.models.Answer.Failure;
import us.nimbusxr.axx.wiremock.models.Answer.Stop;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;
import us.nimbusxr.axx.wiremock.models.ModelCall.Kind;

import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * Ollama's own API ({@code /api/chat}, {@code /api/generate}), which streams newline-delimited
 * JSON unless the request asks it not to. Ollama's OpenAI-compatible API ({@code /v1}) is
 * OpenAI's.
 */
final class Ollama extends Wire {
    Ollama(Memory memory) {
        super(memory);
    }

    @Override
    Rendered answer(ModelCall call, Answer a) {
        Map<String, Object> body = chunk(call, full(call, a));
        body.put("done", true);
        body.put("done_reason", a.stop() == Stop.LENGTH ? "length" : "stop");
        body.putAll(metrics(call, a));
        return Rendered.json(200, body);
    }

    @Override
    Rendered stream(ModelCall call, Answer a) {
        boolean chat = call.kind() == Kind.OLLAMA_CHAT;
        List<Event> events = new ArrayList<>();
        if (a.reasoning() != null) {
            for (String piece : Values.words(a.reasoning())) {
                events.add(Event.of(null, chunk(call, chat ? obj("role", "assistant", "content", "", "thinking", piece) : obj("response", "", "thinking", piece))));
            }
        }
        for (String piece : Values.words(text(a))) {
            events.add(Event.of(null, chunk(call, chat ? obj("role", "assistant", "content", piece) : obj("response", piece))));
        }
        if (!a.toolCalls().isEmpty()) {
            events.add(Event.of(null, chunk(call, obj("role", "assistant", "content", "", "tool_calls", calls(call, a)))));
        }
        Map<String, Object> last = chunk(call, chat ? obj("role", "assistant", "content", "") : obj("response", ""));
        last.put("done", true);
        last.put("done_reason", a.stop() == Stop.LENGTH ? "length" : "stop");
        last.putAll(metrics(call, a));
        events.add(Event.of(null, last));
        events = Streams.shape(events, a, f -> Event.of(null, obj("error", message(f))));
        return Rendered.of(200, "application/x-ndjson", Streams.ndjson(events));
    }

    /** A chunk of a stream, or the whole answer, with the model and the time. */
    private static Map<String, Object> chunk(ModelCall call, Map<String, Object> content) {
        Map<String, Object> out = obj("model", call.model(), "created_at", Instant.now().toString());
        if (call.kind() == Kind.OLLAMA_CHAT) {
            out.put("message", content);
        } else {
            out.putAll(content);
        }
        out.put("done", false);
        return out;
    }

    private List<Object> calls(ModelCall call, Answer a) {
        if (call.kind() != Kind.OLLAMA_CHAT) {
            throw new IllegalArgumentException("Ollama's /api/generate calls no tools: answer tool calls on /api/chat");
        }
        List<Object> calls = new ArrayList<>();
        for (int i = 0; i < a.toolCalls().size(); i++) {
            ToolCall c = a.toolCalls().get(i);
            calls.add(obj("id", callId("call_", c, i), "function", obj("index", i, "name", c.name(), "arguments", c.arguments())));
        }
        return calls;
    }

    /** The whole answer: the message of a chat, or the response of a generation. */
    private Map<String, Object> full(ModelCall call, Answer a) {
        if (call.kind() != Kind.OLLAMA_CHAT) {
            Map<String, Object> out = obj("response", text(a));
            if (a.reasoning() != null) {
                out.put("thinking", a.reasoning());
            }
            if (!a.toolCalls().isEmpty()) {
                calls(call, a);
            }
            return out;
        }
        Map<String, Object> message = obj("role", "assistant", "content", text(a));
        if (a.reasoning() != null) {
            message.put("thinking", a.reasoning());
        }
        if (!a.toolCalls().isEmpty()) {
            message.put("tool_calls", calls(call, a));
        }
        return message;
    }

    private static String text(Answer a) {
        return a.refusal() != null ? a.refusal() : a.content();
    }

    private static Map<String, Object> metrics(ModelCall call, Answer a) {
        return obj("total_duration", 1_200_000_000L, "load_duration", 100_000_000L, "prompt_eval_count", inputTokens(call, a),
                "prompt_eval_duration", 100_000_000L, "eval_count", outputTokens(a), "eval_duration", 1_000_000_000L);
    }

    /** Ollama's error: {@code {"error": "..."}}. */
    @Override
    Rendered error(ModelCall call, Failure f) {
        int status = switch (f.type()) {
            case RATE_LIMIT -> 429;
            case OVERLOADED -> 503;
            case CONTEXT_LENGTH -> 400;
            case AUTH -> 401;
            case SERVER -> 500;
        };
        Rendered r = Rendered.json(status, obj("error", message(f)));
        return f.retryAfter() != null ? r.header("Retry-After", String.valueOf(f.retryAfter())) : r;
    }

    private static String message(Failure f) {
        return message(f, switch (f.type()) {
            case RATE_LIMIT -> "too many requests, please try again later";
            case OVERLOADED -> "server busy, please try again.  maximum pending requests exceeded";
            case CONTEXT_LENGTH -> "the input length exceeds the context length";
            case AUTH -> "unauthorized";
            case SERVER -> "model runner has unexpectedly stopped, this may be due to resource limitations or an internal error, "
                    + "check ollama server logs for details";
        });
    }

    @Override
    Rendered embeddings(ModelCall call, Answer a) {
        List<List<Float>> vectors = vectors(call, a, 768);
        if (call.request().getUrl().contains("/api/embeddings")) {
            return Rendered.json(200, obj("embedding", vectors.get(0)));
        }
        return Rendered.json(200, obj("model", call.model(), "embeddings", new ArrayList<Object>(vectors), "total_duration", 20_000_000L,
                "load_duration", 1_000_000L, "prompt_eval_count", Values.tokens(String.join(" ", call.inputs()))));
    }

    @Override
    Rendered models(ModelCall call, Answer a) {
        List<Object> models = new ArrayList<>();
        for (String m : a.models()) {
            String name = m.contains(":") ? m : m + ":latest";
            String family = name.substring(0, name.indexOf(':'));
            models.add(obj("name", name, "model", name, "modified_at", "2025-01-01T00:00:00Z", "size", 2_019_393_189L,
                    "digest", Values.hash(64, name), "details", obj("parent_model", "", "format", "gguf", "family", family,
                            "families", arr(family), "parameter_size", "3.2B", "quantization_level", "Q4_K_M")));
        }
        return Rendered.json(200, obj("models", models));
    }
}
