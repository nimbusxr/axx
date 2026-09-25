// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.execution.lineMarker.RunLineMarkerContributor;
import com.intellij.openapi.actionSystem.AnAction;
import com.intellij.openapi.project.DumbAware;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.psi.PsiElement;
import com.intellij.psi.PsiFile;
import com.intellij.psi.tree.IElementType;
import com.intellij.psi.util.PsiTreeUtil;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.jetbrains.plugins.cucumber.psi.GherkinRule;
import org.jetbrains.plugins.cucumber.psi.GherkinScenario;
import org.jetbrains.plugins.cucumber.psi.GherkinScenarioOutline;
import org.jetbrains.plugins.cucumber.psi.GherkinStepsHolder;
import org.jetbrains.plugins.cucumber.psi.GherkinTableRow;
import org.jetbrains.plugins.cucumber.psi.GherkinTokenTypes;

import us.nimbusxr.axx.idea.AxxProject;
import us.nimbusxr.axx.idea.run.RunTargets;

import java.util.function.Function;

import javax.swing.Icon;

/**
 * Run and debug icons in the gutter of feature files in axx projects: on the Feature line, each
 * Rule, Scenario and Scenario Outline line, and each Examples row. The icon shows the last run's
 * result for that line.
 */
public final class AxxRunLineMarkerContributor extends RunLineMarkerContributor
        implements DumbAware {

    @Override
    public @Nullable Info getInfo(@NotNull PsiElement element) {
        if (element.getFirstChild() != null) {
            return null;
        }
        IElementType type = element.getNode().getElementType();
        boolean suite;
        if (type == GherkinTokenTypes.FEATURE_KEYWORD) {
            suite = true;
        } else if (type == GherkinTokenTypes.RULE_KEYWORD) {
            if (!(element.getParent() instanceof GherkinRule rule)
                    || rule.getScenarios().length == 0) {
                return null;
            }
            suite = true;
        } else if (GherkinTokenTypes.SCENARIOS_KEYWORDS.contains(type)) {
            PsiElement parent = element.getParent();
            if (!(parent instanceof GherkinStepsHolder)
                    || (parent instanceof GherkinScenario scenario && scenario.isBackground())) {
                return null;
            }
            suite = parent instanceof GherkinScenarioOutline;
        } else if (type == GherkinTokenTypes.PIPE) {
            if (!(element.getParent() instanceof GherkinTableRow row)
                    || PsiTreeUtil.getDeepestFirst(row) != element
                    || !AxxRunSelection.isExamplesRow(row)) {
                return null;
            }
            suite = false;
        } else {
            return null;
        }
        if (!AxxProject.isAxxProject(element.getProject())) {
            return null;
        }
        PsiFile file = element.getContainingFile();
        VirtualFile virtualFile = file.getVirtualFile();
        if (virtualFile == null) {
            return null;
        }
        int line = AxxRunSelection.lineOf(file, element);
        String url = RunTargets.locationUrl(virtualFile.getPath(), line);
        Icon icon = getTestStateIcon(url, element.getProject(), suite);
        Info info = withExecutorActions(icon);
        return new AxxInfo(info.icon, info.actions, info.tooltipProvider);
    }

    /**
     * Replaces the Gherkin plugin's own run marker on the same line. In axx projects its Run and
     * Debug actions create axx's run configurations too, so both markers together would list
     * every action twice.
     */
    private static final class AxxInfo extends Info {
        AxxInfo(
                Icon icon,
                AnAction[] actions,
                Function<? super PsiElement, String> tooltipProvider) {
            super(icon, actions, tooltipProvider);
        }

        @Override
        public boolean shouldReplace(@NotNull Info other) {
            return true;
        }
    }
}
