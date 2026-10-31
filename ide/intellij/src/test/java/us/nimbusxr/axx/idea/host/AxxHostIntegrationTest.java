// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.jetbrains.annotations.NotNull;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Predicate;

/**
 * The IDE's side of a real {@code axx host --ide}, with a real run through it, as an agent's: the
 * events the IDE reads, its approval of a changed definition, and the host's stop when the IDE
 * closes its input.
 *
 * <p>Opt-in: set {@code AXX_BIN} to an axx binary with {@code axx host}, and {@code
 * AXX_HOST_PROJECT} to a project whose axx.yaml declares services, whose scenarios pass, and that
 * no other host serves (the test starts its host there, and runs {@code axx run --compact} in it).
 * When its services changed since they were last approved, the test approves them.
 */
@DisplayName("axx host --ide, for real")
@EnabledIfEnvironmentVariable(named = "AXX_BIN", matches = ".+")
@EnabledIfEnvironmentVariable(named = "AXX_HOST_PROJECT", matches = ".+")
class AxxHostIntegrationTest {
    private static final long TIMEOUT_SECONDS = 300;

    /** What the IDE heard from a host, and the approvals it answered. */
    private static final class Heard implements HostProcess.Listener {
        final BlockingQueue<HostEvent> events = new LinkedBlockingQueue<>();
        final List<String> log = new CopyOnWriteArrayList<>();
        final BlockingQueue<String> exits = new LinkedBlockingQueue<>();
        final AtomicInteger approved = new AtomicInteger();
        volatile HostProcess host;

        @Override
        public void event(@NotNull HostEvent event) {
            // Asked as the host starts (no run), or for a run.
            if (HostEvent.APPROVAL.equals(event.event())) {
                assertTrue(host.answer(event.id(), true));
            }
            if (HostEvent.ANSWERED.equals(event.event())) {
                assertEquals("approved", event.answer());
                approved.incrementAndGet();
            }
            events.add(event);
        }

        @Override
        public void log(@NotNull String line) {
            log.add(line);
        }

        @Override
        public void exited(int exitCode, @NotNull String errors) {
            exits.add(exitCode + ": " + errors);
        }

        HostEvent next(String what) throws InterruptedException {
            HostEvent event = events.poll(TIMEOUT_SECONDS, TimeUnit.SECONDS);
            assertNotNull(event, "no " + what + "; the host's log: " + log);
            return event;
        }

        /** The next event that is what the test waits for, past the questions and answers. */
        HostEvent until(String what, Predicate<HostEvent> wanted) throws InterruptedException {
            for (HostEvent event = next(what); ; event = next(what)) {
                if (wanted.test(event)) {
                    return event;
                }
            }
        }
    }

    @Test
    @DisplayName("serves an agent's run, and stops when the IDE closes its input")
    void servesARun() throws Exception {
        Path axx = Path.of(System.getenv("AXX_BIN"));
        Path project = Path.of(System.getenv("AXX_HOST_PROJECT")).toRealPath();
        ExecutorService threads = Executors.newCachedThreadPool();
        try {
            Heard heard = new Heard();
            HostProcess host = start(axx, project, heard, threads);

            HostEvent serving =
                    heard.until("serving", event -> HostEvent.SERVING.equals(event.event()));
            assertTrue(serving.addr().startsWith("127.0.0.1:"), serving.addr());
            assertEquals(project, Path.of(serving.dir()).toRealPath());
            assertFalse(serving.services().isEmpty());
            assertTrue(Files.exists(project.resolve(".axx/host.json")));

            // A second host in the project does not start: the IDE says one runs in a terminal.
            Heard second = new Heard();
            HostProcess other = start(axx, project, second, threads);
            String exit = second.exits.poll(60, TimeUnit.SECONDS);
            assertNotNull(exit, "the second host did not exit");
            other.stop();
            int code = Integer.parseInt(exit.substring(0, exit.indexOf(':')));
            assertEquals(HostState.Exit.ELSEWHERE, HostState.exitOf(code, exit, false), exit);

            // An agent's run: no AXX_IDE.
            ProcessBuilder run =
                    new ProcessBuilder(axx.toString(), "run", "--compact")
                            .directory(project.toFile())
                            .redirectErrorStream(true);
            run.environment().remove("AXX_IDE");
            Process agent = run.start();
            threads.execute(
                    () -> {
                        try {
                            agent.getInputStream().transferTo(OutputStream.nullOutputStream());
                        } catch (IOException e) {
                            // It ended.
                        }
                    });

            HostEvent queued =
                    heard.until(
                            "the run",
                            event ->
                                    HostEvent.RUN.equals(event.event())
                                            && HostEvent.QUEUED.equals(event.state()));
            assertEquals("axx run --compact", queued.command());
            assertEquals("", queued.ide());
            assertEquals(project, Path.of(queued.dir()).toRealPath());

            HostEvent ended = null;
            int reports = 0;
            String location = null;
            while (ended == null) {
                HostEvent event = heard.next("the run's end");
                switch (event.event()) {
                    case HostEvent.REPORT -> {
                        reports++;
                        if (location == null && event.line().contains("locationHint='file://")) {
                            location = event.line();
                        }
                    }
                    case HostEvent.RUN -> {
                        if (HostEvent.ENDED.equals(event.state())) {
                            ended = event;
                        }
                    }
                    default -> {}
                }
            }
            assertTrue(agent.waitFor(TIMEOUT_SECONDS, TimeUnit.SECONDS));
            assertTrue(
                    !serving.changed() || heard.approved.get() > 0,
                    "the changed definition was not asked about");
            assertNull(ended.error(), String.valueOf(ended.error()));
            assertNotNull(ended.result());
            assertTrue(ended.result().passed() > 0, ended.result().toString());
            assertEquals(0, HostState.exitCode(ended));
            assertEquals(0, agent.exitValue());
            assertTrue(reports > 0);
            // Its features are in the project, where the IDE finds them.
            assertNotNull(location);
            String url = "file://" + ReportLocations.escape(ReportLocations.slashed(serving.dir()));
            assertTrue(
                    ReportLocations.relocate(location, queued.dir(), serving.dir())
                            .contains("locationHint='" + url + "/"),
                    location);

            // The IDE goes away: the host stops and cleans up.
            host.stop();
            assertTrue(host.waitFor(60, TimeUnit.SECONDS), "the host did not stop");
            String stopped = heard.exits.poll(10, TimeUnit.SECONDS);
            assertNotNull(stopped);
            assertTrue(stopped.startsWith("0: "), stopped);
            assertFalse(Files.exists(project.resolve(".axx/host.json")));
        } finally {
            threads.shutdownNow();
        }
    }

    private static HostProcess start(Path axx, Path project, Heard heard, ExecutorService threads)
            throws IOException {
        Process process =
                new ProcessBuilder(axx.toString(), "host", "--ide")
                        .directory(project.toFile())
                        .start();
        HostProcess host = new HostProcess(process, heard);
        heard.host = host;
        host.start(threads);
        return host;
    }
}
