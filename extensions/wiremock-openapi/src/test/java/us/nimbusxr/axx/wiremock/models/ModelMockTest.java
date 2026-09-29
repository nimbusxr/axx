// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;

import java.util.List;

/** A WireMock server with the model mock, and stubs written as mapping files write them. */
abstract class ModelMockTest {
    WireMockServer wm;

    @BeforeEach
    void startWireMock() {
        wm = new WireMockServer(options().dynamicPort().extensions(new ModelsExtensionFactory()));
        wm.start();
    }

    @AfterEach
    void stopWireMock() {
        wm.stop();
    }

    String url() {
        return "http://localhost:" + wm.port();
    }

    /** Adds a stub, from the JSON of a mapping file. */
    void stub(String mapping) {
        wm.addStubMapping(StubMapping.buildFrom(mapping));
    }

    /** Adds a stub of the model's answer to the requests about a text. */
    void answer(String about, String answer) {
        stub(mapping("{\"about\": " + Json.write(about) + "}", answer));
    }

    /** Adds a stub of the model's answer to the requests about a text, after a tool's result. */
    void answerAfter(String about, String tool, String answer) {
        stub(mapping("{\"about\": " + Json.write(about) + ", \"afterTool\": " + Json.write(tool) + "}", answer));
    }

    static String mapping(String parameters, String answer) {
        return "{\"request\": {\"customMatcher\": {\"name\": \"model-request\", \"parameters\": " + parameters + "}}, "
                + "\"response\": {\"transformers\": [\"model-answer\"], \"jsonBody\": " + answer + "}}";
    }

    /** The requests served, the latest last. */
    List<ServeEvent> served() {
        return wm.getAllServeEvents().reversed();
    }

    ServeEvent last() {
        return wm.getAllServeEvents().get(0);
    }
}
