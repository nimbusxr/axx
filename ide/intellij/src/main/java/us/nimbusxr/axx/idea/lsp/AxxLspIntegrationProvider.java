// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.IconLoader;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.lsp.api.LspClient;
import com.intellij.platform.lsp.api.LspIntegrationProvider;
import com.intellij.platform.lsp.api.lsWidget.LspClientWidgetItem;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.AxxConfigurable;
import us.nimbusxr.axx.idea.AxxProject;

import javax.swing.Icon;

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
    private static final Icon ICON =
            IconLoader.getIcon("/icons/axx.svg", AxxLspIntegrationProvider.class);

    @Override
    public void fileOpened(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @NotNull LspClientStarter clientStarter) {
        if (AxxLspClientDescriptor.isFeatureFile(file) && AxxProject.isAxxProject(project)) {
            clientStarter.ensureClientStarted(new AxxLspClientDescriptor(project));
        }
    }

    /** The server's entry in the Language Services status bar widget, linking to the settings. */
    @Override
    public @NotNull LspClientWidgetItem createWidgetItem(
            @NotNull LspClient client, @Nullable VirtualFile currentFile) {
        return new LspClientWidgetItem(client, currentFile, ICON, AxxConfigurable.class);
    }
}
