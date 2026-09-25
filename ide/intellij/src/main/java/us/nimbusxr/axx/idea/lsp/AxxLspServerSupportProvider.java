// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.lsp;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.IconLoader;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.lsp.api.LspServer;
import com.intellij.platform.lsp.api.LspServerSupportProvider;
import com.intellij.platform.lsp.api.lsWidget.LspServerWidgetItem;

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
public final class AxxLspServerSupportProvider implements LspServerSupportProvider {
    private static final Icon ICON =
            IconLoader.getIcon("/icons/axx.svg", AxxLspServerSupportProvider.class);

    @Override
    public void fileOpened(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @NotNull LspServerStarter serverStarter) {
        if (AxxLspServerDescriptor.isFeatureFile(file) && AxxProject.isAxxProject(project)) {
            serverStarter.ensureServerStarted(new AxxLspServerDescriptor(project));
        }
    }

    /** The server's entry in the Language Services status bar widget, linking to the settings. */
    @Override
    public @NotNull LspServerWidgetItem createLspServerWidgetItem(
            @NotNull LspServer lspServer, @Nullable VirtualFile currentFile) {
        return new LspServerWidgetItem(lspServer, currentFile, ICON, AxxConfigurable.class);
    }
}
