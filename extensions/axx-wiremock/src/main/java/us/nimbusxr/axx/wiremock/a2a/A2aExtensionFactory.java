// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import com.github.tomakehurst.wiremock.extension.Extension;
import com.github.tomakehurst.wiremock.extension.ExtensionFactory;
import com.github.tomakehurst.wiremock.extension.WireMockServices;

import java.util.List;

/**
 * Makes the A2A agent mock when {@code A2A_AGENT_CARD_SOURCE} names an agent card: the mock itself
 * and the check of its stubs. The card is read when WireMock starts, and a card that is wrong
 * stops it. WireMock finds the factory through {@link java.util.ServiceLoader} scanning.
 */
public class A2aExtensionFactory implements ExtensionFactory {
    private final A2aSettings settings;

    /** Reads the settings from the environment; WireMock instantiates it reflectively. */
    public A2aExtensionFactory() {
        this(A2aSettings.fromEnv(System.getenv()));
    }

    A2aExtensionFactory(A2aSettings settings) {
        this.settings = settings;
    }

    @Override
    public List<Extension> create(WireMockServices services) {
        if (!settings.enabled()) {
            return List.of();
        }
        AgentCard card = AgentCard.load(settings.cardSource());
        return List.of(new A2aMock(card, new Tasks(), services.getFiles()), new A2aStubs());
    }
}
