// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.gherkin;

import com.intellij.execution.Location;
import com.intellij.execution.PsiLocation;
import com.intellij.execution.actions.ConfigurationContext;
import com.intellij.execution.actions.ConfigurationFromContext;
import com.intellij.execution.actions.RunConfigurationProducer;
import com.intellij.execution.lineMarker.RunLineMarkerContributor;
import com.intellij.openapi.actionSystem.ActionPlaces;
import com.intellij.openapi.actionSystem.CommonDataKeys;
import com.intellij.openapi.actionSystem.DataContext;
import com.intellij.openapi.actionSystem.PlatformCoreDataKeys;
import com.intellij.openapi.actionSystem.impl.SimpleDataContext;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.psi.PsiDirectory;
import com.intellij.psi.PsiElement;
import com.intellij.psi.PsiFile;
import com.intellij.psi.PsiManager;
import com.intellij.psi.SyntaxTraverser;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PsiTestUtil;

import us.nimbusxr.axx.idea.run.AxxRunConfiguration;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/** Run configurations and gutter icons from feature files, with the Gherkin plugin. */
public class AxxRunProducerTest extends HeavyPlatformTestCase {
    private static final String FEATURE =
            String.join(
                    "\n",
                    "Feature: Orders", // 1
                    "",
                    "  Background:", // 3
                    "    Given the orders service with the following properties:",
                    "      | url | http://localhost:8000 |", // 5
                    "",
                    "  Scenario: List orders", // 7
                    "    When a GET request is sent to \"/orders\"",
                    "    Then the response status code is 200", // 9
                    "",
                    "  @smoke", // 11
                    "  Scenario Outline: Status of <file>",
                    "    When a GET request is sent to \"/<file>\"", // 13
                    "    Then the response status code is <status>",
                    "",
                    "    Examples:", // 16
                    "      | file   | status |",
                    "      | a.json | 200    |", // 18
                    "      | b.json | 404    |",
                    "", // 20
                    "  Rule: Paging",
                    "",
                    "    Scenario: First page", // 23
                    "      When a GET request is sent to \"/orders?page=1\"",
                    "",
                    "    Scenario: Second page", // 26
                    "      When a GET request is sent to \"/orders?page=2\"",
                    "");

