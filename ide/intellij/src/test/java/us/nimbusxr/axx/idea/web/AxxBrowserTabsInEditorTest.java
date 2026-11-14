// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.openapi.fileEditor.FileEditor;
import com.intellij.openapi.fileEditor.FileEditorPolicy;
import com.intellij.openapi.fileEditor.FileEditorProvider;
import com.intellij.openapi.fileEditor.FileEditorState;
import com.intellij.openapi.project.DumbAware;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.UserDataHolderBase;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.testFramework.ExtensionTestUtil;
import com.intellij.testFramework.FileEditorManagerTestCase;

import org.jetbrains.annotations.NotNull;

import us.nimbusxr.axx.idea.web.AxxBrowserTabs.BrowserFile;
import us.nimbusxr.axx.idea.web.AxxBrowserTabs.Tab;

import java.beans.PropertyChangeListener;
import java.util.ArrayList;
import java.util.List;

import javax.swing.JComponent;
import javax.swing.JPanel;

/**
 * The tabs of traces and videos among the IDE's editors, with the IDE's own editor manager: one
 * tab for each kind, which shows the next page of its kind. A stand-in opens them here, in place
 * of the JCEF editor.
 */
public class AxxBrowserTabsInEditorTest extends FileEditorManagerTestCase {
    @Override
    public void setUp() {
        super.setUp();
        // In place of the JCEF editor, which a machine with a display would start a browser for.
        List<FileEditorProvider> providers = new ArrayList<>();
        for (FileEditorProvider provider : FileEditorProvider.EP_FILE_EDITOR_PROVIDER.getExtensionList()) {
            if (!(provider instanceof AxxBrowserTabs.Provider)) {
                providers.add(provider);
            }
        }
        providers.add(new StandIn());
        ExtensionTestUtil.maskExtensions(
                FileEditorProvider.EP_FILE_EDITOR_PROVIDER, providers, getTestRootDisposable());
    }

    public void testATabOpensForEachKindAndShowsTheNextPageOfItsKind() {
        AxxBrowserTabs.showTab(
                getProject(), Tab.TRACE, "Trace: register", "http://127.0.0.1:1/a/register.zip");
        assertEquals(List.of("Trace: register"), tabs());
        assertEquals("Trace: register", inFront().getName());
        assertEquals(
                "the tab's own editor, and no other",
                List.of(Page.class),
                manager.getAllEditorList(inFront()).stream().map(Object::getClass).toList());

        AxxBrowserTabs.showTab(
                getProject(), Tab.VIDEO, "Video: register", "http://127.0.0.1:1/a/register.webm");
        assertEquals(List.of("Trace: register", "Video: register"), tabs());
        assertEquals("Video: register", inFront().getName());

        AxxBrowserTabs.showTab(
                getProject(), Tab.TRACE, "Trace: cancel", "http://127.0.0.1:1/b/cancel.zip");
        assertEquals(List.of("Trace: cancel", "Video: register"), tabs());
        assertEquals("the trace's tab is in front again", "Trace: cancel", inFront().getName());
        assertEquals("http://127.0.0.1:1/b/cancel.zip", ((BrowserFile) inFront()).url);
    }

    private List<String> tabs() {
        List<String> names = new ArrayList<>();
        for (VirtualFile file : manager.getOpenFiles()) {
            names.add(file.getName());
        }
        names.sort(null);
        return names;
    }

    private VirtualFile inFront() {
        VirtualFile[] files = manager.getSelectedFiles();
        assertEquals("one tab is in front", 1, files.length);
        return files[0];
    }

    /** Opens the tabs' files, as the JCEF editor does in an IDE that has one. */
    private static final class StandIn implements FileEditorProvider, DumbAware {
        @Override
        public boolean accept(@NotNull Project project, @NotNull VirtualFile file) {
            return file instanceof BrowserFile;
        }

        @Override
        public @NotNull FileEditor createEditor(@NotNull Project project, @NotNull VirtualFile file) {
            return new Page(file);
        }

        @Override
        public @NotNull String getEditorTypeId() {
            return "axx-web-page-stand-in";
        }

        @Override
        public @NotNull FileEditorPolicy getPolicy() {
            return FileEditorPolicy.HIDE_DEFAULT_EDITOR;
        }
    }

    private static final class Page extends UserDataHolderBase implements FileEditor {
        private final VirtualFile file;
        private final JPanel panel = new JPanel();

        Page(VirtualFile file) {
            this.file = file;
        }

        @Override
        public @NotNull JComponent getComponent() {
            return panel;
        }

        @Override
        public JComponent getPreferredFocusedComponent() {
            return panel;
        }

        @Override
        public @NotNull String getName() {
            return "page";
        }

        @Override
        public @NotNull VirtualFile getFile() {
            return file;
        }

        @Override
        public void setState(@NotNull FileEditorState state) {}

        @Override
        public boolean isModified() {
            return false;
        }

        @Override
        public boolean isValid() {
            return true;
        }

        @Override
        public void addPropertyChangeListener(@NotNull PropertyChangeListener listener) {}

        @Override
        public void removePropertyChangeListener(@NotNull PropertyChangeListener listener) {}

        @Override
        public void dispose() {}
    }
}
