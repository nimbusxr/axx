// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.preview;

import com.intellij.lang.documentation.ide.IdeDocumentationTargetProvider;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.application.ReadAction;
import com.intellij.openapi.command.WriteCommandAction;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.colors.EditorColorsManager;
import com.intellij.openapi.fileEditor.FileEditorManager;
import com.intellij.openapi.fileEditor.OpenFileDescriptor;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.backend.documentation.DocumentationTarget;
import com.intellij.platform.lsp.api.LspClient;
import com.intellij.platform.lsp.api.LspClientManager;
import com.intellij.platform.lsp.api.LspServerState;
import com.intellij.psi.PsiDocumentManager;
import com.intellij.psi.PsiFile;
import com.intellij.psi.PsiManager;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;
import com.intellij.testFramework.PsiTestUtil;


import us.nimbusxr.axx.idea.AxxSettings;
import us.nimbusxr.axx.idea.lsp.AxxLspIntegrationProvider;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import java.util.function.Predicate;

/**
 * The file previews' toggles in a headless IDE against a real {@code axx lsp}: one at the end of
 * each line that names a file, from the server's links, which open and close the previews.
 *
 * <p>Opt-in, as {@code AxxLspIntegrationTest}: set {@code AXX_BIN} to an axx binary.
 */
public class FilePreviewIntegrationTest extends HeavyPlatformTestCase {
    private static final String FEATURE =
            String.join(
                    "\n",
                    "Feature: Manifest import",
                    "",
                    "  Scenario: Manifest lines become parcels",
                    "    Given a seeds/manifest-kestrel.yaml db seed",
                    "    And a seeds/missing.yaml db seed",
                    "",
                    "  Scenario Outline: Seeded",
                    "    Given a <seed> db seed",
                    "    And a <other> db seed",
                    "",
                    "    Examples:",
                    "      | seed                        | other                    |",
                    "      | seeds/manifest-kestrel.yaml | seeds/manifest-fjord.yaml |",
                    "");
    private static final int TIMEOUT_MS = 60_000;

    private VirtualFile feature;
    private VirtualFile kestrel;
    private VirtualFile fjord;
    private Editor editor;

