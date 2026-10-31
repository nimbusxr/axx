// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.jetbrains.annotations.NotNull;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

/** The IDE's side of {@code axx host --ide}, against a fake host process. */
@DisplayName("HostProcess")
class HostProcessTest {
    private final ExecutorService threads = Executors.newCachedThreadPool();
    private final Heard heard = new Heard();

    /** What the IDE heard from the host. */
    private static final class Heard implements HostProcess.Listener {
        final BlockingQueue<HostEvent> events = new LinkedBlockingQueue<>();
        final List<String> log = new CopyOnWriteArrayList<>();
        final BlockingQueue<String> exits = new LinkedBlockingQueue<>();

        @Override
        public void event(@NotNull HostEvent event) {
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

        HostEvent next() throws InterruptedException {
            return events.poll(10, TimeUnit.SECONDS);
        }

        String exit() throws InterruptedException {
            return exits.poll(10, TimeUnit.SECONDS);
        }
    }

    @AfterEach
    void tearDown() {
        threads.shutdownNow();
    }

    @Test
    @DisplayName("hears the host's events in order, and its log")
    void readsEvents() throws Exception {
        FakeHostProcess fake = new FakeHostProcess();
        HostProcess host = new HostProcess(fake, heard);
        host.start(threads);

        fake.err("axx: preparing rest (once; cached for later runs)");
        fake.out(
                "{\"event\":\"serving\",\"addr\":\"127.0.0.1:51621\",\"dir\":\"/work/parcels\","
                        + "\"services\":[\"api\"],\"sources\":[\"compose.yaml\"]}");
        fake.out("{\"event\":\"run\",\"run\":1,\"command\":\"axx run\",\"state\":\"queued\"}");
        String big = "y".repeat(LONGER_THAN_A_CONSOLE_LINE);
        fake.out("{\"event\":\"report\",\"run\":1,\"line\":\"" + big + "\"}");
        fake.out("not an event");

        assertEquals(HostEvent.SERVING, heard.next().event());
        assertEquals(HostEvent.QUEUED, heard.next().state());
        assertEquals(big, heard.next().line());
        fake.exit(0);
        assertEquals("0: axx: preparing rest (once; cached for later runs)", heard.exit());
        assertEquals(
                List.of("axx: preparing rest (once; cached for later runs)", "not an event"),
                heard.log.stream().sorted().toList());
    }

    private static final int LONGER_THAN_A_CONSOLE_LINE = 100_000;

    @Test
    @DisplayName("writes the person's answers as lines of JSON")
    void answers() throws Exception {
        FakeHostProcess fake = new FakeHostProcess();
        HostProcess host = new HostProcess(fake, heard);
        host.start(threads);

        assertTrue(host.answer(3, true));
        assertTrue(host.answer(4, false));
        assertEquals("{\"approve\":3}", fake.readInput());
        assertEquals("{\"refuse\":4}", fake.readInput());
        fake.exit(0);
    }

    @Test
    @DisplayName("stops the host by closing its input, then answers nothing")
    void stops() throws Exception {
        FakeHostProcess fake = new FakeHostProcess();
        HostProcess host = new HostProcess(fake, heard);
        host.start(threads);

        host.stop();
        assertNull(fake.readInput(), "the host's input is closed");
        assertFalse(host.answer(1, true));
        assertTrue(host.isAlive());
        // The host cleans up, then exits.
        fake.exit(0);
        assertTrue(host.waitFor(10, TimeUnit.SECONDS));
        assertEquals("0: ", heard.exit());
        assertFalse(fake.wasKilled());
    }

    @Test
    @DisplayName("tells why the host did not start, from the end of its standard error")
    void failsToStart() throws Exception {
        FakeHostProcess fake = new FakeHostProcess();
        HostProcess host = new HostProcess(fake, heard);
        host.start(threads);

        for (int i = 0; i < HostProcess.KEPT_ERROR_LINES; i++) {
            fake.err("noise " + i);
        }
        fake.err(
                "error[AXX-E0503]: an axx host already serves /work/parcels (pid 18719,"
                        + " 127.0.0.1:51727)");
        fake.err("  hint: its runs are yours too: stop it first to start another");
        fake.exit(4);

        String exit = heard.exit();
        assertNotNull(exit);
        assertTrue(exit.startsWith("4: noise 2\n"), exit);
        assertTrue(
                exit.endsWith(
                        "\nerror[AXX-E0503]: an axx host already serves /work/parcels (pid 18719,"
                                + " 127.0.0.1:51727)\n"
                                + "  hint: its runs are yours too: stop it first to start another"),
                exit);
        assertEquals(HostState.Exit.ELSEWHERE, new HostState().exited(4, exit.substring(3)));
    }

    @Test
    @DisplayName("kills the host when asked")
    void kills() throws Exception {
        FakeHostProcess fake = new FakeHostProcess();
        HostProcess host = new HostProcess(fake, heard);
        host.start(threads);

        host.kill();
        assertTrue(fake.wasKilled());
        assertEquals("137: ", heard.exit());
    }

    @Test
    @DisplayName("answers as the host reads them")
    void answerLines() {
        assertEquals("{\"approve\":12}\n", HostProcess.answerLine(12, true));
        assertEquals("{\"refuse\":1}\n", HostProcess.answerLine(1, false));
    }
}
