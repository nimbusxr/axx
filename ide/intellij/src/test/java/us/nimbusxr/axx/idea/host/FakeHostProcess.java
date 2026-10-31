// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.host;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.io.PipedInputStream;
import java.io.PipedOutputStream;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * {@code axx host --ide} as a test plays it: what it writes on its standard output and error is
 * what the test writes, and the test reads what the IDE writes to its standard input. It ends when
 * the test ends it.
 */
final class FakeHostProcess extends Process {
    private final PipedOutputStream stdout = new PipedOutputStream();
    private final PipedOutputStream stderr = new PipedOutputStream();
    private final PipedInputStream stdoutIn;
    private final PipedInputStream stderrIn;
    private final PipedOutputStream stdinOut = new PipedOutputStream();
    private final BufferedReader stdin;
    private final CountDownLatch ended = new CountDownLatch(1);
    private volatile int exitCode;
    private volatile boolean killed;

    FakeHostProcess() throws IOException {
        stdoutIn = new PipedInputStream(stdout, 1 << 20);
        stderrIn = new PipedInputStream(stderr, 1 << 16);
        stdin =
                new BufferedReader(
                        new InputStreamReader(
                                new PipedInputStream(stdinOut, 1 << 16), StandardCharsets.UTF_8));
    }

    /** Writes a line on its standard output. */
    void out(String line) {
        write(stdout, line);
    }

    /** Writes a line on its standard error. */
    void err(String line) {
        write(stderr, line);
    }

    private static void write(OutputStream stream, String line) {
        try {
            stream.write((line + "\n").getBytes(StandardCharsets.UTF_8));
            stream.flush();
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    /** The next line the IDE wrote to its input; null once the IDE closed it. */
    String readInput() throws IOException {
        return stdin.readLine();
    }

    /** Ends it: it closes its output and exits. */
    void exit(int code) {
        exitCode = code;
        try {
            stdout.close();
            stderr.close();
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
        ended.countDown();
    }

    boolean wasKilled() {
        return killed;
    }

    @Override
    public OutputStream getOutputStream() {
        return stdinOut;
    }

    @Override
    public InputStream getInputStream() {
        return stdoutIn;
    }

    @Override
    public InputStream getErrorStream() {
        return stderrIn;
    }

    @Override
    public int waitFor() throws InterruptedException {
        ended.await();
        return exitCode;
    }

    @Override
    public boolean waitFor(long timeout, TimeUnit unit) throws InterruptedException {
        return ended.await(timeout, unit);
    }

    @Override
    public int exitValue() {
        if (ended.getCount() > 0) {
            throw new IllegalThreadStateException("running");
        }
        return exitCode;
    }

    @Override
    public boolean isAlive() {
        return ended.getCount() > 0;
    }

    @Override
    public void destroy() {
        killed = true;
        exit(137);
    }
}
