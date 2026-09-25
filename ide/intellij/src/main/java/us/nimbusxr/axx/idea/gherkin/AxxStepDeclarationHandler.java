// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.codeInsight.navigation.actions.GotoDeclarationHandler;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.project.DumbAware;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.lsp.api.LspServer;
import com.intellij.platform.lsp.api.LspServerManager;
import com.intellij.platform.lsp.api.LspServerState;
import com.intellij.psi.PsiElement;
import com.intellij.psi.PsiFile;
import com.intellij.psi.util.PsiTreeUtil;
import com.intellij.psi.util.PsiUtilCore;

import org.eclipse.lsp4j.DefinitionParams;
import org.eclipse.lsp4j.DocumentLink;
import org.eclipse.lsp4j.DocumentLinkParams;
import org.eclipse.lsp4j.Location;
import org.eclipse.lsp4j.LocationLink;
import org.eclipse.lsp4j.Position;
import org.eclipse.lsp4j.Range;
import org.eclipse.lsp4j.jsonrpc.messages.Either;
import org.jetbrains.annotations.Nullable;
import org.jetbrains.plugins.cucumber.psi.GherkinStep;
import org.jetbrains.plugins.cucumber.psi.GherkinTableCell;

import us.nimbusxr.axx.idea.AxxProject;
import us.nimbusxr.axx.idea.lsp.AxxLspServerSupportProvider;

import java.util.ArrayList;
import java.util.List;

/**
 * Go to Declaration (and Ctrl/Cmd+click) on a step or a table cell in an axx project: asks the axx
 * language server where it leads ({@code textDocument/definition}) and goes there. On a step that is
 * the Go source of the step, or a generated Markdown page about it when the source is not on the
 * machine; on a table cell that names a file (an OpenAPI document, a seed in an Examples row), the
 * file.
 *
 * <p>The IDE's LSP client offers this navigation only where no other reference is, and the Gherkin
 * plugin puts its own step reference on every step, which finds no Java or Kotlin step definition
 * for an axx step. Declaration handlers run before references, so this one answers first; when the
 * server knows no definition, navigation falls through to the Gherkin plugin as before.
 *
 * <p>On a file a step's text names (a seed, a payload, a schema), a link the server reports ({@code
 * textDocument/documentLink}), it does not answer: the IDE's link reference opens the file. The
 * IDE underlines what a declaration handler answers for as a whole text element, which is the whole
 * step text, and keeps an underline while the next answer has the same range; the link's range is
 * the file's own, so the path and the rest of the step each get theirs. A table cell is an element
 * of its own, so it is answered here.
 *
 * <p>The IDE calls declaration handlers in a background read action, both for the action and for
 * the Ctrl/Cmd+hover preview, so the request is synchronous, with a timeout, and cancellable.
 */
public final class AxxStepDeclarationHandler implements GotoDeclarationHandler, DumbAware {
    /** How long to wait for the server's answer. */
    static final int TIMEOUT_MS = 5_000;

    @Override
    public PsiElement @Nullable [] getGotoDeclarationTargets(
            @Nullable PsiElement sourceElement, int offset, Editor editor) {
        if (sourceElement == null
                || ApplicationManager.getApplication().isDispatchThread()
                || PsiTreeUtil.getParentOfType(
                                sourceElement, false, GherkinStep.class, GherkinTableCell.class)
                        == null) {
            return null;
        }
        boolean inCell =
                PsiTreeUtil.getParentOfType(sourceElement, GherkinTableCell.class, false) != null;
        Project project = sourceElement.getProject();
        PsiFile file = sourceElement.getContainingFile();
        VirtualFile virtualFile = PsiUtilCore.getVirtualFile(file);
        if (virtualFile == null || !AxxProject.isAxxProject(project)) {
            return null;
        }
        Document document = FileDocumentManager.getInstance().getDocument(virtualFile);
        LspServer server = runningServer(project, virtualFile);
        if (document == null || server == null || offset < 0 || offset > document.getTextLength()) {
            return null;
        }

        int line = document.getLineNumber(offset);
        Position position = new Position(line, offset - document.getLineStartOffset(line));
        if (!inCell && onLink(server, virtualFile, position)) {
            return null;
        }
        DefinitionParams params =
                new DefinitionParams(server.getDocumentIdentifier(virtualFile), position);
        Either<List<? extends Location>, List<? extends LocationLink>> result =
                server.sendRequestSync(
                        TIMEOUT_MS, lsp -> lsp.getTextDocumentService().definition(params));
        if (result == null) {
            return null;
        }

        List<PsiElement> targets = new ArrayList<>();
        if (result.isLeft()) {
            for (Location location : result.getLeft()) {
                addTarget(targets, file, server, location.getUri(), location.getRange());
            }
        } else {
            for (LocationLink link : result.getRight()) {
                Range range =
                        link.getTargetSelectionRange() != null
                                ? link.getTargetSelectionRange()
                                : link.getTargetRange();
                addTarget(targets, file, server, link.getTargetUri(), range);
            }
        }
        return targets.isEmpty() ? null : targets.toArray(PsiElement.EMPTY_ARRAY);
    }

    /** Whether the position is on a link to a file the step names. */
    private static boolean onLink(LspServer server, VirtualFile file, Position position) {
        DocumentLinkParams params = new DocumentLinkParams(server.getDocumentIdentifier(file));
        List<DocumentLink> links =
                server.sendRequestSync(
                        TIMEOUT_MS, lsp -> lsp.getTextDocumentService().documentLink(params));
        if (links == null) {
            return false;
        }
        for (DocumentLink link : links) {
            if (contains(link.getRange(), position)) {
                return true;
            }
        }
        return false;
    }

    static boolean contains(@Nullable Range range, Position position) {
        return range != null
                && range.getStart().getLine() == position.getLine()
                && range.getStart().getCharacter() <= position.getCharacter()
                && position.getCharacter() <= range.getEnd().getCharacter();
    }

    /** The project's axx language server, when it runs and serves the file. */
    private static @Nullable LspServer runningServer(Project project, VirtualFile file) {
        for (LspServer server :
                LspServerManager.getInstance(project)
                        .getServersForProvider(AxxLspServerSupportProvider.class)) {
            if (server.getState() == LspServerState.Running
                    && server.getDescriptor().isSupportedFile(file)) {
                return server;
            }
        }
        return null;
    }

    private static void addTarget(
            List<PsiElement> targets,
            PsiFile source,
            LspServer server,
            @Nullable String uri,
            @Nullable Range range) {
        if (uri == null || range == null || range.getStart() == null) {
            return;
        }
        AxxDefinitionTarget target =
                AxxDefinitionTarget.create(
                        source,
                        uri,
                        server.getDescriptor().findFileByUri(uri),
                        range.getStart().getLine(),
                        range.getStart().getCharacter());
        if (target != null) {
            targets.add(target);
        }
    }
}
