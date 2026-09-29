// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import java.util.Map;

/**
 * The A2A mock's settings, from the environment.
 *
 * @param cardSource {@code A2A_AGENT_CARD_SOURCE}: the agent's card (A2A 1.0 JSON), which says
 *     where the agent answers; without it the mock is off
 */
record A2aSettings(String cardSource) {
    static A2aSettings fromEnv(Map<String, String> env) {
        String source = env.get("A2A_AGENT_CARD_SOURCE");
        return new A2aSettings(source == null || source.isBlank() ? null : source);
    }

    boolean enabled() {
        return cardSource != null;
    }
}
