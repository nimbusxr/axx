// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.BaseCoverageSuite;
import com.intellij.coverage.CoverageEngine;
import com.intellij.coverage.CoverageLoadErrorReporter;
import com.intellij.coverage.CoverageLoadingResult;
import com.intellij.coverage.CoverageRunner;
import com.intellij.coverage.CoverageSuite;
import com.intellij.coverage.FailedCoverageLoadingResult;
import com.intellij.coverage.SuccessCoverageLoadingResult;
import com.intellij.coverage.lcov.LcovSerializationUtils;
import com.intellij.execution.configurations.RunConfigurationBase;
import com.intellij.rt.coverage.data.ProjectData;

import java.io.File;
import java.io.IOException;
import java.nio.file.Path;
import java.util.List;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.ConfigFinder;

/**
 * Reads the lcov report of a run with coverage, with the platform's lcov reader. The report names
 * the files relative to the axx project: the folder of the axx.yaml above the report, or else the
 * run's working directory.
 */
public final class AxxCoverageRunner extends CoverageRunner {
    @Override
    protected @NotNull CoverageLoadingResult loadCoverageData(
            @NotNull File sessionDataFile,
            @Nullable CoverageSuite suite,
            @NotNull CoverageLoadErrorReporter reporter) {
        Path lcov = sessionDataFile.toPath().toAbsolutePath();
        try {
            return new SuccessCoverageLoadingResult(load(lcov, projectOf(lcov, workingDirectory(suite))));
        } catch (IOException | RuntimeException e) {
            return new FailedCoverageLoadingResult(
                    "cannot read the coverage of a run with coverage, " + sessionDataFile, e);
        }
    }

    /** The coverage of the lcov report at {@code lcov}, its relative paths those of {@code project}. */
    static @NotNull ProjectData load(@NotNull Path lcov, @NotNull Path project) throws IOException {
        return LcovSerializationUtils.convertToProjectData(
                LcovSerializationUtils.readLCOV(List.of(lcov.toFile())),
                path -> {
                    Path p = Path.of(path);
                    return (p.isAbsolute() ? p : project.resolve(p)).normalize().toString();
                });
    }

    /**
     * The axx project of a report: the folder of the axx.yaml above it (coverage-core's folder is in
     * the project), else the run's working directory, else the report's folder.
     */
    static @NotNull Path projectOf(@NotNull Path lcov, @Nullable Path workingDirectory) {
        Path dir = lcov.toAbsolutePath().getParent();
        Path config = dir == null ? null : ConfigFinder.nearestAbove(dir);
        if (config != null) {
            return config.getParent();
        }
        if (workingDirectory != null) {
            return workingDirectory;
        }
        return dir != null ? dir : lcov;
    }

    private static @Nullable Path workingDirectory(@Nullable CoverageSuite suite) {
        if (!(suite instanceof BaseCoverageSuite base)
                || !(base.getConfiguration() instanceof RunConfigurationBase<?> configuration)) {
            return null;
        }
        return AxxCoverageEnabledConfiguration.workingDirectory(configuration);
    }

    @Override
    public @NotNull String getPresentableName() {
        return "axx";
    }

    @Override
    public @NotNull String getId() {
        return "axx";
    }

    @Override
    public @NotNull String getDataFileExtension() {
        return "info";
    }

    @Override
    public boolean acceptsCoverageEngine(@NotNull CoverageEngine engine) {
        return engine instanceof AxxCoverageEngine;
    }
}
