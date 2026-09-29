// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Function;
import java.util.zip.CRC32;

/** The encodings streams come in, and the ways a stub breaks them. */
final class Streams {
    private Streams() {}

    /**
     * A stream's events as the stub's answer shapes them: ended after its first events ({@code
     * cutOffAfter}), or broken by an error after them ({@code error.afterEvents}), and with an
     * event broken off halfway when it is malformed.
     */
    static List<Event> shape(List<Event> events, Answer a, Function<Answer.Failure, Event> error) {
        List<Event> out = new ArrayList<>(events);
        if (a.error() != null && a.error().afterEvents() != null) {
            out = new ArrayList<>(out.subList(0, Math.min(a.error().afterEvents(), out.size())));
            out.add(error.apply(a.error()));
        } else if (a.cutOffAfter() != null) {
            out = new ArrayList<>(out.subList(0, Math.min(a.cutOffAfter(), out.size())));
        }
        if (a.malformed() && !out.isEmpty()) {
            int i = Math.min(1, out.size() - 1);
            out.set(i, out.get(i).broken());
        }
        return out;
    }

    /** Server-sent events: with their names, or data alone. */
    static byte[] sse(List<Event> events, boolean named) {
        StringBuilder b = new StringBuilder();
        for (Event e : events) {
            if (named && e.name() != null) {
                b.append("event: ").append(e.name()).append('\n');
            }
            b.append("data: ").append(e.data()).append("\n\n");
        }
        return b.toString().getBytes(StandardCharsets.UTF_8);
    }

    /** Newline-delimited JSON: a line per event. */
    static byte[] ndjson(List<Event> events) {
        StringBuilder b = new StringBuilder();
        for (Event e : events) {
            b.append(e.data()).append('\n');
        }
        return b.toString().getBytes(StandardCharsets.UTF_8);
    }

    /** A JSON array of the events, as Gemini streams without {@code alt=sse}. */
    static byte[] jsonArray(List<Event> events) {
        StringBuilder b = new StringBuilder("[");
        for (int i = 0; i < events.size(); i++) {
            b.append(i == 0 ? "" : ",\r\n").append(events.get(i).data());
        }
        return b.append(']').toString().getBytes(StandardCharsets.UTF_8);
    }

    /**
     * AWS's event stream ({@code application/vnd.amazon.eventstream}): a binary frame per event,
     * with its headers, its payload and their CRC32s.
     */
    static byte[] eventStream(List<Event> events) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        for (Event e : events) {
            Map<String, String> headers = new LinkedHashMap<>();
            if (e.exception()) {
                headers.put(":exception-type", e.name());
                headers.put(":content-type", "application/json");
                headers.put(":message-type", "exception");
            } else {
                headers.put(":event-type", e.name());
                headers.put(":content-type", "application/json");
                headers.put(":message-type", "event");
            }
            out.writeBytes(frame(headers, e.data().getBytes(StandardCharsets.UTF_8)));
        }
        return out.toByteArray();
    }

    static byte[] frame(Map<String, String> headers, byte[] payload) {
        ByteArrayOutputStream h = new ByteArrayOutputStream();
        for (Map.Entry<String, String> e : headers.entrySet()) {
            byte[] name = e.getKey().getBytes(StandardCharsets.UTF_8);
            byte[] value = e.getValue().getBytes(StandardCharsets.UTF_8);
            h.write(name.length);
            h.writeBytes(name);
            h.write(7); // a string
            h.write(value.length >> 8);
            h.write(value.length);
            h.writeBytes(value);
        }
        byte[] hb = h.toByteArray();
        int total = 12 + hb.length + payload.length + 4;
        ByteBuffer b = ByteBuffer.allocate(total);
        b.putInt(total).putInt(hb.length);
        b.putInt(crc(b.array(), 8));
        b.put(hb).put(payload);
        b.putInt(crc(b.array(), total - 4));
        return b.array();
    }

    private static int crc(byte[] b, int length) {
        CRC32 c = new CRC32();
        c.update(b, 0, length);
        return (int) c.getValue();
    }
}
