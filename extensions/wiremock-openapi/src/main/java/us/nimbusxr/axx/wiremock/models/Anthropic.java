// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static us.nimbusxr.axx.wiremock.models.Values.arr;
import static us.nimbusxr.axx.wiremock.models.Values.obj;

import us.nimbusxr.axx.wiremock.models.Answer.Failure;
import us.nimbusxr.axx.wiremock.models.Answer.Stop;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;
import us.nimbusxr.axx.wiremock.models.ModelCall.Via;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.UUID;

/**
 * Anthropic's Messages API, which Anthropic-compatible servers speak too, and which Anthropic's
 * models answer on Bedrock ({@code invoke}, streamed in AWS's event stream) and on Vertex AI
 * ({@code rawPredict}).
 */
final class Anthropic extends Wire {
    Anthropic(Memory memory) {
        super(memory);
    }

    @Override
    Rendered answer(ModelCall call, Answer a) {
        Map<String, Object> message = message(call, a, blocks(a), stopReason(a), usage(call, a));
        return headers(call, Rendered.json(200, message));
    }

    @Override
    Rendered stream(ModelCall call, Answer a) {
        List<Event> events = new ArrayList<>();
        int in = inputTokens(call, a);
        events.add(event("message_start", "message", message(call, a, arr(), null, obj("input_tokens", in, "output_tokens", 1,
                "cache_creation_input_tokens", 0, "cache_read_input_tokens", 0, "service_tier", "standard"))));
        events.add(event("ping"));
        List<Map<String, Object>> blocks = blocks(a);
        for (int i = 0; i < blocks.size(); i++) {
            Map<String, Object> block = blocks.get(i);
            switch ((String) block.get("type")) {
                case "thinking" -> {
                    events.add(event("content_block_start", "index", i, "content_block", obj("type", "thinking", "thinking", "", "signature", "")));
                    for (String piece : Values.words(a.reasoning())) {
                        events.add(event("content_block_delta", "index", i, "delta", obj("type", "thinking_delta", "thinking", piece)));
                    }
                    events.add(event("content_block_delta", "index", i, "delta", obj("type", "signature_delta", "signature", block.get("signature"))));
                }
                case "tool_use" -> {
                    events.add(event("content_block_start", "index", i, "content_block",
                            obj("type", "tool_use", "id", block.get("id"), "name", block.get("name"), "input", obj())));
                    for (String piece : Values.pieces(Values.write(block.get("input")))) {
                        events.add(event("content_block_delta", "index", i, "delta", obj("type", "input_json_delta", "partial_json", piece)));
                    }
                }
                default -> {
                    events.add(event("content_block_start", "index", i, "content_block", obj("type", "text", "text", "", "citations", null)));
                    for (String piece : Values.words((String) block.get("text"))) {
                        events.add(event("content_block_delta", "index", i, "delta", obj("type", "text_delta", "text", piece)));
                    }
                }
            }
            events.add(event("content_block_stop", "index", i));
        }
        int out = outputTokens(a);
        events.add(event("message_delta", "delta", obj("stop_reason", stopReason(a), "stop_sequence", null),
                "usage", obj("input_tokens", in, "output_tokens", out, "cache_creation_input_tokens", 0, "cache_read_input_tokens", 0)));
        Map<String, Object> stop = obj("type", "message_stop");
        if (call.via() == Via.BEDROCK) {
            stop.put("amazon-bedrock-invocationMetrics",
                    obj("inputTokenCount", in, "outputTokenCount", out, "invocationLatency", 1, "firstByteLatency", 1));
        }
        events.add(Event.of("message_stop", stop));
        if (call.via() == Via.BEDROCK) {
            List<Event> chunks = new ArrayList<>();
            for (Event e : events) {
                chunks.add(Event.of("chunk", obj("bytes", Base64.getEncoder().encodeToString(e.data().getBytes(StandardCharsets.UTF_8)))));
            }
            chunks = Streams.shape(chunks, a, Bedrock::exception);
            return Rendered.of(200, "application/vnd.amazon.eventstream", Streams.eventStream(chunks))
                    .header("x-amzn-RequestId", UUID.randomUUID().toString())
                    .header("X-Amzn-Bedrock-Content-Type", "application/json");
        }
        events = Streams.shape(events, a, f -> Event.of("error", errorBody(f)));
        return headers(call, Rendered.of(200, "text/event-stream; charset=utf-8", Streams.sse(events, true)));
    }

    private static Event event(String type, Object... kv) {
        Map<String, Object> data = obj("type", type);
        data.putAll(obj(kv));
        return Event.of(type, data);
    }

