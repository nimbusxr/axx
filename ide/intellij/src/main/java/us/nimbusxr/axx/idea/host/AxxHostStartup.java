// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.startup.ProjectActivity;

import kotlin.Unit;
import kotlin.coroutines.Continuation;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxSettings;
import us.nimbusxr.axx.idea.ConfigFinder;

import java.nio.file.Path;

/**
 * Starts the axx host when an axx project opens, when <em>Start the axx host when an axx project
 * opens</em> is on (<em>Settings | Tools | axx</em>; off by default).
 */
public final class AxxHostStartup implements ProjectActivity {
    @Override
    public @Nullable Object execute(
            @NotNull Project project, @NotNull Continuation<? super Unit> continuation) {
        String base = project.getBasePath();
        if (AxxSettings.getInstance().isStartHost()
                && base != null
                && ConfigFinder.find(Path.of(base)) != null) {
            AxxHost.getInstance(project).start(true);
        }
        return Unit.INSTANCE;
    }
}
