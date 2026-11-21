// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static us.nimbusxr.axx.wiremock.models.Values.at;
import static us.nimbusxr.axx.wiremock.models.Values.list;
import static us.nimbusxr.axx.wiremock.models.Values.map;
import static us.nimbusxr.axx.wiremock.models.Values.string;
import static us.nimbusxr.axx.wiremock.models.Values.text;

import com.github.tomakehurst.wiremock.http.QueryParameter;
import com.github.tomakehurst.wiremock.http.Request;
import com.github.tomakehurst.wiremock.http.RequestMethod;

import us.nimbusxr.axx.wiremock.models.Conversation.Message;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolResult;

import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * A request to a model, read from any provider's API: which endpoint it is, whether it asks for a
 * stream, and what the model is given.
 *
 * @param kind the API and endpoint
 * @param via how the API is reached: directly, or through Bedrock or Vertex AI
 * @param stream whether the request asks for a stream
 * @param model the model the request names
 * @param conversation what a chat request gives the model
 * @param tools the names of the tools a chat request offers the model
 * @param schema the JSON Schema a chat request asks the answer to follow, or null
 * @param inputs the texts an embeddings request embeds
 * @param dimensions the dimensions an embeddings request asks for, or null
 * @param body the request's JSON body
 * @param request the request
 */
