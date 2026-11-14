// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.openapi.Disposable;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.EditorKind;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.InlayProperties;
import com.intellij.openapi.editor.event.DocumentEvent;
import com.intellij.openapi.editor.event.DocumentListener;
import com.intellij.openapi.editor.event.EditorFactoryEvent;
import com.intellij.openapi.editor.event.EditorFactoryListener;
import com.intellij.openapi.editor.ex.util.EditorUtil;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.Disposer;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.util.Alarm;

import org.jetbrains.annotations.NotNull;

import us.nimbusxr.axx.idea.AxxProject;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;

/**
 * Puts the toggles of the files a feature names ({@link FileToggle}) at the ends of the lines that
 * name them, in each editor of a feature file of an axx project, and keeps them there as the file
 * changes: the files are the axx language server's links ({@link FileLinks}), asked for again a
 * moment after each edit.
 */
public final class FileToggles implements EditorFactoryListener {
    /** How long after an edit the links are asked for again. */
    static final int DELAY_MS = 300;

    @Override
    public void editorCreated(@NotNull EditorFactoryEvent event) {
        Editor editor = event.getEditor();
        Project project = editor.getProject();
        VirtualFile file = FileDocumentManager.getInstance().getFile(editor.getDocument());
        if (project == null
                || !(editor.getEditorKind() == EditorKind.MAIN_EDITOR
                        || editor.getEditorKind() == EditorKind.UNTYPED)
                || file == null
                || !"feature".equals(file.getExtension())
                || !AxxProject.isAxxProject(project)) {
            return;
        }
        Disposable disposable = Disposer.newDisposable("axx file toggles");
        EditorUtil.disposeWithEditor(editor, disposable);
        new Toggles(project, file, editor, disposable).start();
    }

    /** The toggles of one editor. */
    private record Toggles(
            @NotNull Project project,
            @NotNull VirtualFile feature,
            @NotNull Editor editor,
            @NotNull Disposable disposable) {
        void start() {
            Document document = editor.getDocument();
            Alarm alarm = new Alarm(Alarm.ThreadToUse.SWING_THREAD, disposable);
            FileLinks.listen(document, this::refresh, disposable);
            document.addDocumentListener(
                    new DocumentListener() {
                        @Override
                        public void documentChanged(@NotNull DocumentEvent event) {
                            alarm.cancelAllRequests();
                            alarm.addRequest(Toggles.this::refresh, DELAY_MS);
                        }
                    },
                    disposable);
            refresh();
        }

        /** Puts the toggles where the links are. On the event dispatch thread. */
        void refresh() {
            if (editor.isDisposed() || project.isDisposed()) {
                return;
            }
            Document document = editor.getDocument();
            FileLinks.Links links = FileLinks.of(project, feature, document);
            if (links == null) {
                return;
            }
            Map<Integer, List<VirtualFile>> wanted = new TreeMap<>();
            for (FileLinks.Link link : links.links()) {
                int line = link.line(document);
                if (line < 0) {
                    continue;
                }
                List<VirtualFile> files = wanted.computeIfAbsent(line, l -> new ArrayList<>());
                if (!files.contains(link.file())) {
                    files.add(link.file());
                }
            }
            if (!wanted.equals(current(editor))) {
                for (Inlay<? extends FileToggle> inlay : inlays(editor)) {
                    Disposer.dispose(inlay);
                }
                for (Map.Entry<Integer, List<VirtualFile>> line : wanted.entrySet()) {
                    editor.getInlayModel()
                            .addAfterLineEndElement(
                                    document.getLineEndOffset(line.getKey()),
                                    new InlayProperties().relatesToPrecedingText(true),
                                    new FileToggle(editor, line.getValue()));
                }
            }
            if (links.fresh()) {
                // Previews of files their lines no longer name close.
                Map<Integer, Set<VirtualFile>> named = new TreeMap<>();
                wanted.forEach((line, files) -> named.put(line, new HashSet<>(files)));
                FilePreviews.retain(editor, named);
            }
        }
    }

    /** The editor's toggles. */
    static @NotNull List<Inlay<? extends FileToggle>> inlays(@NotNull Editor editor) {
        return editor.getInlayModel()
                .getAfterLineEndElementsInRange(
                        0, editor.getDocument().getTextLength(), FileToggle.class);
    }

    /** The files of the editor's toggles, by line. */
    static @NotNull Map<Integer, List<VirtualFile>> current(@NotNull Editor editor) {
        Map<Integer, List<VirtualFile>> out = new TreeMap<>();
        for (Inlay<? extends FileToggle> inlay : inlays(editor)) {
            out.put(
                    editor.getDocument().getLineNumber(inlay.getOffset()),
                    inlay.getRenderer().files());
        }
        return out;
    }
}
