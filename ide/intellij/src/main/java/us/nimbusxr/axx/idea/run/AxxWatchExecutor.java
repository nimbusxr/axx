// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.Executor;
import com.intellij.icons.AllIcons;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.IconLoader;
import com.intellij.openapi.wm.ToolWindowId;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxProject;

import javax.swing.Icon;

/**
 * Watch: runs axx scenarios with the web pack's browsers in windows on the desktop as they go,
 * slowed down, one scenario at a time, next to Run and Debug in the gutter, the context menus and
 * the run toolbar. The run is an axx run configuration's, with the watch arguments added (see
 * {@link RunTargets#runArguments}), in the Run tool window. It only shows the browsers: it does not
 * pause at breakpoints or failures, as Debug does (see {@link AxxStepBreakpoints}).
 */
public final class AxxWatchExecutor extends Executor {
    /** The executor ID. */
    public static final String ID = "AxxWatch";

    @Override
    public @NotNull String getToolWindowId() {
        return ToolWindowId.RUN;
    }

    @Override
    public @NotNull Icon getToolWindowIcon() {
        return AllIcons.Toolwindows.ToolWindowRun;
    }

    @Override
    public @NotNull Icon getIcon() {
        return AllIcons.Actions.Show;
    }

    @Override
    public Icon getDisabledIcon() {
        return IconLoader.getDisabledIcon(getIcon());
    }

    @Override
    public String getDescription() {
        return "Run the selected scenarios one at a time, with their browsers in windows, slowed"
                + " down";
    }

    @Override
    public @NotNull String getActionName() {
        return "Watch";
    }

    @Override
    public @NotNull String getId() {
        return ID;
    }

    @Override
    public @NotNull String getStartActionText() {
        return "Watch";
    }

    @Override
    public @NotNull String getContextActionId() {
        return "AxxWatchContext";
    }

    @Override
    public @Nullable String getHelpId() {
        return null;
    }

    @Override
    public boolean isApplicable(@NotNull Project project) {
        return AxxProject.isAxxProject(project);
    }
}
