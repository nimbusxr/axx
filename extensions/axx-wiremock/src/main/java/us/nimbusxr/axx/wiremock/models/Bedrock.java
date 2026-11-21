// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static us.nimbusxr.axx.wiremock.models.Values.obj;

import us.nimbusxr.axx.wiremock.models.Answer.Failure;
import us.nimbusxr.axx.wiremock.models.Answer.Stop;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.UUID;

/**
 * Amazon Bedrock's Converse API, streamed in AWS's event stream ({@code converse-stream}), and
 * the embeddings of its Titan and Cohere models ({@code invoke}).
 */
final class Bedrock extends Wire {
    Bedrock(Memory memory) {
        super(memory);
    }

    @Override
    Rendered answer(ModelCall call, Answer a) {
        int in = inputTokens(call, a);
        int out = outputTokens(a);
        return headers(Rendered.json(200, obj("output", obj("message", obj("role", "assistant", "content", blocks(a))),
                "stopReason", stopReason(a), "usage", obj("inputTokens", in, "outputTokens", out, "totalTokens", in + out),
                "metrics", obj("latencyMs", 1))));
    }

    @Override
    Rendered stream(ModelCall call, Answer a) {
        List<Event> events = new ArrayList<>();
        events.add(Event.of("messageStart", obj("role", "assistant")));
        List<Map<String, Object>> blocks = blocks(a);
        for (int i = 0; i < blocks.size(); i++) {
            Map<String, Object> block = blocks.get(i);
            if (block.containsKey("reasoningContent")) {
                for (String piece : Values.words(a.reasoning())) {
                    events.add(delta(i, obj("reasoningContent", obj("text", piece))));
                }
                events.add(delta(i, obj("reasoningContent", obj("signature", signature(a.reasoning())))));
            } else if (block.get("toolUse") instanceof Map<?, ?> use) {
                events.add(Event.of("contentBlockStart", obj("start", obj("toolUse", obj("toolUseId", use.get("toolUseId"), "name", use.get("name"))),
                        "contentBlockIndex", i)));
                for (String piece : Values.pieces(Values.write(use.get("input")))) {
                    events.add(delta(i, obj("toolUse", obj("input", piece))));
                }
            } else {
                for (String piece : Values.words((String) block.get("text"))) {
                    events.add(delta(i, obj("text", piece)));
                }
            }
            events.add(Event.of("contentBlockStop", obj("contentBlockIndex", i)));
        }
        events.add(Event.of("messageStop", obj("stopReason", stopReason(a))));
        int in = inputTokens(call, a);
        int out = outputTokens(a);
        events.add(Event.of("metadata", obj("usage", obj("inputTokens", in, "outputTokens", out, "totalTokens", in + out),
                "metrics", obj("latencyMs", 1))));
        events = Streams.shape(events, a, Bedrock::exception);
        return headers(Rendered.of(200, "application/vnd.amazon.eventstream", Streams.eventStream(events)));
    }

    private static Event delta(int index, Map<String, Object> delta) {
        return Event.of("contentBlockDelta", obj("delta", delta, "contentBlockIndex", index));
    }

    private List<Map<String, Object>> blocks(Answer a) {
        List<Map<String, Object>> blocks = new ArrayList<>();
        if (a.reasoning() != null) {
            blocks.add(obj("reasoningContent", obj("reasoningText", obj("text", a.reasoning(), "signature", signature(a.reasoning())))));
        }
        if (a.refusal() != null) {
            blocks.add(obj("text", a.refusal()));
        } else if (a.text() != null || a.json() != null || a.toolCalls().isEmpty()) {
            blocks.add(obj("text", a.content()));
        }
        for (int i = 0; i < a.toolCalls().size(); i++) {
            ToolCall c = a.toolCalls().get(i);
            blocks.add(obj("toolUse", obj("toolUseId", callId("tooluse_", c, i), "name", c.name(), "input", c.arguments())));
        }
        return blocks;
    }

    private static String signature(String reasoning) {
        return Base64.getEncoder().encodeToString(Values.hash(64, reasoning).getBytes(StandardCharsets.UTF_8));
    }

    private static String stopReason(Answer a) {
        if (a.stop() == Stop.LENGTH) {
            return "max_tokens";
        }
        if (a.stop() == Stop.SAFETY) {
            return "content_filtered";
        }
        return a.toolCalls().isEmpty() ? "end_turn" : "tool_use";
    }

    private static Rendered headers(Rendered r) {
        return r.header("x-amzn-RequestId", UUID.randomUUID().toString());
    }

    /** Bedrock's error: its type in the {@code x-amzn-ErrorType} header, and {@code {"message"}}. */
    @Override
    Rendered error(ModelCall call, Failure f) {
        int status = switch (f.type()) {
            case RATE_LIMIT -> 429;
            case OVERLOADED -> 503;
            case CONTEXT_LENGTH -> 400;
            case AUTH -> 403;
            case SERVER -> 500;
        };
        String type = switch (f.type()) {
            case RATE_LIMIT -> "ThrottlingException";
            case OVERLOADED -> "ServiceUnavailableException";
            case CONTEXT_LENGTH -> "ValidationException";
            case AUTH -> "UnrecognizedClientException";
            case SERVER -> "InternalServerException";
        };
        return headers(Rendered.json(status, obj("message", message(f))))
                .header("x-amzn-ErrorType", type + ":http://internal.amazon.com/coral/com.amazon.bedrock/");
    }

    /** An error in the middle of an event stream: an exception event. */
    static Event exception(Failure f) {
        String type = switch (f.type()) {
            case RATE_LIMIT -> "throttlingException";
            case OVERLOADED -> "serviceUnavailableException";
            case CONTEXT_LENGTH, AUTH -> "validationException";
            case SERVER -> "internalServerException";
        };
        return Event.exception(type, obj("message", message(f)));
    }

    private static String message(Failure f) {
        return message(f, switch (f.type()) {
            case RATE_LIMIT -> "Too many requests, please wait before trying again.";
            case OVERLOADED -> "Bedrock is unable to process your request.";
            case CONTEXT_LENGTH -> "Input is too long for requested model.";
            case AUTH -> "The security token included in the request is invalid.";
            case SERVER -> "The system encountered an unexpected error during processing. Try your request again.";
        });
    }

    /** Titan's embedding ({@code inputText}), or Cohere's ({@code texts}). */
    @Override
    Rendered embeddings(ModelCall call, Answer a) {
        List<List<Float>> vectors = vectors(call, a, 1024);
        if (call.body().containsKey("inputText")) {
            return headers(Rendered.json(200, obj("embedding", vectors.get(0), "embeddingsByType", obj("float", vectors.get(0)),
                    "inputTextTokenCount", Values.tokens(call.inputs().get(0)))));
        }
        return headers(Rendered.json(200, obj("id", UUID.randomUUID().toString(), "embeddings", new ArrayList<Object>(vectors),
                "response_type", "embeddings_floats", "texts", call.inputs())));
    }

    @Override
    Rendered models(ModelCall call, Answer a) {
        throw new IllegalStateException("Bedrock lists its models on its control plane, which the mock does not answer");
    }
}
