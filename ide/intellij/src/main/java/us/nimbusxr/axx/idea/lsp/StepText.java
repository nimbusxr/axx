// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.List;

/**
 * The step text of a Gherkin step line: what follows the step keyword ({@code Given}, {@code When},
 * {@code Then}, {@code And}, {@code But} or {@code *}) and the whitespace after it. And the text of
 * a table cell.
 *
 * <p>It holds no IDE types, so its rules are unit-testable.
 */
final class StepText {
    /** The step keywords, as the axx language server knows them. */
    static final List<String> KEYWORDS = List.of("Given", "When", "Then", "And", "But", "*");

    private StepText() {}

    /**
     * The step text typed before the caret.
     *
     * @param lineBeforeCaret the caret's line, from its start up to the caret
     * @return the text after the keyword and its whitespace (empty right after them), or null when
     *     the line is not a step or the caret is not yet past the keyword's whitespace
     */
    static @Nullable String typedBefore(@NotNull CharSequence lineBeforeCaret) {
        String line = lineBeforeCaret.toString();
        int start = skipBlanks(line, 0);
        for (String keyword : KEYWORDS) {
            if (!line.startsWith(keyword, start)) {
                continue;
            }
            int end = start + keyword.length();
            if (end == line.length() || !isBlank(line.charAt(end))) {
                // "Given" with the caret on it, or a word that only starts like a keyword.
                return null;
            }
            return line.substring(skipBlanks(line, end));
        }
        return null;
    }

    /**
     * The table cell text typed before the caret.
     *
     * @param lineBeforeCaret the caret's line, from its start up to the caret
     * @return the text of the caret's cell after its leading whitespace (empty right after the pipe
     *     and whitespace), or null when the line is not a table row
     */
    static @Nullable String cellBefore(@NotNull CharSequence lineBeforeCaret) {
        String line = lineBeforeCaret.toString();
        int start = skipBlanks(line, 0);
        if (start == line.length() || line.charAt(start) != '|') {
            return null;
        }
        int cell = start + 1;
        for (int i = cell; i < line.length(); i++) {
            char c = line.charAt(i);
            if (c == '\\') {
                i++; // an escaped character, such as \|, is part of the cell
            } else if (c == '|') {
                cell = i + 1;
            }
        }
        return line.substring(skipBlanks(line, Math.min(cell, line.length())));
    }

    private static int skipBlanks(String line, int from) {
        int i = from;
        while (i < line.length() && isBlank(line.charAt(i))) {
            i++;
        }
        return i;
    }

    private static boolean isBlank(char c) {
        return c == ' ' || c == '\t';
    }
}
