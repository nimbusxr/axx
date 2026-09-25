// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.idea;

import org.jetbrains.annotations.NotNull;

import java.util.ArrayList;
import java.util.List;

/**
 * Reassembles complete lines from console output that arrives in arbitrary chunks, so a marker line
 * split across two chunks is still recognized. Not thread-safe; use one per output stream.
 */
final class LineBuffer {
    /** An unterminated line longer than this is dropped rather than buffered without bound. */
    static final int MAX_LINE_LENGTH = 64 * 1024;

    private final StringBuilder pending = new StringBuilder();

    /**
     * Adds a chunk of output.
     *
     * @param chunk the text as it arrived
     * @return the lines this chunk completed, without their line terminators
     */
    @NotNull
    List<String> append(@NotNull String chunk) {
        List<String> lines = new ArrayList<>();
        int start = 0;
        for (int i = 0; i < chunk.length(); i++) {
            if (chunk.charAt(i) == '\n') {
                pending.append(chunk, start, i);
                lines.add(takePending());
                start = i + 1;
            }
        }
        pending.append(chunk, start, chunk.length());
        if (pending.length() > MAX_LINE_LENGTH) {
            pending.setLength(0);
        }
        return lines;
    }

    private String takePending() {
        int end = pending.length();
        if (end > 0 && pending.charAt(end - 1) == '\r') {
            end--;
        }
        String line = pending.substring(0, end);
        pending.setLength(0);
        return line;
    }
}