    private Path base;
    private Path suite;
    private PsiFile feature;
    private Document document;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        base = Path.of(getProject().getBasePath());
        suite = Files.createDirectories(base.resolve("acceptance"));
        Files.writeString(suite.resolve("axx.yaml"), "version: 1\n");
        Files.createDirectories(suite.resolve("features"));
        Files.writeString(suite.resolve("features/orders.feature"), FEATURE);
        Files.createDirectories(base.resolve("other"));
        Files.writeString(base.resolve("other/loose.feature"), "Feature: Loose\n");
        VirtualFile root = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(base);
        VfsUtil.markDirtyAndRefresh(false, true, true, root);
        PsiTestUtil.addContentRoot(getModule(), root);
        VirtualFile file = root.findFileByRelativePath("acceptance/features/orders.feature");
        feature = PsiManager.getInstance(getProject()).findFile(file);
        document = feature.getViewProvider().getDocument();
    }

    public void testFeatureLineRunsTheFile() {
        assertRun(atLine(1), "Feature: Orders", "features/orders.feature");
    }

    public void testBackgroundRunsTheFile() {
        assertRun(atLine(4), "Feature: Orders", "features/orders.feature");
    }

    public void testScenarioLineRunsTheScenario() {
        assertRun(atLine(7), "Scenario: List orders", "features/orders.feature:7");
    }

    public void testStepLineRunsItsScenario() {
        assertRun(atLine(9), "Scenario: List orders", "features/orders.feature:7");
    }

    public void testOutlineRunsFromItsKeywordLine() {
        String name = "Scenario Outline: Status of <file>";
        assertRun(atLine(11), name, "features/orders.feature:12");
        assertRun(atLine(12), name, "features/orders.feature:12");
        assertRun(atLine(16), name, "features/orders.feature:12");
        assertRun(atLine(17), name, "features/orders.feature:12");
    }

    public void testExamplesRowRunsThatExample() {
        assertRun(
                atLine(18),
                "Scenario Outline: Status of <file> [line 18]",
                "features/orders.feature:18");
        assertRun(
                atLine(19),
                "Scenario Outline: Status of <file> [line 19]",
                "features/orders.feature:19");
    }

    public void testRuleRunsItsScenarios() {
        assertRun(
                atLine(21),
                "Rule: Paging",
                "features/orders.feature:23",
                "features/orders.feature:26");
        assertRun(atLine(23), "Scenario: First page", "features/orders.feature:23");
    }

    public void testFileRunsTheFile() {
        assertRun(feature, "orders.feature", "features/orders.feature");
    }

    public void testDirectoryRunsItsFeatures() {
        assertRun(directory("acceptance/features"), "axx: features", "features");
    }

    public void testSuiteDirectoryRunsTheSuite() {
        assertRun(directory("acceptance"), "axx: acceptance");
    }

    public void testDirectoryAboveTheSuiteRunsTheSuite() {
        PsiDirectory root = directory("");
        assertRun(root, "axx: " + root.getName());
    }

    public void testNothingOutsideASuite() {
        PsiFile loose =
                PsiManager.getInstance(getProject())
                        .findFile(
                                LocalFileSystem.getInstance()
                                        .findFileByNioFile(base.resolve("other/loose.feature")));
        assertNull(configurationFor(loose));
        assertNull(configurationFor(directory("other")));
    }

    public void testGutterIconsOnFeatureRuleScenarioOutlineAndExamplesRows() {
        AxxRunLineMarkerContributor contributor = new AxxRunLineMarkerContributor();
        List<Integer> lines = new ArrayList<>();
        for (PsiElement leaf :
                SyntaxTraverser.psiTraverser(feature).filter(e -> e.getFirstChild() == null)) {
            RunLineMarkerContributor.Info info = contributor.getInfo(leaf);
            if (info != null) {
                lines.add(document.getLineNumber(leaf.getTextRange().getStartOffset()) + 1);
                assertTrue(info.actions.length > 0);
            }
        }
        assertEquals(List.of(1, 7, 12, 18, 19, 21, 23, 26), lines);
    }

    /**
     * The Gherkin plugin marks Feature and Scenario lines too. The gutter shows the markers that no
     * other marker on the element replaces; one per line, or every action would be listed twice.
     */
    public void testOneGutterMarkerPerLineWithTheGherkinPlugin() {
        List<RunLineMarkerContributor> contributors =
                RunLineMarkerContributor.EXTENSION.allForLanguage(feature.getLanguage());
        boolean gherkinMarks = false;
        for (PsiElement leaf :
                SyntaxTraverser.psiTraverser(feature).filter(e -> e.getFirstChild() == null)) {
            List<RunLineMarkerContributor.Info> infos = new ArrayList<>();
            for (RunLineMarkerContributor contributor : contributors) {
                RunLineMarkerContributor.Info info = contributor.getInfo(leaf);
                if (info != null) {
                    infos.add(info);
                    gherkinMarks |= !(contributor instanceof AxxRunLineMarkerContributor);
                }
            }
            long shown =
                    infos.stream()
                            .filter(i -> infos.stream().noneMatch(o -> o != i && o.shouldReplace(i)))
                            .count();
            assertTrue(
                    "line "
                            + (document.getLineNumber(leaf.getTextRange().getStartOffset()) + 1)
                            + " shows "
                            + shown
                            + " markers",
                    shown <= 1);
        }
        assertTrue("the Gherkin plugin's marker should be present to be replaced", gherkinMarks);
    }

    private PsiElement atLine(int line) {
        int offset = document.getLineStartOffset(line - 1);
        String text = document.getText().substring(offset, document.getLineEndOffset(line - 1));
        int indent = text.length() - text.stripLeading().length();
        return feature.findElementAt(offset + indent);
    }

    private PsiDirectory directory(String relative) {
        VirtualFile dir = LocalFileSystem.getInstance().findFileByNioFile(base.resolve(relative));
        return PsiManager.getInstance(getProject()).findDirectory(dir);
    }

    private AxxRunConfiguration configurationFor(PsiElement element) {
        ConfigurationContext context = contextFor(element);
        ConfigurationFromContext fromContext =
                RunConfigurationProducer.getInstance(AxxRunConfigurationProducer.class)
                        .createConfigurationFromContext(context);
        if (fromContext == null) {
            return null;
        }
        AxxRunConfiguration configuration = (AxxRunConfiguration) fromContext.getConfiguration();
        assertTrue(
                RunConfigurationProducer.getInstance(AxxRunConfigurationProducer.class)
                        .isConfigurationFromContext(configuration, context));
        return configuration;
    }

    private void assertRun(PsiElement element, String name, String... targets) {
        AxxRunConfiguration configuration = configurationFor(element);
        assertNotNull("no axx run configuration for " + element, configuration);
        assertEquals(name, configuration.getName());
        assertEquals(List.of(targets), configuration.getTargets());
        assertEquals(suite.toString(), configuration.getWorkingDirectory());
    }

    private ConfigurationContext contextFor(PsiElement element) {
        DataContext data =
                SimpleDataContext.builder()
                        .add(CommonDataKeys.PROJECT, getProject())
                        .add(PlatformCoreDataKeys.MODULE, getModule())
                        .add(Location.DATA_KEY, PsiLocation.fromPsiElement(element))
                        .build();
        return ConfigurationContext.getFromContext(data, ActionPlaces.UNKNOWN);
    }
}
