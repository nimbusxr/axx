// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The tasks the mock has answered, which it remembers for the requests that follow (get, list,
 * cancel, subscribe, and the messages that continue a task): the most recently used thousand.
 * Callers hold the store's lock while they read or change a task.
 */
final class Tasks {
    static final int MAX = 1000;

    /** A task as the mock keeps it; changed only under the store's lock. */
    static final class Task {
        final String id;
        final String contextId;
        String state;
        Map<String, Object> statusMessage;
        String timestamp;
        final List<Map<String, Object>> artifacts = new ArrayList<>();
        final List<Map<String, Object>> history = new ArrayList<>();
        /** The stub's answer a task answered at once ({@code returnImmediately}) reaches when it is next looked at. */
        AgentAnswer pending;
        long changed;

        Task(String id, String contextId) {
            this.id = id;
            this.contextId = contextId;
        }

        boolean terminal() {
            return switch (state) {
                case "TASK_STATE_COMPLETED", "TASK_STATE_FAILED", "TASK_STATE_CANCELED", "TASK_STATE_REJECTED" -> true;
                default -> false;
            };
        }

        Map<String, Object> status() {
            Map<String, Object> s = Values.obj("state", state);
            if (statusMessage != null) {
                s.put("message", statusMessage);
            }
            s.put("timestamp", timestamp);
            return s;
        }

        /** The task in A2A's shape, with at most the last historyLength messages of its history (all when null). */
        Map<String, Object> render(Integer historyLength, boolean withArtifacts) {
            Map<String, Object> t = Values.obj("id", id, "contextId", contextId, "status", status());
            if (withArtifacts && !artifacts.isEmpty()) {
                t.put("artifacts", new ArrayList<>(artifacts));
            }
            List<Map<String, Object>> h = history;
            if (historyLength != null) {
                h = history.subList(Math.max(0, history.size() - historyLength), history.size());
            }
            if (!h.isEmpty()) {
                t.put("history", new ArrayList<>(h));
            }
            return t;
        }
    }

    private long clock;

    private final Map<String, Task> tasks = new LinkedHashMap<>(64, 0.75f, true) {
        private static final long serialVersionUID = 1L;

        @Override
        protected boolean removeEldestEntry(Map.Entry<String, Task> eldest) {
            return size() > MAX;
        }
    };

    Object lock() {
        return tasks;
    }

    /** Forgets every task. */
    void clear() {
        synchronized (tasks) {
            tasks.clear();
        }
    }

    Task get(String id) {
        return tasks.get(id);
    }

    void put(Task t) {
        t.changed = ++clock;
        tasks.put(t.id, t);
    }

    /** The tasks of a context (every task when null), in a state (any when null), the most recently changed first. */
    List<Task> list(String contextId, String state) {
        return tasks.values().stream()
                .filter(t -> contextId == null || contextId.equals(t.contextId))
                .filter(t -> state == null || state.equals(t.state))
                .sorted(Comparator.comparingLong((Task t) -> t.changed).reversed())
                .toList();
    }
}
