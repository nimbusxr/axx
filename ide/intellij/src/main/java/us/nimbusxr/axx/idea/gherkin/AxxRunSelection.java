// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.lang.ASTNode;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.vfs.VfsUtilCore;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.openapi.vfs.VirtualFileVisitor;
import com.intellij.psi.PsiDirectory;
import com.intellij.psi.PsiElement;
import com.intellij.psi.PsiFile;
import com.intellij.psi.util.PsiTreeUtil;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.jetbrains.plugins.cucumber.psi.GherkinExamplesBlock;
import org.jetbrains.plugins.cucumber.psi.GherkinFeature;
import org.jetbrains.plugins.cucumber.psi.GherkinRule;
import org.jetbrains.plugins.cucumber.psi.GherkinScenario;
import org.jetbrains.plugins.cucumber.psi.GherkinScenarioOutline;
import org.jetbrains.plugins.cucumber.psi.GherkinStepsHolder;
import org.jetbrains.plugins.cucumber.psi.GherkinTable;
import org.jetbrains.plugins.cucumber.psi.GherkinTableRow;
import org.jetbrains.plugins.cucumber.psi.GherkinTokenTypes;

import us.nimbusxr.axx.idea.AxxProject;
import us.nimbusxr.axx.idea.ConfigFinder;
import us.nimbusxr.axx.idea.run.RunTargets;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * What to run for a place in the IDE: a Feature line runs its file (axx selects nothing by a Feature
 * line), a Rule line runs the lines of the Rule's scenarios (nor by a Rule line), a Scenario or
 * Scenario Outline line (or a line inside one) runs {@code file:line}, an Examples row runs that
 * example, and a feature file or directory runs that path. axx runs in the directory of the axx
 * config above it.
 *
 * @param element the element the run is for
 * @param workingDirectory where axx runs
 * @param targets the targets, relative to the working directory; empty for the whole suite
 * @param name the run configuration's name
 */
record AxxRunSelection(
        @NotNull PsiElement element,
        @NotNull Path workingDirectory,
        @NotNull List<String> targets,
        @NotNull String name) {

    private static final String FEATURE_EXTENSION = "feature";

    /** The selection for a place, or null when it is not in an axx suite. */
    static @Nullable AxxRunSelection of(@Nullable PsiElement location) {
        if (location == null
                || !location.isValid()
                || !AxxProject.isAxxProject(location.getProject())) {
            return null;
        }
        if (location instanceof PsiDirectory directory) {
            return ofDirectory(directory);
        }
        PsiFile file = location.getContainingFile();
        VirtualFile virtualFile = file == null ? null : file.getOriginalFile().getVirtualFile();
        if (virtualFile == null || !FEATURE_EXTENSION.equals(virtualFile.getExtension())) {
            return null;
        }
        Path path = virtualFile.toNioPath();
        Path config = ConfigFinder.nearestAbove(path.getParent());
        if (config == null) {
            return null;
        }
        Path dir = config.getParent();
        if (location instanceof PsiFile) {
            return fileSelection(file, path, dir);
        }

        GherkinTableRow row = PsiTreeUtil.getParentOfType(location, GherkinTableRow.class, false);
        GherkinStepsHolder holder =
                PsiTreeUtil.getParentOfType(location, GherkinStepsHolder.class, false);
        if (row != null && isExamplesRow(row) && holder != null) {
            int line = lineOf(file, row);
            return new AxxRunSelection(
                    row,
                    dir,
                    List.of(RunTargets.target(path, line, dir)),
                    holderName(holder) + " [line " + line + "]");
        }
        if (holder != null && !(holder instanceof GherkinScenario s && s.isBackground())) {
            int line = lineOf(file, keywordOf(holder));
            return new AxxRunSelection(
                    holder, dir, List.of(RunTargets.target(path, line, dir)), holderName(holder));
        }
        GherkinRule rule = PsiTreeUtil.getParentOfType(location, GherkinRule.class, false);
        if (rule != null) {
            List<String> targets = new ArrayList<>();
            for (GherkinStepsHolder scenario : rule.getScenarios()) {
                if (!(scenario instanceof GherkinScenario s && s.isBackground())) {
                    int line = lineOf(file, keywordOf(scenario));
                    targets.add(RunTargets.target(path, line, dir));
                }
            }
            if (!targets.isEmpty()) {
                return new AxxRunSelection(rule, dir, targets, "Rule: " + rule.getRuleName());
            }
        }
        GherkinFeature feature = PsiTreeUtil.getParentOfType(location, GherkinFeature.class, false);
        if (feature != null) {
            return new AxxRunSelection(
                    feature,
                    dir,
                    List.of(RunTargets.target(path, 0, dir)),
                    "Feature: " + feature.getFeatureName());
        }
        return fileSelection(file, path, dir);
    }

    /** Whether a table row is an example: a data row of an outline's Examples table. */
    static boolean isExamplesRow(@NotNull GherkinTableRow row) {
        return row.getParent() instanceof GherkinTable table
                && table.getParent() instanceof GherkinExamplesBlock
                && table.getHeaderRow() != row;
    }

    /** The keyword of a scenario or outline, whose line axx selects it by. */
    static @NotNull PsiElement keywordOf(@NotNull GherkinStepsHolder holder) {
        ASTNode keyword = holder.getNode().findChildByType(GherkinTokenTypes.SCENARIOS_KEYWORDS);
        return keyword != null ? keyword.getPsi() : holder;
    }

    /** The one-based line an element starts on. */
    static int lineOf(@NotNull PsiFile file, @NotNull PsiElement element) {
        Document document = file.getViewProvider().getDocument();
        int offset = element.getTextRange().getStartOffset();
        return document == null ? 1 : document.getLineNumber(offset) + 1;
    }

    private static String holderName(GherkinStepsHolder holder) {
        String keyword = holder instanceof GherkinScenarioOutline ? "Scenario Outline" : "Scenario";
        return keyword + ": " + holder.getScenarioName();
    }

    private static AxxRunSelection fileSelection(PsiFile file, Path path, Path dir) {
        return new AxxRunSelection(
                file, dir, List.of(RunTargets.target(path, 0, dir)), path.getFileName().toString());
    }

    private static @Nullable AxxRunSelection ofDirectory(PsiDirectory directory) {
        Path path = directory.getVirtualFile().toNioPath();
        Path config = ConfigFinder.nearestAbove(path);
        if (config != null) {
            Path dir = config.getParent();
            if (!hasFeatureFile(directory.getVirtualFile())) {
                return null;
            }
            List<String> targets =
                    path.equals(dir) ? List.of() : List.of(RunTargets.target(path, 0, dir));
            return new AxxRunSelection(directory, dir, targets, "axx: " + directory.getName());
        }
        // A directory above the suite, such as the project root: run the whole suite.
        config = ConfigFinder.find(path);
        if (config == null) {
            return null;
        }
        return new AxxRunSelection(
                directory, config.getParent(), List.of(), "axx: " + directory.getName());
    }

    /** Whether a directory holds a feature file, at any depth. */
    private static boolean hasFeatureFile(VirtualFile directory) {
        boolean[] found = {false};
        VfsUtilCore.visitChildrenRecursively(
                directory,
                new VirtualFileVisitor<Void>() {
                    @Override
                    public boolean visitFile(@NotNull VirtualFile file) {
                        if (found[0]) {
                            return false;
                        }
                        if (!file.isDirectory() && FEATURE_EXTENSION.equals(file.getExtension())) {
                            found[0] = true;
                            return false;
                        }
                        return !file.isDirectory() || !file.getName().startsWith(".");
                    }
                });
        return found[0];
    }
}
