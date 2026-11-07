// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.schema;

import com.intellij.openapi.application.ApplicationActivationListener;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.project.ProjectManager;
import com.intellij.openapi.vfs.newvfs.BulkFileListener;
import com.intellij.openapi.vfs.newvfs.events.VFileEvent;
import com.intellij.openapi.wm.IdeFrame;

import org.jetbrains.annotations.NotNull;

import us.nimbusxr.axx.idea.AxxSettings;

import java.util.List;
import java.util.function.Consumer;

/**
 * Reads the open projects' schemas again when the axx they run on may have changed: when the IDE
 * comes back to the front (after an upgrade in a terminal, say), when an axx.yaml changes (the
 * version it names), and when the axx executable setting changes. {@link AxxProjectSchemas} asks
 * axx for them only when {@code axx --version} answers otherwise than it did.
 */
public final class AxxSchemaRefresher
        implements ApplicationActivationListener, BulkFileListener, AxxSettings.Listener {
    @Override
    public void applicationActivated(@NotNull IdeFrame ideFrame) {
        each(AxxProjectSchemas::refreshOnFront);
    }

    @Override
    public void after(@NotNull List<? extends VFileEvent> events) {
        for (VFileEvent event : events) {
            String path = event.getPath();
            if (path.endsWith("/axx.yaml") || path.endsWith("/axx.yml")) {
                each(AxxProjectSchemas::refresh);
                return;
            }
        }
    }

    @Override
    public void executableChanged() {
        each(AxxProjectSchemas::refresh);
    }

    private static void each(Consumer<AxxProjectSchemas> action) {
        for (Project project : ProjectManager.getInstance().getOpenProjects()) {
            if (!project.isDisposed() && !project.isDefault()) {
                action.accept(AxxProjectSchemas.getInstance(project));
            }
        }
    }
}
