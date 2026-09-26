// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.File;
import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Comparator;
import java.util.List;
import java.util.regex.Pattern;

/**
 * What {@code axx run} runs: targets ({@code path} or {@code path:line}, where the line is a
 * Feature, Scenario, or Examples row line) and the command line that runs them, with the steps it
 * pauses before, and the locations of the test tree nodes axx reports ({@code
 * file:///abs/path:line}).
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

    /**
     * The location, among the ones a run reported, of the scenario at a line of a feature file, as
     * the web pack names it: relative to the project directory (or absolute). That is the location
     * in the working directory, or else the only one that ends with the file and line (the working
     * directory may be below the project directory).
     *
     * @return the location, or null when none or several match
     */
    public static @Nullable String findLocation(
            @NotNull Collection<String> locations,
            @Nullable Path workingDirectory,
            @NotNull String file,
            int line) {
        Path path;
        try {
            path = Path.of(file);
        } catch (InvalidPathException e) {
            return null;
        }
        if (path.isAbsolute() || workingDirectory != null) {
            Path absolute = path.isAbsolute() ? path : workingDirectory.resolve(path);
            String exact =
                    locationUrl(
                            absolute.normalize().toString().replace(File.separatorChar, '/'), line);
            if (locations.contains(exact)) {
                return exact;
            }
            if (path.isAbsolute()) {
                return null;
            }
        }
        String tail = "/" + file.replace('\\', '/') + ":" + line;
        String found = null;
        for (String location : locations) {
            if (location.endsWith(tail)) {
                if (found != null) {
                    return null; // several
                }
                found = location;
            }
        }
        return found;
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
     * steps, one scenario at a time ({@code --workers 1}) when watching or debugging, the watch
     * arguments when watching, the debug arguments when debugging, the targets, then the extra
     * arguments.
     *
     * <p>Watching only shows the browsers: in windows on the desktop, with a wait after every
     * action, none for 0 ({@code --set packs.web-core.watch=true --set packs.web-core.slowdown=<n>ms}).
     * Debugging pauses the web pack's scenarios in Playwright's Inspector, one by one: where a
     * scenario fails ({@code --set packs.web-core.pauseOnFailure=true}, which shows its browser), and
     * before the steps with a breakpoint (a {@code --pause-at <step>} each). Projects without the
     * web pack ignore these settings.
     *
     * @param stepsPort the {@code --debug-steps} port, or null; ignored when the extra arguments
     *     already have {@code --debug-steps}
     * @param watchSlowdownMillis the slowdown when watching the browsers, or null for not watching
     * @param pauseAt when debugging, the steps to pause before (see {@link #pauseAt}); null for not
     *     debugging
     */
    public static @NotNull List<String> runArguments(
            @NotNull List<String> targets,
            @NotNull List<String> extra,
            @Nullable Integer stepsPort,
            @Nullable Integer watchSlowdownMillis,
            @Nullable List<String> pauseAt) {
        List<String> args = new ArrayList<>(List.of("run", "--format", "teamcity"));
        if (stepsPort != null && !hasDebugSteps(extra)) {
            args.add("--debug-steps=" + stepsPort);
        }
        if (watchSlowdownMillis != null || pauseAt != null) {
            args.addAll(List.of("--workers", "1"));
        }
        if (watchSlowdownMillis != null) {
            args.addAll(List.of("--set", "packs.web-core.watch=true"));
            if (watchSlowdownMillis > 0) {
                args.addAll(List.of("--set", "packs.web-core.slowdown=" + watchSlowdownMillis + "ms"));
            }
        }
        if (pauseAt != null) {
            args.addAll(List.of("--set", "packs.web-core.pauseOnFailure=true"));
            for (String step : pauseAt) {
                args.add("--pause-at");
                args.add(step);
            }
        }
        args.addAll(targets);
        args.addAll(extra);
        return args;
    }

    /**
     * A step to pause before: a breakpoint on it.
     *
     * @param file the feature file, an absolute path
     * @param line the step's one-based line
     */
    public record Step(@NotNull Path file, int line) {}

    /**
     * The steps a run pauses before, as {@code --pause-at} values, written like targets: the ones
     * in the files the run reaches (its targets, or without targets the working directory, where
     * axx then runs just the scenarios of those steps), in file and line order. A step elsewhere
     * could never pause the run, and would still make axx run as if it could: with the browsers
     * shown, and no timeouts.
     *
     * @param targets the run's targets, relative to the working directory
     */
    public static @NotNull List<String> pauseAt(
            @NotNull Collection<Step> steps,
            @NotNull List<String> targets,
            @NotNull Path workingDirectory) {
        Path dir = workingDirectory.toAbsolutePath().normalize();
        List<Path> reached = new ArrayList<>();
        if (targets.isEmpty()) {
            reached.add(dir);
        }
        for (String target : targets) {
            try {
                reached.add(dir.resolve(pathOf(target)).normalize());
            } catch (InvalidPathException e) {
                // not a path: it reaches no step
            }
        }
        return steps.stream()
                .map(step -> new Step(step.file().toAbsolutePath().normalize(), step.line()))
                .filter(step -> step.line() > 0)
                .filter(step -> reached.stream().anyMatch(step.file()::startsWith))
                .sorted(Comparator.comparing(Step::file).thenComparingInt(Step::line))
                .map(step -> target(step.file(), step.line(), dir))
                .distinct()
                .toList();
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
