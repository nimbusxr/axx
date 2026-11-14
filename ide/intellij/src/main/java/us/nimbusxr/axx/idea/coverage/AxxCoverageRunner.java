// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.CoverageEngine;
import com.intellij.coverage.CoverageRunner;

import org.jetbrains.annotations.NotNull;

/**
 * What the platform knows a run with coverage's report by: its kind and its file's extension.
 *
 * <p>It reads nothing. A run's coverage reads its own report ({@link AxxCoverageSuite}): the
 * platform's way of reading one through a runner takes a {@code File} up to 2026.1 and a {@code
 * Path} from 2026.2, which deprecates the first, so no one method serves every IDE the plugin
 * supports.
 */
public final class AxxCoverageRunner extends CoverageRunner {
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
