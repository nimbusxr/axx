// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import org.jetbrains.annotations.NotNull;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;

/**
 * Talks to a running {@code axx host --ide}: it reads the host's events from its standard output
 * (see {@link HostEvent}) and its log from its standard error, and writes the person's answers to
 * its standard input, {@code {"approve": <id>}} or {@code {"refuse": <id>}}, one per line. Only the
 * IDE holds that input: the host takes approvals from nowhere else.
 *
 * <p>Closing the input stops the host: it stops the services of a run in progress, cleans up,
 * removes {@code .axx/host.json} and exits.
 *
 * <p>It holds no IDE types, so it is unit-testable with a fake process.
 */
public final class HostProcess {
    /** How many of the last lines of standard error {@link Listener#exited} gets. */
    static final int KEPT_ERROR_LINES = 50;

    /**
     * How long the end waits for the rest of the output once the process ended (a process the host
     * started may hold its output open).
     */
    private static final long DRAIN_SECONDS = 5;

    /** Hears from the host, on the threads that read its output. */
    public interface Listener {
        /** The host wrote an event. Events come in order, on one thread. */
        void event(@NotNull HostEvent event);

        /** The host wrote a line of its log, or a line of its output that is not an event. */
        void log(@NotNull String line);

        /**
         * The host's process ended, after its last event and log line (unless a process it
         * started holds its output open for more than a few seconds).
         *
         * @param exitCode its exit code
         * @param errors the last lines of its standard error, which say why it failed
         */
        void exited(int exitCode, @NotNull String errors);
    }

    private final Process process;
    private final Listener listener;
    private final OutputStream input;
    private final Deque<String> errors = new ArrayDeque<>();
    private final CountDownLatch read = new CountDownLatch(2);
    private boolean inputClosed;

    /**
     * @param process the started {@code axx host --ide}
     * @param listener hears its events, its log and its end
     */
    public HostProcess(@NotNull Process process, @NotNull Listener listener) {
        this.process = process;
        this.listener = listener;
        this.input = process.getOutputStream();
    }

    /**
     * Starts reading the host's output, and waiting for its end: three threads from {@code
     * executor}.
     */
    public void start(@NotNull Executor executor) {
        executor.execute(() -> read(process.getInputStream(), true));
        executor.execute(() -> read(process.getErrorStream(), false));
        executor.execute(this::waitForEnd);
    }

    private void read(InputStream stream, boolean events) {
        try (BufferedReader reader =
                new BufferedReader(new InputStreamReader(stream, StandardCharsets.UTF_8))) {
            for (String line = reader.readLine(); line != null; line = reader.readLine()) {
                HostEvent event = events ? HostEvent.parse(line) : null;
                if (event != null) {
                    listener.event(event);
                    continue;
                }
                if (!events) {
                    keep(line);
                }
                if (!line.isBlank()) {
                    listener.log(line);
                }
            }
        } catch (IOException e) {
            // The stream closed: the process ended or was killed.
        } finally {
            read.countDown();
        }
    }

    private synchronized void keep(String line) {
        errors.addLast(line);
        if (errors.size() > KEPT_ERROR_LINES) {
            errors.removeFirst();
        }
    }

    private void waitForEnd() {
        int exitCode;
        try {
            exitCode = process.waitFor();
            read.await(DRAIN_SECONDS, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            exitCode = process.isAlive() ? -1 : process.exitValue();
        }
        String text;
        synchronized (this) {
            text = String.join("\n", errors);
        }
        listener.exited(exitCode, text);
    }

    /**
     * Writes the person's answer to a question of the host.
     *
     * @param id the question's id, from its {@code approval} event
     * @param approve true to approve, false to refuse
     * @return whether it was written; false once the input is closed
     */
    public synchronized boolean answer(int id, boolean approve) {
        if (inputClosed) {
            return false;
        }
        try {
            input.write(answerLine(id, approve).getBytes(StandardCharsets.UTF_8));
            input.flush();
            return true;
        } catch (IOException e) {
            return false;
        }
    }

    /** The line that answers a question: {@code {"approve":<id>}} or {@code {"refuse":<id>}}. */
    static @NotNull String answerLine(int id, boolean approve) {
        return "{\"" + (approve ? "approve" : "refuse") + "\":" + id + "}\n";
    }

    /** Asks the host to stop, by closing its input. */
    public synchronized void stop() {
        if (inputClosed) {
            return;
        }
        inputClosed = true;
        try {
            input.close();
        } catch (IOException e) {
            // It is gone already.
        }
    }

    /** Whether the host's process still runs. */
    public boolean isAlive() {
        return process.isAlive();
    }

    /** Waits for the host's process to end; true when it did. */
    public boolean waitFor(long timeout, @NotNull TimeUnit unit) throws InterruptedException {
        return process.waitFor(timeout, unit);
    }

    /**
     * Kills the host's process and the processes it started (its services' commands, which run in
     * process groups of their own), taken before it goes: theirs leave its tree once it does.
     */
    public void kill() {
        List<ProcessHandle> started;
        try {
            started = process.descendants().toList();
        } catch (UnsupportedOperationException e) {
            started = List.of(); // a Process that is not the system's has no processes it started
        }
        process.destroyForcibly();
        started.forEach(ProcessHandle::destroyForcibly);
    }
}
