// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import com.intellij.openapi.Disposable;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.components.Service;
import com.sun.net.httpserver.Headers;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.SecureRandom;
import java.util.HexFormat;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Serves the traces and videos that axx runs announce, and the trace viewer's files, over HTTP on
 * 127.0.0.1: Playwright's trace viewer loads a trace only from a URL. It serves only the files and
 * folders it is given, each at a path with a random token, a folder only the files under it, to
 * any origin, and nothing else. It starts on first use, on a random port, and stops with the IDE.
 *
 * <p>It holds no IDE state, so its rules are unit-testable.
 */
@Service(Service.Level.APP)
public final class AxxFileServer implements Disposable {
    private static final Map<String, String> TYPES =
            Map.ofEntries(
                    Map.entry(".zip", "application/zip"),
                    Map.entry(".webm", "video/webm"),
                    Map.entry(".mp4", "video/mp4"),
                    Map.entry(".html", "text/html; charset=utf-8"),
                    Map.entry(".js", "text/javascript; charset=utf-8"),
                    Map.entry(".mjs", "text/javascript; charset=utf-8"),
                    Map.entry(".css", "text/css; charset=utf-8"),
                    Map.entry(".json", "application/json"),
                    Map.entry(".map", "application/json"),
                    Map.entry(".webmanifest", "application/manifest+json"),
                    Map.entry(".svg", "image/svg+xml"),
                    Map.entry(".png", "image/png"),
                    Map.entry(".ico", "image/x-icon"),
                    Map.entry(".ttf", "font/ttf"),
                    Map.entry(".woff", "font/woff"),
                    Map.entry(".woff2", "font/woff2"));
    private static final Pattern RANGE = Pattern.compile("^bytes=(\\d*)-(\\d*)$");
    private static final Pattern IN_FOLDER = Pattern.compile("^(/[0-9a-f]{32}/[^/]+/)(.*)$");

    private final SecureRandom random = new SecureRandom();
    private final Map<String, Path> files = new ConcurrentHashMap<>(); // URL path -> file
    private final Map<Path, String> paths = new ConcurrentHashMap<>(); // file -> URL path
    private final Map<String, Path> folders = new ConcurrentHashMap<>(); // URL prefix -> folder
    private final Map<Path, String> prefixes = new ConcurrentHashMap<>(); // folder -> URL prefix
    private @Nullable HttpServer server;
    private @Nullable ExecutorService executor;

    public static @NotNull AxxFileServer getInstance() {
        return ApplicationManager.getApplication().getService(AxxFileServer.class);
    }

    /**
     * The URL a file is served at, starting the server on first use. The same file keeps its URL,
     * which has no characters that need escaping, so it can go in a query as it is.
     */
    public synchronized @NotNull String url(@NotNull Path file) throws IOException {
        Path key = file.toAbsolutePath().normalize();
        String urlPath = paths.get(key);
        if (urlPath == null) {
            urlPath = tokenPath(key);
            paths.put(key, urlPath);
            files.put(urlPath, key);
        }
        return "http://127.0.0.1:" + start().getAddress().getPort() + urlPath;
    }

    /**
     * The URL the files under a folder are served below, ending with {@code /}, starting the
     * server on first use. The same folder keeps its URL.
     */
    public synchronized @NotNull String folderUrl(@NotNull Path dir) throws IOException {
        Path key = dir.toAbsolutePath().normalize();
        String prefix = prefixes.get(key);
        if (prefix == null) {
            prefix = tokenPath(key) + "/";
            prefixes.put(key, prefix);
            folders.put(prefix, key);
        }
        return "http://127.0.0.1:" + start().getAddress().getPort() + prefix;
    }

    /** A new path for a file or folder: a random token, then its name. */
    private String tokenPath(Path path) {
        byte[] token = new byte[16];
        random.nextBytes(token);
        String name = path.getFileName().toString().replaceAll("[^\\w.-]", "_");
        return "/" + HexFormat.of().formatHex(token) + "/" + name;
    }

    @Override
    public synchronized void dispose() {
        if (server != null) {
            server.stop(0);
            server = null;
        }
        if (executor != null) {
            executor.shutdownNow();
            executor = null;
        }
    }

    private HttpServer start() throws IOException {
        if (server == null) {
            HttpServer created =
                    HttpServer.create(
                            new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 0);
            executor =
                    Executors.newFixedThreadPool(
                            4,
                            runnable -> {
                                Thread thread = new Thread(runnable, "axx file server");
                                thread.setDaemon(true);
                                return thread;
                            });
            created.setExecutor(executor);
            created.createContext("/", this::serve);
            created.start();
            server = created;
        }
        return server;
    }

