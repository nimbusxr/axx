// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.openapi.Disposable;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.application.ReadAction;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.RangeMarker;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.Disposer;
import com.intellij.openapi.util.Key;
import com.intellij.openapi.util.UserDataHolderEx;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.lsp.api.LspClient;

import org.eclipse.lsp4j.DocumentLink;
import org.eclipse.lsp4j.DocumentLinkParams;
import org.eclipse.lsp4j.Position;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import us.nimbusxr.axx.idea.lsp.AxxLspIntegrationProvider;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * The files a feature file names, as the axx language server links them ({@code
 * textDocument/documentLink}): the files its steps read, found as the steps find them, next to the
 * feature file and then in the project's resource folders. The plugin never works a path out
 * itself, so a preview shows the file the step reads.
 *
 * <p>The server may take a while to answer: it loads the project as it starts, and builds its packs
 * first when they changed. So the links are asked for in the background and kept with the
 * document, and whoever listens is told when they arrive; until then, the links are the ones the
 * server gave last, which follow the document's edits.
 */
final class FileLinks {
    /** How long to wait for the server to start, and for its answer. */
    static final long START_MS = 120_000;

    static final int ANSWER_MS = 30_000;

    /** A value in the feature file that names a file: its range, which follows edits, and the file. */
    record Link(@NotNull RangeMarker range, @NotNull VirtualFile file) {
        /** The line of the value, or -1 when it was deleted. */
        int line(@NotNull Document document) {
            return range.isValid() ? document.getLineNumber(range.getStartOffset()) : -1;
        }
    }

    /** The links of a document, and whether they are of the document as it is now. */
    record Links(@NotNull List<Link> links, boolean fresh) {}

    /** The links of the document as it was when the server answered. */
    private record Answer(long stamp, List<Link> links) {}

    private static final Key<Answer> ANSWER = Key.create("us.nimbusxr.axx.idea.fileLinks");
    private static final Key<AtomicBoolean> ASKING =
            Key.create("us.nimbusxr.axx.idea.fileLinks.asking");
    private static final Key<List<Runnable>> LISTENERS =
            Key.create("us.nimbusxr.axx.idea.fileLinks.listeners");

    private FileLinks() {}

    /**
     * The links of a feature file: the server's last answer, null before its first. When the
     * document changed since, they are asked for again, and the document's listeners are told when
     * they arrive. In a read action.
     */
    static @Nullable Links of(
            @NotNull Project project, @NotNull VirtualFile feature, @NotNull Document document) {
        Answer answer = document.getUserData(ANSWER);
        boolean fresh = answer != null && answer.stamp() == document.getModificationStamp();
        if (!fresh) {
            ask(project, feature, document);
        }
        return answer == null ? null : new Links(answer.links(), fresh);
    }

    /**
     * Runs a listener on the event dispatch thread whenever the server's answer about the document
     * arrives, until the disposable is disposed.
     */
    static void listen(
            @NotNull Document document, @NotNull Runnable listener, @NotNull Disposable until) {
        if (!(document instanceof UserDataHolderEx holder)) {
            return;
        }
        List<Runnable> listeners = holder.putUserDataIfAbsent(LISTENERS, new CopyOnWriteArrayList<>());
        listeners.add(listener);
        Disposer.register(until, () -> listeners.remove(listener));
    }

    /** Asks the server for the links, unless it is asked already, in the background. */
    private static void ask(
            @NotNull Project project, @NotNull VirtualFile feature, @NotNull Document document) {
        if (!(document instanceof UserDataHolderEx holder)) {
            return;
        }
        AtomicBoolean asking = holder.putUserDataIfAbsent(ASKING, new AtomicBoolean());
        if (!asking.compareAndSet(false, true)) {
            return;
        }
        ApplicationManager.getApplication()
                .executeOnPooledThread(
                        () -> {
                            try {
                                askUntilAnswered(project, feature, document);
                            } finally {
                                asking.set(false);
                            }
                        });
    }

    private static void askUntilAnswered(
            @NotNull Project project, @NotNull VirtualFile feature, @NotNull Document document) {
        long deadline = System.currentTimeMillis() + START_MS;
        boolean first = document.getUserData(ANSWER) == null;
        while (!project.isDisposed() && System.currentTimeMillis() < deadline) {
            LspClient server = AxxLspIntegrationProvider.runningServer(project, feature);
            if (server == null) {
                pause(250);
                continue;
            }
            long stamp = ReadAction.computeBlocking(document::getModificationStamp);
            DocumentLinkParams params = new DocumentLinkParams(server.getDocumentIdentifier(feature));
            List<DocumentLink> links =
                    server.sendRequestSync(
                            ANSWER_MS, lsp -> lsp.getTextDocumentService().documentLink(params));
            if (links == null || (first && links.isEmpty())) {
                // No answer: the server has not opened the document yet, as it starts. Earlier
                // releases of axx answer [] then, so a first answer without links is asked again,
                // once.
                first &= links == null;
                pause(500);
                continue;
            }
            boolean kept =
                    ReadAction.computeBlocking(
                            () -> {
                                if (document.getModificationStamp() != stamp) {
                                    return false;
                                }
                                keep(document, server, links, stamp);
                                return true;
                            });
            if (kept) {
                ApplicationManager.getApplication()
                        .invokeLater(() -> answered(document), o -> project.isDisposed());
                return;
            }
            // The document changed while the server answered: ask about it as it is now.
        }
    }

    private static void pause(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }

    private static void keep(
            @NotNull Document document,
            @NotNull LspClient server,
            @NotNull List<DocumentLink> links,
            long stamp) {
        List<Link> out = new ArrayList<>();
        if (document.getLineCount() > 0) {
            for (DocumentLink link : links) {
                if (link.getRange() == null || link.getTarget() == null) {
                    continue;
                }
                VirtualFile file = server.getDescriptor().findFileByUri(link.getTarget());
                Position start = link.getRange().getStart();
                if (file == null || file.isDirectory() || start.getLine() >= document.getLineCount()) {
                    continue;
                }
                RangeMarker range =
                        document.createRangeMarker(
                                offset(document, start), offset(document, link.getRange().getEnd()));
                out.add(new Link(range, file));
            }
        }
        Answer previous = document.getUserData(ANSWER);
        document.putUserData(ANSWER, new Answer(stamp, List.copyOf(out)));
        if (previous != null) {
            previous.links().forEach(l -> l.range().dispose());
        }
    }

    private static void answered(@NotNull Document document) {
        List<Runnable> listeners = document.getUserData(LISTENERS);
        if (listeners != null) {
            listeners.forEach(Runnable::run);
        }
    }

    /** The document offset of a position, whose character is in UTF-16 code units as Java's. */
    static int offset(@NotNull Document document, @NotNull Position position) {
        int line = Math.min(position.getLine(), document.getLineCount() - 1);
        int start = document.getLineStartOffset(line);
        return Math.min(start + position.getCharacter(), document.getLineEndOffset(line));
    }
}
