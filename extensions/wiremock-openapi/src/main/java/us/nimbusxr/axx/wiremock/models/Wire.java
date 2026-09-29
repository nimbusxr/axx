// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import us.nimbusxr.axx.wiremock.models.Answer.Failure;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;
import us.nimbusxr.axx.wiremock.models.ModelCall.Kind;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.List;

/** A provider's wire format: how its API answers, streams and fails. */
abstract class Wire {
    final Memory memory;

    Wire(Memory memory) {
        this.memory = memory;
    }

    static Wire of(Kind kind, Memory memory) {
        return switch (kind) {
            case OPENAI_CHAT, OPENAI_EMBEDDINGS, OPENAI_MODELS -> new OpenAiChat(memory);
            case OPENAI_RESPONSES -> new OpenAiResponses(memory);
            case ANTHROPIC_MESSAGES, ANTHROPIC_MODELS -> new Anthropic(memory);
            case GEMINI_GENERATE, GEMINI_EMBED, GEMINI_MODELS -> new Gemini(memory);
            case BEDROCK_CONVERSE, BEDROCK_EMBED -> new Bedrock(memory);
            case OLLAMA_CHAT, OLLAMA_GENERATE, OLLAMA_EMBED, OLLAMA_MODELS -> new Ollama(memory);
        };
    }

    /** The stub's answer to the call, in the call's format. */
    Rendered render(ModelCall call, Answer a) {
        if (a.error() != null && (!call.stream() || a.error().afterEvents() == null)) {
            return error(call, a.error());
        }
        Rendered r = switch (call.kind().endpoint) {
            case CHAT -> call.stream() ? stream(call, a) : answer(call, a);
            case EMBEDDINGS -> embeddings(call, a);
            case MODELS -> models(call, a);
        };
        return a.malformed() && !call.stream() ? r.broken() : r;
    }

    abstract Rendered answer(ModelCall call, Answer a);

    abstract Rendered stream(ModelCall call, Answer a);

    abstract Rendered error(ModelCall call, Failure f);

    abstract Rendered embeddings(ModelCall call, Answer a);

    abstract Rendered models(ModelCall call, Answer a);

    /** A tool call's id: the stub's, or one made from the call; the mock remembers which tool it names. */
    String callId(String prefix, ToolCall c, int index) {
        String id = c.id() != null ? c.id() : prefix + Values.hash(24, c.name(), Values.write(c.arguments()), index);
        memory.called(id, c.name());
        return id;
    }

    static int inputTokens(ModelCall call, Answer a) {
        if (a.usage() != null && a.usage().inputTokens() != null) {
            return a.usage().inputTokens();
        }
        return Math.max(1, Values.tokens(call.request().getBodyAsString()));
    }

    static int outputTokens(Answer a) {
        if (a.usage() != null && a.usage().outputTokens() != null) {
            return a.usage().outputTokens();
        }
        StringBuilder b = new StringBuilder(a.content());
        if (a.refusal() != null) {
            b.append(a.refusal());
        }
        for (ToolCall c : a.toolCalls()) {
            b.append(c.name()).append(Values.write(c.arguments()));
        }
        return Values.tokens(b.toString()) + reasoningTokens(a);
    }

    static int reasoningTokens(Answer a) {
        if (a.usage() != null && a.usage().reasoningTokens() != null) {
            return a.usage().reasoningTokens();
        }
        return Values.tokens(a.reasoning());
    }

    /** The error's message: the stub's, or the provider's usual one. */
    static String message(Failure f, String usual) {
        return f.message() != null ? f.message() : usual;
    }

    /**
     * A vector per text: the stub's embedding, or a unit vector made from the SHA-256 of the
     * text, so a text always gets the same vector. It has as many dimensions as the request asks
     * for, or else the stub's, or else the provider's usual.
     */
    static List<List<Float>> vectors(ModelCall call, Answer a, int usual) {
        int dims = call.dimensions() != null ? call.dimensions() : a.dimensions() != null ? a.dimensions() : usual;
        List<List<Float>> out = new ArrayList<>();
        for (String text : call.inputs()) {
            if (a.embedding() != null) {
                out.add(a.embedding().stream().map(Double::floatValue).toList());
            } else {
                out.add(vector(text, dims));
            }
        }
        return out;
    }

    static List<Float> vector(String text, int dims) {
        double[] v = new double[dims];
        double norm = 0;
        byte[] block = new byte[0];
        for (int i = 0; i < dims; i++) {
            if (i % 8 == 0) {
                block = sha256(text + "\0" + i / 8);
            }
            int o = (i % 8) * 4;
            int bits = (block[o] & 0xff) << 24 | (block[o + 1] & 0xff) << 16 | (block[o + 2] & 0xff) << 8 | block[o + 3] & 0xff;
            v[i] = bits / (double) Integer.MAX_VALUE;
            norm += v[i] * v[i];
        }
        List<Float> out = new ArrayList<>(dims);
        double length = Math.sqrt(norm);
        for (double x : v) {
            out.add((float) (length == 0 ? 0 : x / length));
        }
        return out;
    }

    private static byte[] sha256(String s) {
        try {
            return MessageDigest.getInstance("SHA-256").digest(s.getBytes(StandardCharsets.UTF_8));
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException(e);
        }
    }

    static long now() {
        return System.currentTimeMillis() / 1000;
    }
}
