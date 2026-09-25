// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.it;

import com.intellij.codeInsight.completion.CodeCompletionHandlerBase;
import com.intellij.codeInsight.completion.CompletionType;
import com.intellij.codeInsight.lookup.Lookup;
import com.intellij.codeInsight.lookup.LookupElement;
import com.intellij.codeInsight.lookup.LookupManager;
import com.intellij.codeInsight.lookup.impl.LookupImpl;
import com.intellij.codeInsight.daemon.impl.HighlightInfo;
import com.intellij.codeInsight.navigation.CtrlMouseData;
import com.intellij.codeInsight.navigation.actions.GotoDeclarationHandler;
import com.intellij.codeInsight.navigation.actions.GotoDeclarationOrUsageHandler2;
import com.intellij.codeInsight.navigation.actions.GotoDeclarationOrUsageHandler2.GTDUOutcome;
import com.intellij.codeInsight.template.impl.TemplateManagerImpl;
import com.intellij.codeInsight.template.impl.TemplateState;
import com.intellij.codeInspection.InspectionSuppressor;
import com.intellij.codeInspection.LanguageInspectionSuppressors;
import com.intellij.lang.Language;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.application.ReadAction;
import com.intellij.openapi.command.WriteCommandAction;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.fileEditor.FileEditorManager;
import com.intellij.openapi.fileEditor.OpenFileDescriptor;
import com.intellij.openapi.util.TextRange;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.platform.backend.navigation.NavigationRequest;
import com.intellij.platform.lsp.api.LspServer;
import com.intellij.platform.lsp.api.LspServerManager;
import com.intellij.platform.lsp.api.LspServerState;
import com.intellij.pom.Navigatable;
import com.intellij.psi.PsiDocumentManager;
import com.intellij.psi.PsiElement;
import com.intellij.psi.PsiFile;
import com.intellij.psi.PsiManager;
import com.intellij.psi.PsiNamedElement;
import com.intellij.psi.PsiReference;
import com.intellij.testFramework.EditorTestUtil;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;
import com.intellij.testFramework.PsiTestUtil;
import com.intellij.testFramework.fixtures.impl.CodeInsightTestFixtureImpl;

import us.nimbusxr.axx.idea.AxxProject;
import us.nimbusxr.axx.idea.gherkin.AxxStepDeclarationHandler;
import us.nimbusxr.axx.idea.gherkin.AxxUndefinedStepSuppressor;
import us.nimbusxr.axx.idea.lsp.AxxLspServerSupportProvider;
import us.nimbusxr.axx.idea.AxxSettings;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;

/**
 * Runs the plugin in a headless IDE, with the Gherkin plugin, against a real {@code axx lsp}: the
 * plugin's wiring, the server starting for a feature file, Go to Declaration on a step and on a
 * file a step names, and step completion.
 *
 * <p>Opt-in: set {@code AXX_BIN} to an axx binary that has {@code axx lsp}, for example {@code
 * AXX_BIN=$PWD/../../bin/axx ./gradlew test --tests '*AxxLspIntegrationTest*'}. Skipped otherwise.
 */
public class AxxLspIntegrationTest extends HeavyPlatformTestCase {
    private static final String FEATURE =
            String.join(
                    "\n",
                    "Feature: Orders",
                    "  Scenario: List orders",
                    "    Given the orders service with the following properties:",
                    "      | base-url | http://localhost:8080 |",
                    "    When a GET request is sent to \"/orders\"",
                    "    Then the response status code is 200",
                    "",
                    "  Scenario Outline: Seeded orders",
                    "    Given a seeds/orders.yaml db seed",
                    "    And a <seed> db seed",
                    "",
                    "    Examples:",
                    "      | seed               |",
                    "      | seeds/refunds.yaml |",
                    "");
    private static final String STATUS_STEP = "the response status code is ${1:int}";
    private static final int TIMEOUT_MS = 60_000;

    private VirtualFile feature;
    private Editor editor;

