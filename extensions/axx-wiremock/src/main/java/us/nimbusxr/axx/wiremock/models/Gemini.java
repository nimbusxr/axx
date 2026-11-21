// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static us.nimbusxr.axx.wiremock.models.Values.arr;
import static us.nimbusxr.axx.wiremock.models.Values.obj;

import us.nimbusxr.axx.wiremock.models.Answer.Failure;
import us.nimbusxr.axx.wiremock.models.Answer.Stop;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;
import us.nimbusxr.axx.wiremock.models.ModelCall.Via;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.UUID;

/**
 * Gemini's API ({@code generateContent}), on Google AI Studio and on Vertex AI. It streams
 * server-sent events with {@code alt=sse}, and a JSON array without.
 */
final class Gemini extends Wire {
    Gemini(Memory memory) {
        super(memory);
    }

    @Override
    Rendered answer(ModelCall call, Answer a) {
        List<Object> parts = new ArrayList<>();
        if (a.reasoning() != null) {
            parts.add(obj("text", a.reasoning(), "thought", true));
        }
        parts.addAll(content(a));
        return Rendered.json(200, response(call, a, id(), parts, true));
    }

    @Override
    Rendered stream(ModelCall call, Answer a) {
        List<List<Object>> chunks = new ArrayList<>();
        if (a.reasoning() != null) {
            for (String piece : Values.words(a.reasoning())) {
                chunks.add(arr(obj("text", piece, "thought", true)));
            }
        }
        String text = a.refusal() != null ? a.refusal() : a.content();
        for (String piece : Values.words(text)) {
            chunks.add(arr(obj("text", piece)));
        }
        List<Object> calls = calls(a);
        if (!calls.isEmpty()) {
            chunks.add(calls);
        }
        if (chunks.isEmpty()) {
            chunks.add(arr(obj("text", "")));
        }
        String id = id();
        List<Event> events = new ArrayList<>();
        for (int i = 0; i < chunks.size(); i++) {
            events.add(Event.of(null, response(call, a, id, chunks.get(i), i == chunks.size() - 1)));
        }
        events = Streams.shape(events, a, f -> Event.of(null, errorBody(call, f)));
        if ("sse".equals(ModelCall.query(call.request(), "alt"))) {
            return Rendered.of(200, "text/event-stream", Streams.sse(events, false));
        }
        return Rendered.of(200, "application/json; charset=UTF-8", Streams.jsonArray(events));
    }

    /** The answer's parts, besides its thoughts: its text, or its refusal, and its function calls. */
    private List<Object> content(Answer a) {
        List<Object> parts = new ArrayList<>();
        if (a.refusal() != null) {
            parts.add(obj("text", a.refusal()));
        } else if (a.text() != null || a.json() != null || a.toolCalls().isEmpty()) {
            parts.add(obj("text", a.content()));
        }
        parts.addAll(calls(a));
        return parts;
    }

    private List<Object> calls(Answer a) {
        List<Object> parts = new ArrayList<>();
        for (ToolCall c : a.toolCalls()) {
            Map<String, Object> call = obj("name", c.name(), "args", c.arguments());
            if (c.id() != null) {
                call.put("id", c.id());
                memory.called(c.id(), c.name());
            }
            parts.add(obj("functionCall", call));
        }
        return parts;
    }

    /** A response, or a chunk of a stream: the last one says why it finished, and the tokens. */
    private static Map<String, Object> response(ModelCall call, Answer a, String id, List<Object> parts, boolean last) {
        Map<String, Object> candidate = obj("content", obj("parts", parts, "role", "model"));
        int in = inputTokens(call, a);
        Map<String, Object> usage = obj("promptTokenCount", in, "totalTokenCount", in);
        if (last) {
            candidate.put("finishReason", a.stop() == Stop.LENGTH ? "MAX_TOKENS" : a.stop() == Stop.SAFETY ? "SAFETY" : "STOP");
            if (a.stop() == Stop.SAFETY) {
                candidate.put("safetyRatings", arr(obj("category", "HARM_CATEGORY_DANGEROUS_CONTENT", "probability", "HIGH", "blocked", true)));
            }
            int out = outputTokens(a);
            int thoughts = reasoningTokens(a);
            usage = obj("promptTokenCount", in, "candidatesTokenCount", out - thoughts, "totalTokenCount", in + out);
            if (thoughts > 0) {
                usage.put("thoughtsTokenCount", thoughts);
            }
        }
        candidate.put("index", 0);
        return obj("candidates", arr(candidate), "usageMetadata", usage, "modelVersion", call.model(), "responseId", id);
    }

