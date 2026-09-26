// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.run;

import com.intellij.execution.executors.DefaultRunExecutor;
import com.intellij.execution.filters.HyperlinkInfo;
import com.intellij.execution.process.NopProcessHandler;
import com.intellij.execution.process.ProcessHandler;
import com.intellij.execution.process.ProcessOutputTypes;
import com.intellij.execution.testframework.Printable;
import com.intellij.execution.testframework.Printer;
import com.intellij.execution.testframework.sm.SMTestRunnerConnectionUtil;
import com.intellij.execution.testframework.sm.runner.SMTestProxy;
import com.intellij.execution.testframework.sm.runner.ui.SMTRunnerConsoleView;
import com.intellij.execution.ui.ConsoleViewContentType;
import com.intellij.openapi.util.Disposer;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;

import org.jetbrains.annotations.NotNull;

import us.nimbusxr.axx.idea.web.AxxPausedRuns;
import us.nimbusxr.axx.idea.web.KeptFile;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * The lines packs print for the IDE, in fake {@code axx run --format teamcity} output: each trace
 * and video lands on its scenario's node with a link in its console, a paused scenario is known to
 * the project until it resumes or its run ends, while the lines themselves are not printed.
 */
public class AxxIdeLinesTest extends HeavyPlatformTestCase {

    public void testTracesAndVideosLandOnTheirScenarios() throws Exception {
        String base = getProject().getBasePath();
        String feature = base + "/features/shop-portal.feature";
        String traces = base + "/.axx/web/traces";
        String viewer = base + "/cache/playwright/package/lib/vite/traceViewer";
        SMTestProxy.SMRootTestProxy root =
                run(
                        "##teamcity[enteredTheMatrix]",
                        "##teamcity[testingStarted]",
                        suite("f", "0", "Feature: Shop portal", feature, 1),
                        suite("s1", "f", "Scenario: Register a parcel", feature, 12),
                        "[AXX-IDE] trace-viewer dir=" + viewer,
                        "##teamcity[testStarted nodeId='s1/1' parentNodeId='s1' name='When the page"
                                + " is opened']",
                        "##teamcity[testFinished nodeId='s1/1' duration='3']",
                        "##teamcity[testSuiteFinished nodeId='s1']",
                        "[AXX-IDE] trace path="
                                + traces
                                + "/register.zip"
                                + " location=features/shop-portal.feature:12",
                        suite("s2", "f", "Scenario: Quote a parcel", feature, 25),
                        "[AXX-IDE] video path="
                                + base
                                + "/.axx/web/videos/quote%20page.webm"
                                + " location=features/shop-portal.feature:25",
                        "##teamcity[testSuiteFinished nodeId='s2']",
                        suite("s3", "f", "Scenario: Track a parcel", feature, 30),
                        "##teamcity[testSuiteFinished nodeId='s3']",
                        "##teamcity[testSuiteFinished nodeId='f']",
                        "[AXX-IDE] debug-attach-request name=parcels type=go host=localhost"
                                + " port=2345",
                        "3 scenarios (3 passed)");

        SMTestProxy register = scenario(root, "Scenario: Register a parcel");
        List<KeptFile> kept = KeptFile.of(register, KeptFile.Kind.TRACE);
        assertEquals(1, kept.size());
        assertEquals(Path.of(traces, "register.zip"), kept.get(0).path());
        assertEquals(Path.of(viewer), kept.get(0).viewerFolder());
        assertEquals("Register a parcel", kept.get(0).label());
        // A step's node opens its scenario's.
        assertEquals(kept, KeptFile.of(register.getChildren().get(0), KeptFile.Kind.TRACE));
        assertEmpty(KeptFile.of(register, KeptFile.Kind.VIDEO));

        SMTestProxy quote = scenario(root, "Scenario: Quote a parcel");
        List<KeptFile> videos = KeptFile.of(quote, KeptFile.Kind.VIDEO);
        assertEquals(1, videos.size());
        assertEquals(Path.of(base, ".axx/web/videos/quote page.webm"), videos.get(0).path());
        assertEmpty(KeptFile.of(scenario(root, "Scenario: Track a parcel"), KeptFile.Kind.TRACE));

        Output registerOutput = output(register);
        assertTrue(
                registerOutput.text,
                registerOutput.text.contains("Trace: .axx/web/traces/register.zip  Open trace\n"));
        assertEquals(List.of("Open trace"), registerOutput.links);
        assertEquals(List.of("Play video"), output(quote).links);

        String all = output(root).text;
        assertFalse(all, all.contains("[AXX-IDE] trace"));
        assertFalse(all, all.contains("[AXX-IDE] video"));
        assertTrue(all, all.contains("[AXX-IDE] debug-attach-request name=parcels"));
        assertTrue(all, all.contains("3 scenarios (3 passed)"));
    }

    public void testFilesOfUnknownScenariosLandOnTheRoot() throws Exception {
        String base = getProject().getBasePath();
        String feature = base + "/features/shop-portal.feature";
        SMTestProxy.SMRootTestProxy root =
                run(
                        suite("f", "0", "Feature: Shop portal", feature, 1),
                        suite("s1", "f", "Scenario: Register a parcel", feature, 12),
                        "##teamcity[testSuiteFinished nodeId='s1']",
                        "[AXX-IDE] trace path="
                                + base
                                + "/.axx/web/traces/other.zip"
                                + " location=features/other.feature:3",
                        "##teamcity[testSuiteFinished nodeId='f']");

        List<KeptFile> kept = KeptFile.of(root, KeptFile.Kind.TRACE);
        assertEquals(1, kept.size());
        assertEquals("other.zip", kept.get(0).label());
        assertNull(kept.get(0).viewerFolder());
    }

