// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static us.nimbusxr.axx.wiremock.models.Values.arr;
import static us.nimbusxr.axx.wiremock.models.Values.map;
import static us.nimbusxr.axx.wiremock.models.Values.obj;

import us.nimbusxr.axx.wiremock.models.Answer.Failure;
import us.nimbusxr.axx.wiremock.models.Answer.Stop;
import us.nimbusxr.axx.wiremock.models.Conversation.Message;
import us.nimbusxr.axx.wiremock.models.Conversation.ToolCall;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;

/**
 * OpenAI's Responses API. The mock remembers the conversations of its responses, so a request
 * that continues one ({@code previous_response_id}) is read with the conversation before it.
 */
final class OpenAiResponses extends Wire {
    OpenAiResponses(Memory memory) {
        super(memory);
    }

    /** An answer's output items, with their ids. */
    private record Output(Map<String, Object> reasoning, Map<String, Object> message, List<Map<String, Object>> calls) {
        List<Object> all() {
            List<Object> out = new ArrayList<>();
            if (reasoning != null) {
                out.add(reasoning);
            }
            if (message != null) {
                out.add(message);
            }
            out.addAll(calls);
            return out;
        }
    }

    @Override
    Rendered answer(ModelCall call, Answer a) {
        String id = id("resp_");
        Output output = output(a);
        remember(call, a, id, output);
        return Rendered.json(200, response(call, a, id, status(a), output.all(), true)).header("x-request-id", OpenAiChat.requestId());
    }

    @Override
    Rendered stream(ModelCall call, Answer a) {
        String id = id("resp_");
        Output output = output(a);
        remember(call, a, id, output);
        Events events = new Events();
        events.add("response.created", "response", response(call, a, id, "in_progress", arr(), false));
        events.add("response.in_progress", "response", response(call, a, id, "in_progress", arr(), false));
        int index = 0;
        if (output.reasoning != null) {
            Map<String, Object> item = output.reasoning;
            Object itemId = item.get("id");
            events.add("response.output_item.added", "output_index", index, "item", with(item, "summary", arr()));
            events.add("response.reasoning_summary_part.added", "item_id", itemId, "output_index", index, "summary_index", 0,
                    "part", obj("type", "summary_text", "text", ""));
            for (String piece : Values.words(a.reasoning())) {
                events.add("response.reasoning_summary_text.delta", "item_id", itemId, "output_index", index, "summary_index", 0, "delta", piece);
            }
            events.add("response.reasoning_summary_text.done", "item_id", itemId, "output_index", index, "summary_index", 0, "text", a.reasoning());
            events.add("response.reasoning_summary_part.done", "item_id", itemId, "output_index", index, "summary_index", 0,
                    "part", obj("type", "summary_text", "text", a.reasoning()));
            events.add("response.output_item.done", "output_index", index, "item", item);
            index++;
        }
        if (output.message != null) {
            Map<String, Object> item = output.message;
            Object itemId = item.get("id");
            events.add("response.output_item.added", "output_index", index,
                    "item", with(with(item, "status", "in_progress"), "content", arr()));
            if (a.refusal() != null) {
                events.add("response.content_part.added", "item_id", itemId, "output_index", index, "content_index", 0,
                        "part", obj("type", "refusal", "refusal", ""));
                for (String piece : Values.words(a.refusal())) {
                    events.add("response.refusal.delta", "item_id", itemId, "output_index", index, "content_index", 0, "delta", piece);
                }
                events.add("response.refusal.done", "item_id", itemId, "output_index", index, "content_index", 0, "refusal", a.refusal());
            } else {
                events.add("response.content_part.added", "item_id", itemId, "output_index", index, "content_index", 0,
                        "part", obj("type", "output_text", "text", "", "annotations", arr(), "logprobs", arr()));
                for (String piece : Values.words(a.content())) {
                    events.add("response.output_text.delta", "item_id", itemId, "output_index", index, "content_index", 0,
                            "delta", piece, "logprobs", arr());
                }
                events.add("response.output_text.done", "item_id", itemId, "output_index", index, "content_index", 0,
                        "text", a.content(), "logprobs", arr());
            }
            events.add("response.content_part.done", "item_id", itemId, "output_index", index, "content_index", 0,
                    "part", Values.list(item.get("content")).get(0));
            events.add("response.output_item.done", "output_index", index, "item", item);
            index++;
        }
        for (Map<String, Object> item : output.calls) {
            Object itemId = item.get("id");
            events.add("response.output_item.added", "output_index", index,
                    "item", with(with(item, "status", "in_progress"), "arguments", ""));
            for (String piece : Values.pieces((String) item.get("arguments"))) {
                events.add("response.function_call_arguments.delta", "item_id", itemId, "output_index", index, "delta", piece);
            }
            events.add("response.function_call_arguments.done", "item_id", itemId, "output_index", index, "arguments", item.get("arguments"));
            events.add("response.output_item.done", "output_index", index, "item", item);
            index++;
        }
        String status = status(a);
        events.add(status.equals("completed") ? "response.completed" : "response.incomplete",
                "response", response(call, a, id, status, output.all(), true));
        List<Event> shaped = Streams.shape(events.list, a, f -> Event.of("error", obj("type", "error", "code", code(f),
                "message", map(OpenAiChat.errorBody(f).get("error")).get("message"), "param", null,
                "sequence_number", Math.min(f.afterEvents(), events.list.size()))));
        return Rendered.of(200, "text/event-stream; charset=utf-8", Streams.sse(shaped, true)).header("x-request-id", OpenAiChat.requestId());
    }

    /** A stream's events, numbered in their order. */
    private static final class Events {
        final List<Event> list = new ArrayList<>();

