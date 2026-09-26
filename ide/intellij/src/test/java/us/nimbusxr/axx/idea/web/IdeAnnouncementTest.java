// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import us.nimbusxr.axx.idea.DebugRequest;
import us.nimbusxr.axx.idea.web.IdeAnnouncement.Kind;

import java.nio.file.Path;

@DisplayName("IdeAnnouncement")
class IdeAnnouncementTest {
    private static final String VIEWER =
            "/home/dev/.cache/axx/web/playwright-1.62.1-node-24.19.0-linux-x64"
                    + "/package/lib/vite/traceViewer";

    @Test
    @DisplayName("parses the folder of the trace viewer's files")
    void parsesViewerFolder() {
        IdeAnnouncement viewer = IdeAnnouncement.parse("[AXX-IDE] trace-viewer dir=" + VIEWER);
        assertNotNull(viewer);
        assertEquals(Kind.TRACE_VIEWER, viewer.kind());
        assertEquals(Path.of(VIEWER), viewer.path());
        assertNull(viewer.file());
        assertEquals(0, viewer.line());
    }

    @Test
    @DisplayName("parses a trace and a video with their scenario's location")
    void parsesFiles() {
        IdeAnnouncement trace =
                IdeAnnouncement.parse(
                        "[AXX-IDE] trace path=/work/parcels/.axx/web/traces/register.zip"
                                + " location=features/shop-portal.feature:42");
        assertNotNull(trace);
        assertEquals(Kind.TRACE, trace.kind());
        assertEquals(Path.of("/work/parcels/.axx/web/traces/register.zip"), trace.path());
        assertEquals("features/shop-portal.feature", trace.file());
        assertEquals(42, trace.line());

        IdeAnnouncement video =
                IdeAnnouncement.parse(
                        "[AXX-IDE] video location=features/shop-portal.feature:7"
                                + " path=/work/parcels/.axx/web/videos/a.webm");
        assertNotNull(video);
        assertEquals(Kind.VIDEO, video.kind());
        assertEquals(Path.of("/work/parcels/.axx/web/videos/a.webm"), video.path());
        assertEquals(7, video.line());
    }

    @Test
    @DisplayName("follows the marker rules: other text first, ANSI colors, first value wins, %20")
    void markerRules() {
        IdeAnnouncement trace =
                IdeAnnouncement.parse(
                        "\u001B[2m12:00:01\u001B[0m [AXX-IDE] trace stray future=1"
                                + " location=features/My%20Parcels.feature:3"
                                + " path=/p/my%20traces/t.zip path=/other.zip\n");
        assertNotNull(trace);
        assertEquals(Path.of("/p/my traces/t.zip"), trace.path());
        assertEquals("features/My Parcels.feature", trace.file());
        assertEquals(3, trace.line());

        IdeAnnouncement viewer =
                IdeAnnouncement.parse("[AXX-IDE] trace-viewer dir=/cache/my%20viewer");
        assertNotNull(viewer);
        assertEquals(Path.of("/cache/my viewer"), viewer.path());
    }

    @Test
    @DisplayName("debugger requests and announcements do not mix")
    void kindsDoNotMix() {
        assertNull(
                IdeAnnouncement.parse(
                        "[AXX-IDE] debug-attach-request name=a type=go host=h port=1"));
        assertNull(DebugRequest.parse("[AXX-IDE] trace-viewer dir=" + VIEWER));
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("returns null for incomplete lines")
    @ValueSource(
            strings = {
                "",
                "trace-viewer dir=/cache/traceViewer",
                "[AXX-IDE] trace-viewer",
                "[AXX-IDE] trace-viewer dir=",
                "[AXX-IDE] trace-viewer dir=cache/traceViewer",
                "[AXX-IDE] trace-viewer url=http://localhost:1234/",
                "[AXX-IDE] trace-viewers dir=/cache/traceViewer",
                "[AXX-IDE] trace path=/p/t.zip",
                "[AXX-IDE] trace location=features/a.feature:3",
                "[AXX-IDE] trace path=relative/t.zip location=features/a.feature:3",
                "[AXX-IDE] trace path=/p/t.zip location=features/a.feature",
                "[AXX-IDE] trace path=/p/t.zip location=features/a.feature:0",
                "[AXX-IDE] video path=/p/v.webm location=:3",
            })
    void rejectsIncomplete(String line) {
        assertNull(IdeAnnouncement.parse(line));
    }
}
