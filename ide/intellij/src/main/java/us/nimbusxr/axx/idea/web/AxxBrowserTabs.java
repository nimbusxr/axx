// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.ide.BrowserUtil;
import com.intellij.openapi.fileEditor.FileEditor;
import com.intellij.openapi.fileEditor.FileEditorManager;
import com.intellij.openapi.fileEditor.FileEditorPolicy;
import com.intellij.openapi.fileEditor.FileEditorProvider;
import com.intellij.openapi.fileEditor.FileEditorState;
import com.intellij.openapi.fileTypes.FileTypes;
import com.intellij.openapi.project.DumbAware;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.util.Disposer;
import com.intellij.openapi.util.UserDataHolderBase;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.testFramework.LightVirtualFile;
import com.intellij.ui.jcef.JBCefApp;
import com.intellij.ui.jcef.JBCefBrowser;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.beans.PropertyChangeListener;

import javax.swing.JComponent;

/**
 * Web pages in editor tabs, in the IDE's browser (JCEF): Playwright's trace viewer, and videos.
 * Each kind has one tab, reused: showing another page there replaces the one it shows. Without
 * JCEF, pages open in the default browser.
 */
public final class AxxBrowserTabs {
    /** The kinds of tab. */
    public enum Tab {
        TRACE,
        VIDEO
    }

    private AxxBrowserTabs() {}

    /** Whether pages show in the IDE; otherwise they open in the default browser. */
    public static boolean inIde() {
        return JBCefApp.isSupported();
    }

    /** Shows a page in the tab of its kind, which takes the focus. Call on the UI thread. */
    public static void show(
            @NotNull Project project,
            @NotNull Tab tab,
            @NotNull String title,
            @NotNull String url) {
        if (!inIde()) {
            BrowserUtil.browse(url);
            return;
        }
        FileEditorManager manager = FileEditorManager.getInstance(project);
        for (VirtualFile open : manager.getOpenFiles()) {
            if (open instanceof BrowserFile file && file.tab == tab) {
                file.url = url;
                file.title = title;
                manager.updateFilePresentation(file);
                for (FileEditor editor : manager.getAllEditors(file)) {
                    if (editor instanceof BrowserEditor browserEditor) {
                        browserEditor.browser.loadURL(url);
                    }
                }
                manager.openFile(file, true);
                return;
            }
        }
        manager.openFile(new BrowserFile(tab, title, url), true);
    }

    /** The file an editor tab shows a page for. */
    static final class BrowserFile extends LightVirtualFile {
        final Tab tab;
        volatile String url;
        // The tab's title: the page it shows changes it, and the file itself is read-only.
        volatile String title;

        BrowserFile(Tab tab, String title, String url) {
            // No file type of its own: no other editor opens it, whatever its title.
            super(title, FileTypes.UNKNOWN, "");
            this.tab = tab;
            this.url = url;
            this.title = title;
            setWritable(false);
        }

        @Override
        public @NotNull String getName() {
            return title;
        }
    }

    /** Opens browser tabs with a JCEF browser, instead of any other editor. */
    public static final class Provider implements FileEditorProvider, DumbAware {
        @Override
        public boolean accept(@NotNull Project project, @NotNull VirtualFile file) {
            return file instanceof BrowserFile && inIde();
        }

        @Override
        public boolean acceptRequiresReadAction() {
            return false;
        }

        @Override
        public @NotNull FileEditor createEditor(
                @NotNull Project project, @NotNull VirtualFile file) {
            return new BrowserEditor((BrowserFile) file);
        }

        @Override
        public @NotNull String getEditorTypeId() {
            return "axx-web-page";
        }

        @Override
        public @NotNull FileEditorPolicy getPolicy() {
            return FileEditorPolicy.HIDE_DEFAULT_EDITOR;
        }
    }

    private static final class BrowserEditor extends UserDataHolderBase implements FileEditor {
        private final BrowserFile file;
        private final JBCefBrowser browser;

        BrowserEditor(BrowserFile file) {
            this.file = file;
            this.browser = JBCefBrowser.createBuilder().setUrl(file.url).build();
        }

        @Override
        public @NotNull JComponent getComponent() {
            return browser.getComponent();
        }

        @Override
        public @Nullable JComponent getPreferredFocusedComponent() {
            return browser.getComponent();
        }

        @Override
        public @NotNull String getName() {
            return "Browser";
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
        public void dispose() {
            Disposer.dispose(browser);
        }
    }
}
