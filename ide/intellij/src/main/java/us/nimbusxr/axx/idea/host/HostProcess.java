// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import org.jetbrains.annotations.NotNull;

import java.io.ByteArrayOutputStream;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

/**
 * Talks to a running {@code axx host --ide}: it reads the host's events from its standard output
 * (see {@link HostEvent}) and its log from its standard error, and writes the person's answers to
 * its standard input, {@code {"approve": <id>}} or {@code {"refuse": <id>}}, one per line. Only the
 * IDE holds that input: the host takes approvals from nowhere else.
 *
 * <p>Closing the input stops the host: it stops the services of a run in progress, cleans up,
 * removes {@code .axx/host.json} and exits. It does so on its own, as when the IDE quits: it ignores
 * an output nobody reads.
 *
 * <p>Its threads never block on the host: they look for its output and its end every {@link
 * #POLL_MILLIS}, so that they end soon after the IDE lets the host go ({@link #detach}), which a
 * thread blocked reading a pipe would not. A plugin unloads only once none of its code runs.
 *
 * <p>It holds no IDE types, so it is unit-testable with a fake process.
 */
public final class HostProcess {
    /** How many of the last lines of standard error {@link Listener#exited} gets. */
    static final int KEPT_ERROR_LINES = 50;

    /** How often the threads look for the host's output and its end. */
    static final long POLL_MILLIS = 20;

    /** How long the end waits for the threads to read what the host wrote before it ended. */
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

        /**
         * The host has not ended in the time {@link #stop} gave it: it is killed now, and {@link
         * #exited} follows.
         */
        default void killing() {}
    }

    private final Process process;
    private final Listener listener;
    private final OutputStream input;
    private final Deque<String> errors = new ArrayDeque<>();
    private final CountDownLatch read = new CountDownLatch(2);
    // Under this: the input is closed; a stop's deadline to kill the host by (System.nanoTime()).
    private boolean inputClosed;
    private boolean deadline;
    private long killAt;
    // The IDE let the host go: the threads end, and the listener hears nothing more.
    private volatile boolean detached;

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

    /**
     * Reads one of the host's outputs until the host ended and all it wrote is read, or the IDE let
     * it go. It reads only what is there, and waits between: the output's end does not come with
     * the host's when a process it started holds the output open.
     */
    private void read(InputStream stream, boolean events) {
        Lines lines = new Lines(line -> line(line, events));
        byte[] buffer = new byte[8192];
        try {
            while (!detached) {
                // Whether it ended, before looking at its output: what it wrote before it ended is
                // there by then.
                boolean ended = !process.isAlive();
                int available = stream.available();
                if (available > 0) {
                    int n = stream.read(buffer, 0, Math.min(available, buffer.length));
                    if (n < 0) {
                        break;
                    }
                    lines.add(buffer, n);
                } else if (ended) {
                    break;
                } else {
                    Thread.sleep(POLL_MILLIS);
                }
            }
            lines.end();
        } catch (IOException e) {
            // The stream closed: the IDE let the host go.
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        } finally {
            read.countDown();
        }
    }

    private void line(String line, boolean events) {
        if (detached) {
            return;
        }
        HostEvent event = events ? HostEvent.parse(line) : null;
        if (event != null) {
            listener.event(event);
            return;
        }
        if (!events) {
            keep(line);
        }
        if (!line.isBlank()) {
            listener.log(line);
        }
    }

    /** Splits bytes into lines of UTF-8 text without their line breaks. */
    private static final class Lines {
        private final Consumer<String> sink;
        private final ByteArrayOutputStream pending = new ByteArrayOutputStream();

        Lines(Consumer<String> sink) {
            this.sink = sink;
        }

        void add(byte[] bytes, int length) {
            int start = 0;
            for (int i = 0; i < length; i++) {
                if (bytes[i] == '\n') {
                    pending.write(bytes, start, i - start);
                    emit();
                    start = i + 1;
                }
            }
            pending.write(bytes, start, length - start);
        }

        /** The last line, which has no line break. */
        void end() {
            if (pending.size() > 0) {
                emit();
            }
        }

        private void emit() {
            byte[] bytes = pending.toByteArray();
            pending.reset();
            int length = bytes.length;
            if (length > 0 && bytes[length - 1] == '\r') {
                length--;
            }
            sink.accept(new String(bytes, 0, length, StandardCharsets.UTF_8));
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
            while (!process.waitFor(POLL_MILLIS, TimeUnit.MILLISECONDS)) {
                if (detached) {
                    return;
                }
                if (overdue()) {
                    listener.killing();
                    kill();
                }
            }
            exitCode = process.exitValue();
            read.await(DRAIN_SECONDS, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            exitCode = process.isAlive() ? -1 : process.exitValue();
        }
        if (detached) {
            return;
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

    /**
     * Asks the host to stop, by closing its input, and kills it, and the processes it started, if
     * it has not ended in the time given.
     */
    public synchronized void stop(long timeout, @NotNull TimeUnit unit) {
        closeInput();
        if (!deadline) {
            deadline = true;
            killAt = System.nanoTime() + unit.toNanos(timeout);
        }
    }

    /** Whether the time a stop gave the host is up: once. */
    private synchronized boolean overdue() {
        if (!deadline || System.nanoTime() - killAt < 0) {
            return false;
        }
        deadline = false;
        return true;
    }

    /**
     * Lets the host go, as the IDE does when the project closes or the plugin unloads: asks it to
     * stop, by closing its input, and stops reading its output and waiting for its end. The
     * listener hears nothing more, the threads end within {@link #POLL_MILLIS}, and nothing waits
     * to kill the host, so that no code of the plugin's runs on. The host stops its services, cleans
     * up and exits on its own; its output goes nowhere, so that it never waits for a reader.
     */
    public void detach() {
        detached = true;
        synchronized (this) {
            closeInput();
            deadline = false;
        }
        close(process.getInputStream());
        close(process.getErrorStream());
    }

    private void closeInput() {
        if (inputClosed) {
            return;
        }
        inputClosed = true;
        close(input);
    }

    private static void close(Closeable stream) {
        try {
            stream.close();
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
