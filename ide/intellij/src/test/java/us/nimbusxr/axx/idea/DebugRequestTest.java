// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import us.nimbusxr.axx.idea.DebugRequest.Kind;

@DisplayName("DebugRequest")
class DebugRequestTest {

    @Test
    @DisplayName("parses a listener request")
    void parsesListenerRequest() {
        DebugRequest request =
                DebugRequest.parse(
                        "[AXX-IDE] debug-listener-request name=billing type=java"
                                + " host=localhost port=5006");

        assertNotNull(request);
        assertEquals(Kind.LISTEN, request.kind());
        assertEquals("billing", request.name());
        assertEquals("java", request.type());
        assertEquals("localhost", request.host());
        assertEquals(5006, request.port());
        assertEquals("Debugger: billing", request.configurationName());
    }

    @Test
    @DisplayName("parses an attach request")
    void parsesAttachRequest() {
        DebugRequest request =
                DebugRequest.parse(
                        "[AXX-IDE] debug-attach-request name=orders type=java host=127.0.0.1"
                                + " port=5005");

        assertNotNull(request);
        assertEquals(Kind.ATTACH, request.kind());
        assertEquals("orders", request.name());
        assertEquals("127.0.0.1", request.host());
        assertEquals(5005, request.port());
        assertEquals("Debugger: orders", request.configurationName());
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("tolerates extra whitespace")
    @ValueSource(
            strings = {
                "[AXX-IDE]   debug-listener-request   name=api  type=nodejs\thost=localhost   "
                        + " port=9229",
                "  [AXX-IDE] debug-listener-request name=api type=nodejs host=localhost port=9229  "
                        + " ",
                "[AXX-IDE] debug-listener-request name=api type=nodejs host=localhost"
                        + " port=9229\r\n",
                "[AXX-IDE]\tdebug-listener-request  name=api   type=nodejs host=localhost"
                        + " port=9229",
            })
    void toleratesWhitespace(String line) {
        DebugRequest request = DebugRequest.parse(line);

        assertNotNull(request);
        assertEquals("api", request.name());
        assertEquals("nodejs", request.type());
        assertEquals("localhost", request.host());
        assertEquals(9229, request.port());
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("ignores unknown keys and accepts any field order")
    @ValueSource(
            strings = {
                "[AXX-IDE] debug-listener-request name=api type=java host=localhost port=5006"
                        + " pid=42",
                "[AXX-IDE] debug-listener-request run=7f3a name=api type=java host=localhost"
                        + " port=5006",
                "[AXX-IDE] debug-listener-request port=5006 host=localhost type=java name=api",
                "[AXX-IDE] debug-listener-request name=api stray type=java host=localhost"
                        + " port=5006",
                "[AXX-IDE] debug-listener-request name=api type=java future=x=y host=localhost"
                        + " port=5006",
            })
    void ignoresUnknownKeys(String line) {
        DebugRequest request = DebugRequest.parse(line);

        assertNotNull(request);
        assertEquals("api", request.name());
        assertEquals("java", request.type());
        assertEquals("localhost", request.host());
        assertEquals(5006, request.port());
    }

    @Test
    @DisplayName("finds the request inside surrounding console output")
    void parsesEmbeddedInOutput() {
        DebugRequest request =
                DebugRequest.parse(
                        "12:01:33 INFO [AXX-IDE] debug-listener-request name=api type=nodejs"
                                + " host=127.0.0.1 port=9229\n"
                                + "next line name=other port=1\n");

        assertNotNull(request);
        assertEquals("api", request.name());
        assertEquals(9229, request.port());
    }

    @Test
    @DisplayName("ignores ANSI color codes")
    void ignoresAnsiCodes() {
        DebugRequest request =
                DebugRequest.parse(
                        "\u001B[2m12:01:33\u001B[0m [AXX-IDE] debug-attach-request name=api"
                                + " type=java host=localhost port=5005\u001B[0m");

        assertNotNull(request);
        assertEquals("api", request.name());
        assertEquals(5005, request.port());
    }

    @Test
    @DisplayName("the first occurrence of a key wins")
    void firstKeyWins() {
        DebugRequest request =
                DebugRequest.parse(
                        "[AXX-IDE] debug-listener-request name=first name=second type=java host=h"
                                + " port=1");

        assertNotNull(request);
        assertEquals("first", request.name());
    }

    @ParameterizedTest(name = "{0}")
    @DisplayName("returns null for text without a well-formed request")
    @ValueSource(
            strings = {
                "",
                "Listening for transport dt_socket at address: 5006",
                "[AXX-IDE]",
                "[AXX-IDE] debug-listener-request",
                "[AXX-IDE] debug-listener-request name=app",
                "[AXX-IDE] debug-listener-request name=app type=java host=h",
                "[AXX-IDE] debug-listener-request name=app type=java host=h port=abc",
                "[AXX-IDE] debug-listener-request name=app type=java host=h port=0",
                "[AXX-IDE] debug-listener-request name=app type=java host=h port=70000",
                "[AXX-IDE] debug-listener-request name= type=java host=h port=1",
                "[AXX-IDE] something-else name=app type=java host=h port=1",
                "[AXX-IDE]debug-listener-request-v2 name=app type=java host=h port=1",
                "[AXX-IDE] Debug-Listener-Request name=app type=java host=h port=1",
                "[AXX] debug-listener-request name=app type=java host=h port=1",
                "debug-listener-request name=app type=java host=h port=1",
                "[AXX-IDE] debug-listener-request name=app type=java\nhost=h port=1",
            })
    void rejectsMalformed(String line) {
        assertNull(DebugRequest.parse(line));
    }

    @Test
    @DisplayName("returns null for null")
    void rejectsNull() {
        assertNull(DebugRequest.parse(null));
    }
}