    @Override
    Rendered error(ModelCall call, Failure f) {
        return Rendered.json(status(call, f), errorBody(call, f));
    }

    private static int status(ModelCall call, Failure f) {
        return switch (f.type()) {
            case RATE_LIMIT -> 429;
            case OVERLOADED -> 503;
            case CONTEXT_LENGTH -> 400;
            case AUTH -> call.via() == Via.VERTEX ? 401 : 400;
            case SERVER -> 500;
        };
    }

    /**
     * Google's error: {@code {"error": {"code", "message", "status", "details"}}}. A bad key is a
     * 400 on AI Studio, and bad credentials a 401 on Vertex AI.
     */
    private static Map<String, Object> errorBody(ModelCall call, Failure f) {
        Map<String, Object> error = switch (f.type()) {
            case RATE_LIMIT -> obj("code", 429, "message", message(f, "Resource has been exhausted (e.g. check quota)."), "status", "RESOURCE_EXHAUSTED");
            case OVERLOADED -> obj("code", 503, "message", message(f, "The model is overloaded. Please try again later."), "status", "UNAVAILABLE");
            case CONTEXT_LENGTH -> obj("code", 400, "message", message(f,
                    "The input token count (1200000) exceeds the maximum number of tokens allowed (1048576)."), "status", "INVALID_ARGUMENT");
            case AUTH -> call.via() == Via.VERTEX
                    ? obj("code", 401, "message", message(f, "Request had invalid authentication credentials. Expected OAuth 2 access token, "
                            + "login cookie or other valid authentication credential."), "status", "UNAUTHENTICATED")
                    : obj("code", 400, "message", message(f, "API key not valid. Please pass a valid API key."), "status", "INVALID_ARGUMENT",
                            "details", arr(obj("@type", "type.googleapis.com/google.rpc.ErrorInfo", "reason", "API_KEY_INVALID",
                                    "domain", "googleapis.com")));
            case SERVER -> obj("code", 500, "message", message(f, "An internal error has occurred."), "status", "INTERNAL");
        };
        if (f.retryAfter() != null) {
            List<Object> details = new ArrayList<>(Values.list(error.get("details")));
            details.add(obj("@type", "type.googleapis.com/google.rpc.RetryInfo", "retryDelay", f.retryAfter() + "s"));
            error.put("details", details);
        }
        return obj("error", error);
    }

    @Override
    Rendered embeddings(ModelCall call, Answer a) {
        List<Object> embeddings = new ArrayList<>();
        for (List<Float> v : vectors(call, a, 3072)) {
            embeddings.add(obj("values", v));
        }
        if (call.request().getUrl().contains(":batchEmbedContents")) {
            return Rendered.json(200, obj("embeddings", embeddings));
        }
        return Rendered.json(200, obj("embedding", embeddings.get(0)));
    }

    @Override
    Rendered models(ModelCall call, Answer a) {
        List<Object> models = new ArrayList<>();
        for (String m : a.models()) {
            String name = m.startsWith("models/") ? m : "models/" + m;
            models.add(obj("name", name, "version", "001", "displayName", m, "description", m, "inputTokenLimit", 1048576,
                    "outputTokenLimit", 65536, "supportedGenerationMethods", arr("generateContent", "countTokens", "embedContent")));
        }
        return Rendered.json(200, obj("models", models));
    }

    private static String id() {
        return UUID.randomUUID().toString().replace("-", "").substring(0, 22);
    }
}
