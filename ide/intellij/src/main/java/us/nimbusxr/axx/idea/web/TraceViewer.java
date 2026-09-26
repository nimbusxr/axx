// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Playwright's trace viewer, which opens a trace by its URL: a static web app, from the Playwright
 * driver axx downloads, whose files the plugin serves itself (see {@link AxxFileServer}).
 *
 * <p>It holds no IDE types, so its rules are unit-testable.
 */
public final class TraceViewer {
    /** Where to open a trace when there is no trace viewer: drop the file on the page. */
    public static final String SITE = "https://trace.playwright.dev";

    private TraceViewer() {}

    /** The page of the trace viewer's files, served at a URL, that loads the trace at a URL. */
    public static @NotNull String page(@NotNull String files, @NotNull String traceUrl) {
        String base = files.endsWith("/") ? files : files + "/";
        return base + "index.html?trace=" + URLEncoder.encode(traceUrl, StandardCharsets.UTF_8);
    }

    /** The first of the trace viewer's folders that still has the viewer, or null. */
    public static @Nullable Path folder(@Nullable Path... dirs) {
        for (Path dir : dirs) {
            if (dir != null && Files.isRegularFile(dir.resolve("index.html"))) {
                return dir;
            }
        }
        return null;
    }
}
