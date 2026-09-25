// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Optional;

/**
 * Validation keys mapped to levels. A key also covers every key below it: {@code
 * validation.request.body} sets the level of {@code validation.request.body.schema.required} unless
 * a more specific key is configured. The keys are the ones the axx REST steps use for the service
 * under test, but the settings are separate: these apply to a mocked dependency's contract.
 */
final class Levels {
    static final Levels NONE = new Levels(Map.of());

    private final Map<String, Level> levels;

    private Levels(Map<String, Level> levels) {
        this.levels = Collections.unmodifiableMap(new LinkedHashMap<>(levels));
    }

    /** Parses {@code key=LEVEL} pairs separated by commas or new lines. */
    static Levels parse(String spec) {
        Map<String, Level> out = new LinkedHashMap<>();
        if (spec == null) {
            return NONE;
        }
        for (String pair : spec.split("[,\\n]")) {
            if (pair.isBlank()) {
                continue;
            }
            int eq = pair.indexOf('=');
            if (eq <= 0) {
                throw new IllegalArgumentException(
                        "invalid level setting \"" + pair.trim() + "\"; use key=LEVEL");
            }
            out.put(pair.substring(0, eq).trim(), Level.parse(pair.substring(eq + 1)));
        }
        return new Levels(out);
    }

    /** Converts stub metadata ({@code {"key": "LEVEL"}}). */
    static Levels of(Map<?, ?> map) {
        Map<String, Level> out = new LinkedHashMap<>();
        map.forEach((k, v) -> out.put(String.valueOf(k).trim(), Level.parse(String.valueOf(v))));
        return new Levels(out);
    }

    /** Returns these levels with {@code over} taking precedence for equal keys. */
    Levels with(Levels over) {
        Map<String, Level> out = new LinkedHashMap<>(levels);
        out.putAll(over.levels);
        return new Levels(out);
    }

    /** The level of the most specific configured key covering {@code key}, if any. */
    Optional<Level> find(String key) {
        for (String k = key; !k.isEmpty(); ) {
            Level l = levels.get(k);
            if (l != null) {
                return Optional.of(l);
            }
            int dot = k.lastIndexOf('.');
            if (dot < 0) {
                break;
            }
            k = k.substring(0, dot);
        }
        return Optional.empty();
    }

    Map<String, String> asMap() {
        Map<String, String> out = new LinkedHashMap<>();
        levels.forEach((k, v) -> out.put(k, v.name()));
        return out;
    }
}
