// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import us.nimbusxr.axx.idea.web.AxxFileServer.ByteRange;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.Socket;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

@DisplayName("AxxFileServer")
class AxxFileServerTest {
    @TempDir Path dir;

    private final AxxFileServer server = new AxxFileServer();
    private final HttpClient client =
            HttpClient.newBuilder()
                    .version(HttpClient.Version.HTTP_1_1)
                    .proxy(HttpClient.Builder.NO_PROXY)
                    .build();
    private Path trace;
    private Path video;
    private Path viewer;

    @BeforeEach
    void files() throws IOException {
        Files.createDirectories(dir.resolve("traces"));
        trace = Files.writeString(dir.resolve("traces/register parcel.zip"), "PK trace bytes");
        video = Files.writeString(dir.resolve("parcel.webm"), "0123456789");
        Files.writeString(dir.resolve("traces/notes.txt"), "not announced");
        // The trace viewer's files, as the Playwright driver has them.
        viewer =
                Files.createDirectories(dir.resolve("package/lib/vite/traceViewer/assets"))
                        .getParent();
        for (String name :
                List.of(
                        "index.html",
                        "snapshot.html",
                        "sw.bundle.js",
                        "manifest.webmanifest",
                        "playwright-logo.svg",
                        "assets/index-Bq3f.css",
                        "assets/codicon.ttf",
                        "assets/data.json")) {
            Files.writeString(viewer.resolve(name), name);
        }
        // A link out of the folder.
        Files.createSymbolicLink(
                viewer.resolve("assets/link.txt"), dir.resolve("traces/notes.txt"));
    }

    @AfterEach
    void stop() {
        server.dispose();
    }

    @Test
    @DisplayName("serves an announced file on 127.0.0.1, to any origin")
    void servesAnnouncedFile() throws Exception {
        String url = server.url(trace);
        assertTrue(
                url.matches("http://127\\.0\\.0\\.1:\\d+/[0-9a-f]{32}/register_parcel\\.zip"), url);
        HttpResponse<String> response =
                send(
                        HttpRequest.newBuilder(URI.create(url))
                                .header("Origin", "https://trace.playwright.dev"));
        assertEquals(200, response.statusCode());
        assertEquals(
                "*", response.headers().firstValue("Access-Control-Allow-Origin").orElse(null));
        assertEquals("application/zip", response.headers().firstValue("Content-Type").orElse(null));
        assertEquals("PK trace bytes", response.body());
        assertEquals(url, server.url(trace), "a file keeps its URL");
    }

    @Test
    @DisplayName("serves nothing else")
    void servesNothingElse() throws Exception {
        URI url = URI.create(server.url(trace));
        String base = "http://" + url.getAuthority();
        String token = url.getPath().split("/")[1];
        for (String path :
                List.of(
                        "/",
                        "/" + token + "/",
                        "/" + token + "/notes.txt",
                        "/" + token + "/register_parcel.zip/..",
                        "/0f1e2d3c4b5a69788796a5b4c3d2e1f0/register_parcel.zip",
                        dir.resolve("traces/notes.txt").toString(),
                        "/" + token + "/%2e%2e/notes.txt")) {
            HttpResponse<String> response = send(HttpRequest.newBuilder(URI.create(base + path)));
            assertEquals(404, response.statusCode(), path);
            assertTrue(
                    response.headers().firstValue("Access-Control-Allow-Origin").isEmpty(), path);
        }
        HttpResponse<String> post =
                send(HttpRequest.newBuilder(url).POST(HttpRequest.BodyPublishers.ofString("x")));
        assertEquals(405, post.statusCode());
    }

    @Test
    @DisplayName("answers CORS preflights and HEAD")
    void preflightAndHead() throws Exception {
        URI url = URI.create(server.url(trace));
        HttpResponse<String> preflight =
                send(
                        HttpRequest.newBuilder(url)
                                .method("OPTIONS", HttpRequest.BodyPublishers.noBody())
                                .header("Origin", "https://trace.playwright.dev")
                                .header("Access-Control-Request-Method", "GET"));
        assertEquals(204, preflight.statusCode());
        assertEquals(
                "*", preflight.headers().firstValue("Access-Control-Allow-Origin").orElse(null));

        HttpResponse<String> head =
                send(
                        HttpRequest.newBuilder(url)
                                .method("HEAD", HttpRequest.BodyPublishers.noBody()));
        assertEquals(200, head.statusCode());
        assertEquals(
                String.valueOf("PK trace bytes".length()),
                head.headers().firstValue("Content-Length").orElse(null));
    }

    @Test
    @DisplayName("serves byte ranges, for seeking in videos")
    void byteRanges() throws Exception {
        URI url = URI.create(server.url(video));
        HttpResponse<String> part = send(HttpRequest.newBuilder(url).header("Range", "bytes=2-5"));
        assertEquals(206, part.statusCode());
        assertEquals("bytes 2-5/10", part.headers().firstValue("Content-Range").orElse(null));
        assertEquals("video/webm", part.headers().firstValue("Content-Type").orElse(null));
        assertEquals("2345", part.body());

        HttpResponse<String> outside =
                send(HttpRequest.newBuilder(url).header("Range", "bytes=10-"));
        assertEquals(416, outside.statusCode());
        assertEquals("bytes */10", outside.headers().firstValue("Content-Range").orElse(null));
    }

