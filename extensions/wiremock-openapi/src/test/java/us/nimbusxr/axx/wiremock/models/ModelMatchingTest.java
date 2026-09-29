// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import static com.github.tomakehurst.wiremock.core.WireMockConfiguration.options;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.github.tomakehurst.wiremock.WireMockServer;
import com.github.tomakehurst.wiremock.common.Json;
import com.github.tomakehurst.wiremock.standalone.CommandLineOptions;
import com.github.tomakehurst.wiremock.stubbing.StubMapping;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;

/** Which stub answers a request: what it is about, the tool it follows, its endpoint. */
class ModelMatchingTest extends ModelMockTest {
    private static final HttpClient HTTP = HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();

    private HttpResponse<String> chat(String messages) throws Exception {
        return HTTP.send(HttpRequest.newBuilder(URI.create(url() + "/v1/chat/completions")).header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString("{\"model\": \"gpt-4.1-mini\", \"messages\": " + messages + "}")).build(),
                HttpResponse.BodyHandlers.ofString());
    }

    @SuppressWarnings("unchecked")
    private static String content(HttpResponse<String> res) {
        assertThat(res.statusCode()).as(res.body()).isEqualTo(200);
        Map<String, Object> choice = (Map<String, Object>) ((java.util.List<Object>) Json.read(res.body(), Map.class).get("choices")).get(0);
        return (String) ((Map<String, Object>) choice.get("message")).get("content");
    }

    private static final String CALL = "{\"role\": \"assistant\", \"content\": null, \"tool_calls\": [{\"id\": \"call_1\", \"type\": \"function\", "
            + "\"function\": {\"name\": \"track_parcel\", \"arguments\": \"{\\\"reference\\\":\\\"PX-AI-8601\\\"}\"}}]}";

    @Test
    void aStubIsAboutEveryTextItNames() throws Exception {
        // When stubs overlap, WireMock's priority decides, as for any stub.
        stub("{\"priority\": 1, \"request\": {\"customMatcher\": {\"name\": \"model-request\", \"parameters\": "
                + "{\"about\": [\"PX-AI-8601\", \"Leipzig\"]}}}, \"response\": {\"transformers\": [\"model-answer\"], \"jsonBody\": {\"text\": \"both\"}}}");
        stub(mapping("{\"about\": \"PX-AI-8601\"}", "{\"text\": \"one\"}"));
        assertThat(content(chat("[{\"role\": \"user\", \"content\": \"Is PX-AI-8601 in Leipzig?\"}]"))).isEqualTo("both");
        assertThat(content(chat("[{\"role\": \"user\", \"content\": \"Is PX-AI-8601 in Berlin?\"}]"))).isEqualTo("one");
    }

    @Test
    void aStubWithoutAfterToolAnswersOnlyTurnsWithoutToolResults() throws Exception {
        answer("PX-AI-8601", "{\"text\": \"first\"}");
        answerAfter("PX-AI-8601", "track_parcel", "{\"text\": \"after the tool\"}");
        String question = "{\"role\": \"user\", \"content\": \"Where is PX-AI-8601?\"}";
        String result = "{\"role\": \"tool\", \"tool_call_id\": \"call_1\", \"content\": \"OUT_FOR_DELIVERY\"}";
        assertThat(content(chat("[" + question + "]"))).isEqualTo("first");
        assertThat(content(chat("[" + question + ", " + CALL + ", " + result + "]"))).isEqualTo("after the tool");
        // The user writes again: a new turn, without a tool result since.
        assertThat(content(chat("[" + question + ", " + CALL + ", " + result + ", {\"role\": \"assistant\", \"content\": \"Today.\"}, "
                + "{\"role\": \"user\", \"content\": \"Thanks, and PX-AI-8601's driver?\"}]"))).isEqualTo("first");
        // A result for a call the conversation does not have names no tool: no stub answers it.
        assertThat(chat("[" + question + ", {\"role\": \"tool\", \"tool_call_id\": \"call_unknown\", \"content\": \"?\"}]").statusCode())
                .isEqualTo(404);
    }

    @Test
    void escapedTextInToolResultsIsFound() throws Exception {
        answerAfter("Hauptstraße 5", "track_parcel", "{\"text\": \"found\"}");
        assertThat(content(chat("[{\"role\": \"user\", \"content\": \"Where is PX-AI-8602?\"}, " + CALL + ", "
                + "{\"role\": \"tool\", \"tool_call_id\": \"call_1\", \"content\": \"{\\\"street\\\": \\\"Hauptstra\\\\u00dfe 5\\\"}\"}]")))
                .isEqualTo("found");
    }

    @Test
    void aChatStubDoesNotAnswerEmbeddings() throws Exception {
        answer("PX-AI-8603", "{\"text\": \"chat\"}");
        HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create(url() + "/v1/embeddings")).header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString("{\"model\": \"text-embedding-3-small\", \"input\": \"PX-AI-8603\"}")).build(),
                HttpResponse.BodyHandlers.ofString());
        assertThat(res.statusCode()).isEqualTo(404);
    }

    @Test
    void theTransformerSaysWhatItCannotAnswer() throws Exception {
        stub("{\"request\": {\"urlPath\": \"/v1/depots\"}, \"response\": {\"transformers\": [\"model-answer\"], \"jsonBody\": {\"text\": \"?\"}}}");
        HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create(url() + "/v1/depots")).build(), HttpResponse.BodyHandlers.ofString());
        assertThat(res.statusCode()).isEqualTo(500);
        assertThat(res.body()).startsWith("the axx model mock cannot answer: the model-answer transformer answers requests to models");
    }

    @Test
    void aWrongStubIsRefusedWhenItIsAdded() {
        assertThatThrownBy(() -> stub(mapping("{\"about\": \"PX-AI-8604\"}", "{\"txt\": \"typo\"}")))
                .hasMessageContaining("the model answer has no key \"txt\"");
        assertThatThrownBy(() -> stub(mapping("{\"abuot\": \"PX-AI-8604\"}", "{\"text\": \"ok\"}")))
                .hasMessageContaining("the model-request matcher has no parameter \"abuot\"");
        assertThatThrownBy(() -> stub(mapping("{\"endpoint\": \"embeddings\", \"afterTool\": \"track_parcel\"}", "{}")))
                .hasMessageContaining("afterTool is for chat requests");
        assertThatThrownBy(() -> stub(mapping("{\"about\": \"PX-AI-8604\"}", "{\"error\": {\"type\": \"quota\"}}")))
                .hasMessageContaining("rate_limit, overloaded, context_length, auth or server");
    }

    @Test
    void aWrongMappingFileStopsWireMock(@TempDir Path root) throws Exception {
        Files.createDirectories(root.resolve("mappings"));
        Files.writeString(root.resolve("mappings/assistant.json"), mapping("{\"about\": \"PX-AI-8605\"}", "{\"toolcalls\": []}"));
        assertThatThrownBy(() -> new WireMockServer(options().bindAddress("127.0.0.1").dynamicPort().usingFilesUnderDirectory(root.toString())
                .extensions(new ModelsExtensionFactory())))
                .hasMessageContaining("PX-AI-8605")
                .hasMessageContaining("the model answer has no key \"toolcalls\"");
    }

    @Test
    void wireMocksCommandLineFindsTheModelMock(@TempDir Path root) throws Exception {
        // As the image runs WireMock: its command line, which scans for extensions.
        WireMockServer standalone = new WireMockServer(new CommandLineOptions("--port", "0", "--bind-address", "127.0.0.1", "--root-dir", root.toString()));
        standalone.start();
        try {
            standalone.addStubMapping(StubMapping.buildFrom(mapping("{\"about\": \"PX-AI-8606\"}", "{\"text\": \"On its way.\"}")));
            HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + standalone.port() + "/v1/chat/completions"))
                    .header("Content-Type", "application/json").POST(HttpRequest.BodyPublishers.ofString(
                            "{\"model\": \"gpt-4.1-mini\", \"messages\": [{\"role\": \"user\", \"content\": \"PX-AI-8606?\"}]}")).build(),
                    HttpResponse.BodyHandlers.ofString());
            assertThat(content(res)).isEqualTo("On its way.");
        } finally {
            standalone.stop();
        }
    }
}
