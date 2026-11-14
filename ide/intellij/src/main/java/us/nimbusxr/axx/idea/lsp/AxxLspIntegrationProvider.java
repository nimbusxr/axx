// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.lsp.api.LspClient;
import com.intellij.platform.lsp.api.LspClientManager;
import com.intellij.platform.lsp.api.LspIntegrationProvider;
import com.intellij.platform.lsp.api.LspServerState;
import com.intellij.platform.lsp.api.lsWidget.LspClientWidgetItem;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxConfigurable;
import us.nimbusxr.axx.idea.AxxIcons;
import us.nimbusxr.axx.idea.AxxProject;

/**
 * Starts the axx language server ({@code axx lsp}) when a {@code .feature} file opens in an axx
 * project.
 *
 * <p>axx's steps are defined in Go, inside the axx binary, so the IDE cannot find them on its own.
 * The language server knows them: it gives {@code .feature} files step completion, step
 * documentation on hover, go to a step's definition, highlighted step parameters, and diagnostics
 * for undefined or ambiguous steps and Gherkin syntax errors. One server serves the whole project.
 */
public final class AxxLspIntegrationProvider implements LspIntegrationProvider {
    @Override
    public void fileOpened(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @NotNull LspClientStarter clientStarter) {
        if (AxxLspClientDescriptor.isFeatureFile(file) && AxxProject.isAxxProject(project)) {
            clientStarter.ensureClientStarted(new AxxLspClientDescriptor(project));
        }
    }

    /** The project's axx language server, when it runs and serves the file. */
    public static @Nullable LspClient runningServer(
            @NotNull Project project, @NotNull VirtualFile file) {
        for (LspClient server :
                LspClientManager.getInstance(project).getClients(AxxLspIntegrationProvider.class)) {
            if (server.getState() == LspServerState.Running
                    && server.getDescriptor().isSupportedFile(file)) {
                return server;
            }
        }
        return null;
    }

    /** The server's entry in the Language Services status bar widget, linking to the settings. */
    @Override
    public @NotNull LspClientWidgetItem createWidgetItem(
            @NotNull LspClient client, @Nullable VirtualFile currentFile) {
        return new LspClientWidgetItem(client, currentFile, AxxIcons.AXX, AxxConfigurable.class);
    }
}
