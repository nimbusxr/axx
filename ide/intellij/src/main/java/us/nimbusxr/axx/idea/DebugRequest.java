// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.HashMap;
import java.util.Map;
import java.util.regex.Pattern;

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
 * same line.
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
    public static final String MARKER = "[AXX-IDE]";

    /** Run configurations the plugin starts are named with this prefix plus the app name. */
    public static final String CONFIGURATION_PREFIX = "Debugger: ";

    private static final Pattern ANSI_ESCAPE = Pattern.compile("\u001B\\[[0-9;?]*[ -/]*[@-~]");
    private static final Pattern WHITESPACE = Pattern.compile("\\s+");

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
        if (line == null || !line.contains(MARKER)) {
            return null;
        }
        String rest = afterMarker(ANSI_ESCAPE.matcher(line).replaceAll(""));
        if (rest == null) {
            return null;
        }
        int eol = indexOfLineBreak(rest);
        if (eol >= 0) {
            rest = rest.substring(0, eol);
        }
        String[] tokens = WHITESPACE.split(rest.strip());
        Kind kind = Kind.fromToken(tokens[0]);
        if (kind == null) {
            return null;
        }

        Map<String, String> fields = new HashMap<>();
        for (int i = 1; i < tokens.length; i++) {
            int eq = tokens[i].indexOf('=');
            if (eq > 0 && eq < tokens[i].length() - 1) {
                fields.putIfAbsent(tokens[i].substring(0, eq), tokens[i].substring(eq + 1));
            }
        }

        String name = fields.get("name");
        String type = fields.get("type");
        String host = fields.get("host");
        Integer port = parsePort(fields.get("port"));
        if (name == null || type == null || host == null || port == null) {
            return null;
        }
        return new DebugRequest(kind, name, type, host, port);
    }

    /** The name of the run configuration this request starts: {@code "Debugger: <name>"}. */
    public @NotNull String configurationName() {
        return CONFIGURATION_PREFIX + name;
    }

    private static @Nullable String afterMarker(String text) {
        int at = text.indexOf(MARKER);
        return at >= 0 ? text.substring(at + MARKER.length()) : null;
    }

    private static int indexOfLineBreak(String text) {
        for (int i = 0; i < text.length(); i++) {
            char c = text.charAt(i);
            if (c == '\n' || c == '\r') {
                return i;
            }
        }
        return -1;
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
