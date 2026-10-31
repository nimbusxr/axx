// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.Location;
import com.intellij.execution.process.ProcessHandler;
import com.intellij.execution.testframework.sm.runner.SMTestProxy;
import com.intellij.execution.testframework.sm.runner.ui.SMTRunnerConsoleView;
import com.intellij.execution.ui.RunContentDescriptor;
import com.intellij.openapi.util.Disposer;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.psi.search.GlobalSearchScope;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;

import us.nimbusxr.axx.idea.host.ReportLocations;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

/**
 * A run the axx host serves, shown in the test runner from the report lines the host brings, as
 * {@code axx host --ide} writes them for a run in a sandbox that sees the project at {@code
 * /sandbox/parcels}.
 */
public class HostRunConsoleTest extends HeavyPlatformTestCase {
    private static final String SANDBOX = "/sandbox/parcels";
    private static final List<String> REPORT =
            List.of(
                    "##teamcity[enteredTheMatrix]",
                    "##teamcity[testingStarted]",
                    "##teamcity[testSuiteStarted nodeId='feature-1' parentNodeId='0' name='Feature:"
                        + " Health'"
                        + " locationHint='file:///sandbox/parcels/features/health.feature:1']",
                    "##teamcity[testSuiteStarted nodeId='129' parentNodeId='feature-1'"
                            + " name='Scenario: the service is healthy'"
                            + " locationHint='file:///sandbox/parcels/features/health.feature:6']",
                    "##teamcity[testStarted nodeId='129/126' parentNodeId='129' name='Given a GET"
                            + " request to /health'"
                            + " locationHint='file:///sandbox/parcels/features/health.feature:7']",
                    "##teamcity[testFinished nodeId='129/126' duration='0']",
                    "##teamcity[testStarted nodeId='129/127' parentNodeId='129' name='When the"
                            + " request is executed'"
                            + " locationHint='file:///sandbox/parcels/features/health.feature:8']",
                    "##teamcity[testStdOut nodeId='129/127' out='log: GET"
                            + " http://localhost:18080/health -> 200 OK (3ms, 16 bytes)|n']",
                    "##teamcity[testFinished nodeId='129/127' duration='3']",
                    "##teamcity[testStarted nodeId='129/128' parentNodeId='129' name='Then the"
                            + " response status code is 200'"
                            + " locationHint='file:///sandbox/parcels/features/health.feature:9']",
                    "##teamcity[testFinished nodeId='129/128' duration='0']",
                    "##teamcity[testSuiteFinished nodeId='129']",
                    "##teamcity[testSuiteFinished nodeId='feature-1']",
                    "##teamcity[testingFinished]",
                    "",
                    "1 scenario (1 passed)");
    private static final String FEATURE =
            String.join(
                    "\n",
                    "Feature: Health",
                    "  Background:",
                    "    Given the api service with the following properties:",
                    "      | url | http://localhost:18080 |",
                    "",
                    "  Scenario: the service is healthy", // 6
                    "    Given a GET request to /health",
                    "    When the request is executed",
                    "    Then the response status code is 200",
                    "");

