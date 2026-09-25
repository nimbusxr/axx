// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.project.ProjectManager;
import com.intellij.platform.lsp.api.LspServerManager;

import us.nimbusxr.axx.idea.AxxSettings;

/** Restarts running axx language servers when the axx executable setting changes. */
public final class AxxLspRestarter implements AxxSettings.Listener {
    @Override
    public void executableChanged() {
        for (Project project : ProjectManager.getInstance().getOpenProjects()) {
            if (!project.isDisposed()) {
                LspServerManager.getInstance(project)
                        .stopAndRestartIfNeeded(AxxLspServerSupportProvider.class);
            }
        }
    }
}
