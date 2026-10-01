// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.codeInsight.daemon.impl.HighlightInfo;
import com.intellij.codeInspection.LocalInspectionTool;
import com.intellij.codeInspection.ProblemsHolder;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.fileEditor.FileEditorManager;
import com.intellij.openapi.fileEditor.OpenFileDescriptor;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.psi.PsiElementVisitor;
import com.intellij.psi.PsiFile;
import com.intellij.psi.PsiManager;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.InspectionsKt;
import com.intellij.testFramework.PsiTestUtil;
import com.intellij.testFramework.fixtures.impl.CodeInsightTestFixtureImpl;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.plugins.cucumber.inspections.CucumberStepInspection;
import org.jetbrains.plugins.cucumber.psi.GherkinElementVisitor;
import org.jetbrains.plugins.cucumber.psi.GherkinStep;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashSet;
import java.util.Set;

/**
 * The undefined step inspections, with the Gherkin plugin, Cucumber for Java and Cucumber.js
 * installed.
 */
public class AxxUndefinedStepSuppressorTest extends HeavyPlatformTestCase {
    private static final String FEATURE =
            String.join(
                    "\n",
                    "Feature: Orders",
                    "",
                    "  Scenario: List orders",
                    "    When a GET request is sent to \"/orders\"",
                    "    Then the response status code is 200",
                    "");

    public void testAxxProjectsHideThem() throws Exception {
        assertEquals(Set.of(), undefinedStepWarnings(true));
    }

    public void testOtherProjectsKeepThem() throws Exception {
        assertEquals(
                AxxUndefinedStepSuppressor.UNDEFINED_STEP_INSPECTIONS, undefinedStepWarnings(false));
    }

    /** The undefined step inspections that report on the feature file's steps. */
    private Set<String> undefinedStepWarnings(boolean axxProject) throws Exception {
        Path base = Path.of(getProject().getBasePath());
        Path suite = Files.createDirectories(base.resolve("acceptance"));
        if (axxProject) {
            Files.writeString(suite.resolve("axx.yaml"), "version: 1\n");
        }
        Files.createDirectories(suite.resolve("features"));
        Files.writeString(suite.resolve("features/orders.feature"), FEATURE);
        VirtualFile root = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(base);
        VfsUtil.markDirtyAndRefresh(false, true, true, root);
        PsiTestUtil.addContentRoot(getModule(), root);
        VirtualFile file = root.findFileByRelativePath("acceptance/features/orders.feature");

        InspectionsKt.enableInspectionTools(
                getProject(),
                getTestRootDisposable(),
                new CucumberStepInspection(),
                new CucumberPlusUndefinedStep());
        Editor editor =
                FileEditorManager.getInstance(getProject())
                        .openTextEditor(new OpenFileDescriptor(getProject(), file), false);
        PsiFile psi = PsiManager.getInstance(getProject()).findFile(file);
        // The axx language server, in an axx project, restarts highlighting as it starts.
        Set<String> reported = new HashSet<>();
        for (HighlightInfo info :
                CodeInsightTestFixtureImpl.instantiateAndRun(psi, editor, new int[0], true)) {
            String tool = info.getInspectionToolId();
            if (AxxUndefinedStepSuppressor.UNDEFINED_STEP_INSPECTIONS.contains(tool)) {
                reported.add(tool);
            }
        }
        return reported;
    }

    /**
     * Stands in for Cucumber+'s undefined step inspection, which reports every step the Cucumber
     * plugins find no definition for. Cucumber+ itself cannot be loaded in these tests: it brings
     * its own Kotlin, which their single class loader would put before the platform's.
     */
    private static final class CucumberPlusUndefinedStep extends LocalInspectionTool {
        @Override
        public @NotNull String getShortName() {
            return "CucumberPlusUndefinedStep";
        }

        @Override
        public @NotNull PsiElementVisitor buildVisitor(
                @NotNull ProblemsHolder holder, boolean isOnTheFly) {
            return new GherkinElementVisitor() {
                @Override
                public void visitStep(GherkinStep step) {
                    holder.registerProblem(step, "Undefined step");
                }
            };
        }
    }
}
