// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import com.intellij.codeInsight.completion.CompletionParameters;
import com.intellij.openapi.editor.Document;
import com.intellij.platform.lsp.api.customization.LspCompletionSupport;

import org.jetbrains.annotations.NotNull;

/**
 * Matches the server's completions against the whole step text, or the whole table cell text,
 * typed so far.
 *
 * <p>The server completes the step text after the keyword: each item's text edit replaces that
 * text, and its filterText is what is typed of it. In a table cell that names a file, it completes
 * the cell's path the same way. By default the IDE matches items against the word before the caret
 * only, which a multi-word filterText, or a path, does not match.
 */
final class AxxCompletionSupport extends LspCompletionSupport {
    @Override
    public @NotNull String getCompletionPrefix(
            @NotNull CompletionParameters parameters, @NotNull String defaultPrefix) {
        Document document = parameters.getEditor().getDocument();
        int offset = parameters.getOffset();
        int lineStart = document.getLineStartOffset(document.getLineNumber(offset));
        CharSequence text = document.getImmutableCharSequence();
        CharSequence before = text.subSequence(lineStart, offset);
        String typed = StepText.typedBefore(before);
        if (typed == null) {
            typed = StepText.cellBefore(before);
        }
        return typed != null ? typed : defaultPrefix;
    }
}
