// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

/**
 * A debugger request that the axx runner prints to its console when it starts an application in
 * debug mode:
 *
 * <pre>
 * [AXX-IDE] debug-listener-request name=billing type=java host=localhost port=5005
 * [AXX-IDE] debug-attach-request name=parcels type=go host=localhost port=2345
 * </pre>
 *
 * <p>A listener request is printed <em>before</em> the application starts, which then connects to a
 * debugger listening in the IDE. An attach request is printed once the application listens for a
 * debugger, which the IDE then attaches to. Either way the IDE starts the run configuration named
 * {@code "Debugger: <name>"} (see {@link #configurationName()}).
 *
 * <p>Fields are {@code key=value} tokens separated by whitespace, in any order; unknown keys are
 * ignored so the protocol can grow. {@code name}, {@code type}, {@code host} and a numeric {@code
 * port} are required. The marker may be preceded by other output (a timestamp, a log prefix) on the
 * same line (see {@link IdeLine}).
 *
 * @param kind whether the IDE listens for the application or attaches to it
 * @param name the application name
 * @param type the debugger type (for example java, nodejs, python, go)
 * @param host the debug host
 * @param port the debug port
 */
public record DebugRequest(
        @NotNull Kind kind,
        @NotNull String name,
        @NotNull String type,
        @NotNull String host,
        int port) {

    /** The marker that starts every request. */
    public static final String MARKER = IdeLine.MARKER;

    /** Run configurations the plugin starts are named with this prefix plus the app name. */
    public static final String CONFIGURATION_PREFIX = "Debugger: ";

    /** The kind of request, identified by the token that follows the marker. */
    public enum Kind {
        /** Start a listen-mode debugger that the application connects to. */
        LISTEN("debug-listener-request"),
        /** Start an attach-mode debugger that connects to the listening application. */
        ATTACH("debug-attach-request");

        private final String token;

        Kind(String token) {
            this.token = token;
        }

        /** The token that identifies this kind on the marker line. */
        public String token() {
            return token;
        }

        static @Nullable Kind fromToken(String token) {
            for (Kind kind : values()) {
                if (kind.token.equals(token)) {
                    return kind;
                }
            }
            return null;
        }
    }

    /**
     * Parses a line of console output.
     *
     * @param line the console text; parsing stops at the first line break after the marker
     * @return the request, or null when the text holds no well-formed request
     */
    public static @Nullable DebugRequest parse(@Nullable String line) {
        IdeLine ideLine = IdeLine.parse(line);
        Kind kind = ideLine == null ? null : Kind.fromToken(ideLine.kind());
        if (kind == null) {
            return null;
        }

        String name = ideLine.field("name");
        String type = ideLine.field("type");
        String host = ideLine.field("host");
        Integer port = parsePort(ideLine.field("port"));
        if (name == null || type == null || host == null || port == null) {
            return null;
        }
        return new DebugRequest(kind, name, type, host, port);
    }

    /** The name of the run configuration this request starts: {@code "Debugger: <name>"}. */
    public @NotNull String configurationName() {
        return CONFIGURATION_PREFIX + name;
    }

    private static @Nullable Integer parsePort(@Nullable String value) {
        if (value == null) {
            return null;
        }
        try {
            int port = Integer.parseInt(value);
            return port > 0 && port <= 65535 ? port : null;
        } catch (NumberFormatException e) {
            return null;
        }
    }
}
