// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.IOException;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;

/**
 * Finds the axx config file ({@code axx.yaml} or {@code axx.yml}) that {@code axx lsp}, started in
 * a directory, works with. It follows the language server's own search, so the plugin starts the
 * server exactly where the server finds a project:
 *
 * <ol>
 *   <li>upward from the directory, up to the repository root (the directory that holds {@code
 *       .git});
 *   <li>otherwise the shallowest config in the subdirectories, at most {@link #MAX_DEPTH} levels
 *       down, skipping hidden, dependency and build output directories.
 * </ol>
 *
 * <p>{@code axx run} itself only searches upward (see {@link #nearestAbove}).
 *
 * <p>It holds no IDE types, so its rules are unit-testable.
 */
public final class ConfigFinder {
    /** The config file names axx looks for, in order. */
    static final List<String> CONFIG_NAMES = List.of("axx.yaml", "axx.yml");

    /** How many directory levels below the start directory are searched. */
    static final int MAX_DEPTH = 3;

    private static final Set<String> SKIPPED_DIRS =
            Set.of("node_modules", "vendor", "build", "dist", "target", "out", "bin");

    private ConfigFinder() {}

    /**
     * Finds the config file for a directory.
     *
     * @param dir the directory the language server starts in (the IDE project's directory)
     * @return the config file, or null when there is none
     */
    public static @Nullable Path find(@NotNull Path dir) {
        Path start = dir.toAbsolutePath().normalize();
        Path above = nearestAbove(start);
        if (above != null) {
            return above;
        }

        List<Path> level = List.of(start);
        for (int depth = 0; depth < MAX_DEPTH && !level.isEmpty(); depth++) {
            List<Path> next = new ArrayList<>();
            for (Path d : level) {
                for (Path sub : subdirectories(d)) {
                    Path config = configIn(sub);
                    if (config != null) {
                        return config;
                    }
                    next.add(sub);
                }
            }
            level = next;
        }
        return null;
    }

    /**
     * The config file in the directory or the nearest one above it, up to the repository root (the
     * directory that holds {@code .git}): the one {@code axx} uses when it runs in that directory.
     *
     * @param dir a directory
     * @return the config file, or null when there is none
     */
    public static @Nullable Path nearestAbove(@NotNull Path dir) {
        for (Path d = dir.toAbsolutePath().normalize(); d != null; d = d.getParent()) {
            Path config = configIn(d);
            if (config != null) {
                return config;
            }
            if (Files.exists(d.resolve(".git"))) {
                break;
            }
        }
        return null;
    }

    private static @Nullable Path configIn(Path dir) {
        for (String name : CONFIG_NAMES) {
            Path config = dir.resolve(name);
            if (Files.exists(config)) {
                return config;
            }
        }
        return null;
    }

    /** The directories to search below {@code dir}, sorted by name, without following links. */
    private static List<Path> subdirectories(Path dir) {
        List<Path> dirs = new ArrayList<>();
        try (DirectoryStream<Path> entries = Files.newDirectoryStream(dir)) {
            for (Path entry : entries) {
                if (Files.isDirectory(entry, LinkOption.NOFOLLOW_LINKS)
                        && !isSkipped(entry.getFileName().toString())) {
                    dirs.add(entry);
                }
            }
        } catch (IOException | SecurityException e) {
            return List.of();
        }
        dirs.sort(null);
        return dirs;
    }

    private static boolean isSkipped(String name) {
        return name.startsWith(".") || SKIPPED_DIRS.contains(name);
    }
}
