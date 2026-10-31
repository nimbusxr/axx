// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import com.intellij.execution.testframework.sm.runner.SMTestProxy;
import com.intellij.execution.testframework.sm.runner.ui.SMTRunnerConsoleView;
import com.intellij.execution.ui.RunContentDescriptor;
import com.intellij.notification.Notification;
import com.intellij.notification.NotificationsManager;
import com.intellij.openapi.actionSystem.AnAction;
import com.intellij.openapi.util.SystemInfo;
import com.intellij.testFramework.HeavyPlatformTestCase;
import com.intellij.testFramework.PlatformTestUtil;

import us.nimbusxr.axx.idea.AxxSettings;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.List;
import java.util.function.BooleanSupplier;

/**
 * The project's axx host, started in a headless IDE: a script plays {@code axx host --ide} (a run
 * of an agent's in a sandbox that sees the project at {@code /sandbox/parcels}, which asks the
 * person to approve, and a run of the IDE's own), and the test answers as the person would, with a
 * click on the question's notification.
 */
public class AxxHostTest extends HeavyPlatformTestCase {
    /** {@code axx host --ide}: its events before the person's answer, the answer, the rest. */
    private static final String HOST =
            String.join(
                    "\n",
                    "#!/bin/sh",
                    "[ \"$1 $2\" = 'host --ide' ] || exit 2",
                    "cat before.jsonl",
                    "read -r answer",
                    "printf '%s\\n' \"$answer\" > answers.txt",
                    "cat after.jsonl",
                    "echo 'run 1 cleaned up' >&2",
                    // Until the IDE closes the input.
                    "cat > /dev/null",
                    "");

    private static final List<String> BEFORE =
            List.of(
                    "{\"event\":\"serving\",\"addr\":\"127.0.0.1:4000\",\"dir\":\"%s\","
                            + "\"services\":[\"api\"],\"sources\":[\"compose.yaml\"],\"warnings\":"
                            + "[\"services.api: compose service api mounts /tmp of this machine\"],"
                            + "\"changed\":true}",
                    "{\"event\":\"run\",\"dir\":\"/sandbox/parcels\",\"run\":1,\"command\":\"axx"
                        + " run --compact\",\"state\":\"queued\"}",
                    "{\"event\":\"run\",\"run\":1,\"command\":\"axx run --compact\",\"state\":"
                            + "\"approval\"}",
                    "{\"event\":\"approval\",\"run\":1,\"command\":\"axx run"
                        + " --compact\",\"id\":1,\"approved\":\"api:\\n"
                        + "  build: .\\n"
                        + "\",\"proposed\":\"api:\\n"
                        + "  build: .\\n"
                        + "  volumes: [/tmp]\\n"
                        + "\",\"diff\":\"+  volumes: [/tmp]\\n"
                        + "\",\"warnings\":[\"services.api: compose service api mounts /tmp of this"
                        + " machine\"]}");
    private static final List<String> AFTER =
            List.of(
                    "{\"event\":\"answered\",\"run\":1,\"id\":1,\"answer\":\"approved\"}",
                    "{\"event\":\"run\",\"services\":[\"api\"],\"run\":1,\"command\":\"axx run"
                            + " --compact\",\"state\":\"starting\"}",
                    "{\"event\":\"run\",\"services\":[\"api\"],\"run\":1,\"command\":\"axx run"
                            + " --compact\",\"state\":\"started\",\"seconds\":1.0}",
                    "{\"event\":\"run\",\"dir\":\"%s\",\"run\":2,\"command\":\"axx run --format"
                            + " teamcity\",\"ide\":\"intellij\",\"state\":\"queued\"}",
                    "{\"event\":\"report\",\"run\":2,\"line\":\"##teamcity[testingStarted]\"}",
                    "{\"event\":\"report\",\"run\":1,\"line\":\"##teamcity[testingStarted]\"}",
                    "{\"event\":\"report\",\"run\":1,\"line\":\"##teamcity[testSuiteStarted"
                        + " nodeId='feature-1' parentNodeId='0' name='Feature: Health'"
                        + " locationHint='file:///sandbox/parcels/features/health.feature:1']\"}",
                    "{\"event\":\"report\",\"run\":1,\"line\":\"##teamcity[testSuiteFinished"
                            + " nodeId='feature-1']\"}",
                    "{\"event\":\"report\",\"run\":1,\"line\":\"##teamcity[testingFinished]\"}",
                    "{\"event\":\"run\",\"run\":1,\"command\":\"axx run --compact\",\"state\":"
                            + "\"stopping\"}",
                    "{\"event\":\"run\",\"run\":1,\"command\":\"axx run --compact\",\"state\":"
                            + "\"ended\",\"result\":{\"passed\":0,\"failed\":0,\"other\":0},"
                            + "\"seconds\":0.5}",
                    "{\"event\":\"run\",\"run\":2,\"command\":\"axx run --format teamcity\","
                            + "\"ide\":\"intellij\",\"state\":\"ended\",\"seconds\":0.5}");