    private List<Map<String, Object>> blocks(Answer a) {
        List<Map<String, Object>> blocks = new ArrayList<>();
        if (a.reasoning() != null) {
            blocks.add(obj("type", "thinking", "thinking", a.reasoning(), "signature", signature(a.reasoning())));
        }
        if (a.refusal() != null) {
            blocks.add(obj("type", "text", "text", a.refusal(), "citations", null));
        } else if (a.text() != null || a.json() != null || a.toolCalls().isEmpty()) {
            blocks.add(obj("type", "text", "text", a.content(), "citations", null));
        }
        for (int i = 0; i < a.toolCalls().size(); i++) {
            ToolCall c = a.toolCalls().get(i);
            blocks.add(obj("type", "tool_use", "id", callId("toolu_", c, i), "name", c.name(), "input", c.arguments()));
        }
        return blocks;
    }

    /** An opaque signature of the thinking, as Anthropic signs it for the requests that send it back. */
    private static String signature(String thinking) {
        return Base64.getEncoder().encodeToString(Values.hash(64, thinking).getBytes(StandardCharsets.UTF_8));
    }

    private static Map<String, Object> message(ModelCall call, Answer a, List<?> content, String stopReason, Map<String, Object> usage) {
        return obj("id", "msg_" + UUID.randomUUID().toString().replace("-", ""), "type", "message", "role", "assistant",
                "model", call.model(), "content", content, "stop_reason", stopReason, "stop_sequence", null, "usage", usage);
    }

    private static String stopReason(Answer a) {
        if (a.stop() == Stop.LENGTH) {
            return "max_tokens";
        }
        if (a.stop() == Stop.SAFETY || a.refusal() != null) {
            return "refusal";
        }
        return a.toolCalls().isEmpty() ? "end_turn" : "tool_use";
    }

    private static Map<String, Object> usage(ModelCall call, Answer a) {
        return obj("input_tokens", inputTokens(call, a), "output_tokens", outputTokens(a), "cache_creation_input_tokens", 0,
                "cache_read_input_tokens", 0, "cache_creation", obj("ephemeral_5m_input_tokens", 0, "ephemeral_1h_input_tokens", 0),
                "server_tool_use", null, "service_tier", "standard");
    }

    private static Rendered headers(ModelCall call, Rendered r) {
        if (call.via() == Via.BEDROCK) {
            return r.header("x-amzn-RequestId", UUID.randomUUID().toString());
        }
        return r.header("request-id", "req_" + UUID.randomUUID().toString().replace("-", ""));
    }

    @Override
    Rendered error(ModelCall call, Failure f) {
        if (call.via() == Via.BEDROCK) {
            return new Bedrock(memory).error(call, f);
        }
        int status = switch (f.type()) {
            case RATE_LIMIT -> 429;
            case OVERLOADED -> 529;
            case CONTEXT_LENGTH -> 400;
            case AUTH -> 401;
            case SERVER -> 500;
        };
        Rendered r = headers(call, Rendered.json(status, errorBody(f)));
        return f.retryAfter() != null ? r.header("retry-after", String.valueOf(f.retryAfter())) : r;
    }

    /** Anthropic's error: {@code {"type": "error", "error": {"type", "message"}}}. */
    private static Map<String, Object> errorBody(Failure f) {
        Map<String, Object> error = switch (f.type()) {
            case RATE_LIMIT -> obj("type", "rate_limit_error", "message", message(f, "Number of request tokens has exceeded your per-minute rate limit."));
            case OVERLOADED -> obj("type", "overloaded_error", "message", message(f, "Overloaded"));
            case CONTEXT_LENGTH -> obj("type", "invalid_request_error", "message", message(f, "prompt is too long: 210000 tokens > 200000 maximum"));
            case AUTH -> obj("type", "authentication_error", "message", message(f, "invalid x-api-key"));
            case SERVER -> obj("type", "api_error", "message", message(f, "Internal server error"));
        };
        return obj("type", "error", "error", error, "request_id", "req_" + UUID.randomUUID().toString().replace("-", ""));
    }

    @Override
    Rendered embeddings(ModelCall call, Answer a) {
        throw new IllegalStateException("Anthropic's API has no embeddings");
    }

    @Override
    Rendered models(ModelCall call, Answer a) {
        List<Object> data = new ArrayList<>();
        for (String m : a.models()) {
            data.add(obj("type", "model", "id", m, "display_name", m, "created_at", "2025-01-01T00:00:00Z"));
        }
        List<String> names = a.models();
        return headers(call, Rendered.json(200, obj("data", data, "has_more", false,
                "first_id", names.isEmpty() ? null : names.get(0), "last_id", names.isEmpty() ? null : names.get(names.size() - 1))));
    }
}
