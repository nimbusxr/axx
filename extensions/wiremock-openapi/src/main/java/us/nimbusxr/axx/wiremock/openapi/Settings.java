// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.openapi;

import java.util.Locale;
import java.util.Map;
import java.util.Optional;

/**
 * The extension's settings, from environment variables:
 *
 * <ul>
 *   <li>{@code OPENAPI_VALIDATION_MODE}: {@code fail} (default) turns a response with an ERROR
 *       finding into an HTTP 500; {@code report} serves the stub's response unchanged. Findings are
 *       recorded on the request journal in both modes.
 *   <li>{@code OPENAPI_VALIDATION_LEVELS}: default levels, {@code key=LEVEL} pairs separated by
 *       commas.
 *   <li>{@code OPENAPI_SPEC_SOURCE}: a default spec for stubs without {@code openApiSpecSource}.
 *   <li>{@code OPENAPI_SPEC_AUTH_HEADER}: a {@code Name: value} header sent when fetching specs
 *       from URLs.
 *   <li>{@code OPENAPI_VALIDATION_TRUST_ALL_CERTS=true}: skip TLS checks when fetching specs.
 * </ul>
 */
record Settings(Mode mode, Levels levels, Optional<String> defaultSpec, Optional<String[]> authHeader) {
    /** What happens to a response with an ERROR finding. */
    enum Mode {
        FAIL,
        REPORT;

        static Mode parse(String name) {
            try {
                return Mode.valueOf(name.trim().toUpperCase(Locale.ROOT));
            } catch (IllegalArgumentException e) {
                throw new IllegalArgumentException(
                        "invalid validation mode \"" + name + "\"; use fail or report");
            }
        }

        String label() {
            return name().toLowerCase(Locale.ROOT);
        }
    }

    /**
     * Levels that apply before any configuration: the validator's own default (a query parameter
     * the spec does not declare is not an error).
     */
    static final Levels BUILT_IN = Levels.parse("validation.request.parameter.query.unexpected=IGNORE");

    static Settings fromEnv(Map<String, String> env) {
        Mode mode = Optional.ofNullable(blank(env.get("OPENAPI_VALIDATION_MODE")))
                .map(Mode::parse)
                .orElse(Mode.FAIL);
        Levels levels = BUILT_IN.with(Levels.parse(env.get("OPENAPI_VALIDATION_LEVELS")));
        Optional<String[]> auth = Optional.ofNullable(blank(env.get("OPENAPI_SPEC_AUTH_HEADER")))
                .map(Settings::header);
        return new Settings(mode, levels, Optional.ofNullable(blank(env.get("OPENAPI_SPEC_SOURCE"))), auth);
    }

    static boolean trustAllCerts(Map<String, String> env) {
        return "true".equalsIgnoreCase(env.get("OPENAPI_VALIDATION_TRUST_ALL_CERTS"));
    }

    private static String[] header(String value) {
        int colon = value.indexOf(':');
        if (colon <= 0) {
            throw new IllegalArgumentException("OPENAPI_SPEC_AUTH_HEADER must be \"Name: value\"");
        }
        return new String[] {value.substring(0, colon).trim(), value.substring(colon + 1).trim()};
    }

    private static String blank(String s) {
        return s == null || s.isBlank() ? null : s.trim();
    }
}
