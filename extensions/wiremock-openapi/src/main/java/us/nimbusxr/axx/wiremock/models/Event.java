// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

/**
 * An event of a stream: its name (an SSE event, or an AWS event stream's event or exception type)
 * and its data, JSON text.
 */
record Event(String name, String data, boolean exception) {
    static Event of(String name, Object data) {
        return new Event(name, Values.write(data), false);
    }

    /** An event of data that is not a JSON value of ours, such as OpenAI's {@code [DONE]}. */
    static Event raw(String name, String data) {
        return new Event(name, data, false);
    }

    /** An AWS event stream's exception. */
    static Event exception(String type, Object data) {
        return new Event(type, Values.write(data), true);
    }

    /** The event with its data broken off halfway. */
    Event broken() {
        return new Event(name, data.substring(0, data.length() / 2), exception);
    }
}
