// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.Key;
import com.intellij.openapi.vfs.VirtualFileManager;
import com.intellij.psi.util.CachedValue;
import com.intellij.psi.util.CachedValueProvider;
import com.intellij.psi.util.CachedValuesManager;

import org.jetbrains.annotations.NotNull;

import java.nio.file.Path;

/**
 * Tells whether an IDE project is an axx project: whether {@code axx lsp}, started in the project
 * directory, finds an axx config file there (see {@link ConfigFinder}). Only axx projects start the
 * axx language server, and only in axx projects are the Gherkin plugin's undefined-step warnings
 * suppressed.
 */
public final class AxxProject {
    private static final Key<CachedValue<Boolean>> IS_AXX_PROJECT =
            Key.create("us.nimbusxr.axx.idea.isAxxProject");

    private AxxProject() {}

    /**
     * Whether the project is an axx project. The answer is cached until files are created, deleted,
     * moved or renamed.
     */
    public static boolean isAxxProject(@NotNull Project project) {
        if (project.isDefault() || project.isDisposed()) {
            return false;
        }
        return CachedValuesManager.getManager(project)
                .getCachedValue(
                        project,
                        IS_AXX_PROJECT,
                        () -> {
                            String dir = project.getBasePath();
                            boolean found = dir != null && ConfigFinder.find(Path.of(dir)) != null;
                            return CachedValueProvider.Result.create(
                                    found, VirtualFileManager.VFS_STRUCTURE_MODIFICATIONS);
                        },
                        false);
    }
}