    private void serve(HttpExchange exchange) throws IOException {
        try (exchange) {
            String rawPath = exchange.getRequestURI().getRawPath();
            Path file = files.get(rawPath);
            if (file == null) {
                file = inFolder(rawPath);
            }
            long size = file == null ? -1 : sizeOf(file);
            if (size < 0) {
                exchange.sendResponseHeaders(404, -1);
                return;
            }
            Headers headers = exchange.getResponseHeaders();
            headers.set("Access-Control-Allow-Origin", "*");
            String method = exchange.getRequestMethod();
            if (method.equals("OPTIONS")) {
                headers.set("Access-Control-Allow-Methods", "GET, HEAD");
                headers.set("Access-Control-Allow-Headers", "Range");
                exchange.sendResponseHeaders(204, -1);
                return;
            }
            if (!method.equals("GET") && !method.equals("HEAD")) {
                headers.set("Allow", "GET, HEAD, OPTIONS");
                exchange.sendResponseHeaders(405, -1);
                return;
            }
            ByteRange range = ByteRange.of(exchange.getRequestHeaders().getFirst("Range"), size);
            if (range == null) {
                headers.set("Content-Range", "bytes */" + size);
                exchange.sendResponseHeaders(416, -1);
                return;
            }
            headers.set("Content-Type", typeOf(file));
            headers.set("X-Content-Type-Options", "nosniff");
            headers.set("Accept-Ranges", "bytes");
            headers.set("Cache-Control", "no-store");
            if (range.partial()) {
                headers.set(
                        "Content-Range", "bytes " + range.start() + "-" + range.end() + "/" + size);
            }
            int status = range.partial() ? 206 : 200;
            if (method.equals("HEAD") || range.length() == 0) {
                headers.set("Content-Length", String.valueOf(range.length()));
                exchange.sendResponseHeaders(status, -1);
                return;
            }
            exchange.sendResponseHeaders(status, range.length());
            try (InputStream in = Files.newInputStream(file);
                    OutputStream out = exchange.getResponseBody()) {
                in.skipNBytes(range.start());
                byte[] buffer = new byte[64 * 1024];
                long left = range.length();
                while (left > 0) {
                    int read = in.read(buffer, 0, (int) Math.min(buffer.length, left));
                    if (read < 0) {
                        break;
                    }
                    out.write(buffer, 0, read);
                    left -= read;
                }
            }
        }
    }

    /** The file under a served folder a URL path names, following no link out of the folder. */
    private @Nullable Path inFolder(String rawPath) {
        Matcher m = IN_FOLDER.matcher(rawPath);
        Path dir = m.matches() ? folders.get(m.group(1)) : null;
        return dir == null ? null : fileIn(dir, m.group(2));
    }

    /**
     * The file a percent-encoded path names under a folder, or null when there is none there: the
     * path leads out of the folder, or to a link out of it.
     */
    static @Nullable Path fileIn(@NotNull Path dir, @NotNull String rawPath) {
        try {
            String name = URLDecoder.decode(rawPath.replace("+", "%2B"), StandardCharsets.UTF_8);
            if (name.isEmpty() || name.indexOf('\0') >= 0) {
                return null;
            }
            Path root = dir.toRealPath();
            Path file = root.resolve(name).normalize().toRealPath();
            return file.startsWith(root) && !file.equals(root) ? file : null;
        } catch (IllegalArgumentException | IOException e) {
            return null; // malformed, or no such file
        }
    }

    /** The content type of a file, by its extension. */
    static @NotNull String typeOf(@NotNull Path file) {
        String name = file.getFileName().toString().toLowerCase(Locale.ROOT);
        int dot = name.lastIndexOf('.');
        String type = dot < 0 ? null : TYPES.get(name.substring(dot));
        return type != null ? type : "application/octet-stream";
    }

    private static long sizeOf(Path file) {
        try {
            return Files.isRegularFile(file) ? Files.size(file) : -1;
        } catch (IOException e) {
            return -1; // gone since it was announced
        }
    }

    /**
     * The bytes of a file a response carries.
     *
     * @param start the first byte
     * @param end the last byte (start - 1 for an empty file)
     * @param partial whether it is the range a request asked for, rather than the whole file
     */
    record ByteRange(long start, long end, boolean partial) {
        long length() {
            return end - start + 1;
        }

        /**
         * The bytes a {@code Range} header asks for: the whole file without one, or with one this
         * server answers with the whole file (several ranges, another unit, a malformed one).
         *
         * @return the bytes, or null when none of the file is in the range
         */
        static @Nullable ByteRange of(@Nullable String header, long size) {
            ByteRange all = new ByteRange(0, size - 1, false);
            Matcher m = header == null ? null : RANGE.matcher(header.strip());
            if (m == null || !m.matches() || (m.group(1).isEmpty() && m.group(2).isEmpty())) {
                return all;
            }
            try {
                if (m.group(1).isEmpty()) {
                    long suffix = Long.parseLong(m.group(2));
                    return suffix > 0 && size > 0
                            ? new ByteRange(Math.max(0, size - suffix), size - 1, true)
                            : null;
                }
                long start = Long.parseLong(m.group(1));
                long last = m.group(2).isEmpty() ? Long.MAX_VALUE : Long.parseLong(m.group(2));
                if (last < start) {
                    return all;
                }
                return start < size ? new ByteRange(start, Math.min(last, size - 1), true) : null;
            } catch (NumberFormatException e) {
                return all;
            }
        }
    }
}