    public void testPausesLastUntilTheyResumeOrTheRunEnds() throws Exception {
        String base = getProject().getBasePath();
        String feature = base + "/features/shop-portal.feature";
        String url = "http://127.0.0.1:53211/3f9c0a/";
        Path recording = Path.of(base, ".axx/web/my recording.txt");
        AxxPausedRuns runs = AxxPausedRuns.getInstance(getProject());
        Run run = start();
        run.feed(
                suite("f", "0", "Feature: Shop portal", feature, 1),
                suite("s1", "f", "Scenario: Quote a parcel", feature, 20),
                "[AXX-IDE] paused url="
                        + url
                        + " location=features/shop-portal.feature:24 recording="
                        + recording.toString().replace(" ", "%20"));
        assertEquals(
                new AxxPausedRuns.Pause(url, "features/shop-portal.feature:24"), runs.current());
        assertEquals(url, runs.liveUrl());
        assertEquals(recording, runs.recording());

        run.feed("[AXX-IDE] resumed url=http://127.0.0.1:1/other/");
        assertNotNull("another scenario's resume", runs.current());
        run.feed("[AXX-IDE] resumed url=" + url);
        assertNull(runs.current());
        assertEquals("the run still answers for its recorded steps", url, runs.liveUrl());

        run.feed(
                "[AXX-IDE] paused url="
                        + url
                        + " location=features/shop-portal.feature:26 recording="
                        + recording.toString().replace(" ", "%20"));
        assertEquals(
                new AxxPausedRuns.Pause(url, "features/shop-portal.feature:26"), runs.current());
        run.feed(
                "##teamcity[testSuiteFinished nodeId='s1']",
                "##teamcity[testSuiteFinished nodeId='f']",
                "1 scenario (1 failed)");
        SMTestProxy.SMRootTestProxy root = run.finish();
        assertNull(runs.current());
        assertNull(runs.liveUrl());
        assertEquals("the recording file stays", recording, runs.recording());

        String all = output(root).text;
        assertFalse(all, all.contains("[AXX-IDE]"));
        assertTrue(all, all.contains("1 scenario (1 failed)"));
    }

    private SMTestProxy.SMRootTestProxy run(String... lines) throws Exception {
        Run run = start();
        run.feed(lines);
        return run.finish();
    }

    /** A fake axx run in the test runner, fed line by line. */
    private record Run(ProcessHandler handler, SMTRunnerConsoleView console) {
        void feed(String... lines) {
            for (String line : lines) {
                handler.notifyTextAvailable(line + "\n", ProcessOutputTypes.STDOUT);
            }
        }

        SMTestProxy.SMRootTestProxy finish() {
            handler.destroyProcess();
            assertTrue(handler.waitFor(10_000));
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            return console.getResultsViewer().getTestsRootNode();
        }
    }

    private Run start() throws Exception {
        AxxRunConfiguration configuration =
                (AxxRunConfiguration)
                        AxxRunConfigurationType.getInstance()
                                .getFactory()
                                .createTemplateConfiguration(getProject());
        AxxTestConsoleProperties properties =
                new AxxTestConsoleProperties(
                        configuration,
                        DefaultRunExecutor.getRunExecutorInstance(),
                        Path.of(getProject().getBasePath()));
        ProcessHandler handler = new NopProcessHandler();
        SMTRunnerConsoleView console =
                (SMTRunnerConsoleView)
                        SMTestRunnerConnectionUtil.createAndAttachConsole(
                                AxxTestConsoleProperties.FRAMEWORK, handler, properties);
        Disposer.register(getTestRootDisposable(), console);
        handler.startNotify();
        return new Run(handler, console);
    }

    private static String suite(String id, String parent, String name, String file, int line) {
        return "##teamcity[testSuiteStarted nodeId='"
                + id
                + "' parentNodeId='"
                + parent
                + "' name='"
                + name
                + "' locationHint='"
                + RunTargets.locationUrl(file, line)
                + "']";
    }

    private static SMTestProxy scenario(SMTestProxy root, String name) {
        for (SMTestProxy feature : root.getChildren()) {
            for (SMTestProxy scenario : feature.getChildren()) {
                if (scenario.getName().equals(name)) {
                    return scenario;
                }
            }
        }
        throw new AssertionError("no " + name);
    }

    /** What a node's console shows: its text, with links, and the links alone. */
    private record Output(String text, List<String> links) {}

    private static Output output(SMTestProxy node) {
        StringBuilder text = new StringBuilder();
        List<String> links = new ArrayList<>();
        node.printOn(
                new Printer() {
                    @Override
                    public void print(@NotNull String s, @NotNull ConsoleViewContentType type) {
                        text.append(s);
                    }

                    @Override
                    public void onNewAvailable(@NotNull Printable printable) {
                        printable.printOn(this);
                    }

                    @Override
                    public void printHyperlink(@NotNull String s, HyperlinkInfo info) {
                        text.append(s);
                        links.add(s);
                    }

                    @Override
                    public void mark() {}
                });
        return new Output(text.toString(), links);
    }
}
