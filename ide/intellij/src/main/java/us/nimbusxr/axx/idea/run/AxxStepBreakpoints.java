// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.openapi.application.ReadAction;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.io.FileUtil;
import com.intellij.openapi.vfs.StandardFileSystems;
import com.intellij.openapi.vfs.VfsUtilCore;
import com.intellij.xdebugger.XDebuggerManager;
import com.intellij.xdebugger.breakpoints.XBreakpoint;
import com.intellij.xdebugger.breakpoints.XBreakpointType;
import com.intellij.xdebugger.breakpoints.XLineBreakpoint;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.nio.file.InvalidPathException;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * Breakpoints on the steps of feature files, of the {@value #TYPE_ID} type (placed with the Gherkin
 * plugin, see {@code AxxStepBreakpointType}). No debugger stops at them: runs with Debug pause
 * before their steps ({@code axx run --pause-at <file>:<line>}), and the web pack shows the paused
 * page in Playwright's Inspector. Run and Watch ignore them.
 */
public final class AxxStepBreakpoints {
    /** The breakpoint type's ID. */
    public static final String TYPE_ID = "axx-step";

    private AxxStepBreakpoints() {}

    /** The enabled step breakpoints of a project, in files on disk. */
    static @NotNull List<RunTargets.Step> enabled(@NotNull Project project) {
        XBreakpointType<?, ?> type = type();
        if (type == null) {
            return List.of(); // no Gherkin plugin, so no step breakpoints
        }
        return ReadAction.computeBlocking(
                () -> {
                    List<RunTargets.Step> steps = new ArrayList<>();
                    for (XBreakpoint<?> breakpoint :
                            XDebuggerManager.getInstance(project)
                                    .getBreakpointManager()
                                    .getBreakpoints(type)) {
                        if (breakpoint.isEnabled()
                                && breakpoint instanceof XLineBreakpoint<?> line) {
                            Path file = pathOf(line.getFileUrl());
                            if (file != null) {
                                steps.add(new RunTargets.Step(file, line.getLine() + 1));
                            }
                        }
                    }
                    return steps;
                });
    }

    /** The registered step breakpoint type, or null without the Gherkin plugin. */
    static @Nullable XBreakpointType<?, ?> type() {
        for (XBreakpointType<?, ?> type : XBreakpointType.EXTENSION_POINT_NAME.getExtensionList()) {
            if (TYPE_ID.equals(type.getId())) {
                return type;
            }
        }
        return null;
    }

    private static @Nullable Path pathOf(@Nullable String url) {
        if (url == null || !url.startsWith(StandardFileSystems.FILE_PROTOCOL_PREFIX)) {
            return null;
        }
        try {
            return Path.of(FileUtil.toSystemDependentName(VfsUtilCore.urlToPath(url)));
        } catch (InvalidPathException e) {
            return null;
        }
    }
}
