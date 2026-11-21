// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import com.github.tomakehurst.wiremock.extension.Extension;
import com.github.tomakehurst.wiremock.extension.ExtensionFactory;
import com.github.tomakehurst.wiremock.extension.WireMockServices;

import java.util.List;

/**
 * Makes the model mock: the {@code model-request} matcher and the {@code model-answer}
 * transformer, which share what the mock remembers of its answers. WireMock finds it through
 * {@link java.util.ServiceLoader} scanning.
 */
public class ModelsExtensionFactory implements ExtensionFactory {
    @Override
    public List<Extension> create(WireMockServices services) {
        Memory memory = new Memory();
        return List.of(new ModelRequest(memory), new ModelAnswer(memory), new ModelStubs());
    }
}
