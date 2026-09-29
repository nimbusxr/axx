// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static us.nimbusxr.axx.wiremock.models.Values.arr;
import static us.nimbusxr.axx.wiremock.models.Values.at;
import static us.nimbusxr.axx.wiremock.models.Values.obj;

import us.nimbusxr.axx.wiremock.models.Answer.Failure;
import us.nimbusxr.axx.wiremock.models.Answer.Stop;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;

import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.UUID;

/**
 * OpenAI's Chat Completions API, which OpenAI-compatible servers (Azure OpenAI, Ollama's and
 * vLLM's {@code /v1}, llama.cpp, LM Studio, Mistral, DeepSeek, OpenRouter...) speak too, with its
 * embeddings and models.
 */
final class OpenAiChat extends Wire {
    OpenAiChat(Memory memory) {
        super(memory);
    }

    @Override
    Rendered answer(ModelCall call, Answer a) {
        Map<String, Object> message = obj("role", "assistant", "content", content(a), "refusal", a.refusal());
        if (a.reasoning() != null) {
            // Servers that show reasoning disagree on its name: DeepSeek and vLLM's
            // reasoning_content, OpenRouter's and Ollama's reasoning.
            message.put("reasoning_content", a.reasoning());
            message.put("reasoning", a.reasoning());
        }
        if (!a.toolCalls().isEmpty()) {
            List<Object> calls = new ArrayList<>();
            for (int i = 0; i < a.toolCalls().size(); i++) {
                ToolCall c = a.toolCalls().get(i);
                calls.add(obj("id", callId("call_", c, i), "type", "function",
                        "function", obj("name", c.name(), "arguments", Values.write(c.arguments()))));
            }
            message.put("tool_calls", calls);
        }
        message.put("annotations", arr());
        Map<String, Object> body = obj("id", "chatcmpl-" + UUID.randomUUID(), "object", "chat.completion", "created", now(),
                "model", call.model(), "choices", arr(obj("index", 0, "message", message, "logprobs", null, "finish_reason", finish(a))),
                "usage", usage(call, a), "service_tier", "default");
        return Rendered.json(200, body).header("x-request-id", requestId());
    }

    @Override
    Rendered stream(ModelCall call, Answer a) {
        String id = "chatcmpl-" + UUID.randomUUID();
        long created = now();
        boolean usage = Boolean.TRUE.equals(at(call.body(), "stream_options", "include_usage"));
        List<Event> events = new ArrayList<>();
        events.add(chunk(id, created, call, usage, obj("role", "assistant", "content", "", "refusal", null), null));
        if (a.reasoning() != null) {
            for (String piece : Values.words(a.reasoning())) {
                events.add(chunk(id, created, call, usage, obj("reasoning_content", piece, "reasoning", piece), null));
            }
        }
        if (a.refusal() != null) {
            for (String piece : Values.words(a.refusal())) {
                events.add(chunk(id, created, call, usage, obj("refusal", piece), null));
            }
        }
        for (String piece : Values.words(a.content())) {
            events.add(chunk(id, created, call, usage, obj("content", piece), null));
        }
        for (int i = 0; i < a.toolCalls().size(); i++) {
            ToolCall c = a.toolCalls().get(i);
            events.add(chunk(id, created, call, usage, obj("tool_calls", arr(obj("index", i, "id", callId("call_", c, i), "type", "function",
                    "function", obj("name", c.name(), "arguments", "")))), null));
            for (String piece : Values.pieces(Values.write(c.arguments()))) {
                events.add(chunk(id, created, call, usage, obj("tool_calls", arr(obj("index", i, "function", obj("arguments", piece)))), null));
            }
        }
        events.add(chunk(id, created, call, usage, obj(), finish(a)));
        if (usage) {
            events.add(Event.of(null, obj("id", id, "object", "chat.completion.chunk", "created", created, "model", call.model(),
                    "choices", arr(), "usage", usage(call, a))));
        }
        events.add(Event.raw(null, "[DONE]"));
        events = Streams.shape(events, a, f -> Event.of(null, errorBody(f)));
        return Rendered.of(200, "text/event-stream; charset=utf-8", Streams.sse(events, false)).header("x-request-id", requestId());
    }

    private static Event chunk(String id, long created, ModelCall call, boolean usage, Map<String, Object> delta, String finish) {
        Map<String, Object> chunk = obj("id", id, "object", "chat.completion.chunk", "created", created, "model", call.model(),
                "choices", arr(obj("index", 0, "delta", delta, "logprobs", null, "finish_reason", finish)));
        if (usage) {
            chunk.put("usage", null);
        }
        return Event.of(null, chunk);
    }

