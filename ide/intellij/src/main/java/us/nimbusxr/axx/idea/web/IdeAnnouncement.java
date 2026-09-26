// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.IdeLine;

import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * What packs print for the IDE when {@code AXX_IDE} is set: the traces and videos their scenarios
 * keep, and the folder of Playwright's trace viewer, which opens the traces.
 *
 * <pre>
 * [AXX-IDE] trace-viewer dir=/cache/axx/web/.../traceViewer
 * [AXX-IDE] trace path=/work/parcels/.axx/web/traces/x.zip location=features/shop.feature:42
 * [AXX-IDE] video path=/work/parcels/.axx/web/videos/x.webm location=features/shop.feature:42
 * </pre>
 *
 * <p>The lines follow the rules of {@link IdeLine}; spaces in values are written {@code %20}.
 * {@code trace-viewer} needs {@code dir}, the absolute path of the folder of the trace viewer's
 * files, which the IDE serves itself. {@code trace} and {@code video} need an absolute {@code path}
 * and the {@code location} of the scenario that kept the file: its feature file, relative to the
 * project directory, and line.
 *
 * @param kind what the line announces
 * @param path the file, for a trace or video; the folder of its files, for the trace viewer
 * @param file the scenario's feature file, for a trace or video; null otherwise
 * @param line the scenario's line, for a trace or video; 0 otherwise
 */
public record IdeAnnouncement(
        @NotNull Kind kind, @NotNull Path path, @Nullable String file, int line) {

    private static final Pattern LOCATION = Pattern.compile("^(.+):(\\d+)$");

    /** What a line announces, identified by the token that follows the marker. */
    public enum Kind {
        /** The folder of Playwright's trace viewer's files; the viewer opens traces by their URL. */
        TRACE_VIEWER("trace-viewer"),
        /** A Playwright trace a scenario kept. */
        TRACE("trace"),
        /** A video of a browser tab a scenario kept. */
        VIDEO("video");

        private final String token;

        Kind(String token) {
            this.token = token;
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
     * @return the announcement, or null when the text holds no well-formed one
     */
    public static @Nullable IdeAnnouncement parse(@Nullable String text) {
        IdeLine line = IdeLine.parse(text);
        Kind kind = line == null ? null : Kind.fromToken(line.kind());
        if (kind == null) {
            return null;
        }
        if (kind == Kind.TRACE_VIEWER) {
            Path dir = absolutePath(value(line, "dir"));
            return dir != null ? new IdeAnnouncement(kind, dir, null, 0) : null;
        }
        Path path = absolutePath(value(line, "path"));
        String location = value(line, "location");
        Matcher matcher = location == null ? null : LOCATION.matcher(location);
        if (path == null || matcher == null || !matcher.matches()) {
            return null;
        }
        try {
            int number = Integer.parseInt(matcher.group(2));
            return number > 0 ? new IdeAnnouncement(kind, path, matcher.group(1), number) : null;
        } catch (NumberFormatException e) {
            return null;
        }
    }

    private static @Nullable String value(IdeLine line, String key) {
        String value = line.field(key);
        return value == null ? null : value.replace("%20", " ");
    }

    private static @Nullable Path absolutePath(@Nullable String value) {
        if (value == null) {
            return null;
        }
        try {
            Path path = Path.of(value);
            return path.isAbsolute() ? path.normalize() : null;
        } catch (InvalidPathException e) {
            return null;
        }
    }
}
