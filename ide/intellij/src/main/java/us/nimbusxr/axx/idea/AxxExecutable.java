// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.File;
import java.nio.file.Files;
import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;
import java.util.function.Function;
import java.util.regex.Pattern;

/**
 * Resolves the "axx executable" setting to the file the plugin runs.
 *
 * <p>The setting is either a command name, looked up on {@code PATH} (the default, {@code axx}), or
 * a path to the binary. A relative path starts at the project directory, so a project can point at
 * its own build of axx, such as {@code bin/axx}. When the default {@code axx} is not on {@code
 * PATH}, the places axx's installers put it are tried too (see {@link #installLocations}), since
 * the IDE's {@code PATH} often lacks them.
 *
 * <p>It holds no IDE types, so its rules are unit-testable.
 */
final class AxxExecutable {
    /** The default setting: the {@code axx} command on {@code PATH}. */
    static final String DEFAULT = "axx";

    private AxxExecutable() {}

    /** The setting as stored: trimmed, with a blank value meaning {@link #DEFAULT}. */
    static @NotNull String normalize(@Nullable String setting) {
        return setting == null || setting.isBlank() ? DEFAULT : setting.strip();
    }

    /**
     * Resolves the setting.
     *
     * @param setting the configured command name or path
     * @param projectDir the project directory, where relative paths start
     * @param onPath finds a command name on {@code PATH}; returns null when it is not there
     * @param installLocations the files to try, in order, when the setting is the default {@code
     *     axx} and it is not on {@code PATH} (see {@link #installLocations})
     * @return the executable, or null when it does not exist
     */
    static @Nullable Path resolve(
            @Nullable String setting,
            @NotNull Path projectDir,
            @NotNull Function<String, @Nullable Path> onPath,
            @NotNull List<Path> installLocations) {
        String value = normalize(setting);
        if (!isPath(value)) {
            Path found = onPath.apply(value);
            if (found != null || !value.equals(DEFAULT)) {
                return found;
            }
            for (Path candidate : installLocations) {
                if (isExecutableFile(candidate)) {
                    return candidate;
                }
            }
            return null;
        }
        try {
            Path path = projectDir.resolve(value).normalize();
            return Files.isRegularFile(path) ? path : null;
        } catch (InvalidPathException e) {
            return null;
        }
    }

    /**
     * Where axx's installers put the binary, in the order {@link #resolve} tries them:
     *
     * <ol>
     *   <li>{@code go install}: {@code $GOBIN}, then the {@code bin} directory of the first {@code
     *       $GOPATH} entry ({@code ~/go/bin} when {@code GOPATH} is not set);
     *   <li>the install script: {@code ~/.local/bin} ({@code %LOCALAPPDATA%\axx\bin} first on
     *       Windows);
     *   <li>Homebrew and the install script's other choice: {@code /opt/homebrew/bin} and {@code
     *       /usr/local/bin} (not on Windows).
     * </ol>
     *
     * @param env reads an environment variable; returns null when it is not set
     * @param home the user's home directory
     * @param windows whether the binary is {@code axx.exe} and Unix-only locations are skipped
     */
    static @NotNull List<Path> installLocations(
            @NotNull Function<String, @Nullable String> env, @NotNull Path home, boolean windows) {
        String name = windows ? DEFAULT + ".exe" : DEFAULT;
        Set<Path> dirs = new LinkedHashSet<>();
        addDir(dirs, env.apply("GOBIN"));
        String gopath = env.apply("GOPATH");
        String firstGopath =
                gopath == null ? "" : gopath.split(Pattern.quote(File.pathSeparator), -1)[0];
        if (firstGopath.isBlank()) {
            dirs.add(home.resolve("go").resolve("bin"));
        } else {
            addDir(dirs, firstGopath.strip() + File.separator + "bin");
        }
        if (windows) {
            String localAppData = env.apply("LOCALAPPDATA");
            if (localAppData != null && !localAppData.isBlank()) {
                String sep = File.separator;
                addDir(dirs, localAppData.strip() + sep + "axx" + sep + "bin");
            }
        }
        dirs.add(home.resolve(".local").resolve("bin"));
        if (!windows) {
            dirs.add(Path.of("/opt/homebrew/bin"));
            dirs.add(Path.of("/usr/local/bin"));
        }
        List<Path> files = new ArrayList<>();
        for (Path dir : dirs) {
            files.add(dir.resolve(name));
        }
        return files;
    }

    private static void addDir(Set<Path> dirs, @Nullable String dir) {
        if (dir == null || dir.isBlank()) {
            return;
        }
        try {
            dirs.add(Path.of(dir.strip()).normalize());
        } catch (InvalidPathException e) {
            // Not a usable directory.
        }
    }

    private static boolean isExecutableFile(Path file) {
        return Files.isRegularFile(file) && Files.isExecutable(file);
    }

    /**
     * Finds a command in the directories of a {@code PATH} value, the first match winning, as a
     * shell does.
     *
     * @param command the command name
     * @param pathValue the {@code PATH} value, directories separated by {@link File#pathSeparator}
     * @param extensions the executable extensions to try on Windows ({@code PATHEXT}, such as
     *     {@code .exe}); empty elsewhere
     * @return the executable file, or null when no directory has it
     */
    static @Nullable Path findOnPath(
            @NotNull String command, @Nullable String pathValue, @NotNull List<String> extensions) {
        if (pathValue == null) {
            return null;
        }
        List<String> names = new ArrayList<>();
        for (String extension : extensions) {
            names.add(command + (extension.startsWith(".") ? "" : ".") + extension);
        }
        names.add(command);
        for (String dir : pathValue.split(Pattern.quote(File.pathSeparator))) {
            if (dir.isBlank()) {
                continue;
            }
            for (String name : names) {
                try {
                    Path file = Path.of(dir).resolve(name);
                    if (isExecutableFile(file)) {
                        return file;
                    }
                } catch (InvalidPathException e) {
                    break;
                }
            }
        }
        return null;
    }

    /**
     * The error shown when the setting does not resolve.
     *
     * @param action what could not be done, such as "start the axx language server"
     * @param setting the configured command name or path
     * @param installLocations the files {@link #resolve} tried for the default command
     * @param home the user's home directory, shown as {@code ~}
     */
    static @NotNull String notFoundMessage(
            @NotNull String action,
            @Nullable String setting,
            @NotNull List<Path> installLocations,
            @NotNull Path home) {
        String value = normalize(setting);
        String problem;
        if (isPath(value)) {
            problem = "'" + value + "' does not exist";
        } else if (value.equals(DEFAULT) && !installLocations.isEmpty()) {
            List<String> dirs = new ArrayList<>();
            for (Path file : installLocations) {
                dirs.add(abbreviate(file.getParent(), home));
            }
            problem = "the 'axx' command is not on PATH, nor in " + String.join(", ", dirs);
        } else {
            problem = "the '" + value + "' command is not on PATH";
        }
        return "Cannot " + action + ": "
                + problem
                + ". Install axx, or set the axx executable in Settings | Tools | axx.";
    }

    /** A directory for display, with the home directory shown as {@code ~}. */
    private static String abbreviate(Path dir, Path home) {
        if (dir.equals(home)) {
            return "~";
        }
        return dir.startsWith(home) ? "~" + File.separator + home.relativize(dir) : dir.toString();
    }

    private static boolean isPath(String value) {
        return value.indexOf('/') >= 0 || value.indexOf(File.separatorChar) >= 0;
    }
}