    private Path project;

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        project = Path.of(getProject().getBasePath());
        Files.createDirectories(project.resolve("features"));
        Files.writeString(project.resolve("axx.yaml"), "version: 1\n");
        Files.writeString(project.resolve("features/health.feature"), FEATURE);
        LocalFileSystem.getInstance()
                .refreshAndFindFileByNioFile(project.resolve("features/health.feature"));
    }

    public void testShowsTheRunFromTheHostsLines() throws Exception {
        HostRunConsole console =
                new HostRunConsole(getProject(), "axx host: run 1 (axx run --compact)", project);
        // The host's lines come before the tab shows, and after.
        console.print("run 1: axx run --compact");
        console.print("run 1: ready in 1.0s");
        for (String line : REPORT.subList(0, 5)) {
            console.report(ReportLocations.relocate(line, SANDBOX, project.toString()));
        }
        RunContentDescriptor descriptor = console.attach();
        Disposer.register(getTestRootDisposable(), descriptor);
        for (String line : REPORT.subList(5, REPORT.size())) {
            console.report(ReportLocations.relocate(line, SANDBOX, project.toString()));
        }
        console.print("run 1: 1 scenario: 1 passed, 0 failed; stopped and cleaned up in 3.4s");
        assertFalse(console.isEnded());
        console.end(0);
        assertTrue(console.isEnded());

        SMTestProxy.SMRootTestProxy root = finished(descriptor);
        assertTrue(root.isPassed());
        assertEquals(1, root.getChildren().size());
        SMTestProxy feature = root.getChildren().get(0);
        assertEquals("Feature: Health", feature.getName());
        SMTestProxy scenario = feature.getChildren().get(0);
        assertEquals("Scenario: the service is healthy", scenario.getName());
        assertEquals(
                List.of(
                        "Given a GET request to /health",
                        "When the request is executed",
                        "Then the response status code is 200"),
                scenario.getChildren().stream().map(SMTestProxy::getName).toList());

        // Its locations are the project's, where the IDE finds the lines.
        String url = "file://" + project.toString().replace('\\', '/');
        assertEquals(url + "/features/health.feature:6", scenario.getLocationUrl());
        Location<?> location =
                scenario.getLocation(getProject(), GlobalSearchScope.allScope(getProject()));
        assertNotNull("the scenario's line is not found", location);
        assertEquals("health.feature", location.getVirtualFile().getName());
    }

    public void testShowsARunThatEndedBeforeItsTabShows() throws Exception {
        HostRunConsole console =
                new HostRunConsole(getProject(), "axx host: run 2 (axx run)", project);
        console.print("run 2: waits for your approval of the services' changed definition");
        console.print(
                "run 2: the person running the axx host did not approve the services' changed"
                        + " definition [AXX-E0501]");
        console.end(4);
        // Nothing after its end counts.
        console.report(REPORT.get(2));

        RunContentDescriptor descriptor = console.attach();
        Disposer.register(getTestRootDisposable(), descriptor);
        SMTestProxy.SMRootTestProxy root = finished(descriptor);
        assertEquals(Integer.valueOf(4), descriptor.getProcessHandler().getExitCode());
        assertTrue(root.getChildren().isEmpty());
    }

    public void testStopOnlyStopsShowingTheRun() throws Exception {
        HostRunConsole console =
                new HostRunConsole(getProject(), "axx host: run 3 (axx run)", project);
        RunContentDescriptor descriptor = console.attach();
        Disposer.register(getTestRootDisposable(), descriptor);
        for (String line : REPORT.subList(0, 4)) {
            console.report(ReportLocations.relocate(line, SANDBOX, project.toString()));
        }
        ProcessHandler handler = descriptor.getProcessHandler();
        assertTrue(handler.detachIsDefault());
        handler.detachProcess();
        assertTrue(handler.waitFor(10_000));
        assertTrue(console.isEnded());
        // The run goes on; the IDE no longer shows it.
        console.report(REPORT.get(4));
        console.end(0);
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
    }

    private static SMTestProxy.SMRootTestProxy finished(RunContentDescriptor descriptor)
            throws InterruptedException {
        ProcessHandler handler = descriptor.getProcessHandler();
        long deadline = System.currentTimeMillis() + 10_000;
        while (!handler.waitFor(50)) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            assertTrue("the run did not end", System.currentTimeMillis() < deadline);
        }
        SMTRunnerConsoleView console = (SMTRunnerConsoleView) descriptor.getExecutionConsole();
        SMTestProxy.SMRootTestProxy root = console.getResultsViewer().getTestsRootNode();
        while (root.isInProgress() && System.currentTimeMillis() < deadline) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            Thread.sleep(50);
        }
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
        assertFalse("the test tree is still running", root.isInProgress());
        return root;
    }
}
