// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.stubbing.ServeEvent;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

/** A WireMock server with the A2A mock of a partner carrier's agent, and stubs written as mapping files write them. */
abstract class A2aMockTest {
    static final String CARD = Path.of("src/test/resources/a2a/agent-card.json").toAbsolutePath().toString();
    static final HttpClient HTTP = HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();
    static final String JSONRPC = "/a2a/jsonrpc";
    static final String REST = "/a2a/rest";

    WireMockServer wm;

    @BeforeEach
    void startWireMock() {
        // On the loopback address, which the tests call: a server on every address can be given a
        // port another process listens on at 127.0.0.1, which then answers the tests.
        wm = new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort().extensions(new A2aExtensionFactory(new A2aSettings(CARD))));
        wm.start();
    }

    @AfterEach
    void stopWireMock() {
        wm.stop();
    }

    String url() {
        return "http://127.0.0.1:" + wm.port();
    }

    /** Adds a stub, from the JSON of a mapping file. */
    void stub(String mapping) {
        wm.addStubMapping(StubMapping.buildFrom(mapping));
    }

    /** Adds a stub of the agent's answer to the messages whose lookup body matches the body patterns. */
    void answer(String bodyPatterns, String answer) {
        stub("{\"request\": {\"method\": \"POST\", \"urlPath\": \"/a2a/messages\", \"bodyPatterns\": " + bodyPatterns + "},"
                + " \"response\": {\"jsonBody\": " + answer + "}}");
    }

    /** Adds a stub of the agent's answer to the messages whose text contains a text. */
    void answerAbout(String text, String answer) {
        answer("[{\"matchesJsonPath\": {\"expression\": \"$.text\", \"contains\": \"" + text + "\"}}]", answer);
    }

    HttpResponse<String> send(String method, String path, String body, String... headers) throws IOException, InterruptedException {
        HttpRequest.Builder b = HttpRequest.newBuilder(URI.create(url() + path))
                .method(method, body == null ? HttpRequest.BodyPublishers.noBody() : HttpRequest.BodyPublishers.ofString(body));
        if (body != null) {
            b.header("Content-Type", path.startsWith(REST) ? "application/a2a+json" : "application/json");
        }
        if (headers.length % 2 != 0) {
            throw new IllegalArgumentException("headers are names and values, in pairs: " + headers.length + " is odd");
        }
        for (int i = 0; i < headers.length; i += 2) {
            b.header(headers[i], headers[i + 1]);
        }
        return HTTP.send(b.build(), HttpResponse.BodyHandlers.ofString());
    }

    /** A JSON-RPC call, as its JSON. */
    static String rpc(Object id, String method, String params) {
        return "{\"jsonrpc\": \"2.0\", \"id\": " + Json.write(id) + ", \"method\": \"" + method + "\", \"params\": " + params + "}";
    }

    /** A SendMessageRequest of a user's text, as its JSON. */
    static String message(String text) {
        return "{\"message\": {\"messageId\": \"" + java.util.UUID.randomUUID() + "\", \"role\": \"ROLE_USER\", \"parts\": [{\"text\": " + Json.write(text) + "}]}}";
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> json(String body) {
        return Json.read(body, Map.class);
    }

    /** The data of the events of a server-sent event stream. */
    static List<Map<String, Object>> events(String body) {
        return body.lines().filter(l -> l.startsWith("data: ")).map(l -> json(l.substring(6))).toList();
    }

    /** The requests served, the first first. */
    List<ServeEvent> served() {
        return wm.getAllServeEvents().reversed();
    }
}