    @Override
    protected boolean shouldRunTest() {
        return super.shouldRunTest() && System.getenv("AXX_BIN") != null;
    }

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        Path base = Path.of(getProject().getBasePath());
        Files.createDirectories(base.resolve("features/seeds"));
        Files.writeString(base.resolve("axx.yaml"), "version: 1\n");
        Files.writeString(base.resolve("axx-packs.yaml"), "packs: [sql]\n");
        Files.writeString(base.resolve("features/seeds/manifest-kestrel.yaml"), "manifest_lines: []\n");
        Files.writeString(base.resolve("features/seeds/manifest-fjord.yaml"), "manifest_lines: []\n");
        Files.writeString(base.resolve("features/manifest.feature"), FEATURE);
        VirtualFile root = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(base);
        VfsUtil.markDirtyAndRefresh(false, true, true, root);
        PsiTestUtil.addContentRoot(getModule(), root);
        AxxSettings.getInstance().setExecutable(System.getenv("AXX_BIN"));
        feature = root.findFileByRelativePath("features/manifest.feature");
        kestrel = root.findFileByRelativePath("features/seeds/manifest-kestrel.yaml");
        fjord = root.findFileByRelativePath("features/seeds/manifest-fjord.yaml");
        editor =
                FileEditorManager.getInstance(getProject())
                        .openTextEditor(new OpenFileDescriptor(getProject(), feature, 0), false);
    }

    @Override
    protected void tearDown() throws Exception {
        try {
            LspClientManager.getInstance(getProject())
                    .stopClients(AxxLspIntegrationProvider.class);
            FileEditorManager.getInstance(getProject()).closeFile(feature);
            AxxSettings.getInstance().setExecutable(null);
        } catch (Throwable e) {
            addSuppressedException(e);
        } finally {
            super.tearDown();
        }
    }

    public void testToggles() throws Exception {
        waitForServer();

        // A toggle on each line that names a file that is there: the step's, and the Examples
        // row's, which names two, each by its name.
        assertEquals(
                Map.of(
                        3, List.of("▸ preview"),
                        12, List.of("▸ manifest-kestrel.yaml", "▸ manifest-fjord.yaml")),
                waitForToggles(t -> !t.isEmpty()));
        Inlay<? extends FileToggle> row = FileToggles.inlays(editor).get(1);
        assertEquals(List.of(kestrel, fjord), row.getRenderer().files());
        // The second toggle of the row is past the first, and nothing is between them.
        int first = row.getRenderer().calcWidthInPixels(row) / 4;
        assertEquals(kestrel, row.getRenderer().fileAt(row, first));
        assertEquals(fjord, row.getRenderer().fileAt(row, row.getRenderer().calcWidthInPixels(row) - 2));
        assertNull(row.getRenderer().fileAt(row, 1));

        // A click opens the preview, and the toggle says so.
        FilePreviews.toggle(editor, 3, kestrel);
        FilePreviews.toggle(editor, 12, fjord);
        assertEquals(List.of("▾ preview"), toggles().get(3));
        assertEquals(List.of("▸ manifest-kestrel.yaml", "▾ manifest-fjord.yaml"), toggles().get(12));
        assertEquals(2, FilePreviews.all(editor).size());

        // A line that no longer names the file loses its toggle, and the preview closes.
        Document document = editor.getDocument();
        WriteCommandAction.runWriteCommandAction(
                getProject(),
                () -> {
                    int at = document.getText().indexOf("seeds/manifest-kestrel.yaml db seed");
                    document.replaceString(at, at + "seeds/manifest-kestrel".length(), "seeds/missing");
                });
        PsiDocumentManager.getInstance(getProject()).commitAllDocuments();
        assertEquals(
                List.of(12),
                new ArrayList<>(waitForToggles(t -> !t.containsKey(3)).keySet()));
        assertEquals(1, FilePreviews.all(editor).size());
        assertEquals(fjord, FilePreviews.all(editor).get(0).getRenderer().file());
    }

    /**
     * Hovering a file's path, or Quick Documentation on it, shows the file first: the IDE's link
     * has a documentation target of its own, which has none.
     */
    public void testDocumentation() throws Exception {
        waitForServer();
        waitForToggles(t -> !t.isEmpty());
        PsiFile psi = PsiManager.getInstance(getProject()).findFile(feature);
        int offset = FEATURE.indexOf("seeds/manifest-kestrel.yaml") + 5;
        List<? extends DocumentationTarget> targets =
                PlatformTestUtil.waitForFuture(
                        ApplicationManager.getApplication()
                                .executeOnPooledThread(
                                        () ->
                                                ReadAction.computeBlocking(
                                                        () ->
                                                                IdeDocumentationTargetProvider
                                                                        .getInstance(getProject())
                                                                        .documentationTargets(
                                                                                editor, psi, offset))),
                        TIMEOUT_MS);
        assertFalse(targets.isEmpty());
        assertEquals(
                new FilePreviewDocumentation.Target(getProject(), kestrel), targets.get(0));
        FilePreview.Content content =
                FilePreview.load(
                        getProject(),
                        kestrel,
                        EditorColorsManager.getInstance().getGlobalScheme(),
                        FilePreviewDocumentation.MAX_LINES);
        String html = FilePreviewDocumentation.html(getProject(), kestrel, content).html();
        assertTrue(html, html.contains("features/seeds/manifest-kestrel.yaml · 1 line"));
        assertTrue(html, html.contains("manifest_lines"));

        // Elsewhere on the step, the step's documentation.
        int step = FEATURE.indexOf("db seed");
        assertFalse(
                new FilePreviewDocumentation().documentationTargets(psi, step).iterator().hasNext());
    }

    /** The toggles' labels, by line. */
    private Map<Integer, List<String>> toggles() {
        Map<Integer, List<String>> out = new TreeMap<>();
        for (Inlay<? extends FileToggle> inlay : FileToggles.inlays(editor)) {
            List<String> labels = new ArrayList<>();
            for (int i = 0; i < inlay.getRenderer().files().size(); i++) {
                labels.add(inlay.getRenderer().label(inlay, i));
            }
            out.put(editor.getDocument().getLineNumber(inlay.getOffset()), labels);
        }
        return out;
    }

    /**
     * The toggles once they are as wanted: they follow the server's answers, which come in the
     * background.
     */
    private Map<Integer, List<String>> waitForToggles(Predicate<Map<Integer, List<String>>> wanted)
            throws Exception {
        long deadline = System.currentTimeMillis() + TIMEOUT_MS;
        while (System.currentTimeMillis() < deadline) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            Map<Integer, List<String>> toggles = toggles();
            if (wanted.test(toggles)) {
                return toggles;
            }
            Thread.sleep(100);
        }
        fail("the toggles are " + toggles());
        return null;
    }

    private void waitForServer() throws Exception {
        LspClientManager manager = LspClientManager.getInstance(getProject());
        manager.startClientsIfNeeded(AxxLspIntegrationProvider.class);
        long deadline = System.currentTimeMillis() + TIMEOUT_MS;
        while (System.currentTimeMillis() < deadline) {
            for (LspClient server : manager.getClients(AxxLspIntegrationProvider.class)) {
                if (server.getState() == LspServerState.Running) {
                    Thread.sleep(300);
                    PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
                    return;
                }
            }
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            Thread.sleep(100);
        }
        fail("the axx language server did not start");
    }
}