    @Test
    @DisplayName("a file deleted after it was announced is gone")
    void deletedFile() throws Exception {
        Path gone = Files.writeString(dir.resolve("gone.webm"), "x");
        URI url = URI.create(server.url(gone));
        Files.delete(gone);
        assertEquals(404, send(HttpRequest.newBuilder(url)).statusCode());
    }

    @Test
    @DisplayName("serves the trace viewer's files, with their content types")
    void servesFolder() throws Exception {
        String base = server.folderUrl(viewer);
        assertTrue(base.matches("http://127\\.0\\.0\\.1:\\d+/[0-9a-f]{32}/traceViewer/"), base);
        assertEquals(base, server.folderUrl(viewer), "a folder keeps its URL");
        Map<String, String> types =
                Map.of(
                        "index.html", "text/html; charset=utf-8",
                        "snapshot.html", "text/html; charset=utf-8",
                        // A service worker needs a JavaScript type.
                        "sw.bundle.js", "text/javascript; charset=utf-8",
                        "manifest.webmanifest", "application/manifest+json",
                        "playwright-logo.svg", "image/svg+xml",
                        "assets/index-Bq3f.css", "text/css; charset=utf-8",
                        "assets/codicon.ttf", "font/ttf",
                        "assets/data.json", "application/json");
        for (Map.Entry<String, String> type : types.entrySet()) {
            HttpResponse<String> response =
                    send(HttpRequest.newBuilder(URI.create(base + type.getKey() + "?trace=x")));
            assertEquals(200, response.statusCode(), type.getKey());
            assertEquals(
                    type.getValue(),
                    response.headers().firstValue("Content-Type").orElse(null),
                    type.getKey());
            assertEquals(type.getKey(), response.body());
        }
        assertEquals("font/woff2", AxxFileServer.typeOf(Path.of("a.woff2")));
        assertEquals("application/octet-stream", AxxFileServer.typeOf(Path.of("a.unknown")));
    }

    @Test
    @DisplayName("serves nothing outside the folder")
    void servesNothingOutsideTheFolder() throws Exception {
        URI base = URI.create(server.folderUrl(viewer));
        String prefix = base.getRawPath(); // /<token>/traceViewer/
        String token = prefix.split("/")[1];
        String notes = dir.resolve("traces/notes.txt").toString();
        for (String path :
                List.of(
                        prefix,
                        prefix + "assets",
                        prefix + "missing.js",
                        prefix + "../../../../traces/notes.txt",
                        prefix + "%2e%2e/%2e%2e/%2e%2e/%2e%2e/traces/notes.txt",
                        prefix + "..%2f..%2f..%2f..%2ftraces%2fnotes.txt",
                        prefix + "assets/..%5c..%5c..%5c..%5c..%5ctraces%5cnotes.txt",
                        prefix + notes.replace("/", "%2f"),
                        prefix + "/" + notes,
                        prefix + "assets/link.txt",
                        prefix + "index.html%00.js",
                        "/" + token + "/other/index.html",
                        "/" + token + "/index.html",
                        "/0f1e2d3c4b5a69788796a5b4c3d2e1f0/traceViewer/index.html")) {
            assertEquals(404, raw(base, path), path);
        }
        // A malformed escape: the server refuses the request.
        int malformed = raw(base, prefix + "%E0%A4%A");
        assertTrue(malformed == 400 || malformed == 404, String.valueOf(malformed));
        // Dot segments that stay in the folder are fine.
        assertEquals(200, raw(base, prefix + "assets/../index.html"));
    }

    @Test
    @DisplayName("reads Range headers")
    void readsRanges() {
        assertEquals(new ByteRange(0, 9, false), ByteRange.of(null, 10));
        assertEquals(new ByteRange(0, 9, true), ByteRange.of("bytes=0-", 10));
        assertEquals(new ByteRange(3, 9, true), ByteRange.of("bytes=3-100", 10));
        assertEquals(new ByteRange(6, 9, true), ByteRange.of("bytes=-4", 10));
        assertEquals(new ByteRange(0, 9, true), ByteRange.of("bytes=-40", 10));
        // Several ranges, another unit, a malformed range: the whole file.
        assertEquals(new ByteRange(0, 9, false), ByteRange.of("bytes=0-1,4-5", 10));
        assertEquals(new ByteRange(0, 9, false), ByteRange.of("items=0-1", 10));
        assertEquals(new ByteRange(0, 9, false), ByteRange.of("bytes=5-2", 10));
        assertNull(ByteRange.of("bytes=10-", 10));
        assertNull(ByteRange.of("bytes=-0", 10));
        assertNull(ByteRange.of("bytes=0-", 0));
    }

    private HttpResponse<String> send(HttpRequest.Builder request) throws Exception {
        return client.send(request.build(), HttpResponse.BodyHandlers.ofString());
    }

    /** The status of a GET of a path exactly as written: HTTP clients resolve dot segments. */
    private static int raw(URI server, String path) throws IOException {
        try (Socket socket = new Socket(server.getHost(), server.getPort());
                BufferedReader in =
                        new BufferedReader(
                                new InputStreamReader(
                                        socket.getInputStream(), StandardCharsets.US_ASCII))) {
            OutputStream out = socket.getOutputStream();
            out.write(
                    ("GET " + path + " HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
                            .getBytes(StandardCharsets.US_ASCII));
            out.flush();
            String status = in.readLine();
            String[] parts = status == null ? new String[0] : status.split(" ");
            try {
                return Integer.parseInt(parts.length > 1 ? parts[1] : "");
            } catch (NumberFormatException e) {
                throw new IOException("not an HTTP status line: " + status, e);
            }
        }
    }
}
