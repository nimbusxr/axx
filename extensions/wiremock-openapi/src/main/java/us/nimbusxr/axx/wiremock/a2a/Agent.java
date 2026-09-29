// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import com.github.tomakehurst.wiremock.common.FileSource;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;
import com.github.tomakehurst.wiremock.stubbing.StubMappings;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import us.nimbusxr.axx.wiremock.stubs.InProcessStubs;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.function.Supplier;

/**
 * What the mocked agent does, in either binding: it answers each message with its stub's answer,
 * and remembers the tasks it answered.
 *
 * <p>A message is answered by a stub of a POST to {@code /a2a/messages}, matched in-process with
 * the body {@code {"text": <the text of the message's parts, one per line>, "message": <the
 * message>, "conversation": <the text of every message of the task it continues, and this one>,
 * "task": {"id", "state"} or null}}. A task the stub answers is new, or the one the message
 * continues ({@code taskId}); {@code returnImmediately} answers it working, and the stub's state is
 * the task's when it is next looked at.
 */
final class Agent {
    static final String MESSAGES_PATH = "/a2a/messages";
    /** Metadata that marks the mock's own stubs, which lookups skip. */
    static final String ENDPOINT_KEY = "axxA2aEndpoint";

    private static final Logger log = LoggerFactory.getLogger(Agent.class);
    private static final Set<String> PART_CONTENT = Set.of("text", "raw", "url", "data");

    /** A result, or the events of a stream. */
    record Outcome(Object result, List<Object> events) {
        static Outcome of(Object result) {
            return new Outcome(result, null);
        }

        static Outcome stream(List<Object> events) {
            return new Outcome(null, events);
        }
    }

    private final AgentCard card;
    private final Tasks tasks;
    private final Supplier<StubMappings> stubs;
    private final FileSource files;

    Agent(AgentCard card, Tasks tasks, Supplier<StubMappings> stubs, FileSource files) {
        this.card = card;
        this.tasks = tasks;
        this.stubs = stubs;
        this.files = files;
    }

