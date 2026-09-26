// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.execution.testframework.AbstractTestProxy;
import com.intellij.openapi.actionSystem.ActionUpdateThread;
import com.intellij.openapi.actionSystem.AnActionEvent;
import com.intellij.openapi.project.DumbAwareAction;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.ui.popup.JBPopupFactory;
import com.intellij.ui.ColoredListCellRenderer;

import org.jetbrains.annotations.NotNull;

import java.util.List;

import javax.swing.JList;

/**
 * <em>Open Trace</em> and <em>Play Video</em> in the context menu of a test tree node whose
 * scenario kept one (see {@link KeptFile}). With several, it asks which.
 */
public abstract class KeptFileAction extends DumbAwareAction {
    private final KeptFile.Kind kind;

    KeptFileAction(KeptFile.Kind kind) {
        this.kind = kind;
    }

    @Override
    public @NotNull ActionUpdateThread getActionUpdateThread() {
        return ActionUpdateThread.BGT;
    }

    @Override
    public void update(@NotNull AnActionEvent e) {
        AbstractTestProxy node = e.getData(AbstractTestProxy.DATA_KEY);
        e.getPresentation()
                .setEnabledAndVisible(e.getProject() != null && !KeptFile.of(node, kind).isEmpty());
    }

    @Override
    public void actionPerformed(@NotNull AnActionEvent e) {
        Project project = e.getProject();
        List<KeptFile> files = KeptFile.of(e.getData(AbstractTestProxy.DATA_KEY), kind);
        if (project == null || files.isEmpty()) {
            return;
        }
        if (files.size() == 1) {
            AxxWebViewer.open(project, files.get(0));
            return;
        }
        JBPopupFactory.getInstance()
                .createPopupChooserBuilder(files)
                .setTitle(kind == KeptFile.Kind.TRACE ? "Open Trace" : "Play Video")
                .setRenderer(
                        new ColoredListCellRenderer<KeptFile>() {
                            @Override
                            protected void customizeCellRenderer(
                                    @NotNull JList<? extends KeptFile> list,
                                    KeptFile file,
                                    int index,
                                    boolean selected,
                                    boolean hasFocus) {
                                append(file.path().getFileName().toString());
                            }
                        })
                .setItemChosenCallback(file -> AxxWebViewer.open(project, file))
                .createPopup()
                .showInBestPositionFor(e.getDataContext());
    }

    /** Opens the Playwright trace the scenario kept. */
    public static final class OpenTrace extends KeptFileAction {
        public OpenTrace() {
            super(KeptFile.Kind.TRACE);
        }
    }

    /** Plays the video the scenario kept. */
    public static final class PlayVideo extends KeptFileAction {
        public PlayVideo() {
            super(KeptFile.Kind.VIDEO);
        }
    }
}
