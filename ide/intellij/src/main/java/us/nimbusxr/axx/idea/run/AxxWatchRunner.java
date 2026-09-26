// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.ExecutionException;
import com.intellij.execution.ExecutionResult;
import com.intellij.execution.configurations.RunProfile;
import com.intellij.execution.configurations.RunProfileState;
import com.intellij.execution.configurations.RunnerSettings;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.runners.GenericProgramRunner;
import com.intellij.execution.runners.RunContentBuilder;
import com.intellij.execution.ui.RunContentDescriptor;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

/** Runs axx run configurations with Watch (see {@link AxxWatchExecutor}). */
public final class AxxWatchRunner extends GenericProgramRunner<RunnerSettings> {
    @Override
    public @NotNull String getRunnerId() {
        return "AxxWatchRunner";
    }

    @Override
    public boolean canRun(@NotNull String executorId, @NotNull RunProfile profile) {
        return AxxWatchExecutor.ID.equals(executorId) && AxxRunConfiguration.of(profile) != null;
    }

    @Override
    protected @Nullable RunContentDescriptor doExecute(
            @NotNull RunProfileState state, @NotNull ExecutionEnvironment environment)
            throws ExecutionException {
        ExecutionResult result = state.execute(environment.getExecutor(), this);
        if (result == null) {
            return null;
        }
        return new RunContentBuilder(result, environment)
                .showRunContent(environment.getContentToReuse());
    }
}
