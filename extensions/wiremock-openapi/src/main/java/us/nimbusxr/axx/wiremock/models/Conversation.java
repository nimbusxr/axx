// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * What a model was given, read from any provider's request: its system prompt and its messages,
 * with the tools it called and what they answered.
 */
record Conversation(String system, List<Message> messages) {
    /** A message: its role (user, assistant or tool), its text, and the tool calls and results in it. */
    record Message(String role, String text, List<ToolCall> calls, List<ToolResult> results) {
        Message {
            text = text == null ? "" : text;
            calls = List.copyOf(calls);
            results = List.copyOf(results);
        }

        static Message of(String role, String text) {
            return new Message(role, text, List.of(), List.of());
        }
    }

    /** A tool the model called: its id, its name, and its arguments (a JSON value). */
    record ToolCall(String id, String name, Object arguments) {}

    /**
     * What a tool answered: the id of the call it answers, the tool's name (null until it is
     * resolved from the call), and its text.
     */
    record ToolResult(String id, String name, String text) {}

    Conversation {
        system = system == null ? "" : system;
        messages = List.copyOf(messages);
    }

    /** This conversation after an earlier one: its messages first. */
    Conversation after(Conversation earlier) {
        List<Message> all = new ArrayList<>(earlier.messages);
        all.addAll(messages);
        return new Conversation(system, all);
    }

    /**
     * Names the tools of results that name only their call, from the calls earlier in the
     * conversation, or else from the calls the mock answered.
     */
    Conversation resolve(Memory memory) {
        Map<String, String> names = new HashMap<>();
        List<Message> out = new ArrayList<>();
        for (Message m : messages) {
            for (ToolCall c : m.calls) {
                if (c.id() != null) {
                    names.put(c.id(), c.name());
                }
            }
            List<ToolResult> results = new ArrayList<>();
            for (ToolResult r : m.results) {
                String name = r.name();
                if (name == null && r.id() != null) {
                    name = names.getOrDefault(r.id(), memory.toolName(r.id()));
                }
                results.add(new ToolResult(r.id(), name, r.text()));
            }
            out.add(new Message(m.role, m.text, m.calls, results));
        }
        return new Conversation(system, out);
    }

    /** Everything the model was given as text: what an answer is "about". */
    String text() {
        StringBuilder b = new StringBuilder(Values.searchable(system));
        for (Message m : messages) {
            b.append('\n').append(Values.searchable(m.text));
            for (ToolCall c : m.calls) {
                b.append('\n').append(c.name()).append('\n').append(Values.searchable(Values.write(c.arguments())));
            }
            for (ToolResult r : m.results) {
                b.append('\n').append(Values.searchable(r.text()));
            }
        }
        return b.toString();
    }

    /**
     * The tool whose result the model was given last in this turn, since the user last wrote: an
     * answer "after" that tool. Empty when the model has had no tool result since, and an empty
     * name when the result names no call the mock knows.
     */
    Optional<String> lastTool() {
        for (int i = messages.size() - 1; i >= 0; i--) {
            Message m = messages.get(i);
            if (!m.results.isEmpty()) {
                String name = m.results.get(m.results.size() - 1).name();
                return Optional.of(name == null ? "" : name);
            }
            if (m.role.equals("user") && !m.text.isBlank()) {
                return Optional.empty();
            }
        }
        return Optional.empty();
    }
}
