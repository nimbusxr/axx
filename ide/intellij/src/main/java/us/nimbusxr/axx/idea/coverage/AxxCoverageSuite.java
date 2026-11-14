// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.BaseCoverageSuite;
import com.intellij.coverage.CoverageEngine;
import com.intellij.coverage.CoverageFileProvider;
import com.intellij.coverage.CoverageRunner;
import com.intellij.execution.configurations.RunConfigurationBase;
import com.intellij.openapi.diagnostic.Logger;
import com.intellij.openapi.project.Project;
import com.intellij.rt.coverage.data.ProjectData;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

/** A run with coverage's coverage: its lcov report. */
public final class AxxCoverageSuite extends BaseCoverageSuite {
    private static final Logger LOG = Logger.getInstance(AxxCoverageSuite.class);

    public AxxCoverageSuite() {}

    AxxCoverageSuite(
            @NotNull String name,
            @NotNull Project project,
            @NotNull CoverageRunner runner,
            @NotNull CoverageFileProvider fileProvider,
            long timestamp) {
        super(name, project, runner, fileProvider, timestamp);
    }

    /**
     * Reads the run's lcov report, when it is there. A report that cannot be read is no coverage,
     * and the IDE's log says why.
     */
    @Override
    protected @Nullable ProjectData loadProjectInfo() {
        CoverageFileProvider provider = getCoverageDataFileProvider();
        String path = provider == null ? null : provider.getCoverageDataFilePath();
        if (path == null) {
            return null;
        }
        Path lcov = Path.of(path).toAbsolutePath();
        if (!Files.exists(lcov)) {
            return null;
        }
        try {
            return LcovReport.load(lcov, LcovReport.projectOf(lcov, workingDirectory()));
        } catch (IOException | RuntimeException e) {
            LOG.warn("cannot read the coverage of a run with coverage, " + lcov, e);
            return null;
        }
    }

    private @Nullable Path workingDirectory() {
        return getConfiguration() instanceof RunConfigurationBase<?> configuration
                ? AxxCoverageEnabledConfiguration.workingDirectory(configuration)
                : null;
    }

    @Override
    public @NotNull CoverageEngine getCoverageEngine() {
        return CoverageEngine.EP_NAME.findExtensionOrFail(AxxCoverageEngine.class);
    }
}
