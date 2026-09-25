// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.codeInspection.InspectionSuppressor;
import com.intellij.codeInspection.SuppressQuickFix;
import com.intellij.openapi.project.DumbAware;
import com.intellij.psi.PsiElement;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxProject;

/**
 * Keeps the Gherkin plugin from reporting axx steps as undefined in axx projects.
 *
 * <p>axx's steps are defined in Go, inside the axx binary, where the Cucumber plugins cannot see
 * them, so their "Undefined step reference" inspection would flag every step. In an axx project the
 * axx language server reports the steps axx does not define instead.
 */
public final class AxxUndefinedStepSuppressor implements InspectionSuppressor, DumbAware {
    /**
     * The short name of the Gherkin plugin's undefined step inspection ({@code
     * org.jetbrains.plugins.cucumber.inspections.CucumberStepInspection}).
     */
    static final String UNDEFINED_STEP_INSPECTION = "CucumberUndefinedStep";

    @Override
    public boolean isSuppressedFor(@NotNull PsiElement element, @NotNull String toolId) {
        return UNDEFINED_STEP_INSPECTION.equals(toolId)
                && AxxProject.isAxxProject(element.getProject());
    }

    @Override
    public SuppressQuickFix @NotNull [] getSuppressActions(
            @Nullable PsiElement element, @NotNull String toolId) {
        return SuppressQuickFix.EMPTY_ARRAY;
    }
}
