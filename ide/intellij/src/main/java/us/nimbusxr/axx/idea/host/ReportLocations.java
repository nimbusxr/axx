// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

/**
 * Moves the locations in a run's TeamCity report to the project the IDE has open. A run in a
 * sandbox may see the project in another directory than the host does (a sandbox that mounts it
 * elsewhere): its report locates features as {@code file://<its directory>/features/x.feature:12},
 * which become {@code file://<the project directory>/features/x.feature:12}.
 *
 * <p>Directories are written as axx writes them in locations: with forward slashes, starting with
 * one (Windows drive paths too), and escaped as TeamCity service message values. It holds no IDE
 * types, so it is unit-testable.
 */
public final class ReportLocations {
    private static final String FILE_URL = "file://";

    private ReportLocations() {}

    /**
     * A report line with the locations under {@code from} moved under {@code to}.
     *
     * @param line a line of the report
     * @param from the run's project directory, as the run sees it; null or empty when unknown
     * @param to the project directory, as the host sees it; null or empty when unknown
     */
    public static @NotNull String relocate(
            @NotNull String line, @Nullable String from, @Nullable String to) {
        if (from == null || to == null || from.isEmpty() || to.isEmpty()) {
            return line;
        }
        String old = FILE_URL + escape(slashed(from));
        String moved = FILE_URL + escape(slashed(to));
        if (old.equals(moved) || !line.contains(old)) {
            return line;
        }
        StringBuilder out = new StringBuilder(line.length());
        int start = 0;
        for (int at = line.indexOf(old); at >= 0; at = line.indexOf(old, start)) {
            int end = at + old.length();
            // Only the directory itself: not /work/parcels-api for /work/parcels.
            boolean whole =
                    end == line.length()
                            || line.charAt(end) == '/'
                            || line.charAt(end) == ':'
                            || line.charAt(end) == '|';
            out.append(line, start, at).append(whole ? moved : old);
            start = end;
        }
        return out.append(line, start, line.length()).toString();
    }

    /** A directory as locations start with it: forward slashes, a leading one, no trailing one. */
    static @NotNull String slashed(@NotNull String dir) {
        String path = dir.replace('\\', '/');
        while (path.length() > 1 && path.endsWith("/")) {
            path = path.substring(0, path.length() - 1);
        }
        return path.startsWith("/") ? path : "/" + path;
    }

    /** A value escaped as in a TeamCity service message, as axx escapes them. */
    static @NotNull String escape(@NotNull String value) {
        StringBuilder out = new StringBuilder(value.length());
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            switch (c) {
                case '|' -> out.append("||");
                case '\'' -> out.append("|'");
                case '\n' -> out.append("|n");
                case '\r' -> out.append("|r");
                case '[' -> out.append("|[");
                case ']' -> out.append("|]");
                case '\u0085' -> out.append("|x");
                case ' ' -> out.append("|l");
                case ' ' -> out.append("|p");
                default -> out.append(c);
            }
        }
        return out.toString();
    }
}
