// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.HashMap;
import java.util.Map;
import java.util.regex.Pattern;

/**
 * A line axx prints for the IDE: the marker, a kind, then {@code key=value} fields, such as
 *
 * <pre>
 * [AXX-IDE] debug-attach-request name=parcels type=go host=localhost port=2345
 * [AXX-IDE] trace path=/work/parcels/.axx/web/traces/x.zip location=features/shop.feature:42
 * </pre>
 *
 * <p>The marker may be preceded by other output (a timestamp, a log prefix) on the same line, and
 * ANSI color codes are ignored. Fields are separated by whitespace, in any order; tokens without a
 * value are skipped, and the first value of a repeated key wins. Parsing stops at the first line
 * break after the marker.
 *
 * @param kind the token after the marker, such as {@code debug-attach-request}
 * @param fields the fields, by key
 */
public record IdeLine(@NotNull String kind, @NotNull Map<String, String> fields) {

    /** The marker that starts every line for the IDE. */
    public static final String MARKER = "[AXX-IDE]";

    private static final Pattern ANSI_ESCAPE = Pattern.compile("\u001B\\[[0-9;?]*[ -/]*[@-~]");
    private static final Pattern WHITESPACE = Pattern.compile("\\s+");

    /**
     * Parses a line of console output.
     *
     * @return the line, or null when the text has no marker followed by a kind
     */
    public static @Nullable IdeLine parse(@Nullable String text) {
        if (text == null || !text.contains(MARKER)) {
            return null;
        }
        String clean = ANSI_ESCAPE.matcher(text).replaceAll("");
        int at = clean.indexOf(MARKER);
        if (at < 0) {
            return null;
        }
        String rest = clean.substring(at + MARKER.length());
        int eol = indexOfLineBreak(rest);
        if (eol >= 0) {
            rest = rest.substring(0, eol);
        }
        String[] tokens = WHITESPACE.split(rest.strip());
        if (tokens[0].isEmpty()) {
            return null;
        }
        Map<String, String> fields = new HashMap<>();
        for (int i = 1; i < tokens.length; i++) {
            int eq = tokens[i].indexOf('=');
            if (eq > 0 && eq < tokens[i].length() - 1) {
                fields.putIfAbsent(tokens[i].substring(0, eq), tokens[i].substring(eq + 1));
            }
        }
        return new IdeLine(tokens[0], Map.copyOf(fields));
    }

    /** A field, or null when the line has none. */
    public @Nullable String field(@NotNull String key) {
        return fields.get(key);
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
}
