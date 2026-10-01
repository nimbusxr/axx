// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;
import java.util.TreeSet;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Stream;

/**
 * The profiles of an axx project, as {@code --profile} takes them: the keys of {@code profiles} in
 * its config file, in their order, then the names of the {@code axx.<name>.yaml} files next to it
 * ({@code axx.local.yaml} always applies, so it is none).
 *
 * <p>The VS Code extension finds them the same way (ide/vscode/src/profiles.ts). It holds no IDE
 * types, so its rules are unit-testable.
 */
public final class AxxProfiles {
    private static final Pattern BLOCK = Pattern.compile("^profiles\\s*:\\s*(#.*)?$");
    private static final Pattern FLOW = Pattern.compile("^profiles\\s*:\\s*\\{(.*)}\\s*(#.*)?$");
    private static final Pattern FILE = Pattern.compile("^axx\\.(.+)\\.yaml$");

    private AxxProfiles() {}

    /**
     * Finds the profiles of the axx project in a directory.
     *
     * @param dir the directory of the project's axx.yaml
     * @return the profile names, without duplicates; empty when there are none
     */
    public static @NotNull List<String> find(@NotNull Path dir) {
        Set<String> out = new LinkedHashSet<>();
        for (String name : ConfigFinder.CONFIG_NAMES) {
            Path config = dir.resolve(name);
            if (Files.isRegularFile(config)) {
                try {
                    out.addAll(keys(Files.readAllLines(config)));
                } catch (IOException | RuntimeException e) {
                    // An unreadable config has no profiles to offer; axx run says what is wrong.
                }
                break;
            }
        }
        Set<String> files = new TreeSet<>();
        try (Stream<Path> entries = Files.list(dir)) {
            entries.forEach(
                    p -> {
                        Matcher m = FILE.matcher(p.getFileName().toString());
                        if (m.matches() && !m.group(1).equals("local") && Files.isRegularFile(p)) {
                            files.add(m.group(1));
                        }
                    });
        } catch (IOException e) {
            // No directory, no profile files.
        }
        out.addAll(files);
        return new ArrayList<>(out);
    }

    /** The keys of the top-level {@code profiles} mapping of a config file's lines. */
    static @NotNull List<String> keys(@NotNull List<String> lines) {
        List<String> keys = new ArrayList<>();
        int i = 0;
        for (; i < lines.size(); i++) {
            String line = lines.get(i);
            Matcher flow = FLOW.matcher(line);
            if (flow.matches()) {
                return flowKeys(flow.group(1));
            }
            if (BLOCK.matcher(line).matches()) {
                break;
            }
        }
        int indent = -1;
        for (i++; i < lines.size(); i++) {
            String line = lines.get(i);
            String text = line.strip();
            if (text.isEmpty() || text.startsWith("#")) {
                continue;
            }
            int at = line.length() - line.stripLeading().length();
            if (at == 0) {
                break; // the next top-level key
            }
            if (indent < 0) {
                indent = at;
            }
            if (at < indent) {
                break;
            }
            if (at == indent && !text.startsWith("-")) {
                String key = key(text);
                if (!key.isEmpty()) {
                    keys.add(key);
                }
            }
        }
        return keys;
    }

    /** The keys of a one-line flow mapping's content: {@code a: {..}, b: x}. */
    private static List<String> flowKeys(String content) {
        List<String> keys = new ArrayList<>();
        int depth = 0;
        StringBuilder entry = new StringBuilder();
        for (char c : (content + ",").toCharArray()) {
            if (c == '{' || c == '[') {
                depth++;
            } else if (c == '}' || c == ']') {
                depth--;
            } else if (c == ',' && depth == 0) {
                String key = key(entry.toString().strip());
                if (!key.isEmpty()) {
                    keys.add(key);
                }
                entry.setLength(0);
                continue;
            }
            entry.append(c);
        }
        return keys;
    }

    /** The key of a {@code key: value} entry, unquoted; empty when it has none. */
    private static String key(String entry) {
        if (entry.startsWith("\"") || entry.startsWith("'")) {
            int end = entry.indexOf(entry.charAt(0), 1);
            return end > 0 ? entry.substring(1, end) : "";
        }
        int colon = entry.indexOf(':');
        return colon > 0 ? entry.substring(0, colon).strip() : "";
    }

    /** Profiles as the run configuration stores them and {@code --profile} takes them. */
    public static @NotNull String join(@NotNull List<String> profiles) {
        return String.join(",", profiles);
    }

    /** The profiles of a stored {@code a,b} value, in order. */
    public static @NotNull List<String> parse(@NotNull String value) {
        List<String> out = new ArrayList<>();
        for (String p : value.split(",")) {
            if (!p.isBlank()) {
                out.add(p.strip());
            }
        }
        return out;
    }
}
