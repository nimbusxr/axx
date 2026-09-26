// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.openapi.editor.Document;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.psi.PsiDocumentManager;
import com.intellij.psi.PsiElement;
import com.intellij.psi.PsiFile;
import com.intellij.xdebugger.breakpoints.XBreakpointProperties;
import com.intellij.xdebugger.breakpoints.XLineBreakpointType;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.jetbrains.plugins.cucumber.psi.GherkinFile;
import org.jetbrains.plugins.cucumber.psi.GherkinStep;
import org.jetbrains.plugins.cucumber.psi.GherkinTokenTypes;

import us.nimbusxr.axx.idea.AxxProject;
import us.nimbusxr.axx.idea.run.AxxStepBreakpoints;

import java.util.EnumSet;

/**
 * Breakpoints on the steps of feature files in axx projects, set in the gutter of a step's line.
 * No debugger stops at them: runs with Debug pause before their steps ({@code axx run --pause-at}),
 * with the web pack's page in Playwright's Inspector (see {@link AxxStepBreakpoints}).
 */
public final class AxxStepBreakpointType extends XLineBreakpointType<XBreakpointProperties<?>> {
    private static final String FEATURE_EXTENSION = "feature";

    public AxxStepBreakpointType() {
        super(AxxStepBreakpoints.TYPE_ID, "axx Step Breakpoints");
    }

    /** Only on the lines of steps: a line that starts with a step's keyword. */
    @Override
    public boolean canPutAt(@NotNull VirtualFile file, int line, @NotNull Project project) {
        if (!FEATURE_EXTENSION.equals(file.getExtension()) || !AxxProject.isAxxProject(project)) {
            return false;
        }
        Document document = FileDocumentManager.getInstance().getDocument(file);
        if (document == null) {
            return false;
        }
        PsiFile psi = PsiDocumentManager.getInstance(project).getPsiFile(document);
        return psi instanceof GherkinFile && isStepLine(psi, document, line);
    }

    /** Whether a line (zero-based) of a feature file starts with a step's keyword. */
    static boolean isStepLine(@NotNull PsiFile file, @NotNull Document document, int line) {
        if (line < 0 || line >= document.getLineCount()) {
            return false;
        }
        CharSequence text = document.getImmutableCharSequence();
        int offset = document.getLineStartOffset(line);
        int end = document.getLineEndOffset(line);
        while (offset < end && Character.isWhitespace(text.charAt(offset))) {
            offset++;
        }
        if (offset == end) {
            return false;
        }
        PsiElement leaf = file.findElementAt(offset);
        return leaf != null
                && leaf.getNode().getElementType() == GherkinTokenTypes.STEP_KEYWORD
                && leaf.getParent() instanceof GherkinStep;
    }

    @Override
    public @Nullable XBreakpointProperties<?> createBreakpointProperties(
            @NotNull VirtualFile file, int line) {
        return null;
    }

    /** No suspend policy, logging or dependencies: nothing but the pause applies to a step. */
    @Override
    public @NotNull EnumSet<StandardPanels> getVisibleStandardPanels() {
        return EnumSet.noneOf(StandardPanels.class);
    }
}
