// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import java.util.List;
import java.util.Map;

/**
 * An A2A error: its JSON-RPC code, and how the HTTP+JSON binding carries it (A2A 1.0, section
 * 5.4): an HTTP status, the gRPC status name, and the reason of its {@code google.rpc.ErrorInfo}.
 */
final class A2aError extends RuntimeException {
    private static final long serialVersionUID = 1L;

    static final int TASK_NOT_FOUND = -32001;
    static final int TASK_NOT_CANCELABLE = -32002;
    static final int PUSH_NOTIFICATION_NOT_SUPPORTED = -32003;
    static final int UNSUPPORTED_OPERATION = -32004;
    static final int EXTENDED_AGENT_CARD_NOT_CONFIGURED = -32007;
    static final int VERSION_NOT_SUPPORTED = -32009;
    static final int PARSE_ERROR = -32700;
    static final int INVALID_REQUEST = -32600;
    static final int METHOD_NOT_FOUND = -32601;
    static final int INVALID_PARAMS = -32602;
    static final int INTERNAL = -32603;

    /** How the HTTP+JSON binding carries an error code. */
    record Rest(int status, String grpcStatus, String reason) {}

    private static final Map<Integer, Rest> REST = Map.ofEntries(
            Map.entry(TASK_NOT_FOUND, new Rest(404, "NOT_FOUND", "TASK_NOT_FOUND")),
            Map.entry(TASK_NOT_CANCELABLE, new Rest(400, "FAILED_PRECONDITION", "TASK_NOT_CANCELABLE")),
            Map.entry(PUSH_NOTIFICATION_NOT_SUPPORTED, new Rest(400, "FAILED_PRECONDITION", "PUSH_NOTIFICATION_NOT_SUPPORTED")),
            Map.entry(UNSUPPORTED_OPERATION, new Rest(400, "FAILED_PRECONDITION", "UNSUPPORTED_OPERATION")),
            Map.entry(-32005, new Rest(400, "INVALID_ARGUMENT", "CONTENT_TYPE_NOT_SUPPORTED")),
            Map.entry(-32006, new Rest(500, "INTERNAL", "INVALID_AGENT_RESPONSE")),
            Map.entry(EXTENDED_AGENT_CARD_NOT_CONFIGURED, new Rest(400, "FAILED_PRECONDITION", "EXTENDED_AGENT_CARD_NOT_CONFIGURED")),
            Map.entry(-32008, new Rest(400, "FAILED_PRECONDITION", "EXTENSION_SUPPORT_REQUIRED")),
            Map.entry(VERSION_NOT_SUPPORTED, new Rest(400, "FAILED_PRECONDITION", "VERSION_NOT_SUPPORTED")),
            Map.entry(PARSE_ERROR, new Rest(400, "INVALID_ARGUMENT", "JSON_PARSE")),
            Map.entry(INVALID_REQUEST, new Rest(400, "INVALID_ARGUMENT", "INVALID_REQUEST")),
            Map.entry(METHOD_NOT_FOUND, new Rest(404, "NOT_FOUND", "METHOD_NOT_FOUND")),
            Map.entry(INVALID_PARAMS, new Rest(400, "INVALID_ARGUMENT", "INVALID_PARAMS")),
            Map.entry(INTERNAL, new Rest(500, "INTERNAL", "INTERNAL")));

    final int code;

    A2aError(int code, String message) {
        super(message);
        this.code = code;
    }

    /** The error as the HTTP+JSON binding carries it: other codes (a stub's own) are internal errors. */
    Rest rest() {
        return REST.getOrDefault(code, new Rest(500, "INTERNAL", null));
    }

    /** The JSON-RPC error object. */
    Map<String, Object> jsonRpc() {
        return Values.obj("code", code, "message", getMessage());
    }

    /** The HTTP+JSON binding's error body: a google.rpc.Status, with the A2A error's ErrorInfo. */
    Map<String, Object> restBody() {
        Rest r = rest();
        Map<String, Object> error = Values.obj("code", r.status(), "status", r.grpcStatus(), "message", getMessage());
        if (r.reason() != null) {
            error.put("details", List.of(Values.obj("@type", "type.googleapis.com/google.rpc.ErrorInfo", "reason", r.reason(),
                    "domain", "a2a-protocol.org")));
        }
        return Values.obj("error", error);
    }
}