record ModelCall(
        Kind kind,
        Via via,
        boolean stream,
        String model,
        Conversation conversation,
        List<String> tools,
        Object schema,
        List<String> inputs,
        Integer dimensions,
        Map<String, Object> body,
        Request request) {

    /** The endpoints a model API has, which the stubs of the mock answer. */
    enum Endpoint {
        CHAT,
        EMBEDDINGS,
        MODELS;

        static Endpoint of(String name) {
            return switch (name) {
                case "chat" -> CHAT;
                case "embeddings" -> EMBEDDINGS;
                case "models" -> MODELS;
                default -> throw new IllegalArgumentException(
                        "the model-request endpoint is chat, embeddings or models, not \"" + name + "\"");
            };
        }
    }

    /** An endpoint of a provider's API. */
    enum Kind {
        OPENAI_CHAT(Endpoint.CHAT),
        OPENAI_RESPONSES(Endpoint.CHAT),
        OPENAI_EMBEDDINGS(Endpoint.EMBEDDINGS),
        OPENAI_MODELS(Endpoint.MODELS),
        ANTHROPIC_MESSAGES(Endpoint.CHAT),
        ANTHROPIC_MODELS(Endpoint.MODELS),
        GEMINI_GENERATE(Endpoint.CHAT),
        GEMINI_EMBED(Endpoint.EMBEDDINGS),
        GEMINI_MODELS(Endpoint.MODELS),
        BEDROCK_CONVERSE(Endpoint.CHAT),
        BEDROCK_EMBED(Endpoint.EMBEDDINGS),
        OLLAMA_CHAT(Endpoint.CHAT),
        OLLAMA_GENERATE(Endpoint.CHAT),
        OLLAMA_EMBED(Endpoint.EMBEDDINGS),
        OLLAMA_MODELS(Endpoint.MODELS);

        final Endpoint endpoint;

        Kind(Endpoint endpoint) {
            this.endpoint = endpoint;
        }
    }

    /** How a provider's API is reached: Anthropic's models also answer through Bedrock and Vertex AI. */
    enum Via {
        DIRECT,
        BEDROCK,
        VERTEX
    }

    private static final Pattern GEMINI_MODEL = Pattern.compile("/(?:models|tunedModels)/([^/:]+):");
    private static final Pattern BEDROCK_MODEL =
            Pattern.compile("/model/([^/]+)/(converse|converse-stream|invoke|invoke-with-response-stream)$");

    /** What the model is asked about: the conversation's text, or the texts to embed. */
    String askedAbout() {
        return switch (kind.endpoint) {
            case CHAT -> conversation.text();
            case EMBEDDINGS -> String.join("\n", inputs.stream().map(Values::searchable).toList());
            case MODELS -> "";
        };
    }

    /**
     * Reads a request to a model; null when it is none: a request to an endpoint of no model API
     * the mock knows.
     */
    static ModelCall read(Request request, Memory memory) {
        ModelCall call = find(request, memory);
        if (call == null || call.kind.endpoint != Endpoint.CHAT) {
            return call;
        }
        return new ModelCall(call.kind, call.via, call.stream, call.model, call.conversation.resolve(memory), call.tools,
                call.schema, call.inputs, call.dimensions, call.body, call.request);
    }

    private static ModelCall find(Request request, Memory memory) {
        String path = request.getUrl();
        int q = path.indexOf('?');
        if (q >= 0) {
            path = path.substring(0, q);
        }
        if (request.getMethod().equals(RequestMethod.GET)) {
            return models(request, path);
        }
        if (!request.getMethod().equals(RequestMethod.POST)) {
            return null;
        }
        Object parsed = Values.parse(request.getBodyAsString());
        if (!(parsed instanceof Map<?, ?>)) {
            return null;
        }
        Map<String, Object> body = map(parsed);
        Matcher bedrock = BEDROCK_MODEL.matcher(path);
        if (path.endsWith("/chat/completions")) {
            return openaiChat(request, body);
        } else if (path.endsWith("/responses")) {
            return openaiResponses(request, body, memory);
        } else if (path.endsWith("/api/embeddings")) {
            return embeddings(Kind.OLLAMA_EMBED, request, body, List.of(str(body.get("prompt"))), null);
        } else if (path.endsWith("/embeddings")) {
            return embeddings(Kind.OPENAI_EMBEDDINGS, request, body, openaiInputs(body.get("input")), integer(body.get("dimensions")));
        } else if (path.endsWith("/v1/messages")) {
            return anthropic(request, body, Via.DIRECT, str(body.get("model")), Boolean.TRUE.equals(body.get("stream")));
        } else if (path.contains("/publishers/anthropic/models/") && (path.endsWith(":rawPredict") || path.endsWith(":streamRawPredict"))) {
            String model = path.substring(path.indexOf("/publishers/anthropic/models/") + 29, path.lastIndexOf(':'));
            return anthropic(request, body, Via.VERTEX, model, path.endsWith(":streamRawPredict") || Boolean.TRUE.equals(body.get("stream")));
        } else if (path.endsWith(":generateContent") || path.endsWith(":streamGenerateContent")) {
            return gemini(request, body, path);
        } else if (path.endsWith(":embedContent") || path.endsWith(":batchEmbedContents")) {
            return geminiEmbeddings(request, body, path);
        } else if (bedrock.find()) {
            String model = URLDecoder.decode(bedrock.group(1), StandardCharsets.UTF_8);
            String op = bedrock.group(2);
            if (op.startsWith("converse")) {
                return bedrockConverse(request, body, model, op.equals("converse-stream"));
            }
            if (body.containsKey("anthropic_version")) {
                return anthropic(request, body, Via.BEDROCK, model, op.equals("invoke-with-response-stream"));
            }
            if (body.containsKey("inputText")) {
                return embeddings(Kind.BEDROCK_EMBED, request, body, List.of(str(body.get("inputText"))), integer(body.get("dimensions")));
            }
            if (body.get("texts") instanceof List<?> texts) {
                return embeddings(Kind.BEDROCK_EMBED, request, body, texts.stream().map(ModelCall::str).toList(), null);
            }
            return null;
        } else if (path.endsWith("/api/chat")) {
            return ollamaChat(request, body);
        } else if (path.endsWith("/api/generate")) {
            List<Message> messages = List.of(Message.of("user", str(body.get("prompt"))));
            return chat(Kind.OLLAMA_GENERATE, request, body, !Boolean.FALSE.equals(body.get("stream")),
                    str(body.get("model")), new Conversation(str(body.get("system")), messages), List.of(),
                    body.get("format") instanceof Map<?, ?> f ? f : null);
        } else if (path.endsWith("/api/embed")) {
            return embeddings(Kind.OLLAMA_EMBED, request, body, openaiInputs(body.get("input")), integer(body.get("dimensions")));
        }
        return null;
    }

    private static ModelCall models(Request request, String path) {
        Kind kind;
        if (path.endsWith("/api/tags")) {
            kind = Kind.OLLAMA_MODELS;
        } else if (!path.endsWith("/models")) {
            return null;
        } else if (request.containsHeader("anthropic-version")) {
            kind = Kind.ANTHROPIC_MODELS;
        } else if (request.containsHeader("x-goog-api-key") || query(request, "key") != null || path.contains("/v1beta/")) {
            kind = Kind.GEMINI_MODELS;
        } else {
            kind = Kind.OPENAI_MODELS;
        }
        return new ModelCall(kind, Via.DIRECT, false, "", new Conversation("", List.of()), List.of(), null, List.of(), null, Map.of(), request);
    }

    private static ModelCall openaiChat(Request request, Map<String, Object> body) {
        StringBuilder system = new StringBuilder();
        List<Message> messages = new ArrayList<>();
        for (Object o : list(body.get("messages"))) {
            Map<String, Object> m = map(o);
            String role = str(m.get("role"));
            String text = text(m.get("content"));
            switch (role) {
                case "system", "developer" -> append(system, text);
                case "tool" -> messages.add(new Message("tool", "", List.of(), List.of(new ToolResult(str(m.get("tool_call_id")), null, text))));
                case "function" -> messages.add(new Message("tool", "", List.of(), List.of(new ToolResult(null, str(m.get("name")), text))));
                case "assistant" -> {
                    List<ToolCall> calls = new ArrayList<>();
                    for (Object c : list(m.get("tool_calls"))) {
                        Map<String, Object> call = map(c);
                        Map<String, Object> fn = map(call.get("function"));
                        Map<String, Object> custom = map(call.get("custom"));
                        if (!fn.isEmpty()) {
                            calls.add(new ToolCall(str(call.get("id")), str(fn.get("name")), arguments(fn.get("arguments"))));
                        } else {
                            calls.add(new ToolCall(str(call.get("id")), str(custom.get("name")), custom.get("input")));
                        }
                    }
                    Map<String, Object> fc = map(m.get("function_call"));
                    if (!fc.isEmpty()) {
                        calls.add(new ToolCall(null, str(fc.get("name")), arguments(fc.get("arguments"))));
                    }
                    messages.add(new Message("assistant", join(text, str(m.get("refusal"))), calls, List.of()));
                }
                default -> messages.add(Message.of("user", text));
            }
        }
        List<String> tools = new ArrayList<>();
        for (Object t : list(body.get("tools"))) {
            String name = str(at(t, "function", "name"));
            tools.add(name.isEmpty() ? str(at(t, "custom", "name")) : name);
        }
        Object schema = "json_schema".equals(at(body, "response_format", "type")) ? at(body, "response_format", "json_schema", "schema") : null;
        return chat(Kind.OPENAI_CHAT, request, body, Boolean.TRUE.equals(body.get("stream")), str(body.get("model")),
                new Conversation(system.toString(), messages), tools, schema);
    }

    private static ModelCall openaiResponses(Request request, Map<String, Object> body, Memory memory) {
        StringBuilder system = new StringBuilder(text(instructions(body.get("instructions"))));
        List<Message> messages = new ArrayList<>();
        Object input = body.get("input");
        if (input instanceof String s) {
            messages.add(Message.of("user", s));
        }
        for (Object o : list(input)) {
            Map<String, Object> item = map(o);
            String type = str(item.get("type"));
            switch (type) {
                case "function_call" -> messages.add(new Message("assistant", "",
                        List.of(new ToolCall(str(item.get("call_id")), str(item.get("name")), arguments(item.get("arguments")))), List.of()));
                case "custom_tool_call" -> messages.add(new Message("assistant", "",
                        List.of(new ToolCall(str(item.get("call_id")), str(item.get("name")), item.get("input"))), List.of()));
                case "function_call_output", "custom_tool_call_output" -> messages.add(new Message("tool", "", List.of(),
                        List.of(new ToolResult(str(item.get("call_id")), null, text(item.get("output"))))));
                case "message", "" -> {
                    String role = str(item.get("role"));
                    String text = text(item.get("content"));
                    if (role.equals("system") || role.equals("developer")) {
                        append(system, text);
                    } else {
                        messages.add(Message.of(role.isEmpty() ? "user" : role, text));
                    }
                }
                default -> {
                    // Reasoning items and references to stored items carry nothing a stub matches.
                }
            }
        }
        Conversation conversation = new Conversation(system.toString(), messages);
        String previous = str(body.get("previous_response_id"));
        if (!previous.isEmpty() && memory.response(previous) != null) {
            conversation = conversation.after(memory.response(previous));
        }
        List<String> tools = new ArrayList<>();
        for (Object t : list(body.get("tools"))) {
            if (map(t).get("name") instanceof String name) {
                tools.add(name);
            }
        }
        Object schema = "json_schema".equals(at(body, "text", "format", "type")) ? at(body, "text", "format", "schema") : null;
        return chat(Kind.OPENAI_RESPONSES, request, body, Boolean.TRUE.equals(body.get("stream")), str(body.get("model")),
                conversation, tools, schema);
    }

    private static Object instructions(Object v) {
        if (!(v instanceof List<?> items)) {
            return v;
        }
        List<Object> out = new ArrayList<>();
        for (Object item : items) {
            out.add(text(map(item).get("content")));
        }
        return out;
    }

    private static ModelCall anthropic(Request request, Map<String, Object> body, Via via, String model, boolean stream) {
        List<Message> messages = new ArrayList<>();
        for (Object o : list(body.get("messages"))) {
            Map<String, Object> m = map(o);
            String role = str(m.get("role"));
            if (m.get("content") instanceof String s) {
                messages.add(Message.of(role, s));
                continue;
            }
            List<String> texts = new ArrayList<>();
            List<ToolCall> calls = new ArrayList<>();
            List<ToolResult> results = new ArrayList<>();
            for (Object b : list(m.get("content"))) {
                Map<String, Object> block = map(b);
                switch (str(block.get("type"))) {
                    case "text" -> texts.add(str(block.get("text")));
                    case "tool_use", "server_tool_use" -> calls.add(new ToolCall(str(block.get("id")), str(block.get("name")), block.get("input")));
                    case "tool_result" -> results.add(new ToolResult(str(block.get("tool_use_id")), null, text(block.get("content"))));
                    default -> {
                        // Thinking, images and documents carry nothing a stub matches.
                    }
                }
            }
            messages.add(new Message(role, String.join("\n", texts), calls, results));
        }
        List<String> tools = new ArrayList<>();
        for (Object t : list(body.get("tools"))) {
            tools.add(str(map(t).get("name")));
        }
        Object schema = at(body, "output_format", "schema");
        if (schema == null) {
            schema = at(body, "output_config", "format", "schema");
        }
        return new ModelCall(Kind.ANTHROPIC_MESSAGES, via, stream, model, new Conversation(text(body.get("system")), messages),
                tools, schema, List.of(), null, body, request);
    }

    private static ModelCall gemini(Request request, Map<String, Object> body, String path) {
        List<Message> messages = new ArrayList<>();
        for (Object o : list(body.get("contents"))) {
            Map<String, Object> c = map(o);
            List<String> texts = new ArrayList<>();
            List<ToolCall> calls = new ArrayList<>();
            List<ToolResult> results = new ArrayList<>();
            for (Object p : list(c.get("parts"))) {
                Map<String, Object> part = map(p);
                if (part.get("text") instanceof String t && !Boolean.TRUE.equals(part.get("thought"))) {
                    texts.add(t);
                }
                Map<String, Object> call = map(part.get("functionCall"));
                if (!call.isEmpty()) {
                    calls.add(new ToolCall(string(call.get("id")), str(call.get("name")), call.get("args")));
                }
                Map<String, Object> result = map(part.get("functionResponse"));
                if (!result.isEmpty()) {
                    results.add(new ToolResult(string(result.get("id")), str(result.get("name")), Values.write(result.get("response"))));
                }
            }
            String role = "model".equals(c.get("role")) ? "assistant" : "user";
            messages.add(new Message(role, String.join("\n", texts), calls, results));
        }
        List<String> tools = new ArrayList<>();
        for (Object t : list(body.get("tools"))) {
            for (Object d : list(map(t).get("functionDeclarations"))) {
                tools.add(str(map(d).get("name")));
            }
        }
        Map<String, Object> config = map(body.get("generationConfig"));
        Object schema = config.get("responseJsonSchema");
        if (schema == null) {
            schema = config.get("_responseJsonSchema");
        }
        if (schema == null) {
            schema = config.get("responseSchema");
        }
        Matcher model = GEMINI_MODEL.matcher(path);
        Via via = path.contains("/projects/") && path.contains("/locations/") ? Via.VERTEX : Via.DIRECT;
        return new ModelCall(Kind.GEMINI_GENERATE, via, path.endsWith(":streamGenerateContent"), model.find() ? model.group(1) : "",
                new Conversation(text(at(body, "systemInstruction", "parts")), messages), tools, schema, List.of(), null, body, request);
    }

    private static ModelCall geminiEmbeddings(Request request, Map<String, Object> body, String path) {
        List<String> inputs = new ArrayList<>();
        Integer dimensions = integer(body.get("outputDimensionality"));
        if (path.endsWith(":batchEmbedContents")) {
            for (Object r : list(body.get("requests"))) {
                inputs.add(text(at(r, "content", "parts")));
                if (dimensions == null) {
                    dimensions = integer(map(r).get("outputDimensionality"));
                }
            }
        } else {
            inputs.add(text(at(body, "content", "parts")));
        }
        Matcher model = GEMINI_MODEL.matcher(path);
        Via via = path.contains("/projects/") && path.contains("/locations/") ? Via.VERTEX : Via.DIRECT;
        return new ModelCall(Kind.GEMINI_EMBED, via, false, model.find() ? model.group(1) : "", new Conversation("", List.of()),
                List.of(), null, inputs, dimensions, body, request);
    }

    private static ModelCall bedrockConverse(Request request, Map<String, Object> body, String model, boolean stream) {
        List<Message> messages = new ArrayList<>();
        for (Object o : list(body.get("messages"))) {
            Map<String, Object> m = map(o);
            List<String> texts = new ArrayList<>();
            List<ToolCall> calls = new ArrayList<>();
            List<ToolResult> results = new ArrayList<>();
            for (Object b : list(m.get("content"))) {
                Map<String, Object> block = map(b);
                if (block.get("text") instanceof String t) {
                    texts.add(t);
                }
                Map<String, Object> use = map(block.get("toolUse"));
                if (!use.isEmpty()) {
                    calls.add(new ToolCall(str(use.get("toolUseId")), str(use.get("name")), use.get("input")));
                }
                Map<String, Object> result = map(block.get("toolResult"));
                if (!result.isEmpty()) {
                    results.add(new ToolResult(str(result.get("toolUseId")), null, text(result.get("content"))));
                }
            }
            messages.add(new Message(str(m.get("role")), String.join("\n", texts), calls, results));
        }
        List<String> tools = new ArrayList<>();
        for (Object t : list(at(body, "toolConfig", "tools"))) {
            if (at(t, "toolSpec", "name") instanceof String name) {
                tools.add(name);
            }
        }
        Object schema = at(body, "outputConfig", "textFormat", "structure", "jsonSchema", "schema");
        if (schema instanceof String s) {
            schema = Values.parse(s);
        }
        return new ModelCall(Kind.BEDROCK_CONVERSE, Via.DIRECT, stream, model, new Conversation(text(body.get("system")), messages),
                tools, schema, List.of(), null, body, request);
    }

    private static ModelCall ollamaChat(Request request, Map<String, Object> body) {
        StringBuilder system = new StringBuilder();
        List<Message> messages = new ArrayList<>();
        for (Object o : list(body.get("messages"))) {
            Map<String, Object> m = map(o);
            String role = str(m.get("role"));
            String content = str(m.get("content"));
            switch (role) {
                case "system" -> append(system, content);
                case "tool" -> messages.add(new Message("tool", "", List.of(),
                        List.of(new ToolResult(string(m.get("tool_call_id")), string(m.get("tool_name")), content))));
                case "assistant" -> {
                    List<ToolCall> calls = new ArrayList<>();
                    for (Object c : list(m.get("tool_calls"))) {
                        Map<String, Object> fn = map(map(c).get("function"));
                        calls.add(new ToolCall(string(map(c).get("id")), str(fn.get("name")), fn.get("arguments")));
                    }
                    messages.add(new Message("assistant", content, calls, List.of()));
                }
                default -> messages.add(Message.of("user", content));
            }
        }
        List<String> tools = new ArrayList<>();
        for (Object t : list(body.get("tools"))) {
            tools.add(str(at(t, "function", "name")));
        }
        return chat(Kind.OLLAMA_CHAT, request, body, !Boolean.FALSE.equals(body.get("stream")), str(body.get("model")),
                new Conversation(system.toString(), messages), tools, body.get("format") instanceof Map<?, ?> f ? f : null);
    }

    private static ModelCall chat(Kind kind, Request request, Map<String, Object> body, boolean stream, String model,
            Conversation conversation, List<String> tools, Object schema) {
        return new ModelCall(kind, Via.DIRECT, stream, model, conversation, tools, schema, List.of(), null, body, request);
    }

    private static ModelCall embeddings(Kind kind, Request request, Map<String, Object> body, List<String> inputs, Integer dimensions) {
        return new ModelCall(kind, Via.DIRECT, false, str(body.get("model")), new Conversation("", List.of()), List.of(), null,
                inputs, dimensions, body, request);
    }

    /** The texts of OpenAI's (and Ollama's) input: a string, strings, or tokens, which are embedded as their JSON. */
    private static List<String> openaiInputs(Object input) {
        if (input instanceof String s) {
            return List.of(s);
        }
        List<Object> items = list(input);
        if (!items.isEmpty() && items.get(0) instanceof Number) {
            return List.of(Values.write(items));
        }
        return items.stream().map(i -> i instanceof String s ? s : Values.write(i)).toList();
    }

    /** A tool call's arguments: JSON text, parsed, or the text when it is not JSON. */
    private static Object arguments(Object v) {
        if (v instanceof String s) {
            Object parsed = Values.parse(s);
            return parsed != null ? parsed : s;
        }
        return v;
    }

    /** A query parameter's first value, or null. */
    static String query(Request request, String name) {
        QueryParameter p = request.queryParameter(name);
        return p != null && p.isPresent() ? p.firstValue() : null;
    }

    private static Integer integer(Object v) {
        return v instanceof Number n ? n.intValue() : null;
    }

    private static String str(Object v) {
        return v instanceof String s ? s : "";
    }

    private static String join(String a, String b) {
        return b.isEmpty() ? a : a.isEmpty() ? b : a + "\n" + b;
    }

    private static void append(StringBuilder b, String text) {
        if (!b.isEmpty()) {
            b.append('\n');
        }
        b.append(text);
    }
}