    @Override
    protected boolean shouldRunTest() {
        return super.shouldRunTest() && System.getenv("AXX_BIN") != null;
    }

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        Path base = Path.of(getProject().getBasePath());
        Files.createDirectories(base.resolve("features"));
        Files.writeString(base.resolve("axx.yaml"), "version: 1\n");
        Files.writeString(base.resolve("axx-packs.yaml"), "packs: [rest, sql]\n");
        Files.createDirectories(base.resolve("seeds"));
        Files.writeString(base.resolve("seeds/orders.yaml"), "orders: []\n");
        Files.writeString(base.resolve("seeds/refunds.yaml"), "refunds: []\n");
        Files.writeString(base.resolve("features/orders.feature"), FEATURE);
        VirtualFile root = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(base);
        VfsUtil.markDirtyAndRefresh(false, true, true, root);
        PsiTestUtil.addContentRoot(getModule(), root);
        AxxSettings.getInstance().setExecutable(System.getenv("AXX_BIN"));
        feature = root.findFileByRelativePath("features/orders.feature");
        editor =
                FileEditorManager.getInstance(getProject())
                        .openTextEditor(new OpenFileDescriptor(getProject(), feature, 0), false);
    }

    @Override
    protected void tearDown() throws Exception {
        try {
            LspServerManager.getInstance(getProject())
                    .stopServers(AxxLspServerSupportProvider.class);
            FileEditorManager.getInstance(getProject()).closeFile(feature);
            AxxSettings.getInstance().setExecutable(null);
        } catch (Throwable e) {
            addSuppressedException(e);
        } finally {
            super.tearDown();
        }
    }

    public void testPluginWiring() {
        assertTrue(AxxProject.isAxxProject(getProject()));

        List<Class<?>> handlers = new ArrayList<>();
        for (GotoDeclarationHandler handler : GotoDeclarationHandler.EP_NAME.getExtensionList()) {
            handlers.add(handler.getClass());
        }
        assertTrue(handlers.toString(), handlers.contains(AxxStepDeclarationHandler.class));

        Language gherkin = Language.findLanguageByID("Gherkin");
        assertNotNull("the Gherkin plugin is not loaded", gherkin);
        List<Class<?>> suppressors = new ArrayList<>();
        for (InspectionSuppressor s :
                LanguageInspectionSuppressors.INSTANCE.allForLanguage(gherkin)) {
            suppressors.add(s.getClass());
        }
        assertTrue(suppressors.toString(), suppressors.contains(AxxUndefinedStepSuppressor.class));
    }

    public void testGoToDeclaration() throws Exception {
        waitForServer();
        PsiFile psi = PsiManager.getInstance(getProject()).findFile(feature);
        int offset = FEATURE.indexOf("status code");

        // The Gherkin plugin's own reference is there, and finds nothing.
        PsiReference gherkinReference = psi.findReferenceAt(offset);
        assertNotNull(gherkinReference);
        assertNull(gherkinReference.resolve());

        PsiElement element = psi.findElementAt(offset);
        PsiElement[] targets =
                inBackgroundReadAction(
                        () ->
                                new AxxStepDeclarationHandler()
                                        .getGotoDeclarationTargets(element, offset, editor));
        assertNotNull("no definition from the server", targets);
        assertEquals(1, targets.length);
        String name = ((PsiNamedElement) targets[0]).getName();
        assertTrue(name, name.matches("responses\\.go:\\d+"));

        NavigationRequest request =
                inBackgroundReadAction(() -> ((Navigatable) targets[0]).navigationRequest());
        assertNotNull(request);

        // The action goes to the declaration rather than saying "Cannot find declaration".
        assertEquals(
                GTDUOutcome.GTD,
                GotoDeclarationOrUsageHandler2.testGTDUOutcomeInNonBlockingReadAction(
                        editor, psi, offset));
    }

    public void testGoToFile() throws Exception {
        waitForServer();
        PsiFile psi = PsiManager.getInstance(getProject()).findFile(feature);
        // The editor's highlighting takes in the server's links.
        CodeInsightTestFixtureImpl.instantiateAndRun(psi, editor, new int[0], false);
        String step = "a seeds/orders.yaml db seed";
        int stepStart = FEATURE.indexOf(step);

        // Ctrl/Cmd+hover underlines the file a step names on its own, and clicking it opens the
        // file; the rest of the step leads to the step's definition, with its own underline.
        int onPath = stepStart + 8;
        CtrlMouseData path =
                inBackgroundReadAction(
                        () -> GotoDeclarationOrUsageHandler2.getCtrlMouseData(editor, psi, onPath));
        assertNotNull(path);
        int pathStart = FEATURE.indexOf("seeds/orders.yaml");
        assertEquals(
                List.of(new TextRange(pathStart, pathStart + "seeds/orders.yaml".length())),
                path.getRanges());
        assertEquals("orders.yaml", goToDeclaration(onPath));

        int onStep = stepStart + step.indexOf("db seed");
        CtrlMouseData rest =
                inBackgroundReadAction(
                        () -> GotoDeclarationOrUsageHandler2.getCtrlMouseData(editor, psi, onStep));
        assertNotNull(rest);
        assertEquals(List.of(new TextRange(stepStart, stepStart + step.length())), rest.getRanges());
        assertEquals("sql.go", goToDeclaration(onStep));

        // A file an Examples cell names.
        assertEquals("refunds.yaml", goToDeclaration(FEATURE.indexOf("seeds/refunds.yaml") + 7));
    }

    public void testParameterColors() throws Exception {
        waitForServer();
        PsiFile psi = PsiManager.getInstance(getProject()).findFile(feature);
        List<HighlightInfo> infos =
                CodeInsightTestFixtureImpl.instantiateAndRun(psi, editor, new int[0], false);
        // Step parameter values get the Gherkin plugin's step parameter color.
        int value = FEATURE.indexOf("200");
        boolean colored = false;
        for (HighlightInfo info : infos) {
            colored |=
                    info.getStartOffset() == value
                            && info.getEndOffset() == value + 3
                            && info.forcedTextAttributesKey != null
                            && "GHERKIN_REGEXP_PARAMETER"
                                    .equals(info.forcedTextAttributesKey.getExternalName());
        }
        assertTrue(infos.toString(), colored);
    }

    /** Runs Go to Declaration at an offset of the feature and returns the name of the file it opens. */
    private String goToDeclaration(int offset) throws Exception {
        Editor at =
                FileEditorManager.getInstance(getProject())
                        .openTextEditor(new OpenFileDescriptor(getProject(), feature, offset), true);
        EditorTestUtil.executeAction(at, "GotoDeclaration");
        long deadline = System.currentTimeMillis() + TIMEOUT_MS;
        while (System.currentTimeMillis() < deadline) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            VirtualFile[] selected = FileEditorManager.getInstance(getProject()).getSelectedFiles();
            if (selected.length > 0 && !selected[0].equals(feature)) {
                return selected[0].getName();
            }
            Thread.sleep(50);
        }
        return feature.getName();
    }

    public void testPathCompletion() throws Exception {
        waitForServer();
        Document document = editor.getDocument();
        // A {filepath} step parameter, and a file property in a table row being typed.
        String step = "    Given a seeds/or";
        String row = "      | openapi | seeds/re";
        for (String[] c :
                List.of(
                        new String[] {step, "a seeds/or", "orders.yaml", "    Given a seeds/orders.yaml"},
                        new String[] {
                            row, "seeds/re", "refunds.yaml", "      | openapi | seeds/refunds.yaml"
                        })) {
            String typed = c[0];
            String prefix = c[1];
            if (typed.equals(row)) {
                WriteCommandAction.runWriteCommandAction(
                        getProject(),
                        () ->
                                document.insertString(
                                        document.getTextLength(),
                                        "\n    And the orders service with the following"
                                                + " properties:\n"));
            }
            WriteCommandAction.runWriteCommandAction(
                    getProject(), () -> document.insertString(document.getTextLength(), typed));
            PsiDocumentManager.getInstance(getProject()).commitAllDocuments();
            editor.getCaretModel().moveToOffset(document.getTextLength());

            new CodeCompletionHandlerBase(CompletionType.BASIC).invokeCompletion(getProject(), editor);
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            LookupImpl lookup = (LookupImpl) LookupManager.getActiveLookup(editor);
            LookupElement file = null;
            List<String> items = new ArrayList<>();
            if (lookup != null) {
                for (LookupElement item : lookup.getItems()) {
                    items.add(item.getLookupString());
                    if (item.getLookupString().endsWith(c[2])) {
                        file = item;
                        assertEquals(prefix, lookup.itemPattern(item));
                    }
                }
            } else {
                // A single match is inserted without a popup.
                items.add("(inserted)");
            }
            String text = document.getText();
            String lastLine = text.substring(text.lastIndexOf('\n') + 1);
            if (file != null) {
                lookup.setCurrentItem(file);
                lookup.finishLookup(Lookup.NORMAL_SELECT_CHAR);
                PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
                text = document.getText();
                lastLine = text.substring(text.lastIndexOf('\n') + 1);
            }
            assertEquals(items.toString(), c[3], lastLine);
        }
    }

    public void testCompletion() throws Exception {
        waitForServer();
        Document document = editor.getDocument();
        String typed = "    Then the response sta";
        WriteCommandAction.runWriteCommandAction(
                getProject(), () -> document.insertString(document.getTextLength(), typed));
        PsiDocumentManager.getInstance(getProject()).commitAllDocuments();
        editor.getCaretModel().moveToOffset(document.getTextLength());

        new CodeCompletionHandlerBase(CompletionType.BASIC).invokeCompletion(getProject(), editor);
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
        LookupImpl lookup = (LookupImpl) LookupManager.getActiveLookup(editor);
        assertNotNull("no completion popup", lookup);
        LookupElement status = null;
        List<String> items = new ArrayList<>();
        for (LookupElement item : lookup.getItems()) {
            items.add(item.getLookupString());
            // Matched against the whole step text typed so far, not just "sta".
            assertEquals("the response sta", lookup.itemPattern(item));
            if (item.getLookupString().equals(STATUS_STEP)) {
                status = item;
            }
        }
        assertNotNull(items.toString(), status);

        // Choosing it replaces the typed step text with the step, its parameter a placeholder.
        TemplateManagerImpl.setTemplateTesting(getTestRootDisposable());
        lookup.setCurrentItem(status);
        lookup.finishLookup(Lookup.NORMAL_SELECT_CHAR);
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
        String lastLine = document.getText().substring(FEATURE.length());
        assertEquals("    Then the response status code is int", lastLine);
        TemplateState template = TemplateManagerImpl.getTemplateState(editor);
        assertNotNull("the parameter is not a placeholder", template);
        template.gotoEnd(false);
    }

    private void waitForServer() throws Exception {
        LspServerManager manager = LspServerManager.getInstance(getProject());
        manager.startServersIfNeeded(AxxLspServerSupportProvider.class);
        long deadline = System.currentTimeMillis() + TIMEOUT_MS;
        while (System.currentTimeMillis() < deadline) {
            for (LspServer server :
                    manager.getServersForProvider(AxxLspServerSupportProvider.class)) {
                if (server.getState() == LspServerState.Running) {
                    // Let the server receive the open file.
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

    private static <T> T inBackgroundReadAction(Callable<T> computation) {
        return PlatformTestUtil.waitForFuture(
                ApplicationManager.getApplication()
                        .executeOnPooledThread(() -> ReadAction.compute(computation::call)),
                TIMEOUT_MS);
    }
}
