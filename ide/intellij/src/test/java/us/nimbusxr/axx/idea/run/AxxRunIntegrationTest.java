// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.ExecutionResult;
import com.intellij.execution.Executor;
import com.intellij.execution.Location;
import com.intellij.execution.TestStateStorage;
import com.intellij.execution.executors.DefaultRunExecutor;
import com.intellij.execution.process.ProcessHandler;
import com.intellij.execution.runners.ExecutionEnvironment;
import com.intellij.execution.runners.ExecutionEnvironmentBuilder;
import com.intellij.execution.testframework.AbstractTestProxy;
import com.intellij.execution.testframework.sm.runner.SMTestProxy;
import com.intellij.execution.testframework.sm.runner.ui.SMTRunnerConsoleView;
import com.intellij.execution.testframework.stacktrace.DiffHyperlink;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.util.Disposer;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.openapi.vfs.VfsUtil;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.psi.PsiDocumentManager;
import com.intellij.psi.search.GlobalSearchScope;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;
import com.intellij.testFramework.PsiTestUtil;

import us.nimbusxr.axx.idea.AxxSettings;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Runs an axx run configuration in a headless IDE against a real {@code axx}: the test tree it
 * builds, navigation from it, the scenarios "Rerun Failed Tests" picks, and that rerun.
 *
 * <p>Opt-in: set {@code AXX_BIN} to an axx binary with {@code run --format teamcity}. The suite's
 * app is {@code python3 -m http.server 8000}, so python3 must be on PATH and port 8000 free.
 */
public class AxxRunIntegrationTest extends HeavyPlatformTestCase {
    private static final String AXX_YAML =
            String.join(
                    "\n",
                    "version: 1",
                    "run:",
                    "  paths: [features]",
                    "properties:",
                    "  local.host: localhost",
                    "apps:",
                    "  hello-axx:",
                    "    command: python3 -m http.server 8000",
                    "    ready:",
                    "      http:",
                    "        url: http://${sys:local.host}:8000/",
                    "");
    private static final String FEATURE =
            String.join(
                    "\n",
                    "Feature: Hello axx", // 1
                    "",
                    "  Background:",
                    "    Given the hello-axx service with the following properties:",
                    "      | url | http://${sys:local.host}:8000 |", // 5
                    "",
                    "  Scenario: The service says hello", // 7
                    "    Given a GET request to /hello.json",
                    "    When the request is executed",
                    "    Then the response status code is 200", // 10
                    "",
                    "  Scenario: A wrong expectation", // 12
                    "    Given a GET request to /hello.json",
                    "    When the request is executed",
                    "    Then the response payload property message is 'Hello, world'", // 15
                    "    And the response status code is 200",
                    "",
                    "  Scenario Outline: Status of <file>", // 18
                    "    Given a GET request to /<file>",
                    "    When the request is executed", // 20
                    "    Then the response status code is <status>",
                    "",
                    "    Examples:",
                    "      | file         | status |",
                    "      | hello.json   | 200    |", // 25
                    "      | missing.json | 404    |",
                    "",
                    "  Scenario: An undefined step", // 28
                    "    Given something axx does not know",
                    "");
    private static final int TIMEOUT_MS = 180_000;

    private Path suite;
    private VirtualFile feature;
    private AxxRunConfiguration configuration;

