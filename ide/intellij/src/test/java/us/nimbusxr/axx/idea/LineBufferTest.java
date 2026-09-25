// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;

@DisplayName("LineBuffer")
class LineBufferTest {

    @Test
    @DisplayName("returns complete lines and keeps the rest")
    void splitsLines() {
        LineBuffer buffer = new LineBuffer();

        assertEquals(List.of("one", "two"), buffer.append("one\ntwo\nthr"));
        assertEquals(List.of("three"), buffer.append("ee\n"));
        assertEquals(List.of(), buffer.append("four"));
    }

    @Test
    @DisplayName("strips CRLF line endings")
    void stripsCarriageReturn() {
        LineBuffer buffer = new LineBuffer();

        assertEquals(List.of("one", "", "two"), buffer.append("one\r\n\r\ntwo\r\n"));
    }

    @Test
    @DisplayName("reassembles a request split across chunks")
    void reassemblesSplitRequest() {
        LineBuffer buffer = new LineBuffer();
        List<String> lines = new ArrayList<>();

        lines.addAll(buffer.append("[AXX-IDE] debug-listener-req"));
        lines.addAll(buffer.append("uest name=api type=java host=localhost po"));
        lines.addAll(buffer.append("rt=5006\n"));

        assertEquals(1, lines.size());
        DebugRequest request = DebugRequest.parse(lines.get(0));
        assertNotNull(request);
        assertEquals("api", request.name());
        assertEquals(5006, request.port());
    }

    @Test
    @DisplayName("drops an unterminated line that grows past the limit")
    void boundsPendingText() {
        LineBuffer buffer = new LineBuffer();

        buffer.append("x".repeat(LineBuffer.MAX_LINE_LENGTH + 1));

        assertEquals(List.of("tail"), buffer.append("tail\n"));
    }
}
