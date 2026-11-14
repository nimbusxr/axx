// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.CoverageAnnotator;
import com.intellij.coverage.CoverageEngine;
import com.intellij.coverage.CoverageFileProvider;
import com.intellij.coverage.CoverageRunner;
import com.intellij.coverage.CoverageSuite;
import com.intellij.coverage.CoverageSuitesBundle;
import com.intellij.execution.configurations.RunConfigurationBase;
import com.intellij.execution.configurations.coverage.CoverageEnabledConfiguration;
import com.intellij.openapi.project.Project;
import com.intellij.psi.PsiFile;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.run.AxxRunConfiguration;

/**
 * The coverage of the system under test, as a run with coverage measures it ({@code axx run
 * --coverage}): the lines of the files of every language its lcov report names, in the editor's
 * gutters, the project view and the Coverage tool window.
 */
public final class AxxCoverageEngine extends CoverageEngine {
    @Override
    public @NotNull String getPresentableText() {
        return "axx";
    }

    @Override
    public boolean isApplicableTo(@NotNull RunConfigurationBase<?> configuration) {
        return AxxRunConfiguration.of(configuration) != null;
    }

    @Override
    public @NotNull CoverageEnabledConfiguration createCoverageEnabledConfiguration(
            @NotNull RunConfigurationBase<?> configuration) {
        return new AxxCoverageEnabledConfiguration(configuration);
    }

    @Override
    public @Nullable CoverageSuite createCoverageSuite(
            @NotNull String name,
            @NotNull Project project,
            @NotNull CoverageRunner runner,
            @NotNull CoverageFileProvider fileProvider,
            long timestamp) {
        return new AxxCoverageSuite(name, project, runner, fileProvider, timestamp);
    }

    /** A run's suite knows its run configuration: the report's relative paths are its project's. */
    @Override
    public @Nullable CoverageSuite createCoverageSuite(
            @NotNull String name,
            @NotNull Project project,
            @NotNull CoverageRunner runner,
            @NotNull CoverageFileProvider fileProvider,
            long timestamp,
            @NotNull CoverageEnabledConfiguration config) {
        AxxCoverageSuite suite = new AxxCoverageSuite(name, project, runner, fileProvider, timestamp);
        suite.setConfiguration(config.getConfiguration());
        return suite;
    }

    @Override
    public @NotNull CoverageSuite createEmptyCoverageSuite(@NotNull CoverageRunner runner) {
        return new AxxCoverageSuite();
    }

    @Override
    public @NotNull CoverageAnnotator getCoverageAnnotator(@NotNull Project project) {
        return AxxCoverageAnnotator.getInstance(project);
    }

    /** Every language's files: the report names the files of the services' every runtime. */
    @Override
    public boolean coverageEditorHighlightingApplicableTo(@NotNull PsiFile file) {
        return true;
    }

    @Override
    public boolean acceptedByFilters(@NotNull PsiFile file, @NotNull CoverageSuitesBundle suite) {
        return true;
    }
}
