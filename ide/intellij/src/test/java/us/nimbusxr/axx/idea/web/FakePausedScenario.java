// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

/**
 * A paused scenario's HTTP server, as the web pack runs it, on 127.0.0.1: it answers what a test
 * sets, below {@link #url}, and keeps the requests it got.
 */
final class FakePausedScenario implements AutoCloseable {
    /** A request it got: its method, its path below the URL, and its body. */
    record Request(String method, String path, String body) {}

    private record Answer(int status, String text, long delayMillis) {}

    final BlockingQueue<Request> requests = new LinkedBlockingQueue<>();
    final String url;
    private final Map<String, Answer> answers = new ConcurrentHashMap<>();
    private final HttpServer server;
    private final ExecutorService executor = Executors.newCachedThreadPool();
    private boolean closed;

    FakePausedScenario() throws IOException {
        server = HttpServer.create(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 0);
        server.createContext("/", this::handle);
        server.setExecutor(executor);
        server.start();
        url = "http://127.0.0.1:" + server.getAddress().getPort() + "/3f9c0a/";
    }

    /** Answers a method and path below the URL, such as {@code POST highlight}. */
    FakePausedScenario answer(String method, String path, int status, String text) {
        return answer(method, path, status, text, 0);
    }

    /** Answers a method and path below the URL after a delay. */
    FakePausedScenario answer(String method, String path, int status, String text, long delayMillis) {
        answers.put(method + " " + path, new Answer(status, text, delayMillis));
        return this;
    }

    /** The next request it gets, waiting up to 10 seconds; null when none comes. */
    Request next() throws InterruptedException {
        return requests.poll(10, TimeUnit.SECONDS);
    }

    private void handle(HttpExchange exchange) throws IOException {
        String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
        String path = exchange.getRequestURI().getRawPath();
        String prefix = url.substring(url.indexOf('/', "http://".length()));
        String below = path.startsWith(prefix) ? path.substring(prefix.length()) : path;
        requests.add(new Request(exchange.getRequestMethod(), below, body));
        Answer answer =
                path.startsWith(prefix)
                        ? answers.getOrDefault(
                                exchange.getRequestMethod() + " " + below,
                                new Answer(404, "404 page not found", 0))
                        : new Answer(404, "404 page not found", 0);
        if (answer.delayMillis() > 0) {
            try {
                Thread.sleep(answer.delayMillis());
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
        byte[] bytes = answer.text().getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", "text/plain; charset=utf-8");
        exchange.sendResponseHeaders(answer.status(), bytes.length == 0 ? -1 : bytes.length);
        try (OutputStream out = exchange.getResponseBody()) {
            out.write(bytes);
        }
    }

    @Override
    public synchronized void close() {
        if (!closed) {
            closed = true;
            server.stop(0);
            executor.shutdownNow();
        }
    }
}
