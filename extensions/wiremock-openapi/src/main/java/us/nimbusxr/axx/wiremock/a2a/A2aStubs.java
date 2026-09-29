// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import com.github.tomakehurst.wiremock.extension.StubLifecycleListener;
import com.github.tomakehurst.wiremock.matching.RequestPattern;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

import us.nimbusxr.axx.wiremock.stubs.InProcessStubs;

/**
 * Refuses a wrong stub of the agent's answers ({@code POST /a2a/messages}) when it is added, so a
 * mistake in a mapping file stops WireMock at startup instead of answering nothing: an answer with
 * an unknown key, or one of its keys of the wrong shape.
 */
public class A2aStubs implements StubLifecycleListener {
    @Override
    public String getName() {
        return "a2a-stubs";
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
        RequestPattern request = stub.getRequest();
        String path = InProcessStubs.exactPath(request);
        String pattern = request.getUrlPathPattern() != null ? request.getUrlPathPattern() : request.getUrlPattern();
        boolean answer = path != null ? path.equals(Agent.MESSAGES_PATH) : pattern != null && pattern.replace("\\", "").startsWith(Agent.MESSAGES_PATH);
        if (!answer || stub.getResponse() == null) {
            return;
        }
        String body = InProcessStubs.inlineBody(stub.getResponse());
        if (body == null && stub.getResponse().getBodyFileName() != null) {
            return;
        }
        try {
            AgentAnswer.read(body);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("the stub " + InProcessStubs.describe(stub) + " is not an A2A agent stub: " + e.getMessage(), e);
        }
    }
}