    /** SendMessage, or SendStreamingMessage when stream. */
    Outcome send(Map<String, Object> request, boolean stream) {
        if (stream && !card.streaming) {
            throw new A2aError(A2aError.UNSUPPORTED_OPERATION, "the agent does not stream: its card's capabilities.streaming is not true");
        }
        Map<String, Object> received = Values.map(request.get("message"));
        if (received == null) {
            throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: the request has no message");
        }
        if (Values.text(received.get("messageId")) == null) {
            throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: the message has no messageId");
        }
        List<Object> parts = Values.list(received.get("parts"));
        if (parts == null || parts.isEmpty()) {
            throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: the message has no parts");
        }
        Map<String, Object> config = Values.map(request.get("configuration"));
        boolean immediately = config != null && Boolean.TRUE.equals(config.get("returnImmediately"));
        Integer historyLength = config != null && config.get("historyLength") instanceof Number n ? n.intValue() : null;
        Map<String, Object> message = message(received);
        String taskId = Values.text(message.get("taskId"));
        String contextId = Values.text(message.get("contextId"));
        String text = text(message);

        synchronized (tasks.lock()) {
            Tasks.Task task = null;
            if (taskId != null) {
                task = tasks.get(taskId);
                if (task == null) {
                    throw new A2aError(A2aError.TASK_NOT_FOUND, "Task not found: " + taskId);
                }
                settle(task, null);
                if (task.terminal()) {
                    throw new A2aError(A2aError.UNSUPPORTED_OPERATION, "the task " + taskId + " is " + AgentAnswer.shortState(task.state)
                            + ": a task in a terminal state takes no more messages");
                }
                if (contextId != null && !contextId.equals(task.contextId)) {
                    throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: the message's contextId " + contextId + " is not its task's, "
                            + task.contextId);
                }
                contextId = task.contextId;
            }
            List<String> conversation = new ArrayList<>();
            if (task != null) {
                task.history.forEach(m -> conversation.add(text(m)));
            }
            conversation.add(text);
            Map<String, Object> lookup = Values.obj("text", text, "message", message, "conversation", String.join("\n", conversation),
                    "task", task == null ? null : Values.obj("id", task.id, "state", AgentAnswer.shortState(task.state)));
            StubMapping stub = InProcessStubs.find(stubs.get(), MESSAGES_PATH, Values.write(lookup), ENDPOINT_KEY).orElseThrow(() -> {
                log.warn("the A2A mock of {}: no stub answers the message: {}", card.name, text);
                return new A2aError(A2aError.INTERNAL, "no stub answers the message: " + text);
            });
            AgentAnswer answer;
            try {
                answer = AgentAnswer.read(InProcessStubs.body(stub.getResponse(), files));
            } catch (RuntimeException e) {
                throw new A2aError(A2aError.INTERNAL, "the stub " + InProcessStubs.describe(stub) + " is not an A2A agent stub: " + e.getMessage());
            }
            if (answer.error() != null) {
                throw new A2aError(answer.error().code(), answer.error().message());
            }
            if (contextId == null) {
                contextId = UUID.randomUUID().toString();
            }
            message.put("contextId", contextId);
            if (!answer.isTask()) {
                Map<String, Object> reply = reply(answer, contextId, task == null ? null : task.id);
                if (task != null) {
                    task.history.add(message);
                    task.history.add(reply);
                    tasks.put(task);
                }
                return stream ? Outcome.stream(List.of(Values.obj("message", reply))) : Outcome.of(Values.obj("message", reply));
            }
            if (task == null) {
                task = new Tasks.Task(UUID.randomUUID().toString(), contextId);
            }
            message.put("taskId", task.id);
            task.history.add(message);
            task.state = "TASK_STATE_SUBMITTED";
            task.statusMessage = null;
            task.timestamp = Values.now();
            Map<String, Object> submitted = task.render(null, true);
            List<Object> events = new ArrayList<>();
            if (!stream && immediately) {
                task.state = "TASK_STATE_WORKING";
                task.pending = answer;
            } else {
                events.addAll(settle(task, answer));
            }
            tasks.put(task);
            if (!stream) {
                return Outcome.of(Values.obj("task", task.render(historyLength, true)));
            }
            events.add(0, Values.obj("task", submitted));
            return Outcome.stream(events);
        }
    }

    /** GetTask: the task, which reaches its stub's state now if it was answered at once. */
    Map<String, Object> get(Map<String, Object> request) {
        synchronized (tasks.lock()) {
            Tasks.Task task = task(request);
            settle(task, null);
            tasks.put(task);
            return task.render(historyLength(request.get("historyLength")), true);
        }
    }

    /** ListTasks: the tasks of a context (or all), the most recently changed first, a page at a time. */
    Map<String, Object> list(Map<String, Object> request) {
        String contextId = Values.text(request.get("contextId"));
        String state = Values.text(request.get("status"));
        if ("TASK_STATE_UNSPECIFIED".equals(state)) {
            state = null;
        }
        int pageSize = number(request.get("pageSize"), 50);
        pageSize = pageSize <= 0 ? 50 : Math.min(pageSize, 100);
        int start = number(request.get("pageToken"), 0);
        boolean withArtifacts = Boolean.TRUE.equals(request.get("includeArtifacts")) || "true".equals(request.get("includeArtifacts"));
        Integer historyLength = historyLength(request.get("historyLength"));
        synchronized (tasks.lock()) {
            List<Tasks.Task> all = tasks.list(contextId, state);
            List<Object> page = new ArrayList<>();
            for (int i = Math.max(0, start); i < Math.min(all.size(), start + pageSize); i++) {
                page.add(all.get(i).render(historyLength, withArtifacts));
            }
            String next = start + pageSize < all.size() ? String.valueOf(start + pageSize) : "";
            // pageSize says how many tasks this page has, as the SDKs read it.
            return Values.obj("tasks", page, "nextPageToken", next, "pageSize", page.size(), "totalSize", all.size());
        }
    }

    /** CancelTask: canceled, unless it has ended. */
    Map<String, Object> cancel(Map<String, Object> request) {
        synchronized (tasks.lock()) {
            Tasks.Task task = task(request);
            if (task.terminal()) {
                throw new A2aError(A2aError.TASK_NOT_CANCELABLE, "the task " + task.id + " is " + AgentAnswer.shortState(task.state) + ": it cannot be canceled");
            }
            task.pending = null;
            task.state = "TASK_STATE_CANCELED";
            task.statusMessage = null;
            task.timestamp = Values.now();
            tasks.put(task);
            return task.render(null, true);
        }
    }

    /** SubscribeToTask: the task as it is, then how it ends (its stub's state, if it was answered at once), then the stream ends. */
    Outcome subscribe(Map<String, Object> request) {
        if (!card.streaming) {
            throw new A2aError(A2aError.UNSUPPORTED_OPERATION, "the agent does not stream: its card's capabilities.streaming is not true");
        }
        synchronized (tasks.lock()) {
            Tasks.Task task = task(request);
            if (task.terminal()) {
                throw new A2aError(A2aError.UNSUPPORTED_OPERATION, "the task " + task.id + " is " + AgentAnswer.shortState(task.state)
                        + ": a task in a terminal state has no updates to subscribe to");
            }
            List<Object> events = new ArrayList<>();
            events.add(Values.obj("task", task.render(null, true)));
            if (task.pending != null) {
                events.addAll(settle(task, null));
            } else {
                events.add(statusUpdate(task, task.status()));
            }
            tasks.put(task);
            return Outcome.stream(events);
        }
    }

    /**
     * Brings a task to a stub's answer (or to the answer it was answered at once with, when answer
     * is null), and returns the stream's events on the way: its working updates, its new artifacts
     * and its final status.
     */
    private List<Object> settle(Tasks.Task task, AgentAnswer answer) {
        if (answer == null) {
            answer = task.pending;
        }
        if (answer == null) {
            return List.of();
        }
        task.pending = null;
        List<Object> events = new ArrayList<>();
        for (String update : answer.updates()) {
            events.add(statusUpdate(task, Values.obj("state", "TASK_STATE_WORKING", "message", agentMessage(update, task), "timestamp", Values.now())));
        }
        for (AgentAnswer.Artifact a : answer.artifacts()) {
            Map<String, Object> artifact = artifact(a);
            task.artifacts.add(artifact);
            events.add(Values.obj("artifactUpdate", Values.obj("taskId", task.id, "contextId", task.contextId, "artifact", artifact,
                    "append", false, "lastChunk", true)));
        }
        task.state = AgentAnswer.protoState(answer.state());
        task.statusMessage = answer.message() == null ? null : agentMessage(answer.message(), task);
        if (task.statusMessage != null) {
            task.history.add(task.statusMessage);
        }
        task.timestamp = Values.now();
        events.add(statusUpdate(task, task.status()));
        return events;
    }

    private Tasks.Task task(Map<String, Object> request) {
        String id = Values.text(request.get("id"));
        if (id == null) {
            throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: the request names its task (id)");
        }
        Tasks.Task task = tasks.get(id);
        if (task == null) {
            throw new A2aError(A2aError.TASK_NOT_FOUND, "Task not found: " + id);
        }
        return task;
    }

    private static Map<String, Object> statusUpdate(Tasks.Task task, Map<String, Object> status) {
        return Values.obj("statusUpdate", Values.obj("taskId", task.id, "contextId", task.contextId, "status", status));
    }

    private static Map<String, Object> agentMessage(String text, Tasks.Task task) {
        return Values.obj("messageId", UUID.randomUUID().toString(), "contextId", task.contextId, "taskId", task.id, "role", "ROLE_AGENT",
                "parts", List.of(Values.obj("text", text)));
    }

    private static Map<String, Object> reply(AgentAnswer answer, String contextId, String taskId) {
        List<Object> parts = new ArrayList<>();
        parts.add(Values.obj("text", answer.reply()));
        if (answer.data() != null) {
            parts.add(Values.obj("data", answer.data(), "mediaType", "application/json"));
        }
        Map<String, Object> m = Values.obj("messageId", UUID.randomUUID().toString(), "contextId", contextId);
        if (taskId != null) {
            m.put("taskId", taskId);
        }
        m.put("role", "ROLE_AGENT");
        m.put("parts", parts);
        return m;
    }

    private static Map<String, Object> artifact(AgentAnswer.Artifact a) {
        Map<String, Object> part = new java.util.LinkedHashMap<>();
        if (a.text() != null) {
            part.put("text", a.text());
        } else if (a.url() != null) {
            part.put("url", a.url());
        } else {
            part.put("data", a.data());
        }
        if (a.mediaType() != null) {
            part.put("mediaType", a.mediaType());
        }
        Map<String, Object> artifact = Values.obj("artifactId", UUID.randomUUID().toString(), "name", a.name());
        if (a.description() != null) {
            artifact.put("description", a.description());
        }
        artifact.put("parts", List.of(part));
        return artifact;
    }

    /**
     * The message as A2A 1.0 has it, with what says nothing left out: a client that writes every
     * field (as ProtoJSON can) sends empty strings and lists for the fields it did not set.
     */
    static Map<String, Object> message(Map<String, Object> m) {
        Map<String, Object> out = Values.obj("messageId", m.get("messageId"));
        for (String k : List.of("contextId", "taskId")) {
            if (Values.text(m.get(k)) != null) {
                out.put(k, m.get(k));
            }
        }
        String role = Values.text(m.get("role"));
        out.put("role", "ROLE_AGENT".equals(role) ? "ROLE_AGENT" : "ROLE_USER");
        List<Object> parts = new ArrayList<>();
        for (Object o : Values.list(m.get("parts"))) {
            Map<String, Object> p = Values.map(o);
            if (p == null) {
                throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: each part of a message is an object with text, raw, url or data");
            }
            Map<String, Object> part = new java.util.LinkedHashMap<>();
            for (String k : List.of("text", "raw", "url", "data")) {
                if (p.containsKey(k) && PART_CONTENT.contains(k) && (p.get(k) != null || k.equals("data"))) {
                    part.put(k, p.get(k));
                }
            }
            if (part.size() != 1) {
                throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: each part of a message has one of text, raw, url or data: " + Values.write(o));
            }
            for (String k : List.of("metadata", "filename", "mediaType")) {
                Object v = p.get(k);
                if (v != null && !"".equals(v)) {
                    part.put(k, v);
                }
            }
            parts.add(part);
        }
        out.put("parts", parts);
        for (String k : List.of("metadata", "extensions", "referenceTaskIds")) {
            Object v = m.get(k);
            if (v != null && !(v instanceof List<?> l && l.isEmpty())) {
                out.put(k, v);
            }
        }
        return out;
    }

    /** The text of a message: its text parts, one per line. */
    static String text(Map<String, Object> message) {
        List<String> out = new ArrayList<>();
        List<Object> parts = Values.list(message.get("parts"));
        for (Object o : parts == null ? List.of() : parts) {
            Map<String, Object> p = Values.map(o);
            if (p != null && p.get("text") instanceof String s) {
                out.add(s);
            }
        }
        return String.join("\n", out);
    }

    private static Integer historyLength(Object v) {
        if (v instanceof Number n) {
            return n.intValue();
        }
        if (v instanceof String s && !s.isBlank()) {
            try {
                return Integer.parseInt(s.strip());
            } catch (NumberFormatException e) {
                throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: historyLength is a whole number, not " + s);
            }
        }
        return null;
    }

    private static int number(Object v, int otherwise) {
        if (v instanceof Number n) {
            return n.intValue();
        }
        if (v instanceof String s && !s.isBlank()) {
            try {
                return Integer.parseInt(s.strip());
            } catch (NumberFormatException e) {
                throw new A2aError(A2aError.INVALID_PARAMS, "Invalid parameters: " + s + " is not a whole number");
            }
        }
        return otherwise;
    }
}
