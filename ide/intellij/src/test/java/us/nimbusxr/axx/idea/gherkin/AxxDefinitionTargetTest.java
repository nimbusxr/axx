// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.application.ReadAction;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.EditorFactory;
import com.intellij.openapi.fileEditor.FileEditorManager;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.backend.navigation.NavigationRequest;
import com.intellij.psi.PsiFile;
import com.intellij.psi.PsiManager;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;

import java.nio.file.Files;
import java.nio.file.Path;

/** Navigation to a definition the axx language server names, in a headless IDE. */
public class AxxDefinitionTargetTest extends HeavyPlatformTestCase {
    private static final String GO =
            String.join(
                    "\n",
                    "package rest",
                    "",
                    "func init() {",
                    "\tsteps.Then(`the response status code is {int}`, statusIs)",
                    "}",
                    "");

    private Path base;
    private PsiFile source;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        base = Files.createDirectories(Path.of(getProject().getBasePath()));
        Files.writeString(base.resolve("orders.feature"), "Feature: Orders\n");
        Files.createDirectories(base.resolve("defs"));
        VirtualFile root = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(base);
        VfsUtil.markDirtyAndRefresh(false, true, true, root);
        // The IDE knows what the directory holds, so a file added later is new to it.
        root.findChild("defs").getChildren();
        source = PsiManager.getInstance(getProject()).findFile(root.findChild("orders.feature"));
    }

    public void testGoesToTheLineAndColumnOfAKnownFile() throws Exception {
        Path go = Files.writeString(base.resolve("defs/responses.go"), GO);
        VirtualFile file = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(go);
        AxxDefinitionTarget target =
                AxxDefinitionTarget.create(source, go.toUri().toString(), file, 3, 7);
        assertNotNull(target);
        assertEquals("responses.go:4", target.getName());

        NavigationRequest request = inBackgroundReadAction(target);
        assertNotNull(request);

        target.navigate(true);
        assertCaretAt(go, GO.indexOf("Then("));
    }

    public void testGoesToAFileTheIdeHasNotSeenYet() throws Exception {
        // The server generates reference pages; the IDE may know the directory, not the page.
        Path page = base.resolve("defs/rest.response-status.md");
        Files.writeString(page, "# Then the response status code is {int}\n\nChecks the status.\n");
        assertNull(LocalFileSystem.getInstance().findFileByNioFile(page));
        AxxDefinitionTarget target =
                AxxDefinitionTarget.create(source, page.toUri().toString(), null, 2, 0);
        assertNotNull(target);

        // Navigation then happens on the UI thread, where the file can be refreshed in.
        assertNotNull(inBackgroundReadAction(target));
        target.navigate(true);
        assertCaretAt(page, Files.readString(page).indexOf("Checks"));
    }

    public void testIgnoresLocationsThatAreNotFiles() {
        assertNull(AxxDefinitionTarget.create(source, "https://example.test/steps.go", null, 0, 0));
    }

    public void testClampsPositionsToTheText() {
        Document document = EditorFactory.getInstance().createDocument("ab\ncde\n");
        assertEquals(1, AxxDefinitionTarget.offset(document, 0, 1));
        assertEquals(5, AxxDefinitionTarget.offset(document, 1, 2));
        assertEquals(6, AxxDefinitionTarget.offset(document, 1, 99));
        assertEquals(7, AxxDefinitionTarget.offset(document, 9, 0));
    }

    private void assertCaretAt(Path file, int offset) {
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
        Editor editor = FileEditorManager.getInstance(getProject()).getSelectedTextEditor();
        assertNotNull("no editor opened", editor);
        VirtualFile opened = FileEditorManager.getInstance(getProject()).getSelectedFiles()[0];
        assertEquals(file, opened.toNioPath());
        assertEquals(offset, editor.getCaretModel().getOffset());
        FileEditorManager.getInstance(getProject()).closeFile(opened);
    }

    private static NavigationRequest inBackgroundReadAction(AxxDefinitionTarget target) {
        return PlatformTestUtil.waitForFuture(
                ApplicationManager.getApplication()
                        .executeOnPooledThread(() -> ReadAction.compute(target::navigationRequest)),
                10_000);
    }
}