    @Override
    protected boolean shouldRunTest() {
        return super.shouldRunTest() && System.getenv("AXX_BIN") != null;
    }

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        suite = Path.of(getProject().getBasePath());
        Files.createDirectories(suite.resolve("features"));
        Files.writeString(suite.resolve("axx.yaml"), AXX_YAML);
        Files.writeString(suite.resolve("axx-packs.yaml"), "packs: [rest]\n");
        Files.writeString(suite.resolve("hello.json"), "{\"message\": \"Hello, axx\"}\n");
        Files.writeString(suite.resolve("features/smoke.feature"), FEATURE);
        VirtualFile root = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(suite);
        VfsUtil.markDirtyAndRefresh(false, true, true, root);
        PsiTestUtil.addContentRoot(getModule(), root);
        feature = root.findFileByRelativePath("features/smoke.feature");
        AxxSettings.getInstance().setExecutable(System.getenv("AXX_BIN"));
        configuration =
                (AxxRunConfiguration)
                        AxxRunConfigurationType.getInstance()
                                .getFactory()
                                .createTemplateConfiguration(getProject());
        configuration.setTargets(List.of("features"));
        configuration.setWorkingDirectory(suite.toString());
    }

    @Override
    protected void tearDown() throws Exception {
        try {
            AxxSettings.getInstance().setExecutable(null);
        } finally {
            super.tearDown();
        }
    }

    public void testRunBuildsTheTestTree() throws Exception {
        SMTestProxy.SMRootTestProxy root = runAxx(null);

        assertEquals(1, root.getChildren().size());
        SMTestProxy featureNode = root.getChildren().get(0);
        assertEquals("Feature: Hello axx", featureNode.getName());
        assertEquals(1, lineOf(featureNode));

        Set<String> scenarios = new HashSet<>();
        for (SMTestProxy scenario : featureNode.getChildren()) {
            scenarios.add(scenario.getName() + " @" + lineOf(scenario));
            assertTrue(scenario.isSuite());
        }
        assertEquals(
                Set.of(
                        "Scenario: The service says hello @7",
                        "Scenario: A wrong expectation @12",
                        "Scenario Outline: Status of hello.json (example 1) @25",
                        "Scenario Outline: Status of missing.json (example 2) @26",
                        "Scenario: An undefined step @28"),
                scenarios);

        SMTestProxy wrong = child(featureNode, "Scenario: A wrong expectation");
        assertTrue(wrong.isDefect());
        SMTestProxy failed =
                child(wrong, "Then the response payload property message is 'Hello, world'");
        assertTrue(failed.isDefect());
        assertEquals(15, lineOf(failed));
        DiffHyperlink diff = failed.getDiffViewerProvider();
        assertNotNull("no expected/actual comparison", diff);
        assertEquals("\"Hello, world\"", diff.getLeft());
        assertEquals("\"Hello, axx\"", diff.getRight());
        assertTrue(child(wrong, "And the response status code is 200").isIgnored());

        SMTestProxy undefined = child(featureNode, "Scenario: An undefined step");
        assertTrue(child(undefined, "Given something axx does not know").isDefect());
        assertTrue(child(featureNode, "Scenario: The service says hello").isPassed());

        List<AbstractTestProxy> defects = new ArrayList<>();
        for (SMTestProxy test : root.getAllTests()) {
            if (test.isDefect()) {
                defects.add(test);
            }
        }
        List<String> rerun = AxxRerunFailedTestsAction.failedScenarioTargets(defects, suite);
        assertEquals(
                Set.of("features/smoke.feature:12", "features/smoke.feature:28"),
                new HashSet<>(rerun));

        // The gutter shows each line's last result.
        String wrongUrl = RunTargets.locationUrl(feature.getPath(), 12);
        long deadline = System.currentTimeMillis() + 10_000;
        while (TestStateStorage.getInstance(getProject()).getState(wrongUrl) == null
                && System.currentTimeMillis() < deadline) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            Thread.sleep(100);
        }
        assertNotNull(
                "no stored result for " + wrongUrl,
                TestStateStorage.getInstance(getProject()).getState(wrongUrl));

        // Rerunning runs only those scenarios.
        SMTestProxy.SMRootTestProxy again = runAxx(rerun);
        Set<String> rerunScenarios = new HashSet<>();
        for (SMTestProxy scenario : again.getChildren().get(0).getChildren()) {
            rerunScenarios.add(scenario.getName());
        }
        assertEquals(
                Set.of("Scenario: A wrong expectation", "Scenario: An undefined step"),
                rerunScenarios);
    }

    public void testRunsOneExample() throws Exception {
        configuration.setTargets(List.of("features/smoke.feature:26"));
        SMTestProxy.SMRootTestProxy root = runAxx(null);
        List<? extends SMTestProxy> scenarios = root.getChildren().get(0).getChildren();
        assertEquals(1, scenarios.size());
        assertEquals(
                "Scenario Outline: Status of missing.json (example 2)", scenarios.get(0).getName());
        assertTrue(scenarios.get(0).isPassed());
    }

    private SMTestProxy.SMRootTestProxy runAxx(List<String> targets) throws Exception {
        Executor executor = DefaultRunExecutor.getRunExecutorInstance();
        ExecutionEnvironment environment =
                ExecutionEnvironmentBuilder.create(getProject(), executor, configuration).build();
        ExecutionResult result =
                new AxxRunState(configuration, environment, targets)
                        .execute(executor, environment.getRunner());
        SMTRunnerConsoleView console = (SMTRunnerConsoleView) result.getExecutionConsole();
        Disposer.register(getTestRootDisposable(), console);
        ProcessHandler handler = result.getProcessHandler();
        handler.startNotify();
        long deadline = System.currentTimeMillis() + TIMEOUT_MS;
        while (!handler.waitFor(100)) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            assertTrue("axx did not finish", System.currentTimeMillis() < deadline);
        }
        SMTestProxy.SMRootTestProxy root = console.getResultsViewer().getTestsRootNode();
        deadline = System.currentTimeMillis() + 10_000;
        while (root.isInProgress() && System.currentTimeMillis() < deadline) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            Thread.sleep(50);
        }
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
        assertFalse("the test tree is still running", root.isInProgress());
        return root;
    }

    private int lineOf(SMTestProxy proxy) {
        Location<?> location =
                proxy.getLocation(getProject(), GlobalSearchScope.allScope(getProject()));
        assertNotNull("no location for " + proxy.getLocationUrl(), location);
        Document document =
                PsiDocumentManager.getInstance(getProject())
                        .getDocument(location.getPsiElement().getContainingFile());
        assertEquals(feature, location.getVirtualFile());
        return document.getLineNumber(location.getPsiElement().getTextRange().getStartOffset()) + 1;
    }

    private static SMTestProxy child(SMTestProxy parent, String name) {
        for (SMTestProxy child : parent.getChildren()) {
            if (child.getName().equals(name)) {
                return child;
            }
        }
        List<String> names = new ArrayList<>();
        for (SMTestProxy child : parent.getChildren()) {
            names.add(child.getName());
        }
        throw new AssertionError("no " + name + " in " + names);
    }
}
