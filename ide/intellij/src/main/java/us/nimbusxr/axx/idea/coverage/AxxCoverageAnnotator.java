// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.coverage;

import com.intellij.coverage.SimpleCoverageAnnotator;
import com.intellij.openapi.components.Service;
import com.intellij.openapi.project.Project;

import org.jetbrains.annotations.NotNull;

/** The coverage of files and folders in the project view, from a run with coverage. */
@Service(Service.Level.PROJECT)
public final class AxxCoverageAnnotator extends SimpleCoverageAnnotator {
    public AxxCoverageAnnotator(@NotNull Project project) {
        super(project);
    }

    static @NotNull AxxCoverageAnnotator getInstance(@NotNull Project project) {
        return project.getService(AxxCoverageAnnotator.class);
    }
}
