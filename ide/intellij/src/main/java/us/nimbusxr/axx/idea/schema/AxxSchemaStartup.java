// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.startup.ProjectActivity;

import kotlin.Unit;
import kotlin.coroutines.Continuation;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

/** Reads the schemas of the axx a project runs on when the project opens. */
public final class AxxSchemaStartup implements ProjectActivity {
    @Override
    public @Nullable Object execute(
            @NotNull Project project, @NotNull Continuation<? super Unit> continuation) {
        AxxProjectSchemas.getInstance(project).refresh();
        return Unit.INSTANCE;
    }
}
