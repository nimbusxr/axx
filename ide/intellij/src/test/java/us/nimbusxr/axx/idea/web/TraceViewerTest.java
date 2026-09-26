// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.net.URI;
import java.nio.file.Files;
import java.nio.file.Path;

@DisplayName("TraceViewer")
class TraceViewerTest {

    @Test
    @DisplayName("opens a trace in the trace viewer's files, served with it")
    void page() {
        String files = "http://127.0.0.1:51234/9a8b7c6d/traceViewer/";
        String trace = "http://127.0.0.1:51234/0f1e2d3c/register.zip";
        String expected =
                files + "index.html?trace=http%3A%2F%2F127.0.0.1%3A51234%2F0f1e2d3c%2Fregister.zip";
        assertEquals(expected, TraceViewer.page(files, trace));
        assertEquals(expected, TraceViewer.page(files.substring(0, files.length() - 1), trace));
        assertEquals("trace=" + trace, URI.create(expected).getQuery());
    }

    @Test
    @DisplayName("uses the first trace viewer's folder that still has it")
    void folder(@TempDir Path dir) throws Exception {
        Path current = Files.createDirectories(dir.resolve("new/traceViewer"));
        Files.writeString(current.resolve("index.html"), "<html>");
        Path gone = dir.resolve("old/traceViewer");
        assertEquals(current, TraceViewer.folder(gone, current));
        assertEquals(current, TraceViewer.folder(null, current));
        assertEquals(current, TraceViewer.folder(current, null));
        assertNull(TraceViewer.folder(gone, null));
        assertNull(TraceViewer.folder());
    }
}
