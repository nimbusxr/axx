// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.ExecutionException;
import com.intellij.execution.ExecutionResult;
import com.intellij.execution.configurations.RunProfile;
import com.intellij.execution.configurations.RunProfileState;
import com.intellij.execution.configurations.RunnerSettings;
import com.intellij.execution.executors.DefaultDebugExecutor;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.runners.GenericProgramRunner;
import com.intellij.execution.runners.RunContentBuilder;
import com.intellij.execution.ui.RunContentDescriptor;
import com.intellij.notification.NotificationGroupManager;
import com.intellij.notification.NotificationType;
import com.intellij.openapi.util.Key;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

/**
 * Debug for axx run configurations: runs {@code axx run --debug-steps}, which waits under Delve for
 * a Go debugger; the plugin then starts {@code "Debugger: axx-steps"} to attach, so breakpoints in
 * step code stop. Without a Go debugger in the IDE, the scenarios run without it, and the plugin
 * says why.
 */
public final class AxxDebugRunner extends GenericProgramRunner<RunnerSettings> {
    /** The Delve port for {@code --debug-steps}, set when a Go debugger can attach. */
    static final Key<Integer> STEPS_PORT = Key.create("us.nimbusxr.axx.stepsPort");

    /** A note to show at the top of the run's console. */
    static final Key<String> NOTICE = Key.create("us.nimbusxr.axx.runNotice");

    @Override
    public @NotNull String getRunnerId() {
        return "AxxDebugRunner";
    }

    @Override
    public boolean canRun(@NotNull String executorId, @NotNull RunProfile profile) {
        return DefaultDebugExecutor.EXECUTOR_ID.equals(executorId)
                && AxxRunConfiguration.of(profile) != null;
    }

    @Override
    protected @Nullable RunContentDescriptor doExecute(
            @NotNull RunProfileState state, @NotNull ExecutionEnvironment environment)
            throws ExecutionException {
        Integer port = AxxStepsDebugger.prepare(environment.getProject());
        environment.putUserData(STEPS_PORT, port);
        environment.putUserData(NOTICE, port == null ? AxxStepsDebugger.NO_GO_DEBUGGER : null);
        if (port == null) {
            NotificationGroupManager.getInstance()
                    .getNotificationGroup("axx")
                    .createNotification(AxxStepsDebugger.NO_GO_DEBUGGER, NotificationType.WARNING)
                    .notify(environment.getProject());
        }
        ExecutionResult result = state.execute(environment.getExecutor(), this);
        if (result == null) {
            return null;
        }
        return new RunContentBuilder(result, environment)
                .showRunContent(environment.getContentToReuse());
    }
}
