// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import java.nio.file.Path;

@DisplayName("ScenarioPause")
class ScenarioPauseTest {
    private static final String URL = "http://127.0.0.1:53211/3f9c0a/";

    @Test
    @DisplayName("parses a pause with its location and recording file")
    void parsesPause() {
        assertEquals(
                new ScenarioPause(
                        true,
                        URL,
                        "features/shop-portal.feature:24",
                        Path.of("/work/parcels/.axx/web/recording.txt")),
                ScenarioPause.parse(
                        "[AXX-IDE] paused url="
                                + URL
                                + " location=features/shop-portal.feature:24"
                                + " recording=/work/parcels/.axx/web/recording.txt"));
    }

    @Test
    @DisplayName("parses a resume")
    void parsesResume() {
        assertEquals(
                new ScenarioPause(false, URL, null, null),
                ScenarioPause.parse("[AXX-IDE] resumed url=" + URL));
    }

    @Test
    @DisplayName("follows the marker rules: other text first, ANSI colors, any order, %20")
    void markerRules() {
        assertEquals(
                new ScenarioPause(
                        true,
                        URL,
                        "features/My Parcels.feature:3",
                        Path.of("/work/my parcels/.axx/web/recording.txt")),
                ScenarioPause.parse(
                        "\u001B[2m12:00:01\u001B[0m [AXX-IDE] paused future=1"
                                + " recording=/work/my%20parcels/.axx/web/recording.txt"
                                + " location=features/My%20Parcels.feature:3 url="
                                + URL
                                + " url=http://127.0.0.1:1/other/\n"));
    }

    @Test
    @DisplayName("keeps a pause without a location or recording file, or with a relative one")
    void optionalFields() {
        assertEquals(
                new ScenarioPause(true, URL, null, null),
                ScenarioPause.parse("[AXX-IDE] paused url=" + URL));
        assertEquals(
                new ScenarioPause(true, URL, null, null),
                ScenarioPause.parse(
                        "[AXX-IDE] paused url=" + URL + " recording=.axx/web/recording.txt"));
    }

    @Test
    @DisplayName("lines of other kinds do not mix")
    void kindsDoNotMix() {
        assertNull(
                ScenarioPause.parse("[AXX-IDE] trace path=/p/t.zip location=features/a.feature:3"));
        assertNull(IdeAnnouncement.parse("[AXX-IDE] paused url=" + URL));
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("returns null without a loopback http url ending with /")
    @ValueSource(
            strings = {
                "",
                "paused url=http://127.0.0.1:53211/3f9c0a/",
                "[AXX-IDE] paused",
                "[AXX-IDE] paused location=features/a.feature:3",
                "[AXX-IDE] resumed url=",
                "[AXX-IDE] paused url=http://127.0.0.1:53211/3f9c0a",
                "[AXX-IDE] paused url=https://127.0.0.1:53211/3f9c0a/",
                "[AXX-IDE] paused url=http://example.com:53211/3f9c0a/",
                "[AXX-IDE] paused url=http://10.0.0.7:53211/3f9c0a/",
                "[AXX-IDE] paused url=http://127.0.0.1:53211/3f9c0a/?x=1",
                "[AXX-IDE] paused url=file:///tmp/",
                "[AXX-IDE] pause url=http://127.0.0.1:53211/3f9c0a/",
            })
    void rejectsIncomplete(String line) {
        assertNull(ScenarioPause.parse(line));
    }

    @Test
    @DisplayName("accepts only http URLs on the loopback interface")
    void loopbackUrls() {
        assertTrue(ScenarioPause.isLoopbackUrl("http://127.0.0.1:1/"));
        assertTrue(ScenarioPause.isLoopbackUrl("http://localhost:1/a/"));
        assertTrue(ScenarioPause.isLoopbackUrl("http://[::1]:1/a/"));
        assertFalse(ScenarioPause.isLoopbackUrl("http://127.0.0.1.example.com:1/a/"));
        assertFalse(ScenarioPause.isLoopbackUrl("http://user@example.com:1/a/"));
        assertFalse(ScenarioPause.isLoopbackUrl("http://user@127.0.0.1:1/a/"));
        assertFalse(ScenarioPause.isLoopbackUrl("http://127.0.0.1:1"));
        assertFalse(ScenarioPause.isLoopbackUrl("not a url/"));
    }
}
