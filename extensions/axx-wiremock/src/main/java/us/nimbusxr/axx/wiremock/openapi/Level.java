// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import java.util.Locale;

/** How a validation finding is reported, from the least to the most severe. */
enum Level {
    IGNORE,
    INFO,
    WARN,
    ERROR;

    /** Parses a level name, case-insensitively; FAIL is an alias of ERROR. */
    static Level parse(String name) {
        String n = name == null ? "" : name.trim().toUpperCase(Locale.ROOT);
        if (n.equals("FAIL")) {
            return ERROR;
        }
        try {
            return Level.valueOf(n);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException(
                    "invalid level \"" + name + "\"; supported levels: ERROR (or FAIL), WARN, INFO, IGNORE");
        }
    }
}
