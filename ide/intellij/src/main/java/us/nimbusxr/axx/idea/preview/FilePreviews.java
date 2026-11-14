// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.openapi.Disposable;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.InlayProperties;
import com.intellij.openapi.editor.RangeMarker;
import com.intellij.openapi.editor.event.DocumentEvent;
import com.intellij.openapi.editor.event.DocumentListener;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.Disposer;
import com.intellij.openapi.util.Key;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.openapi.vfs.VirtualFileManager;
import com.intellij.openapi.vfs.newvfs.BulkFileListener;
import com.intellij.openapi.vfs.newvfs.events.VFileEvent;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * The previews open in an editor of a feature file: one under a line for each file of the line
 * whose toggle was clicked, until it is clicked again, the line no longer names the file, or the
 * editor closes.
 */
final class FilePreviews {
    private static final Key<List<Open>> OPEN = Key.create("us.nimbusxr.axx.idea.filePreviews");

    /** An open preview: the line it is under, the file, and its inlay. */
    private record Open(
            @NotNull RangeMarker line, @NotNull VirtualFile file, @NotNull Inlay<FilePreview> inlay) {}

    private FilePreviews() {}

    /** Whether the file's preview is open under the line. Any thread, in a read action. */
    static boolean isOpen(@NotNull Editor editor, int line, @NotNull VirtualFile file) {
        return find(editor, line, file) != null;
    }

    /** Opens the file's preview under the line, or closes it. On the event dispatch thread. */
    static void toggle(@NotNull Editor editor, int line, @NotNull VirtualFile file) {
        Open open = find(editor, line, file);
        if (open != null) {
            Disposer.dispose(open.inlay());
        } else {
            open(editor, line, file);
        }
        // The toggle says whether the preview is open.
        for (Inlay<?> inlay : editor.getInlayModel().getAfterLineEndElementsForLogicalLine(line)) {
            if (inlay.getRenderer() instanceof FileToggle) {
                inlay.update();
                inlay.repaint();
            }
        }
    }

    /**
     * Closes the previews of the files their lines no longer name. On the event dispatch thread.
     *
     * @param links the files each line names, by line
     */
    static void retain(@NotNull Editor editor, @NotNull Map<Integer, Set<VirtualFile>> links) {
        List<Open> opened = editor.getUserData(OPEN);
        if (opened == null || editor.isDisposed()) {
            return;
        }
        for (Open open : opened) {
            Set<VirtualFile> files = links.get(lineOf(editor, open));
            if (files == null || !files.contains(open.file())) {
                Disposer.dispose(open.inlay());
            }
        }
    }

    /** The previews open in the editor, for tests. */
    static @NotNull List<Inlay<FilePreview>> all(@NotNull Editor editor) {
        List<Open> opened = editor.getUserData(OPEN);
        return opened == null ? List.of() : opened.stream().map(Open::inlay).toList();
    }

    private static @Nullable Open find(@NotNull Editor editor, int line, @NotNull VirtualFile file) {
        List<Open> opened = editor.getUserData(OPEN);
        if (opened == null) {
            return null;
        }
        for (Open open : opened) {
            if (open.file().equals(file) && lineOf(editor, open) == line) {
                return open;
            }
        }
        return null;
    }

    private static int lineOf(@NotNull Editor editor, @NotNull Open open) {
        return open.line().isValid()
                ? editor.getDocument().getLineNumber(open.line().getStartOffset())
                : -1;
    }

    private static void open(@NotNull Editor editor, int line, @NotNull VirtualFile file) {
        Project project = editor.getProject();
        Document document = editor.getDocument();
        if (project == null || line < 0 || line >= document.getLineCount()) {
            return;
        }
        int end = document.getLineEndOffset(line);
        RangeMarker marker = document.createRangeMarker(document.getLineStartOffset(line), end);
        FilePreview preview = new FilePreview(project, file, editor, marker);
        Inlay<FilePreview> inlay =
                editor.getInlayModel()
                        .addBlockElement(
                                end,
                                new InlayProperties()
                                        .relatesToPrecedingText(true)
                                        .showAbove(false)
                                        .priority(0),
                                preview);
        if (inlay == null) {
            marker.dispose();
            return;
        }
        List<Open> opened = editor.getUserData(OPEN);
        if (opened == null) {
            opened = new CopyOnWriteArrayList<>();
            editor.putUserData(OPEN, opened);
        }
        Open open = new Open(marker, file, inlay);
        opened.add(open);
        List<Open> list = opened;
        Disposer.register(
                inlay,
                () -> {
                    list.remove(open);
                    marker.dispose();
                });
        follow(project, file, preview, inlay);
    }

    /** Shows the file again whenever it changes, on disk or in an editor, until the preview closes. */
    private static void follow(
            @NotNull Project project,
            @NotNull VirtualFile file,
            @NotNull FilePreview preview,
            @NotNull Inlay<FilePreview> inlay) {
        Disposable until = inlay;
        project.getMessageBus()
                .connect(until)
                .subscribe(
                        VirtualFileManager.VFS_CHANGES,
                        new BulkFileListener() {
                            @Override
                            public void after(@NotNull List<? extends VFileEvent> events) {
                                for (VFileEvent event : events) {
                                    if (file.equals(event.getFile())) {
                                        ApplicationManager.getApplication()
                                                .invokeLater(
                                                        () -> preview.reload(inlay),
                                                        o -> !inlay.isValid());
                                        return;
                                    }
                                }
                            }
                        });
        if (!file.getFileType().isBinary() && file.getLength() <= FilePreview.MAX_BYTES) {
            Document document = FileDocumentManager.getInstance().getDocument(file);
            if (document != null) {
                document.addDocumentListener(
                        new DocumentListener() {
                            @Override
                            public void documentChanged(@NotNull DocumentEvent event) {
                                preview.reload(inlay);
                            }
                        },
                        until);
            }
        }
    }
}