        void add(String type, Object... kv) {
            Map<String, Object> data = obj("type", type, "sequence_number", list.size());
            data.putAll(obj(kv));
            list.add(Event.of(type, data));
        }
    }

    private Output output(Answer a) {
        Map<String, Object> reasoning = null;
        if (a.reasoning() != null) {
            reasoning = obj("id", id("rs_"), "type", "reasoning", "summary", arr(obj("type", "summary_text", "text", a.reasoning())));
        }
        Map<String, Object> message = null;
        if (a.refusal() != null || a.text() != null || a.json() != null || a.toolCalls().isEmpty()) {
            Map<String, Object> part = a.refusal() != null
                    ? obj("type", "refusal", "refusal", a.refusal())
                    : obj("type", "output_text", "text", a.content(), "annotations", arr(), "logprobs", arr());
            message = obj("id", id("msg_"), "type", "message", "status", a.stop() == null ? "completed" : "incomplete",
                    "role", "assistant", "content", arr(part));
        }
        List<Map<String, Object>> calls = new ArrayList<>();
        for (int i = 0; i < a.toolCalls().size(); i++) {
            ToolCall c = a.toolCalls().get(i);
            calls.add(obj("id", id("fc_"), "type", "function_call", "status", "completed", "arguments", Values.write(c.arguments()),
                    "call_id", callId("call_", c, i), "name", c.name()));
        }
        return new Output(reasoning, message, calls);
    }

    /** Remembers the response's conversation, for the requests that continue it. */
    private void remember(ModelCall call, Answer a, String id, Output output) {
        List<ToolCall> calls = new ArrayList<>();
        for (Map<String, Object> c : output.calls) {
            calls.add(new ToolCall((String) c.get("call_id"), (String) c.get("name"), Values.parse((String) c.get("arguments"))));
        }
        String text = a.refusal() != null ? a.refusal() : a.content();
        memory.responded(id, new Conversation(call.conversation().system(), List.of(new Message("assistant", text, calls, List.of())))
                .after(call.conversation()));
    }

    private static String status(Answer a) {
        return a.stop() == null ? "completed" : "incomplete";
    }

    private static Map<String, Object> response(ModelCall call, Answer a, String id, String status, List<Object> output, boolean done) {
        Map<String, Object> body = call.body();
        Object incomplete = null;
        if (a.stop() == Stop.LENGTH && done) {
            incomplete = obj("reason", "max_output_tokens");
        } else if (a.stop() == Stop.SAFETY && done) {
            incomplete = obj("reason", "content_filter");
        }
        Object usage = null;
        if (done) {
            int in = inputTokens(call, a);
            int out = outputTokens(a);
            usage = obj("input_tokens", in, "input_tokens_details", obj("cached_tokens", 0, "cache_write_tokens", 0), "output_tokens", out,
                    "output_tokens_details", obj("reasoning_tokens", reasoningTokens(a)), "total_tokens", in + out);
        }
        return obj("id", id, "object", "response", "created_at", now(), "status", status, "background", false,
                "completed_at", done ? now() : null, "error", null, "incomplete_details", incomplete,
                "instructions", body.get("instructions"), "max_output_tokens", body.get("max_output_tokens"), "max_tool_calls", null,
                "model", call.model(), "output", output, "parallel_tool_calls", body.getOrDefault("parallel_tool_calls", true),
                "previous_response_id", body.get("previous_response_id"),
                "reasoning", obj("effort", Values.at(body, "reasoning", "effort"), "summary", Values.at(body, "reasoning", "summary")),
                "service_tier", "default",
                "temperature", body.getOrDefault("temperature", 1.0), "text", body.getOrDefault("text", obj("format", obj("type", "text"))),
                "tool_choice", body.getOrDefault("tool_choice", "auto"), "tools", tools(body), "top_logprobs", 0,
                "top_p", body.getOrDefault("top_p", 1.0), "truncation", body.getOrDefault("truncation", "disabled"),
                "usage", usage, "user", null, "metadata", body.getOrDefault("metadata", obj()), "access_programs", null);
    }

    /** The request's tools, as a response shows them: a function tool with its strictness and parameters. */
    private static List<Object> tools(Map<String, Object> body) {
        List<Object> out = new ArrayList<>();
        for (Object t : Values.list(body.get("tools"))) {
            Map<String, Object> tool = new LinkedHashMap<>(map(t));
            if ("function".equals(tool.get("type"))) {
                tool.putIfAbsent("strict", true);
                tool.putIfAbsent("parameters", null);
                tool.putIfAbsent("description", null);
            }
            out.add(tool);
        }
        return out;
    }

    private static Map<String, Object> with(Map<String, Object> m, String key, Object value) {
        Map<String, Object> out = new LinkedHashMap<>(m);
        out.put(key, value);
        return out;
    }

    @Override
    Rendered error(ModelCall call, Failure f) {
        return new OpenAiChat(memory).error(call, f);
    }

    /** The code of an error event. */
    private static String code(Failure f) {
        return switch (f.type()) {
            case RATE_LIMIT -> "rate_limit_exceeded";
            case CONTEXT_LENGTH -> "context_length_exceeded";
            case AUTH -> "invalid_api_key";
            case OVERLOADED, SERVER -> "server_error";
        };
    }

    @Override
    Rendered embeddings(ModelCall call, Answer a) {
        throw new IllegalStateException("the Responses API has no embeddings");
    }

    @Override
    Rendered models(ModelCall call, Answer a) {
        throw new IllegalStateException("the Responses API has no models");
    }

    private static String id(String prefix) {
        return prefix + UUID.randomUUID().toString().replace("-", "");
    }
}
