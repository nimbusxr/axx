// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.BaseCoverageSuite;
import com.intellij.coverage.CoverageEngine;
import com.intellij.coverage.CoverageFileProvider;
import com.intellij.coverage.CoverageRunner;
import com.intellij.openapi.project.Project;

import org.jetbrains.annotations.NotNull;

/** A run with coverage's coverage: its lcov report. */
public final class AxxCoverageSuite extends BaseCoverageSuite {
    public AxxCoverageSuite() {}

    AxxCoverageSuite(
            @NotNull String name,
            @NotNull Project project,
            @NotNull CoverageRunner runner,
            @NotNull CoverageFileProvider fileProvider,
            long timestamp) {
        super(name, project, runner, fileProvider, timestamp);
    }

    @Override
    public @NotNull CoverageEngine getCoverageEngine() {
        return CoverageEngine.EP_NAME.findExtensionOrFail(AxxCoverageEngine.class);
    }
}
