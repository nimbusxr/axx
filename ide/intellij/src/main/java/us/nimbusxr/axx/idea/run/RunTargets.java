// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.File;
import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.regex.Pattern;

/**
 * What {@code axx run} runs: targets ({@code path} or {@code path:line}, where the line is a
 * Feature, Scenario, or Examples row line) and the command line that runs them, and the locations
 * of the test tree nodes axx reports ({@code file:///abs/path:line}).
 *
 * <p>It holds no IDE types, so its rules are unit-testable.
 */
public final class RunTargets {
    private static final String FILE_URL = "file://";
    private static final Pattern LINE_SUFFIX = Pattern.compile(":(\\d+)$");
    private static final Pattern WINDOWS_DRIVE = Pattern.compile("^/[A-Za-z]:.*");

    private RunTargets() {}

    /**
     * The target for a file or directory, relative to the working directory when it is inside it.
     *
     * @param path an absolute path
     * @param line the one-based line, or 0 or less for the whole file or directory
     * @param workingDirectory the directory axx runs in, or null
     */
    public static @NotNull String target(
            @NotNull Path path, int line, @Nullable Path workingDirectory) {
        Path absolute = path.toAbsolutePath().normalize();
        String text = absolute.toString();
        if (workingDirectory != null) {
            Path dir = workingDirectory.toAbsolutePath().normalize();
            if (absolute.startsWith(dir)) {
                String relative = dir.relativize(absolute).toString();
                text = relative.isEmpty() ? "." : relative;
            }
        }
        text = text.replace(File.separatorChar, '/');
        return line > 0 ? text + ":" + line : text;
    }

    /**
     * The target for a test tree location, as axx reports it ({@code file:///abs/path:line}).
     *
     * @return the target, relative to the working directory when inside it, or null when the
     *     location is not a file
     */
    public static @Nullable String fromLocationUrl(
            @Nullable String url, @Nullable Path workingDirectory) {
        if (url == null || !url.startsWith(FILE_URL)) {
            return null;
        }
        String path = url.substring(FILE_URL.length());
        int line = 0;
        var matcher = LINE_SUFFIX.matcher(path);
        if (matcher.find()) {
            line = Integer.parseInt(matcher.group(1));
            path = path.substring(0, matcher.start());
        }
        if (WINDOWS_DRIVE.matcher(path).matches()) {
            path = path.substring(1);
        }
        try {
            return target(Path.of(path), line, workingDirectory);
        } catch (InvalidPathException e) {
            return null;
        }
    }

    /** The test tree location axx reports for a line of a file ({@code file:///abs/path:line}). */
    public static @NotNull String locationUrl(@NotNull String systemIndependentPath, int line) {
        String path =
                systemIndependentPath.startsWith("/")
                        ? systemIndependentPath
                        : "/" + systemIndependentPath;
        return FILE_URL + path + ":" + line;
    }

    /** The path of a target, without its line. */
    public static @NotNull String pathOf(@NotNull String target) {
        return LINE_SUFFIX.matcher(target).replaceFirst("");
    }

    /** The line of a target, or 0 without one. */
    public static int lineOf(@NotNull String target) {
        var matcher = LINE_SUFFIX.matcher(target);
        return matcher.find() ? Integer.parseInt(matcher.group(1)) : 0;
    }

    /**
     * The arguments of {@code axx}: {@code run --format teamcity}, the Delve port when debugging
     * steps, the targets, then the extra arguments.
     *
     * @param stepsPort the {@code --debug-steps} port, or null; ignored when the extra arguments
     *     already have {@code --debug-steps}
     */
    public static @NotNull List<String> runArguments(
            @NotNull List<String> targets,
            @NotNull List<String> extra,
            @Nullable Integer stepsPort) {
        List<String> args = new ArrayList<>(List.of("run", "--format", "teamcity"));
        if (stepsPort != null && !hasDebugSteps(extra)) {
            args.add("--debug-steps=" + stepsPort);
        }
        args.addAll(targets);
        args.addAll(extra);
        return args;
    }

    private static boolean hasDebugSteps(List<String> args) {
        for (String arg : args) {
            if (arg.equals("--debug-steps") || arg.startsWith("--debug-steps=")) {
                return true;
            }
        }
        return false;
    }
}
