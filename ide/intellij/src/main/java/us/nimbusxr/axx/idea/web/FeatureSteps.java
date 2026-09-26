// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.ArrayList;
import java.util.List;
import java.util.regex.Pattern;

/**
 * The steps in a feature file's text, for what the IDE does with a paused scenario: which lines are
 * steps, a step with the rows of its table, and recorded steps to insert. It reads the text, not
 * the Gherkin plugin's PSI, so it works without that plugin; English keywords only.
 *
 * <p>A step's line starts with Given, When, Then, And, But or {@code *} and more text, in a
 * Background, Scenario, Scenario Outline or Example, outside doc strings. Lines are zero-based.
 */
public final class FeatureSteps {
    private static final Pattern STEP = Pattern.compile("^\\s*(?:Given|When|Then|And|But|\\*)\\s+\\S.*$");
    private static final Pattern WITH_STEPS =
            Pattern.compile(
                    "^\\s*(?:Background|Scenario Outline|Scenario Template|Scenario|Example)\\s*:.*$");
    private static final Pattern WITHOUT_STEPS =
            Pattern.compile("^\\s*(?:Feature|Business Need|Ability|Rule|Examples|Scenarios)\\s*:.*$");
    private static final Pattern LINE_BREAK = Pattern.compile("\\r\\n|\\r|\\n");

    private FeatureSteps() {}

    /** The lines of a text. */
    public static @NotNull List<String> lines(@NotNull CharSequence text) {
        return List.of(LINE_BREAK.split(text, -1));
    }

    /** The step on a line, trimmed, such as {@code And the "Quote" button is clicked}; else null. */
    public static @Nullable String stepAt(@NotNull List<String> lines, int line) {
        if (line < 0 || line >= lines.size()) {
            return null;
        }
        boolean inSteps = false;
        String docString = null; // the open doc string's delimiter
        for (int i = 0; i <= line; i++) {
            String trimmed = lines.get(i).strip();
            if (docString != null) {
                if (trimmed.startsWith(docString)) {
                    docString = null;
                }
                continue;
            }
            if (trimmed.startsWith("\"\"\"") || trimmed.startsWith("```")) {
                docString = trimmed.substring(0, 3);
                continue;
            }
            if (WITH_STEPS.matcher(trimmed).matches()) {
                inSteps = true;
            } else if (WITHOUT_STEPS.matcher(trimmed).matches()) {
                inSteps = false;
            } else if (i == line && inSteps && STEP.matcher(trimmed).matches()) {
                return trimmed;
            }
        }
        return null;
    }

    /**
     * The step on a line with the rows of its data table, the lines starting with {@code |} right
     * below it, each trimmed, one per line; null when the line is not a step's.
     */
    public static @Nullable String stepWithTable(@NotNull List<String> lines, int line) {
        String step = stepAt(lines, line);
        if (step == null) {
            return null;
        }
        StringBuilder text = new StringBuilder(step);
        for (int i = line + 1; i < lines.size(); i++) {
            String row = lines.get(i).strip();
            if (!row.startsWith("|")) {
                break;
            }
            text.append('\n').append(row);
        }
        return text.toString();
    }

    /** The whitespace a line starts with. */
    public static @NotNull String indentOf(@NotNull String line) {
        int end = 0;
        while (end < line.length() && Character.isWhitespace(line.charAt(end))) {
            end++;
        }
        return line.substring(0, end);
    }

    /**
     * Recorded steps, re-indented: their lines lose the indentation they share and start with
     * {@code indent} instead, keeping the rest; blank lines become empty, and blank lines at the
     * start and end are dropped. With a null indent, the lines keep theirs.
     */
    public static @NotNull String reindent(@NotNull String steps, @Nullable String indent) {
        List<String> lines = new ArrayList<>(lines(steps));
        while (!lines.isEmpty() && lines.get(0).isBlank()) {
            lines.remove(0);
        }
        while (!lines.isEmpty() && lines.get(lines.size() - 1).isBlank()) {
            lines.remove(lines.size() - 1);
        }
        int common = Integer.MAX_VALUE;
        for (String line : lines) {
            if (!line.isBlank()) {
                common = Math.min(common, indentOf(line).length());
            }
        }
        List<String> out = new ArrayList<>(lines.size());
        for (String line : lines) {
            if (line.isBlank()) {
                out.add("");
            } else if (indent == null) {
                out.add(line.stripTrailing());
            } else {
                out.add(indent + line.substring(common).stripTrailing());
            }
        }
        return String.join("\n", out);
    }

    /**
     * The recorded steps in the text of the recording file a paused scenario names: without its
     * first line when that is a comment (which says what the file is).
     */
    public static @NotNull String fromRecordingFile(@NotNull String text) {
        int eol = text.indexOf('\n');
        String first = eol < 0 ? text : text.substring(0, eol);
        if (!first.strip().startsWith("#")) {
            return text;
        }
        return eol < 0 ? "" : text.substring(eol + 1);
    }
}