    /** The message's content: null when the answer is only a refusal or tool calls. */
    private static String content(Answer a) {
        if (a.refusal() != null || (a.text() == null && a.json() == null && !a.toolCalls().isEmpty())) {
            return null;
        }
        return a.content();
    }

    private static String finish(Answer a) {
        if (a.stop() == Stop.LENGTH) {
            return "length";
        }
        if (a.stop() == Stop.SAFETY) {
            return "content_filter";
        }
        return a.toolCalls().isEmpty() ? "stop" : "tool_calls";
    }

    private static Map<String, Object> usage(ModelCall call, Answer a) {
        int in = inputTokens(call, a);
        int out = outputTokens(a);
        return obj("prompt_tokens", in, "completion_tokens", out, "total_tokens", in + out,
                "prompt_tokens_details", obj("cached_tokens", 0, "audio_tokens", 0, "cache_write_tokens", 0),
                "completion_tokens_details", obj("reasoning_tokens", reasoningTokens(a), "audio_tokens", 0,
                        "accepted_prediction_tokens", 0, "rejected_prediction_tokens", 0));
    }

    @Override
    Rendered error(ModelCall call, Failure f) {
        Rendered r = Rendered.json(status(f), errorBody(f)).header("x-request-id", requestId());
        return f.retryAfter() != null ? r.header("retry-after", String.valueOf(f.retryAfter())) : r;
    }

    static int status(Failure f) {
        return switch (f.type()) {
            case RATE_LIMIT -> 429;
            case OVERLOADED -> 503;
            case CONTEXT_LENGTH -> 400;
            case AUTH -> 401;
            case SERVER -> 500;
        };
    }

    /** OpenAI's error: {@code {"error": {"message", "type", "param", "code"}}}. */
    static Map<String, Object> errorBody(Failure f) {
        return obj("error", switch (f.type()) {
            case RATE_LIMIT -> obj("message", message(f, "Rate limit reached for requests. Please try again later."),
                    "type", "requests", "param", null, "code", "rate_limit_exceeded");
            case OVERLOADED -> obj("message", message(f, "The server is overloaded or not ready yet."),
                    "type", "server_error", "param", null, "code", null);
            case CONTEXT_LENGTH -> obj("message", message(f, "This model's maximum context length is 128000 tokens. However, your messages "
                    + "resulted in more tokens. Please reduce the length of the messages."),
                    "type", "invalid_request_error", "param", "messages", "code", "context_length_exceeded");
            case AUTH -> obj("message", message(f, "Incorrect API key provided."),
                    "type", "invalid_request_error", "param", null, "code", "invalid_api_key");
            case SERVER -> obj("message", message(f, "The server had an error while processing your request. Sorry about that!"),
                    "type", "server_error", "param", null, "code", null);
        });
    }

    @Override
    Rendered embeddings(ModelCall call, Answer a) {
        boolean base64 = "base64".equals(call.body().get("encoding_format"));
        List<Object> data = new ArrayList<>();
        List<List<Float>> vectors = vectors(call, a, 1536);
        for (int i = 0; i < vectors.size(); i++) {
            data.add(obj("object", "embedding", "index", i, "embedding", base64 ? base64(vectors.get(i)) : vectors.get(i)));
        }
        int tokens = a.usage() != null && a.usage().inputTokens() != null ? a.usage().inputTokens() : Values.tokens(String.join(" ", call.inputs()));
        return Rendered.json(200, obj("object", "list", "data", data, "model", call.model(),
                "usage", obj("prompt_tokens", tokens, "total_tokens", tokens))).header("x-request-id", requestId());
    }

    /** A vector as the base64 of its little-endian float32s, as OpenAI's base64 encoding sends it. */
    static String base64(List<Float> v) {
        ByteBuffer b = ByteBuffer.allocate(v.size() * 4).order(ByteOrder.LITTLE_ENDIAN);
        v.forEach(b::putFloat);
        return Base64.getEncoder().encodeToString(b.array());
    }

    @Override
    Rendered models(ModelCall call, Answer a) {
        List<Object> data = new ArrayList<>();
        for (String m : a.models()) {
            data.add(obj("id", m, "object", "model", "created", now(), "owned_by", "system"));
        }
        return Rendered.json(200, obj("object", "list", "data", data));
    }

    static String requestId() {
        return "req_" + UUID.randomUUID().toString().replace("-", "");
    }
}
