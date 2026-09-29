// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.extension.StubLifecycleListener;
import com.github.tomakehurst.wiremock.http.ResponseDefinition;
import com.github.tomakehurst.wiremock.matching.CustomMatcherDefinition;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

/**
 * Refuses a stub whose model matcher or model answer is wrong when it is added, so a mistake in a
 * mapping file stops WireMock at startup instead of answering nothing.
 */
public class ModelStubs implements StubLifecycleListener {
    @Override
    public String getName() {
        return "model-stubs";
    }

    @Override
    public void beforeStubCreated(StubMapping stub) {
        check(stub);
    }

    @Override
    public void beforeStubEdited(StubMapping oldStub, StubMapping newStub) {
        check(newStub);
    }

    static void check(StubMapping stub) {
        try {
            CustomMatcherDefinition matcher = stub.getRequest().getCustomMatcher();
            if (matcher != null && ModelRequest.NAME.equals(matcher.getName())) {
                Criteria.of(matcher.getParameters());
            }
            ResponseDefinition response = stub.getResponse();
            if (response.getTransformers() != null && response.getTransformers().contains(ModelAnswer.NAME)
                    && response.getBodyFileName() == null) {
                Answer.read(response.getJsonBody() != null ? response.getJsonBody().toString() : response.getBody());
            }
        } catch (IllegalArgumentException e) {
            // A stub from a mapping file has no name: its request says which it is.
            String name = stub.getName() != null ? stub.getName() : Json.write(stub.getRequest()).replaceAll("\\s+", " ");
            throw new IllegalArgumentException("the stub " + name + " is not a model stub: " + e.getMessage(), e);
        }
    }
}