    private Path base;

    @Override
    protected boolean shouldRunTest() {
        return super.shouldRunTest() && !SystemInfo.isWindows;
    }

    @Override
    protected void setUp() throws Exception {
        super.setUp();
        base = Files.createDirectories(Path.of(getProject().getBasePath()));
        Files.writeString(base.resolve("axx.yaml"), "version: 1\nservices:\n  api: {}\n");
    }

    @Override
    protected void tearDown() throws Exception {
        try {
            AxxHost host = AxxHost.getIfCreated(getProject());
            if (host != null) {
                host.stop();
                waitFor("the host stops", () -> !host.isRunning());
            }
            AxxSettings.getInstance().setExecutable(null);
        } catch (Throwable e) {
            addSuppressedException(e);
        } finally {
            super.tearDown();
        }
    }

    private void host(String script) throws Exception {
        Path axx = Files.writeString(base.resolve("axx-host"), script);
        assertTrue(axx.toFile().setExecutable(true));
        AxxSettings.getInstance().setExecutable(axx.toString());
    }

    public void testAsksAndShowsTheAgentsRun() throws Exception {
        host(HOST);
        Files.write(base.resolve("before.jsonl"), events(BEFORE));
        Files.write(base.resolve("after.jsonl"), events(AFTER));
        AxxHost host = AxxHost.getInstance(getProject());
        host.start(false);
        waitFor("the host's question", host::hasPendingApproval);
        assertTrue(host.isRunning());
        assertEquals("axx host: waiting for your approval", host.text());

        Notification question = question();
        assertNotNull("no question asked", question);
        assertEquals("axx host: approve run 1?", question.getTitle());
        assertTrue(
                question.getContent(),
                question.getContent()
                        .contains("services.api: compose service api mounts /tmp of this machine"));
        List<String> buttons =
                question.getActions().stream().map(AnAction::getTemplateText).toList();
        assertEquals(List.of("Show Diff", "Approve", "Refuse"), buttons);
        assertFalse(Files.exists(base.resolve("answers.txt")));

        // The person's click.
        Notification.fire(question, question.getActions().get(1), null);
        waitFor("the answer", () -> Files.exists(base.resolve("answers.txt")));
        waitFor("the run's end", () -> host.state().currentRun() == null);
        assertEquals("{\"approve\":1}\n", Files.readString(base.resolve("answers.txt")));
        assertEquals("axx host: ready", host.text());
        waitFor("the question to close", question::isExpired);

        // The agent's run shows in the Run tool window, at the project's locations. The IDE's own,
        // which came after it, shows already: it gets no tab.
        waitFor("the run's tab", () -> host.lastShown() != null);
        RunContentDescriptor descriptor = host.lastShown().descriptor();
        assertEquals("axx host: run 1 (axx run --compact)", descriptor.getDisplayName());
        waitFor("the run's test tree", () -> descriptor.getProcessHandler().isProcessTerminated());
        SMTestProxy.SMRootTestProxy root =
                ((SMTRunnerConsoleView) descriptor.getExecutionConsole())
                        .getResultsViewer()
                        .getTestsRootNode();
        waitFor("the run's test tree", () -> !root.isInProgress());
        assertEquals(1, root.getChildren().size());
        assertEquals(
                "file://" + base.toString().replace('\\', '/') + "/features/health.feature:1",
                root.getChildren().get(0).getLocationUrl());

        host.stop();
        waitFor("the host stops", () -> !host.isRunning());
        assertEquals("axx host: stopped", host.text());
    }

