// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import com.intellij.openapi.actionSystem.ActionUpdateThread;
import com.intellij.openapi.actionSystem.AnActionEvent;
import com.intellij.openapi.project.DumbAwareAction;
import com.intellij.openapi.project.Project;

import org.jetbrains.annotations.NotNull;

import us.nimbusxr.axx.idea.AxxProject;

/**
 * Start axx Host, Stop axx Host and Show axx Host Approval, in the Tools menu, and Restart axx Host,
 * with Stop in the menu of the host's item in the status bar. None of them answers a question of
 * the host: only the person's click on it does.
 */
public abstract class AxxHostActions extends DumbAwareAction {
    @Override
    public @NotNull ActionUpdateThread getActionUpdateThread() {
        return ActionUpdateThread.BGT;
    }

    @Override
    public void update(@NotNull AnActionEvent event) {
        Project project = event.getProject();
        event.getPresentation()
                .setEnabledAndVisible(
                        project != null
                                && AxxProject.isAxxProject(project)
                                && isEnabled(AxxHost.getIfCreated(project)));
    }

    /** Whether the action does something, given the project's host (null: none started yet). */
    abstract boolean isEnabled(AxxHost host);

    /** Starts the axx host in the project. */
    public static final class Start extends AxxHostActions {
        @Override
        boolean isEnabled(AxxHost host) {
            return host == null || !host.isRunning();
        }

        @Override
        public void actionPerformed(@NotNull AnActionEvent event) {
            Project project = event.getProject();
            if (project != null) {
                AxxHost.getInstance(project).start(false);
            }
        }
    }

    /** Stops the axx host the IDE started in the project. */
    public static final class Stop extends AxxHostActions {
        @Override
        boolean isEnabled(AxxHost host) {
            return host != null && host.isRunning();
        }

        @Override
        public void actionPerformed(@NotNull AnActionEvent event) {
            AxxHost host = AxxHost.getIfCreated(event.getProject());
            if (host != null) {
                host.stop();
            }
        }
    }

    /**
     * Stops the axx host the IDE started and starts it again, which stops the services a run keeps,
     * like an agent's {@code axx up}; starts one when none runs.
     */
    public static final class Restart extends AxxHostActions {
        @Override
        boolean isEnabled(AxxHost host) {
            return host != null;
        }

        @Override
        public void actionPerformed(@NotNull AnActionEvent event) {
            AxxHost host = AxxHost.getIfCreated(event.getProject());
            if (host != null) {
                host.restart();
            }
        }
    }

    /** Shows the host's question that waits for an answer again; it does not answer it. */
    public static final class ShowApproval extends AxxHostActions {
        @Override
        boolean isEnabled(AxxHost host) {
            return host != null && host.hasPendingApproval();
        }

        @Override
        public void actionPerformed(@NotNull AnActionEvent event) {
            AxxHost host = AxxHost.getIfCreated(event.getProject());
            if (host != null) {
                host.showApproval();
            }
        }
    }
}