    public void testAsksAboutAChangeAsTheHostStarts() throws Exception {
        host(HOST);
        Files.write(
                base.resolve("before.jsonl"),
                events(List.of(BEFORE.get(0), HostEventTest.START_APPROVAL)));
        // Refused, then a newer question that the host drops as it stops.
        Files.write(
                base.resolve("after.jsonl"),
                List.of(
                        "{\"event\":\"answered\",\"id\":1,\"answer\":\"refused\"}",
                        "{\"event\":\"approval\",\"id\":2,\"proposed\":\"api: {}\\n\"}",
                        "{\"event\":\"answered\",\"id\":2,\"answer\":\"abandoned\"}"));
        AxxHost host = AxxHost.getInstance(getProject());
        host.start(false);
        waitFor("the host's question", host::hasPendingApproval);
        assertEquals("axx host: waiting for your approval", host.text());

        Notification question = question();
        assertNotNull("no question asked", question);
        assertEquals("axx host: approve the services?", question.getTitle());
        assertTrue(
                question.getContent(),
                question.getContent()
                        .startsWith(
                                "The services' definition changed since you last approved it."
                                        + " Approve?<ul><li>services.api: compose service api is"
                                        + " privileged</li></ul>"));

        Notification.fire(question, question.getActions().get(2), null);
        waitFor("the answer", () -> Files.exists(base.resolve("answers.txt")));
        assertEquals("{\"refuse\":1}\n", Files.readString(base.resolve("answers.txt")));
        waitFor("the questions to close", () -> !host.hasPendingApproval() && question() == null);
        assertTrue(question.isExpired());
        assertEquals("axx host: ready", host.text());
    }

    public void testSaysWhenAHostServesTheProjectAlready() throws Exception {
        host(
                String.join(
                        "\n",
                        "#!/bin/sh",
                        "echo 'error[AXX-E0503]: an axx host already serves /work/parcels (pid"
                            + " 18719, 127.0.0.1:51727)' >&2",
                        "echo '  hint: its runs are yours too: stop it first to start another' >&2",
                        "exit 4",
                        ""));
        AxxHost host = AxxHost.getInstance(getProject());
        host.start(false);
        waitFor("the host's end", () -> host.state() != null && !host.isRunning());
        assertEquals("axx host: already running in a terminal", host.text());
        assertTrue(host.isShown());
        assertEquals(
                List.of("an axx host already serves /work/parcels (pid 18719, 127.0.0.1:51727)"),
                host.tooltip());
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
        assertFalse(
                "an error popped up",
                Arrays.stream(notifications())
                        .anyMatch(n -> n.getContent().contains("did not start")));
    }

    public void testNoServicesNoHost() throws Exception {
        host(
                String.join(
                        "\n",
                        "#!/bin/sh",
                        "echo 'Error[AXX-E0001]: axx.yaml declares no services: an axx host has"
                                + " nothing to start' >&2",
                        "exit 2",
                        ""));
        AxxHost host = AxxHost.getInstance(getProject());
        host.start(true);
        waitFor("the host's end", () -> host.state() != null && !host.isRunning());
        assertFalse(host.isShown());
    }

    /** The events, with the project directory for the host's and the IDE's own run's. */
    private List<String> events(List<String> lines) {
        return lines.stream().map(line -> line.replace("%s", base.toString())).toList();
    }

    private Notification question() {
        return Arrays.stream(notifications())
                .filter(n -> AxxHost.QUESTIONS.equals(n.getGroupId()) && !n.isExpired())
                .findFirst()
                .orElse(null);
    }

    private Notification[] notifications() {
        return NotificationsManager.getNotificationsManager()
                .getNotificationsOfType(Notification.class, getProject());
    }

    private static void waitFor(String what, BooleanSupplier condition) throws Exception {
        long deadline = System.currentTimeMillis() + 20_000;
        while (!condition.getAsBoolean()) {
            PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
            assertTrue("no " + what, System.currentTimeMillis() < deadline);
            Thread.sleep(20);
        }
        PlatformTestUtil.dispatchAllEventsInIdeEventQueue();
    }
}
